import contextlib
import fcntl
import importlib.util
import os
from unittest import mock
from pathlib import Path
import subprocess
import tempfile
import unittest

REPO = Path(__file__).resolve().parent.parent
SCRIPT = REPO / "deploy/install-ai-state.sh"


class InstallAIStateTest(unittest.TestCase):
    def run_installer(self, home):
        return subprocess.run(
            ["bash", str(SCRIPT), str(home)], cwd=REPO,
            env={**os.environ, "HOME": str(home)}, capture_output=True, text=True,
        )

    def test_seed_from_explicit_public_root(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "private-home"
            home.mkdir(mode=0o700)
            seed = Path(tmp) / "public-seed"
            (seed / "deploy").mkdir(parents=True)
            (seed / "internal/helper").mkdir(parents=True)
            for name in ("install-ai-state.sh", "install-ai-state.py", "soul-default.md", "knowledge-base-default.md", "knowledge-base-placeholder.md", "prompt-deploy-shared-default.md", "install-shared-adapters-reference.sh"):
                (seed / "deploy" / name).write_bytes((REPO / "deploy" / name).read_bytes())
            for name in ("grounded-search.py", "policy-seed.md"):
                (seed / "internal/helper" / name).write_bytes((REPO / "internal/helper" / name).read_bytes())
            result = subprocess.run(["bash", str(seed / "deploy/install-ai-state.sh"), str(home), str(seed)], cwd="/", env={"HOME": str(home), "PATH": "/usr/bin:/bin", "LC_ALL": "C"}, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual((home / "DATA/AppData/linux-dashboard/SOUL.md").read_bytes(), (seed / "deploy/soul-default.md").read_bytes())

    def test_first_install_without_vault_and_reinstall_preserves_data(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            result = self.run_installer(home)
            self.assertEqual(result.returncode, 0, result.stderr)
            root = home / "DATA/AppData/linux-dashboard"
            self.assertEqual((root / "SOUL.md").read_bytes(), (REPO / "deploy/soul-default.md").read_bytes())
            self.assertTrue((root / "grounded-search.py").is_file())
            self.assertEqual((root / "knowledge-base.md").read_bytes(), (REPO / "deploy/knowledge-base-default.md").read_bytes())
            self.assertEqual((root / "PROMPT-DEPLOY-SHARED.md").read_bytes(), (REPO / "deploy/prompt-deploy-shared-default.md").read_bytes())
            self.assertEqual((root / "install-shared-adapters-reference.sh").read_bytes(), (REPO / "deploy/install-shared-adapters-reference.sh").read_bytes())
            self.assertEqual((root / "Sessions").stat().st_mode & 0o777, 0o700)
            (root / "SOUL.md").write_text("custom policy\n")
            (root / "knowledge-base.md").write_text("custom knowledge\n")
            (root / "Sessions" / "old.md").write_text("prior answer\n")
            result = self.run_installer(home)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual((root / "SOUL.md").read_text(), "custom policy\n")
            self.assertEqual((root / "knowledge-base.md").read_text(), "custom knowledge\n")
            self.assertEqual((root / "Sessions" / "old.md").read_text(), "prior answer\n")

    def test_old_short_seed_is_preserved(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            root = home / "DATA/AppData/linux-dashboard"
            root.mkdir(parents=True)
            (root / "SOUL.md").write_bytes((REPO / "internal/helper/policy-seed.md").read_bytes())
            result = self.run_installer(home)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual((root / "SOUL.md").read_bytes(), (REPO / "internal/helper/policy-seed.md").read_bytes())

    def test_old_kb_placeholder_is_preserved(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            root = home / "DATA/AppData/linux-dashboard"
            root.mkdir(parents=True)
            (root / "knowledge-base.md").write_bytes((REPO / "deploy/knowledge-base-placeholder.md").read_bytes())
            result = self.run_installer(home)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual((root / "knowledge-base.md").read_bytes(), (REPO / "deploy/knowledge-base-placeholder.md").read_bytes())

    def test_hardlink_managed_script_does_not_modify_other_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            root = home / "DATA/AppData/linux-dashboard"
            root.mkdir(parents=True)
            other = home / "other.py"
            other.write_text("preserve\n")
            (root / "grounded-search.py").hardlink_to(other)
            result = self.run_installer(home)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(other.read_text(), "preserve\n")

    def test_hardlink_old_policy_does_not_modify_other_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            root = home / "DATA/AppData/linux-dashboard"
            root.mkdir(parents=True)
            other = home / "other.md"
            other.write_bytes((REPO / "internal/helper/policy-seed.md").read_bytes())
            (root / "SOUL.md").hardlink_to(other)
            result = self.run_installer(home)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(other.read_bytes(), (REPO / "internal/helper/policy-seed.md").read_bytes())

    def test_other_writable_appdata_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            appdata = home / "DATA/AppData"
            appdata.mkdir(parents=True)
            appdata.chmod(0o777)
            result = self.run_installer(home)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((appdata / "linux-dashboard/SOUL.md").exists())

    def test_unsafe_directories_are_rejected_without_chmod(self):
        for relative in (".", "DATA", "DATA/AppData", "DATA/AppData/linux-dashboard",
                         "DATA/AppData/linux-dashboard/Sessions", "DATA/AppData/linux-dashboard/Skills"):
            with self.subTest(relative=relative), tempfile.TemporaryDirectory() as tmp:
                home = Path(tmp)
                directory = home / relative
                directory.mkdir(parents=True, exist_ok=True)
                directory.chmod(0o777)
                result = self.run_installer(home)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(directory.stat().st_mode & 0o777, 0o777)

    def load_helper(self):
        spec = importlib.util.spec_from_file_location("ai_seed", REPO / "deploy/install-ai-state.py")
        assert spec is not None and spec.loader is not None
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def test_first_install_publication_never_clobbers_new_policy(self):
        module = self.load_helper()
        for name in ("SOUL.md", "knowledge-base.md"):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                original = os.link
                def compete(source, target, **kwargs):
                    (root / name).write_text("concurrent policy")
                    return original(source, target, **kwargs)
                with contextlib.ExitStack() as stack:
                    fd = module.open_home(stack, str(root))
                    with mock.patch.object(module.os, "link", side_effect=compete):
                        module.publish(fd, name, b"seed")
                self.assertEqual((root / name).read_text(), "concurrent policy")
                self.assertEqual([p.name for p in root.iterdir()], [name])

    def test_parent_swap_does_not_redirect_writes(self):
        module = self.load_helper()
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            outside = home / "outside"
            outside.mkdir()
            original = module.publish
            swapped = False
            def swap(parent, name, content, replace=False):
                nonlocal swapped
                if not swapped:
                    (home / "DATA").rename(home / "pinned")
                    (home / "DATA").symlink_to(outside)
                    swapped = True
                return original(parent, name, content, replace)
            with mock.patch.dict(os.environ, HOME=str(home)), mock.patch.object(module, "publish", side_effect=swap):
                module.install(str(home), REPO)
            self.assertEqual(list(outside.iterdir()), [])
            self.assertTrue((home / "pinned/AppData/linux-dashboard/SOUL.md").is_file())

    def test_installer_waits_for_lock_and_preserves_concurrent_policy(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            root = home / "DATA/AppData/linux-dashboard"
            root.mkdir(parents=True)
            with (root / ".ai-state.lock").open("wb") as lock:
                fcntl.flock(lock, fcntl.LOCK_EX)
                processes = [subprocess.Popen(["bash", str(SCRIPT), str(home)], cwd=REPO,
                             env={**os.environ, "HOME": str(home)}, stdout=subprocess.PIPE,
                             stderr=subprocess.PIPE, text=True) for _ in range(4)]
                try:
                    for process in processes:
                        with self.assertRaises(subprocess.TimeoutExpired):
                            process.wait(timeout=0.15)
                    self.assertFalse((root / "grounded-search.py").exists())
                    (root / "SOUL.md").write_text("concurrent policy")
                    fcntl.flock(lock, fcntl.LOCK_UN)
                    for process in processes:
                        _, error = process.communicate(timeout=10)
                        self.assertEqual(process.returncode, 0, error)
                    self.assertEqual((root / "SOUL.md").read_text(), "concurrent policy")
                finally:
                    for process in processes:
                        if process.poll() is None:
                            process.kill()
                        process.communicate()

    def test_other_writable_files_are_rejected(self):
        for name in ("SOUL.md", "knowledge-base.md", "grounded-search.py", ".ai-state.lock"):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                home = Path(tmp)
                root = home / "DATA/AppData/linux-dashboard"
                root.mkdir(parents=True)
                target = root / name
                target.write_text("untouched")
                target.chmod(0o666)
                result = self.run_installer(home)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(target.read_text(), "untouched")

    def test_unsafe_lock_and_policy_types_are_rejected(self):
        for name in (".ai-state.lock", "SOUL.md", "knowledge-base.md"):
            for kind in ("symlink", "hardlink", "fifo"):
                with self.subTest(name=name, kind=kind), tempfile.TemporaryDirectory() as tmp:
                    home = Path(tmp)
                    root = home / "DATA/AppData/linux-dashboard"
                    root.mkdir(parents=True)
                    other = home / "other"
                    other.write_text("preserve")
                    target = root / name
                    if kind == "symlink":
                        target.symlink_to(other)
                    elif kind == "hardlink":
                        target.hardlink_to(other)
                    else:
                        os.mkfifo(target)
                    result = self.run_installer(home)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(other.read_text(), "preserve")

    def test_foreign_owner_is_rejected(self):
        module = self.load_helper()
        with tempfile.TemporaryDirectory() as tmp:
            # Exercise the owner check without privileged chown in the test suite.
            info = Path(tmp).stat()
            with mock.patch.object(module.os, "geteuid", return_value=info.st_uid + 1):
                with self.assertRaises(ValueError):
                    module.validate(info, directory=True)

    def test_failed_atomic_write_preserves_existing_managed_script(self):
        module = self.load_helper()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "grounded-search.py").write_bytes(b"old")
            with contextlib.ExitStack() as stack:
                fd = module.open_home(stack, str(root))
                with mock.patch.object(module.os, "fsync", side_effect=OSError("disk failure")):
                    with self.assertRaises(OSError):
                        module.publish(fd, "grounded-search.py", b"new", True)
            self.assertEqual((root / "grounded-search.py").read_bytes(), b"old")
            self.assertEqual([p.name for p in root.iterdir()], ["grounded-search.py"])

    def test_symlink_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            (home / "DATA").mkdir()
            (home / "DATA/AppData").symlink_to(home)
            result = self.run_installer(home)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse((home / "linux-dashboard").exists())


if __name__ == "__main__":
    unittest.main()
