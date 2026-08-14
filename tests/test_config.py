"""Claude Code settings integration tests for helm-x."""
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

HELMX = Path(os.environ.get(
    "HELMX_TEST_BIN", Path(__file__).resolve().parents[1] / "build-check" / "helmx.exe"
))


def run(cmd, cwd, env=None, timeout=60):
    proc = subprocess.run(
        cmd, capture_output=True, text=True, timeout=timeout, cwd=cwd,
        env={**os.environ, **(env or {})},
    )
    return proc.returncode, proc.stdout + proc.stderr


class TestConfig(unittest.TestCase):
    def setUp(self):
        self.tmp_obj = tempfile.TemporaryDirectory(prefix="helmx-test-")
        self.tmp = Path(self.tmp_obj.name)
        self.claude_home = self.tmp / ".claude"
        self.claude_home.mkdir()
        self.codex_home = self.tmp / ".codex"
        self.codex_home.mkdir()
        self.codex_marker = self.codex_home / "config.toml"
        self.codex_marker.write_text('model = "untouched"\n', encoding="utf-8")
        self.settings = self.claude_home / "settings.json"
        self.original = {
            "env": {
                "ANTHROPIC_BASE_URL": "https://relay.example/gateway",
                "ANTHROPIC_AUTH_TOKEN": "fixture-token",
            },
            "model": "claude-sonnet-4-5",
            "permissions": {"allow": ["Read", "Bash(git status)"]},
        }
        self.settings.write_text(json.dumps(self.original, indent=2) + "\n", encoding="utf-8")
        self.env = {
            "CLAUDE_CONFIG_DIR": str(self.claude_home),
            "CODEX_HOME": str(self.codex_home),
        }

    def tearDown(self):
        self.tmp_obj.cleanup()

    def _run(self, command, args=None):
        return run([str(HELMX), command] + (args or []), self.tmp, self.env)

    def _json(self):
        return json.loads(self.settings.read_text(encoding="utf-8"))

    def test_apply_points_claude_at_local_proxy_and_creates_backup(self):
        rc, out = self._run("apply")
        self.assertEqual(rc, 0, out)
        self.assertEqual(self._json()["env"]["ANTHROPIC_BASE_URL"], "http://127.0.0.1:1800")
        backup = self.settings.with_name("settings.json.helmx-bak")
        self.assertEqual(json.loads(backup.read_text(encoding="utf-8")), self.original)

    def test_apply_preserves_unrelated_settings(self):
        rc, out = self._run("apply")
        self.assertEqual(rc, 0, out)
        current = self._json()
        self.assertEqual(current["model"], self.original["model"])
        self.assertEqual(current["permissions"], self.original["permissions"])
        self.assertEqual(current["env"]["ANTHROPIC_AUTH_TOKEN"], "fixture-token")

    def test_apply_adds_missing_env_object(self):
        self.settings.write_text('{"theme":"dark"}\n', encoding="utf-8")
        rc, out = self._run("apply")
        self.assertEqual(rc, 0, out)
        self.assertEqual(self._json()["env"]["ANTHROPIC_BASE_URL"], "http://127.0.0.1:1800")
        self.assertEqual(self._json()["theme"], "dark")

    def test_apply_rejects_invalid_json_without_overwrite(self):
        broken = '{"env": '
        self.settings.write_text(broken, encoding="utf-8")
        rc, _ = self._run("apply")
        self.assertNotEqual(rc, 0)
        self.assertEqual(self.settings.read_text(encoding="utf-8"), broken)

    def test_remove_restores_original_settings(self):
        self._run("apply")
        rc, out = self._run("remove")
        self.assertEqual(rc, 0, out)
        self.assertEqual(self._json(), self.original)

    def test_verify_passes_after_apply(self):
        self._run("apply")
        rc, out = self._run("verify")
        self.assertEqual(rc, 0, out)
        self.assertIn("ANTHROPIC_BASE_URL", out)

    def test_verify_fails_before_apply(self):
        rc, _ = self._run("verify")
        self.assertNotEqual(rc, 0)

    def test_proxy_reads_anthropic_relay(self):
        proc = subprocess.Popen(
            [str(HELMX), "proxy", "--listen", "0"],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
            cwd=self.tmp, env={**os.environ, **self.env},
        )
        try:
            out, _ = proc.communicate(timeout=2)
        except subprocess.TimeoutExpired:
            proc.kill()
            out, _ = proc.communicate()
        self.assertIn("auto relay: https://relay.example/gateway", out)

    def test_proxy_restore_recovers_original_settings(self):
        proc = subprocess.Popen(
            [str(HELMX), "proxy", "--listen", "1800"],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
            cwd=self.tmp, env={**os.environ, **self.env},
        )
        try:
            proc.communicate(timeout=2)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.communicate()
        self.assertTrue(self.settings.with_name("settings.json.helmx-proxy-bak").exists())
        rc, out = self._run("proxy", ["--restore"])
        self.assertEqual(rc, 0, out)
        self.assertEqual(self._json(), self.original)

    def test_codex_settings_are_untouched(self):
        before = self.codex_marker.read_bytes()
        self._run("apply")
        self._run("verify")
        self._run("remove")
        self.assertEqual(self.codex_marker.read_bytes(), before)
        self.assertEqual(list(self.codex_home.iterdir()), [self.codex_marker])


if __name__ == "__main__":
    unittest.main()
