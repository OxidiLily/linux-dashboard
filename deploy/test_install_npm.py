import pathlib
import subprocess
import unittest


INSTALLER = pathlib.Path(__file__).with_name('install.sh').read_text()


class InstallNpmTest(unittest.TestCase):
    def run_npm(self, installed='11.19.0', latest='12.2.0', failed=False):
        function = INSTALLER.split('pastikan_npm() {', 1)[1].split('\n}\n', 1)[0]
        script = '''set -eu
log() { printf '%s\\n' "$*"; }
ok() { printf '%s\\n' "$*"; }
die() { printf '%s\\n' "$*"; exit 1; }
installed="$1"
latest="$2"
failed="$3"
npm() {
  case "$1" in
    --version) printf '%s\\n' "$installed" ;;
    view) printf '%s\\n' "$latest" ;;
    install) [ "$failed" = False ] || return 1; installed="$latest" ;;
  esac
}
pastikan_npm() {''' + function + '\n}\npastikan_npm\n'
        return subprocess.run(['bash', '-c', script, 'test', installed, latest, str(failed)],
                              capture_output=True, text=True, timeout=5)

    def test_failed_upgrade_stops_install(self):
        result = self.run_npm(failed=True)
        self.assertNotEqual(result.returncode, 0, result.stdout)

    def test_successful_upgrade(self):
        result = self.run_npm()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('12.2.0', result.stdout)

    def test_build_checks_toolchain_even_when_npm_exists(self):
        build = INSTALLER.split('if (( need_build )); then', 1)[1].split('\nfi', 1)[0]
        self.assertNotIn('if ! command -v go', build)
        self.assertIn('install_build_deps', build)

    def test_node_minimum_matches_latest_npm(self):
        function = INSTALLER.split('install_build_deps() {', 1)[1].split('\n}\n', 1)[0]
        self.assertIn('24.15.0', function)
        self.assertIn('apt-get install -y -qq nodejs', function)

    def test_current_version(self):
        result = self.run_npm(installed='12.2.0')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('sudah terbaru', result.stdout)


if __name__ == '__main__':
    unittest.main()
