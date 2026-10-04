#!/usr/bin/env python3
"""Move only root-owned legacy helper state; never trust web-writable entries."""
import os
import stat
import sys

OLD, NEW = sys.argv[1:]
FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
old = os.open(OLD, FLAGS)
new = os.open(NEW, FLAGS)


def directory(fd, mode):
    info = os.fstat(fd)
    if info.st_uid != 0 or stat.S_IMODE(info.st_mode) != mode:
        raise ValueError("untrusted directory")


def move(src, dst, name, marker=False):
    try:
        source = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=src)
    except FileNotFoundError:
        return
    except OSError:
        return  # symlinks and inaccessible entries are not migration candidates
    try:
        info = os.fstat(source)
        mode = stat.S_IMODE(info.st_mode)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_nlink != 1 or (mode != 0o644 if marker else mode != 0o600):
            return
        try:
            target = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=dst)
        except FileExistsError:
            return  # never replace newer helper state
        try:
            with os.fdopen(target, "wb", closefd=False) as output:
                while block := os.read(source, 65536):
                    output.write(block)
                output.flush()
                os.fsync(target)
            os.fchmod(target, 0o600)
        except BaseException:
            os.unlink(name, dir_fd=dst)
            raise
        finally:
            os.close(target)
        # Do not unlink a different inode if the web owner replaced the entry.
        current = os.stat(name, dir_fd=src, follow_symlinks=False)
        if (current.st_dev, current.st_ino) == (info.st_dev, info.st_ino):
            os.unlink(name, dir_fd=src)
    finally:
        os.close(source)


try:
    directory(new, 0o750)
    # A web-owned parent lets the web service replace even root-owned entries.
    # No entry below it can establish trustworthy provenance.
    parent = os.fstat(old)
    if parent.st_uid != 0 or parent.st_mode & 0o022:
        sys.exit(0)
    for name in ("9router-password", "stalwart-password", "tailscale-authkey.mask", "ponytail.terpasang"):
        move(old, new, name, name == "ponytail.terpasang")
finally:
    os.close(old)
    os.close(new)
