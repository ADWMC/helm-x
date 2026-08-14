import json
import http.server
import os
import socket
import subprocess
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request
from pathlib import Path


HELMX = Path(os.environ.get(
    "HELMX_TEST_BIN", Path(__file__).resolve().parents[1] / "build-check" / "helmx.exe"
))


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


class TestUi(unittest.TestCase):
    def setUp(self):
        self.tmp_obj = tempfile.TemporaryDirectory(prefix="helmx-proxy-test-")
        root = Path(self.tmp_obj.name)
        appdata = root / "AppData" / "Roaming"
        claude_home = root / ".claude"
        appdata.mkdir(parents=True)
        claude_home.mkdir()
        (claude_home / "settings.json").write_text(
            '{"env":{"ANTHROPIC_BASE_URL":"https://fixture.invalid"}}\n', encoding="utf-8")
        self.clean_env = {
            **os.environ,
            "APPDATA": str(appdata),
            "CLAUDE_CONFIG_DIR": str(claude_home),
        }

    def tearDown(self):
        self.tmp_obj.cleanup()

    def test_context_ui_saves_gardener_settings(self):
        with tempfile.TemporaryDirectory(prefix="helmx-context-ui-") as tmp:
            appdata = Path(tmp) / "AppData" / "Roaming"
            claude_home = Path(tmp) / ".claude"
            appdata.mkdir(parents=True)
            claude_home.mkdir()
            (claude_home / "settings.json").write_text(
                '{"env":{"ANTHROPIC_BASE_URL":"https://example.com"}}\n', encoding="utf-8")
            port = free_port()
            env = {**os.environ, "CLAUDE_CONFIG_DIR": str(claude_home), "APPDATA": str(appdata)}
            proc = subprocess.Popen(
                [str(HELMX), "ui", "--port", str(port)], env=env,
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            )
            try:
                url = f"http://127.0.0.1:{port}/api/context"
                for _ in range(30):
                    try:
                        urllib.request.urlopen(f"http://127.0.0.1:{port}/api/status", timeout=1)
                        break
                    except OSError:
                        time.sleep(0.1)
                payload = json.dumps({
                    "enabled": False, "threshold_bytes": 65536,
                }).encode()
                request = urllib.request.Request(
                    url, data=payload, method="POST", headers={"Content-Type": "application/json"},
                )
                self.assertTrue(json.load(urllib.request.urlopen(request, timeout=2))["ok"])
                context = json.load(urllib.request.urlopen(url, timeout=2))
                self.assertEqual(context["threshold_bytes"], 65536)
                self.assertFalse(context["enabled"])
            finally:
                proc.kill()
                proc.wait(timeout=5)

    def test_proxy_forwards_anthropic_headers_and_injects_system(self):
        captured = {}

        class CaptureUpstream(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                captured["headers"] = dict(self.headers.items())
                captured["path"] = self.path
                length = int(self.headers.get("Content-Length", "0"))
                captured["body"] = json.loads(self.rfile.read(length))
                response = (b'{"id":"msg_fixture","type":"message","role":"assistant",'
                            b'"content":[{"type":"text","text":"ok"}],'
                            b'"model":"fixture","stop_reason":"end_turn",'
                            b'"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}')
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(response)))
                self.end_headers()
                self.wfile.write(response)

            def log_message(self, *_):
                pass

        upstream = http.server.ThreadingHTTPServer(("127.0.0.1", 0), CaptureUpstream)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        proxy_port = free_port()
        proc = subprocess.Popen([
            str(HELMX), "proxy", "--listen", str(proxy_port),
            "--upstream", f"http://127.0.0.1:{upstream.server_port}/v1",
        ], env=self.clean_env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        headers = {
            "Content-Type": "application/json",
            "x-api-key": "fixture-key",
            "anthropic-version": "2023-06-01",
            "anthropic-beta": "prompt-caching-2024-07-31",
            "anthropic-dangerous-direct-browser-access": "true",
            "x-claude-code-session-id": "fixture-session",
            "x-stainless-runtime": "node",
            "x-stainless-package-version": "0.94.0",
            "x-app": "cli",
            "User-Agent": "claude-cli/2.1.220 (external, sdk-cli)",
        }
        payload = {
            "context_management": {"edits": []},
            "max_tokens": 64,
            "messages": [{"role": "user", "content": [{"type": "text", "text": "helmx"}]}],
            "metadata": {"user_id": "fixture"},
            "model": "claude-test",
            "output_config": {"effort": "high"},
            "system": [{"type": "text", "text": "original system"}],
            "thinking": {"type": "enabled", "budget_tokens": 32},
            "tools": [{"name": "fixture_tool", "description": "fixture", "input_schema": {"type": "object"}}],
        }
        try:
            request = urllib.request.Request(
                f"http://127.0.0.1:{proxy_port}/v1/messages?beta=true",
                data=json.dumps(payload, separators=(",", ":")).encode(), headers=headers,
            )
            for _ in range(30):
                try:
                    urllib.request.urlopen(request, timeout=2).read()
                    break
                except OSError:
                    time.sleep(0.1)
            else:
                self.fail("Proxy did not start")
            received = {key.lower(): value for key, value in captured["headers"].items()}
            for key, value in headers.items():
                if key.lower() != "content-type":
                    self.assertEqual(received[key.lower()], value)
            self.assertEqual(captured["path"], "/v1/messages?beta=true")
            self.assertIn("system", captured["body"])
            self.assertIn("helm-x online", captured["body"]["system"][0]["text"])
            self.assertEqual(captured["body"]["system"][1]["text"], "original system")
            self.assertEqual(captured["body"]["thinking"], payload["thinking"])
            self.assertEqual(captured["body"]["tools"], payload["tools"])
            self.assertEqual(captured["body"]["context_management"], payload["context_management"])
            self.assertEqual(captured["body"]["output_config"], payload["output_config"])
            self.assertNotIn("stream", captured["body"])
        finally:
            proc.kill()
            proc.wait(timeout=5)
            upstream.shutdown()
            upstream.server_close()

    def test_proxy_prunes_large_historical_tool_output(self):
        captured = {}

        class CaptureUpstream(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                length = int(self.headers.get("Content-Length", "0"))
                captured["body"] = self.rfile.read(length)
                response = b'{"type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}'
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(response)))
                self.end_headers()
                self.wfile.write(response)

            def log_message(self, *_):
                pass

        upstream = http.server.ThreadingHTTPServer(("127.0.0.1", 0), CaptureUpstream)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        proxy_port = free_port()
        proc = subprocess.Popen([
            str(HELMX), "proxy", "--listen", str(proxy_port),
            "--upstream", f"http://127.0.0.1:{upstream.server_port}/v1",
        ], env=self.clean_env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            huge_image = "data:image/png;base64," + ("A" * 100000)
            payload = {
                "model": "test",
                "messages": [
                    {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "1", "content": [
                        {"type": "image", "source": {"type": "base64", "media_type": "image/png",
                                                        "data": huge_image}}
                    ]}]},
                    {"role": "user", "content": [{"type": "text", "text": "continue"}]},
                ],
                "stream": False,
            }
            request = urllib.request.Request(
                f"http://127.0.0.1:{proxy_port}/v1/messages",
                data=json.dumps(payload).encode(),
                headers={"Content-Type": "application/json"},
            )
            for _ in range(30):
                try:
                    urllib.request.urlopen(request, timeout=2).read()
                    break
                except OSError:
                    time.sleep(0.1)
            else:
                self.fail("Proxy did not start")
            forwarded = captured["body"].decode()
            json.loads(forwarded)
            self.assertNotIn("A" * 1000, forwarded)
            self.assertIn("helm-x context guard", forwarded)
            self.assertLess(len(forwarded), 20000)
        finally:
            proc.kill()
            proc.wait(timeout=5)
            upstream.shutdown()
            upstream.server_close()

    def test_proxy_normalizes_invalid_upstream_error(self):
        class EmptyUpstream(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                self.send_response(200)
                self.send_header("Content-Length", "0")
                self.end_headers()

            def log_message(self, *_):
                pass

        upstream = http.server.ThreadingHTTPServer(("127.0.0.1", 0), EmptyUpstream)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        proxy_port = free_port()
        proc = subprocess.Popen([
            str(HELMX), "proxy", "--listen", str(proxy_port),
            "--upstream", f"http://127.0.0.1:{upstream.server_port}/v1",
        ], env=self.clean_env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            request = urllib.request.Request(
                f"http://127.0.0.1:{proxy_port}/v1/messages",
                data=b'{}', headers={"Content-Type": "application/json"},
            )
            for _ in range(30):
                try:
                    urllib.request.urlopen(request, timeout=1)
                except urllib.error.HTTPError as error:
                    self.assertEqual(error.code, 502)
                    self.assertEqual(json.load(error)["error"]["code"], "upstream_response_error")
                    break
                except OSError:
                    time.sleep(0.1)
            else:
                self.fail("Proxy did not return a structured upstream error")
        finally:
            proc.kill()
            proc.wait(timeout=5)
            upstream.shutdown()
            upstream.server_close()

    def test_zxwn_poll_does_not_spam_request_log(self):
        with tempfile.TemporaryDirectory(prefix="helmx-ui-") as tmp:
            claude_home = Path(tmp) / ".claude"
            appdata = Path(tmp) / "AppData" / "Roaming"
            claude_home.mkdir()
            appdata.mkdir(parents=True)
            (claude_home / "settings.json").write_text(
                '{"env":{"ANTHROPIC_BASE_URL":"https://example.com"}}\n', encoding="utf-8")
            env = {**os.environ, "CLAUDE_CONFIG_DIR": str(claude_home), "APPDATA": str(appdata)}
            proc = subprocess.Popen(
                [str(HELMX), "ui", "--port", "18083"], env=env,
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            )
            try:
                for _ in range(30):
                    try:
                        urllib.request.urlopen("http://127.0.0.1:18083/api/zxwn", timeout=1)
                        break
                    except OSError:
                        time.sleep(0.1)
                else:
                    self.fail("UI server did not start")
                urllib.request.urlopen("http://127.0.0.1:18083/api/zxwn", timeout=1).read()
                urllib.request.urlopen("http://127.0.0.1:18083/api/rewriter", timeout=1).read()
                log = (claude_home / "helmx.log").read_text(encoding="utf-8")
                self.assertNotIn("req GET /api/zxwn", log)
                self.assertNotIn("no helmx-claudecode.config.json", log)
                self.assertNotRegex(log, r"key=sk-[A-Za-z0-9]+")
            finally:
                proc.kill()
                proc.wait(timeout=5)

    def test_rewriter_save_preserves_existing_key(self):
        with tempfile.TemporaryDirectory(prefix="helmx-ui-") as tmp:
            appdata = Path(tmp) / "AppData" / "Roaming"
            appdata.mkdir(parents=True)
            shared_config = appdata / "helmx.config.json"
            shared_config.write_text(
                '{"rewriter":{"enabled":true,"provider":"shared","api_key":"shared-key"}}\n',
                encoding="utf-8",
            )
            env = {**os.environ, "CLAUDE_CONFIG_DIR": tmp, "APPDATA": str(appdata)}
            proc = subprocess.Popen(
                [str(HELMX), "ui", "--port", "18082"], env=env,
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            )
            try:
                for _ in range(30):
                    try:
                        urllib.request.urlopen("http://127.0.0.1:18082/api/status", timeout=1)
                        break
                    except OSError:
                        time.sleep(0.1)
                initial = json.load(urllib.request.urlopen("http://127.0.0.1:18082/api/rewriter", timeout=2))
                self.assertFalse(initial["enabled"])
                self.assertNotEqual(initial["provider"], "shared")
                self._post({
                    "enabled": True, "provider": "first", "model": "model-a",
                    "api_key": "secret-key", "base_url": "https://first.example/v1",
                    "proxy_url": "http://127.0.0.1:7890", "timeout_sec": 45,
                    "use_proxy": True,
                })
                self._post({
                    "enabled": True, "provider": "second", "model": "model-b",
                    "api_key": "", "base_url": "https://second.example/v1",
                    "proxy_url": "http://127.0.0.1:7891", "timeout_sec": 50,
                    "use_proxy": True,
                })
                config_path = appdata / "helmx-claudecode.config.json"
                cfg = json.loads(config_path.read_text(encoding="utf-8"))
                self.assertEqual(json.loads(shared_config.read_text(encoding="utf-8"))["rewriter"]["provider"], "shared")
                self.assertEqual(cfg["rewriter"]["api_key"], "secret-key")
                self.assertEqual(cfg["rewriter"]["provider"], "second")
                self.assertEqual(cfg["rewriter"]["proxy_url"], "http://127.0.0.1:7891")
                self.assertEqual(cfg["rewriter"]["timeout_sec"], 50)
                self.assertTrue(cfg["rewriter"]["use_proxy"])
            finally:
                proc.kill()
                proc.wait(timeout=5)

    def _post(self, data):
        request = urllib.request.Request(
            "http://127.0.0.1:18082/api/rewriter/save",
            data=json.dumps(data).encode(), method="POST",
            headers={"Content-Type": "application/json"},
        )
        with urllib.request.urlopen(request, timeout=5) as response:
            self.assertTrue(json.load(response)["ok"])


if __name__ == "__main__":
    unittest.main()
