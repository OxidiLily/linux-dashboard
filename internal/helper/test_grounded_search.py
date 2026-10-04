import importlib.util
import os
from pathlib import Path
import tempfile
import time
import unittest

SPEC = importlib.util.spec_from_file_location("grounded_search", Path(__file__).with_name("grounded-search.py"))
assert SPEC is not None and SPEC.loader is not None
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class GroundedSearchTest(unittest.TestCase):
    def test_search_and_record(self):
        with tempfile.TemporaryDirectory() as base:
            root = Path(base)
            sessions = root / "Sessions"
            sessions.mkdir(mode=0o700)
            (root / "kredensial").mkdir()
            (root / "kredensial" / "secret.md").write_text("answer hidden\n")
            (sessions / "old.md").write_text("answer verified\npassword=hidden\n")
            (sessions / "link.md").symlink_to(root / "kredensial" / "secret.md")
            results = MODULE.search(root, "answer")
            self.assertEqual(len(results), 1)
            self.assertIn("old.md", results[0][1])
            self.assertEqual(len(results[0]), 3)  # path:line only; no raw Session text
            self.assertFalse(MODULE.search(root, "hidden"))
            note = MODULE.record(root, "hermes", "contoh", "pertanyaan", "jawaban", "sumber")
            self.assertIn("Agent: hermes", note.read_text())
            self.assertEqual(note.stat().st_mode & 0o777, 0o600)
            MODULE.record(root, "codex", "contoh", "lanjutan", "hasil", "sumber")
            self.assertIn("Agent: codex", note.read_text())
            self.assertEqual(len(MODULE.search(root, "lanjutan")), 1)
            with self.assertRaises(ValueError):
                MODULE.record(root, "hermes", "contoh", "q", "password=hidden", "source")
            self.assertNotIn("password=hidden", note.read_text())
            with self.assertRaises(ValueError):
                MODULE.record(root, "evil", "contoh", "q", "a", "s")

    def test_record_uses_machine_timezone_at_runtime(self):
        with tempfile.TemporaryDirectory() as base:
            root = Path(base)
            (root / "Sessions").mkdir(mode=0o700)
            previous = os.environ.get("TZ")
            try:
                os.environ["TZ"] = "Asia/Jakarta"
                time.tzset()
                note = MODULE.record(root, "hermes", "zona-waktu", "q", "a", "s")
                self.assertRegex(note.read_text(), r"### [^\n]+\+07:00 — Agent: hermes")
                os.environ["TZ"] = "UTC"
                time.tzset()
                MODULE.record(root, "codex", "zona-waktu", "q", "a", "s")
                self.assertRegex(note.read_text(), r"### [^\n]+\+00:00 — Agent: codex")
            finally:
                if previous is None:
                    os.environ.pop("TZ", None)
                else:
                    os.environ["TZ"] = previous
                time.tzset()

    def test_symlink_target_rejected(self):
        with tempfile.TemporaryDirectory() as base:
            root = Path(base)
            sessions = root / "Sessions"
            sessions.mkdir(mode=0o700)
            destination = root / "outside"
            destination.write_text("preserve")
            (sessions / "panel-contoh.md").symlink_to(destination)
            with self.assertRaises(ValueError):
                MODULE.record(root, "hermes", "contoh", "q", "a", "s")
            self.assertEqual(destination.read_text(), "preserve")


if __name__ == "__main__":
    unittest.main()
