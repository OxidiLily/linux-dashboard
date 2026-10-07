"""Regresi anggaran build Go; tidak menjalankan installer root."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class BuildMemoryTest(unittest.TestCase):
    def test_go_targets_use_serial_memory_budget(self):
        with tempfile.TemporaryDirectory() as tmp:
            go = Path(tmp) / "go"
            go.write_text('#!/bin/sh\nprintf "%s|%s|%s|%s\\n" "$GOMAXPROCS" "$GOGC" "$GOMEMLIMIT" "$*"\n')
            go.chmod(0o700)
            env = dict(os.environ, PATH=tmp + os.pathsep + os.environ["PATH"])
            for name in ("GOMAXPROCS", "GOGC", "GOMEMLIMIT", "MAKEFLAGS"):
                env.pop(name, None)
            result = subprocess.run(
                ["make", "-s", "server", "helper", "BINDIR=" + tmp],
                cwd=ROOT, env=env, text=True, capture_output=True, check=True,
            )
            lines = result.stdout.splitlines()
            self.assertEqual(len(lines), 2, result.stdout)
            for line in lines:
                self.assertTrue(line.startswith("1|20|256MiB|build -p=1 -gcflags=modernc.org/sqlite/lib=-c=1 "), line)


if __name__ == "__main__":
    unittest.main()
