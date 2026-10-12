"""POSIX private file handling: directory-fd traversal, no symlinks or Git secrets."""
import json
import os
from pathlib import Path
import re
import stat
from urllib.parse import urlsplit

TOKEN_RE = re.compile(r'finance_ro_[a-f0-9]{64}\Z')


def origin(value):
    if not isinstance(value, str):
        raise ValueError('An explicit HTTPS origin is required')
    parsed = urlsplit(value)
    if (parsed.scheme != 'https' or not parsed.hostname or parsed.username is not None
            or parsed.password is not None or parsed.path or parsed.query or parsed.fragment
            or not re.fullmatch(r'https://[a-z0-9]+(?:[.-][a-z0-9]+)*(?::[1-9][0-9]{0,4})?', value)
            or parsed.port is not None and not 1 <= parsed.port <= 65535):
        raise ValueError('Use an exact HTTPS origin without path or userinfo')
    return value


def parent_fd(path, *, private=False, create=False):
    path = Path(path).expanduser()
    if '..' in path.parts or path.name in ('', '.', '..'):
        raise ValueError('Unsafe file path')
    path = Path(os.path.abspath(path))
    fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        for segment in path.parts[1:-1]:
            if private:
                try:
                    os.stat('.git', dir_fd=fd, follow_symlinks=False)
                except FileNotFoundError:
                    pass
                else:
                    raise ValueError('Secrets cannot be stored in any Git checkout')
            if create:
                try:
                    os.mkdir(segment, 0o700, dir_fd=fd)
                except FileExistsError:
                    pass
                else:
                    # Persist the new child entry before traversing further.
                    os.fsync(fd)
            child = os.open(segment, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
            os.close(fd)
            fd = child
            info = os.fstat(fd)
            # A sticky shared root such as /tmp is traversable; final parent must be private.
            if private and info.st_mode & 0o022 and not info.st_mode & stat.S_ISVTX:
                raise ValueError('Unsafe writable ancestor')
        if private:
            try:
                os.stat('.git', dir_fd=fd, follow_symlinks=False)
            except FileNotFoundError:
                pass
            else:
                raise ValueError('Secrets cannot be stored in any Git checkout')
            info = os.fstat(fd)
            if info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
                raise ValueError('Credential parent must be owned by this user with mode 0700')
        return fd, path.name, path
    except BaseException:
        os.close(fd)
        raise


def read_json(path, *, private=False, max_bytes=65536):
    directory, name, _ = parent_fd(path, private=private)
    try:
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory)
        with os.fdopen(fd) as stream:
            info = os.fstat(stream.fileno())
            if (not stat.S_ISREG(info.st_mode) or info.st_size > max_bytes
                    or info.st_mode & 0o022 or info.st_nlink != 1
                    or private and (info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o600)):
                raise ValueError('Unsafe file permissions or type')
            raw = stream.read(max_bytes + 1)
            if len(raw) > max_bytes:
                raise ValueError('File too large')
            return json.loads(raw)
    finally:
        os.close(directory)


def write_new_private(directory, name, value):
    fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=directory)
    try:
        with os.fdopen(fd, 'w') as stream:
            os.fchmod(stream.fileno(), 0o600)
            json.dump(value, stream, ensure_ascii=False)
            stream.write('\n')
            stream.flush()
            os.fsync(stream.fileno())
        os.fsync(directory)
    except BaseException:
        remove_file(directory, name)
        raise


def remove_file(directory, name):
    """Persist cleanup too; a failed sync is a recovery-required error."""
    os.unlink(name, dir_fd=directory)
    os.fsync(directory)
