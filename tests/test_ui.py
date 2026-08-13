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
    "HELMX_TEST_BIN", Path(__file__).resolve().parents[1] / "build" / "helmx.exe"
))


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


class TestUi(unittest.TestCase):
    def test_context_ui_saves_gardener_and_codex_settings(self):
        with tempfile.TemporaryDirectory(prefix="helmx-context-ui-") as tmp:
            appdata = Path(tmp) / "AppData" / "Roaming"
            codex_home = Path(tmp) / ".codex"
            appdata.mkdir(parents=True)
            codex_home.mkdir()
            (codex_home / "config.toml").write_text(
                'model_provider = "custom"\n[model_providers.custom]\nbase_url = "https://example.com/v1"\n',
                encoding="utf-8",
            )
            port = free_port()
            env = {**os.environ, "CODEX_HOME": str(codex_home), "APPDATA": str(appdata)}
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
                    "tool_output_token_limit": 12000,
                    "auto_compact_token_limit": 160000, "scope": "total",
                }).encode()
                request = urllib.request.Request(
                    url, data=payload, method="POST", headers={"Content-Type": "application/json"},
                )
                self.assertTrue(json.load(urllib.request.urlopen(request, timeout=2))["ok"])
                context = json.load(urllib.request.urlopen(url, timeout=2))
                self.assertEqual(context["threshold_bytes"], 65536)
                self.assertEqual(context["auto_compact_token_limit"], 160000)
                self.assertFalse(context["enabled"])
                toml = (codex_home / "config.toml").read_text(encoding="utf-8")
                self.assertIn("tool_output_token_limit = 12000", toml)
                self.assertIn('model_auto_compact_token_limit_scope = "total"', toml)
            finally:
                proc.kill()
                proc.wait(timeout=5)

    def test_proxy_forwards_codex_identity_headers(self):
        captured = {}

        class CaptureUpstream(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                captured.update(self.headers.items())
                length = int(self.headers.get("Content-Length", "0"))
                self.rfile.read(length)
                response = b'{"output_text":"ok"}'
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
        ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        headers = {
            "Content-Type": "application/json",
            "session-id": "session-1",
            "session_id": "session-2",
            "thread-id": "thread-1",
            "x-client-request-id": "request-1",
            "x-codex-installation-id": "install-1",
            "x-codex-window-id": "window-1",
            "x-codex-turn-metadata": "turn-1",
            "User-Agent": "codex-test/1.0",
            "Originator": "codex-test",
        }
        try:
            request = urllib.request.Request(
                f"http://127.0.0.1:{proxy_port}/v1/responses", data=b'{}', headers=headers,
            )
            for _ in range(30):
                try:
                    urllib.request.urlopen(request, timeout=2).read()
                    break
                except OSError:
                    time.sleep(0.1)
            else:
                self.fail("Proxy did not start")
            received = {key.lower(): value for key, value in captured.items()}
            for key, value in headers.items():
                if key.lower() != "content-type":
                    self.assertEqual(received[key.lower()], value)
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
                response = b'{"output_text":"ok"}'
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
        ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            huge_image = "data:image/png;base64," + ("A" * 100000)
            payload = {
                "model": "test",
                "input": [
                    {"type": "custom_tool_call_output", "call_id": "1", "output": [
                        {"type": "input_image", "image_url": huge_image}
                    ]},
                    {"type": "message", "role": "user", "content": [
                        {"type": "input_text", "text": "continue"}
                    ]},
                ],
                "stream": False,
            }
            request = urllib.request.Request(
                f"http://127.0.0.1:{proxy_port}/v1/responses",
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
        ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            request = urllib.request.Request(
                f"http://127.0.0.1:{proxy_port}/v1/responses",
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
            codex_home = Path(tmp) / ".codex"
            appdata = Path(tmp) / "AppData" / "Roaming"
            codex_home.mkdir()
            appdata.mkdir(parents=True)
            (codex_home / "config.toml").write_text(
                'model_provider = "test"\n'
                '[model_providers.test]\n'
                'base_url = "https://example.com/v1"\n',
                encoding="utf-8",
            )
            env = {**os.environ, "CODEX_HOME": str(codex_home), "APPDATA": str(appdata)}
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
                log = (codex_home / "helmx.log").read_text(encoding="utf-8")
                self.assertNotIn("req GET /api/zxwn", log)
                self.assertNotIn("no helmx.config.json", log)
                self.assertNotRegex(log, r"key=sk-[A-Za-z0-9]+")
            finally:
                proc.kill()
                proc.wait(timeout=5)

    def test_rewriter_save_preserves_existing_key(self):
        with tempfile.TemporaryDirectory(prefix="helmx-ui-") as tmp:
            appdata = Path(tmp) / "AppData" / "Roaming"
            appdata.mkdir(parents=True)
            env = {**os.environ, "CODEX_HOME": tmp, "APPDATA": str(appdata)}
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
                config_path = appdata / "helmx.config.json"
                cfg = json.loads(config_path.read_text(encoding="utf-8"))
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
