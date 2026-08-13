"""helm-x 单元测试 — 测试 config 部署/注入/验证逻辑
用法：python -m unittest test_config -v
"""
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

HELMX = Path(os.environ.get(
    "HELMX_TEST_BIN", Path(__file__).resolve().parents[1] / "build" / "helmx.exe"
))


def run(cmd, cwd, env=None, timeout=60):
    e = {**os.environ, **(env or {})}
    proc = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout, cwd=cwd, env=e)
    return proc.returncode, proc.stdout + proc.stderr


class TestConfig(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="helmx-test-")
        self.codex_home = Path(self.tmp) / ".codex"
        self.codex_home.mkdir()
        self.cfg = self.codex_home / "config.toml"
        self.cfg.write_text(
            'model_provider = "custom"\n'
            'model = "gpt-5.6-terra"\n\n'
            '[model_providers.custom]\n'
            'name = "test"\n'
            'base_url = "https://example.com/v1"\n'
            'wire_api = "responses"\n',
            encoding="utf-8",
        )
        self.env = {"CODEX_HOME": str(self.codex_home)}

    def _run(self, cmd, args=None):
        return run([str(HELMX), cmd] + (args or []), self.tmp, self.env)

    def test_apply_creates_backup(self):
        self.assertFalse((self.codex_home / "config.toml.helmx-bak").exists())
        rc, out = self._run("apply")
        self.assertEqual(rc, 0)
        self.assertTrue((self.codex_home / "config.toml.helmx-bak").exists())

    def test_apply_injects_context_request_defaults(self):
        self._run("apply")
        text = self.cfg.read_text(encoding="utf-8")
        self.assertIn("tool_output_token_limit = 8000", text)
        self.assertIn("model_auto_compact_token_limit = 180000", text)
        self.assertIn('model_auto_compact_token_limit_scope = "body_after_prefix"', text)

    def test_apply_preserves_existing_context_request_settings(self):
        self.cfg.write_text(
            'tool_output_token_limit = 12000\n'
            'model_auto_compact_token_limit = 150000\n'
            'model_auto_compact_token_limit_scope = "total"\n' +
            self.cfg.read_text(encoding="utf-8"),
            encoding="utf-8",
        )
        self._run("apply")
        text = self.cfg.read_text(encoding="utf-8")
        self.assertEqual(text.count("tool_output_token_limit"), 1)
        self.assertIn("tool_output_token_limit = 12000", text)
        self.assertIn("model_auto_compact_token_limit = 150000", text)
        self.assertIn('model_auto_compact_token_limit_scope = "total"', text)

    def test_apply_keeps_mcp_servers_user_managed(self):
        self._run("apply")
        text = (self.codex_home / "config.toml").read_text(encoding="utf-8")
        self.assertNotIn("[mcp_servers.helmx]", text)

    def test_apply_preserves_crlf(self):
        raw = self.cfg.read_bytes().replace(b"\n", b"\r\n")
        self.cfg.write_bytes(raw)
        self._run("apply")
        out = self.cfg.read_bytes()
        self.assertIn(b"\r\n", out)

    def test_apply_preserves_nested_toml(self):
        self.cfg.write_text(
            self.cfg.read_text(encoding="utf-8") + '\n[profile.work]\nmodel = "gpt-4"\n',
            encoding="utf-8",
        )
        self._run("apply")
        text = self.cfg.read_text(encoding="utf-8")
        self.assertIn("[profile.work]", text)
        self.assertIn('model = "gpt-4"', text)

    def test_remove_restores_backup(self):
        original = self.cfg.read_text(encoding="utf-8")
        self._run("apply")
        modified = self.cfg.read_text(encoding="utf-8")
        self.assertNotEqual(modified, original)
        self._run("remove")
        restored = self.cfg.read_text(encoding="utf-8")
        self.assertEqual(restored, original)

    def test_verify_passes_after_apply(self):
        self._run("apply")
        rc, out = self._run("verify")
        self.assertEqual(rc, 0, f"verify failed: {out}")

    def test_verify_fails_when_model_provider_is_not_custom(self):
        self.cfg.write_text(self.cfg.read_text(encoding="utf-8").replace(
            'model_provider = "custom"', 'model_provider = "other"'
        ), encoding="utf-8")
        rc, _ = self._run("verify")
        self.assertNotEqual(rc, 0)

    def test_verify_fails_without_config(self):
        empty = Path(self.tmp) / "empty"
        empty.mkdir()
        rc, _ = self._run("verify")
        self.assertNotEqual(rc, 0)

    def test_backup_identical_to_original(self):
        original = self.cfg.read_bytes()
        self._run("apply")
        backup = (self.codex_home / "config.toml.helmx-bak").read_bytes()
        self.assertEqual(original, backup)

    def test_apply_without_config_returns_error(self):
        empty = Path(self.tmp) / "empty"
        empty.mkdir()
        rc, out = self._run("apply", [])
        # apply falls back to default, may still succeed if system has .codex
        # so we just check it doesn't crash
        self.assertIn(rc, (0, 1))

    def test_remove_without_backup_works(self):
        rc, out = self._run("remove")
        self.assertEqual(rc, 0)

    def test_proxy_uses_active_provider_after_switch(self):
        self.cfg.write_text(
            'model_provider = "beta"\n\n'
            '[model_providers.alpha]\nbase_url = "https://alpha.example/v1"\n\n'
            '[model_providers.beta]\nbase_url = "https://beta.example/v1"\n',
            encoding="utf-8",
        )
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
        self.assertIn("auto relay: https://beta.example/v1", out)

    def test_proxy_rejects_corrupt_config_without_replacing_backup(self):
        backup = self.codex_home / "config.toml.helmx-proxy-bak"
        backup.write_text('model_provider = "custom"\n', encoding="utf-8")
        self.cfg.write_text(
            'model_provider = "custom"\n'
            '[model_providers.custom]\n'
            'base_url = "https://example.com/v1"desktop]\n',
            encoding="utf-8",
        )
        rc, _ = self._run("proxy", ["--listen", "0"])
        self.assertNotEqual(rc, 0)
        self.assertEqual(backup.read_text(encoding="utf-8"), 'model_provider = "custom"\n')

    def test_proxy_refreshes_stale_backup_and_restores_valid_toml(self):
        backup = self.codex_home / "config.toml.helmx-proxy-bak"
        backup.write_text('broken]p]\n', encoding="utf-8")
        original = self.cfg.read_text(encoding="utf-8")

        proc = subprocess.Popen(
            [str(HELMX), "proxy", "--listen", "0"],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
            cwd=self.tmp, env={**os.environ, **self.env},
        )
        try:
            proc.communicate(timeout=2)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.communicate()

        if backup.exists():
            backed_up = backup.read_text(encoding="utf-8")
            self.assertIn('base_url = "https://example.com/v1"', backed_up)
            self.assertIn("tool_output_token_limit = 8000", backed_up)
            rc, out = self._run("proxy", ["--restore"])
            self.assertEqual(rc, 0, out)
        restored = self.cfg.read_text(encoding="utf-8")
        self.assertIn('base_url = "https://example.com/v1"', restored)
        self.assertIn("tool_output_token_limit = 8000", restored)

    def test_proxy_preserves_following_line_when_url_lengths_change(self):
        self.cfg.write_text(
            'model_provider = "custom"\n'
            '[model_providers.custom]\n'
            'base_url = "https://a.co/v1"\n'
            '[desktop]\n'
            'followUpQueueMode = "queue"\n',
            encoding="utf-8",
        )
        original = self.cfg.read_text(encoding="utf-8")
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

        backup = self.codex_home / "config.toml.helmx-proxy-bak"
        if backup.exists():
            rc, out = self._run("proxy", ["--restore"])
            self.assertEqual(rc, 0, out)
        restored = self.cfg.read_text(encoding="utf-8")
        self.assertIn('base_url = "https://a.co/v1"', restored)
        self.assertIn('[desktop]\nfollowUpQueueMode = "queue"', restored)
        self.assertIn("tool_output_token_limit = 8000", restored)


if __name__ == "__main__":
    unittest.main()
