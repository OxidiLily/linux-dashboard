#!/usr/bin/env python3
"""Per-account seed. Trust root/account code; do not defend against hostile same-UID code.

flock serializes cooperating installers. Pinned directories prevent pathname
redirection; link publication never replaces existing user policy, even without
cooperation. Only grounded-search.py is managed/replaced. Never migrate by cmp.
"""
import contextlib
import fcntl
import os
from pathlib import Path
import secrets
import stat
import sys

FILES = (
    ("internal/helper/grounded-search.py", "grounded-search.py", True),
    ("deploy/soul-default.md", "SOUL.md", False),
    ("deploy/knowledge-base-default.md", "knowledge-base.md", False),
    ("deploy/prompt-deploy-shared-default.md", "PROMPT-DEPLOY-SHARED.md", False),
    ("deploy/install-shared-adapters-reference.sh", "install-shared-adapters-reference.sh", False),
)
DIR_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC


def validate(info, directory=False, owners=None):
    owners = {os.geteuid()} if owners is None else owners
    kind = stat.S_ISDIR if directory else stat.S_ISREG
    if (not kind(info.st_mode) or info.st_uid not in owners
            or info.st_mode & 0o7022 or (not directory and info.st_nlink != 1)):
        raise ValueError("Unsafe ownership, mode, type or hardlink")


def keep(stack, fd):
    stack.callback(os.close, fd)
    return fd


def open_home(stack, home):
    if not os.path.isabs(home) or ".." in Path(home).parts:
        raise ValueError("Home must be an absolute path without traversal")
    fd = keep(stack, os.open("/", DIR_FLAGS))
    validate(os.fstat(fd), True, {0, os.geteuid()})
    for part in Path(home).parts[1:]:
        fd = keep(stack, os.open(part, DIR_FLAGS, dir_fd=fd))
        validate(os.fstat(fd), True, {0, os.geteuid()})
    validate(os.fstat(fd), True)
    return fd


def directory(stack, parent, name):
    try:
        os.mkdir(name, 0o700, dir_fd=parent)
    except FileExistsError:
        pass
    fd = keep(stack, os.open(name, DIR_FLAGS, dir_fd=parent))
    validate(os.fstat(fd), True)
    return fd


def existing(parent, name):
    try:
        info = os.stat(name, dir_fd=parent, follow_symlinks=False)
    except FileNotFoundError:
        return False
    validate(info)
    return True


def publish(parent, name, data, replace=False):
    if existing(parent, name) and not replace:
        return
    temporary = ".ai-state." + secrets.token_hex(16)
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                 0o600, dir_fd=parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fchmod(stream.fileno(), 0o644)
            os.fsync(stream.fileno())
        if replace:
            existing(parent, name)
            os.replace(temporary, name, src_dir_fd=parent, dst_dir_fd=parent)
        else:
            try:
                os.link(temporary, name, src_dir_fd=parent, dst_dir_fd=parent,
                        follow_symlinks=False)
            except FileExistsError:
                existing(parent, name)  # Reject unsafe competing entries, preserve safe ones.
    finally:
        try:
            os.unlink(temporary, dir_fd=parent)
        except FileNotFoundError:
            pass
    os.fsync(parent)


def install(home, seed):
    if home != os.environ.get("HOME"):
        raise ValueError("Home does not match account environment")
    with contextlib.ExitStack() as stack:
        fd = open_home(stack, home)
        for name in ("DATA", "AppData", "linux-dashboard"):
            fd = directory(stack, fd, name)
        lock = keep(stack, os.open(".ai-state.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW
                                  | os.O_NONBLOCK | os.O_CLOEXEC, 0o600, dir_fd=fd))
        validate(os.fstat(lock))
        fcntl.flock(lock, fcntl.LOCK_EX)
        validate(os.fstat(lock))
        sessions = directory(stack, fd, "Sessions")
        directory(stack, fd, "Skills")
        for _, name, _ in FILES:
            existing(fd, name)
        # Source is installer-owned trusted code/data, never account runtime state.
        data = [(name, (Path(seed) / source).read_bytes(), replace)
                for source, name, replace in FILES]
        os.fchmod(fd, 0o700)
        os.fchmod(sessions, 0o700)
        for name, content, replace in data:
            publish(fd, name, content, replace)
    print("AI state: " + home + "/DATA/AppData/linux-dashboard")


if __name__ == "__main__":
    try:
        if len(sys.argv) != 3:
            raise ValueError("Usage: install-ai-state.py <home> <seed-root>")
        install(sys.argv[1], sys.argv[2])
    except (OSError, ValueError) as error:
        print("AI state rejected: " + str(error), file=sys.stderr)
        sys.exit(1)
