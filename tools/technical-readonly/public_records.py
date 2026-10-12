"""Cooperating POSIX writers share a persistent lock, never the replaced JSON inode."""
import fcntl
import json
import os
import re
import secrets
import stat
from datetime import datetime

from private_files import parent_fd, remove_file

NAME_RE = re.compile(r'[A-Za-z0-9_-]{1,80}\Z')


def validate_records(records):
    if not isinstance(records, list) or len(records) > 64:
        raise ValueError('Invalid public records')
    names, digests = set(), set()
    for record in records:
        if (not isinstance(record, dict)
                or set(record) != {'sha256', 'name', 'subject_email', 'expires_at'}
                or not isinstance(record['sha256'], str)
                or not re.fullmatch(r'[a-f0-9]{64}', record['sha256'])
                or not isinstance(record['name'], str) or not NAME_RE.fullmatch(record['name'])
                or record['name'] in names or record['sha256'] in digests
                or not isinstance(record['subject_email'], str)
                or len(record['subject_email']) > 254
                or not re.fullmatch(r'[^\s@\x00]+@[^\s@\x00]+', record['subject_email'])
                or not isinstance(record['expires_at'], str)):
            raise ValueError('Invalid public record')
        expiry = datetime.fromisoformat(record['expires_at'].replace('Z', '+00:00'))
        if expiry.tzinfo is None:
            raise ValueError('Invalid public expiry')
        names.add(record['name'])
        digests.add(record['sha256'])
    return records


def identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


class PublicRecords:
    """Read and replace under lock; abort if an out-of-workflow edit is observed.

    All mutations, including removal, must use this workflow. flock is advisory:
    no portable rename CAS can protect against an editor ignoring the lock in
    the interval between the final comparison and rename.
    """
    def __init__(self, path):
        self.path = path
        self.directory = self.lock = None
        self.install_attempted = False

    def __enter__(self):
        try:
            self.directory, self.name, self.absolute = parent_fd(self.path)
            self.directory_identity = identity(os.fstat(self.directory))[:2]
            self.lock_name = '.' + self.name + '.lock'
            self.lock = os.open(self.lock_name, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK,
                                0o600, dir_fd=self.directory)
            self.check_lock()
            fcntl.flock(self.lock, fcntl.LOCK_EX)
            self.check_lock()
            # Persistent sidecar: never unlink it, including on failure/revocation.
            os.fsync(self.lock)
            os.fsync(self.directory)
            self.snapshot = self.read_snapshot()
            self.records = validate_records(json.loads(self.snapshot[1]))
            return self
        except BaseException:
            self.__exit__(None, None, None)
            raise

    def check_lock(self):
        info = os.fstat(self.lock)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid()
                or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1
                or identity(info)[:2] != identity(os.stat(self.lock_name, dir_fd=self.directory,
                                                         follow_symlinks=False))[:2]):
            raise ValueError('Unsafe or replaced public lock')

    def read_snapshot(self):
        fd = os.open(self.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=self.directory)
        with os.fdopen(fd, 'rb') as stream:
            before = os.fstat(stream.fileno())
            if (not stat.S_ISREG(before.st_mode) or before.st_mode & 0o022
                    or before.st_nlink != 1 or before.st_size > 65536):
                raise ValueError('Unsafe public configuration')
            raw = stream.read(65537)
            after = os.fstat(stream.fileno())
            destination = os.stat(self.name, dir_fd=self.directory, follow_symlinks=False)
            if len(raw) > 65536 or identity(before) != identity(after) or identity(after) != identity(destination):
                raise ValueError('Public configuration changed during read')
            return identity(after), raw

    def check_unchanged(self):
        self.check_lock()
        directory, _, _ = parent_fd(self.absolute)
        try:
            if identity(os.fstat(directory))[:2] != self.directory_identity:
                raise ValueError('Public parent changed')
        finally:
            os.close(directory)
        if self.read_snapshot() != self.snapshot:
            raise ValueError('Public configuration changed outside locked workflow')

    def install(self, records):
        validate_records(records)
        temporary = '.finance-readonly-public-' + secrets.token_hex(16)
        created = False
        try:
            fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                         0o644, dir_fd=self.directory)
            created = True
            with os.fdopen(fd, 'w') as stream:
                os.fchmod(stream.fileno(), 0o644)
                json.dump(records, stream, ensure_ascii=False, indent=2)
                stream.write('\n')
                stream.flush()
                os.fsync(stream.fileno())
            self.check_unchanged()
            # Once attempted, errors can have an ambiguous installation outcome.
            # Issuance retains the durable private file; never roll back public data.
            self.install_attempted = True
            os.replace(temporary, self.name, src_dir_fd=self.directory, dst_dir_fd=self.directory)
            created = False
            os.fsync(self.directory)
        finally:
            if created:
                try:
                    remove_file(self.directory, temporary)
                except FileNotFoundError:
                    # replace may have succeeded before an injected/interrupted error.
                    os.fsync(self.directory)

    def __exit__(self, *args):
        if self.lock is not None:
            os.close(self.lock)
            self.lock = None
        if self.directory is not None:
            os.close(self.directory)
            self.directory = None
