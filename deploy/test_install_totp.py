"""Agent: hermes. Exercise the installer's TOTP block without root."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


class InstallTOTPTest(unittest.TestCase):
    def run_block(self, denied=False, existing=True):
        source = Path(__file__).with_name('install.sh').read_text()
        block = source.split('totp_key=/var/lib/linux-dashboard-helper/totp.key', 1)[1].split('# Folder data per akun:', 1)[0]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            key = root / 'totp.key'
            original = b'K' * 32
            if existing:
                key.write_bytes(original)
                key.chmod(0o600)
            tools = root / 'tools'
            tools.mkdir()
            for name, body in {
                'chown': 'exit 0',
                'install': 'exit 0',
                'runuser': 'exit 1' if denied else 'while [ "$1" != "--" ]; do shift; done; shift; exec "$@"',
            }.items():
                script = tools / name
                script.write_text('#!/bin/sh\n' + body + '\n')
                script.chmod(0o700)
            script = 'set -euo pipefail\nSERVICE_USER=test\ndie() { printf "%s\\n" "$*" >&2; exit 1; }\nset_env_dashboard() { :; }\n'
            script += 'totp_key="$TEST_KEY"\n' + block
            result = subprocess.run(['bash', '-c', script], env={**os.environ, 'TEST_KEY': str(key), 'PATH': str(tools) + ':' + os.environ['PATH']}, capture_output=True, text=True)
            return result, key.read_bytes(), key.stat().st_mode & 0o777

    def test_unreadable_key_aborts_install(self):
        result, content, _ = self.run_block(denied=True)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn('Key TOTP tidak dapat dibaca', result.stderr)
        self.assertEqual(content, b'K' * 32)

    def test_existing_key_preserved_permissions_repaired(self):
        result, content, mode = self.run_block()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(content, b'K' * 32)
        self.assertEqual(mode, 0o640)

    def test_new_key_generated(self):
        result, content, mode = self.run_block(existing=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(content), 32)
        self.assertEqual(mode, 0o640)


if __name__ == '__main__':
    unittest.main()
