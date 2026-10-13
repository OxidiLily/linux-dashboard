"""Durable Go regression template; *_test.go is globally ignored."""
import os
from pathlib import Path
import subprocess
import unittest

class PostfixRegression(unittest.TestCase):
    def test_auth_contract(self):
        root = Path(__file__).resolve().parents[1]
        api = (root / "internal/api/postfix.go").read_text()
        self.assertEqual(api.count("if !requireSudo(w, r)"), 2)
        self.assertIn("sessionFrom(r).HelperToken", api)
        self.assertIn("sess.HelperToken, in, &out", api)
        auth = (root / "internal/helper/authz.go").read_text()
        for command in ["CmdPostfixRead", "CmdPostfixSave"]:
            self.assertRegex(auth, command + r"\s*:\s*true")
            self.assertIn(command, (root / "internal/helper/server.go").read_text())

    def test_transport(self):
        root = Path(__file__).resolve().parents[1]
        source = root / "internal/helper/postfix_regression.go.txt"
        target = root / "internal/helper/postfix_durable_test.go"
        with target.open("x") as f:
            f.write(source.read_text())
        try:
            subprocess.run(["go", "test", "./internal/helper", "-run", "^TestPostfixDurable", "-count=1", "-v"], cwd=root, check=True)
        finally:
            target.unlink()

if __name__ == "__main__":
    unittest.main()
