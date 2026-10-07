import os
import runpy
import stat
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

SCRIPT = str(Path(__file__).with_name("migrate-helper-state.py"))


class MigrationTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.old = Path(self.tmp.name) / "old"
        self.new = Path(self.tmp.name) / "new"
        self.old.mkdir(mode=0o750)
        self.new.mkdir(mode=0o750)

    def file(self, name, data=b"synthetic", mode=0o600):
        path = self.old / name
        path.parent.mkdir(exist_ok=True)
        if path.parent != self.old:
            path.parent.chmod(0o700)
        path.write_bytes(data)
        path.chmod(mode)
        return path

    def migrate(self, untrusted=()):
        actual = os.fstat
        def owned(fd):
            info = actual(fd)
            values = list(info)
            if info.st_ino not in untrusted:
                values[stat.ST_UID] = 0
            return os.stat_result(values)
        with patch.object(sys, "argv", [SCRIPT, str(self.old), str(self.new)]), patch.object(os, "fstat", owned):
            try:
                runpy.run_path(SCRIPT, run_name="__main__")
            except SystemExit as exc:
                self.assertEqual(exc.code, 0)

    def test_web_owned_parent_never_promotes_legacy_file(self):
        self.file("9router-password")
        self.migrate((self.old.stat().st_ino,))
        self.assertFalse((self.new / "9router-password").exists())
        self.assertTrue((self.old / "9router-password").exists())

    def test_trusted_files_moved_without_overwriting_target(self):
        self.file("9router-password")
        self.file("retired-password")
        self.file("tailscale-authkey.mask")
        self.file("ponytail.terpasang", mode=0o644)
        (self.new / "9router-password").write_bytes(b"newer")
        self.migrate()
        for name in ("tailscale-authkey.mask", "ponytail.terpasang"):
            self.assertFalse((self.old / name).exists(), name)
            self.assertEqual((self.new / name).read_bytes(), b"synthetic")
            self.assertEqual(stat.S_IMODE((self.new / name).stat().st_mode), 0o600)
        self.assertEqual((self.new / "9router-password").read_bytes(), b"newer")
        self.assertTrue((self.old / "9router-password").exists())
        self.assertFalse((self.new / "retired-password").exists())
        self.assertTrue((self.old / "retired-password").exists())
        self.assertEqual((self.old / "retired-password").read_bytes(), b"synthetic")
        self.migrate()  # idempotent

    def test_rejects_web_owned_loose_symlink_hardlink_and_untrusted_directory(self):
        owner = self.file("9router-password")
        loose = self.file("ponytail.terpasang", mode=0o666)
        hard = self.file("tailscale-authkey.mask")
        os.symlink("retired-password", self.old / "decoy-symlink")
        os.link(hard, self.old / "decoy-hardlink")  # nlink=2 pada entri daftar-migrasi
        self.migrate((owner.stat().st_ino,))
        self.assertFalse((self.new / owner.name).exists())
        self.assertFalse((self.new / loose.name).exists())
        self.assertFalse((self.new / "ponytail.terpasang").exists())
        self.assertFalse((self.new / "tailscale-authkey.mask").exists())  # hardlinked source rejected
        self.assertTrue(owner.exists())


if __name__ == "__main__":
    unittest.main()
