import json
import os
import subprocess
import tempfile
import time
import unittest
import urllib.request
from pathlib import Path


HELMX = Path(os.environ.get(
    "HELMX_TEST_BIN", Path(__file__).resolve().parents[1] / "build" / "helmx.exe"
))


class TestUi(unittest.TestCase):
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
