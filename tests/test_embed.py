import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


class TestEmbed(unittest.TestCase):
    def test_missing_optional_rewriter_config_emits_valid_defaults(self):
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "resources_generated.cpp"
            subprocess.run(
                [sys.executable, str(ROOT / "tools" / "embed.py"), tmp, out],
                check=True,
                capture_output=True,
                text=True,
            )
            generated = out.read_text(encoding="utf-8")
            match = re.search(r"kRewriterBuiltinCipherLen = (\d+)", generated)
            self.assertIsNotNone(match)
            self.assertGreater(int(match.group(1)), 0)

    def test_qa_json_is_valid_and_embedded(self):
        qa = __import__("json").loads((ROOT / "assets" / "qa.json").read_text(encoding="utf-8"))
        self.assertTrue(qa["items"])
        self.assertTrue(all(item.get("question") and item.get("answer") for item in qa["items"]))
        generated = (ROOT / "src" / "resources_generated.cpp").read_text(encoding="utf-8")
        match = re.search(r"kQaJsonCipherLen = (\d+)", generated)
        self.assertIsNotNone(match)
        self.assertGreater(int(match.group(1)), 0)


if __name__ == "__main__":
    unittest.main()
