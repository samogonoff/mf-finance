"""Local process interleavings and injected POSIX failures; synthetic fixtures only."""
import hashlib
import json
import multiprocessing
import os
from pathlib import Path
import queue
import stat
import tempfile
import unittest
from unittest.mock import patch

import check
import issue
from private_files import read_json
from public_records import PublicRecords
import revoke


def fixture_hex(character):
    return lambda size: character * (size * 2)


def mutate_process(records, private, operation, name, character, attempted, acquired, release, result):
    """First writer pauses after its locked read; second must wait before reading."""
    original_enter = PublicRecords.__enter__

    def enter(public):
        value = original_enter(public)
        acquired.set()
        if release is not None and not release.wait(10):
            raise RuntimeError('test interleaving timeout')
        return value

    attempted.set()
    try:
        with patch.object(PublicRecords, '__enter__', enter), patch('issue.secrets.token_hex', fixture_hex(character)):
            if operation == 'issue':
                issue.issue(records, private, 'reader@example.test', name, 'https://api.example.test')
            else:
                revoke.revoke(records, name)
        result.put('ok')
    except Exception as error:
        # Never return a fixture secret, traceback or public digest through queue.
        result.put(type(error).__name__)


class LifecycleTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.records = self.root / 'public.json'
        self.records.write_text('[]\n')
        os.chmod(self.records, 0o644)
        self.private = self.root / 'private' / 'credential.json'

    def tearDown(self):
        self.tmp.cleanup()

    def issue_fixture(self, name='old', character='1', path=None):
        with patch('issue.secrets.token_hex', fixture_hex(character)):
            return issue.issue(self.records, path or self.private, 'reader@example.test', name, 'https://api.example.test')

    def names(self):
        return {record['name'] for record in read_json(self.records)}

    def interleave(self, first, second):
        ctx = multiprocessing.get_context('fork')
        attempted1, attempted2, acquired1, acquired2, release = [ctx.Event() for _ in range(5)]
        result1, result2 = ctx.Queue(), ctx.Queue()
        a = ctx.Process(target=mutate_process, args=(self.records, self.root / 'private' / 'a.json',
                        *first, attempted1, acquired1, release, result1))
        b = ctx.Process(target=mutate_process, args=(self.records, self.root / 'private' / 'b.json',
                        *second, attempted2, acquired2, None, result2))
        a.start()
        try:
            self.assertTrue(acquired1.wait(5), 'first writer did not acquire lock')
            b.start()
            self.assertTrue(attempted2.wait(5), 'second writer did not start')
            self.assertFalse(acquired2.wait(0.2), 'second writer read before lock release')
            with self.assertRaises(queue.Empty):
                result2.get(timeout=0.1)
            release.set()
            results = result1.get(timeout=5), result2.get(timeout=5)
            a.join(5)
            b.join(5)
            self.assertEqual((a.exitcode, b.exitcode), (0, 0))
            return results
        finally:
            release.set()
            for process in (a, b):
                if process.pid is not None:
                    if process.is_alive():
                        process.terminate()
                    process.join(5)
            result1.close()
            result2.close()

    def test_two_issuers_preserve_both_records_and_stable_lock(self):
        self.assertEqual(self.interleave(('issue', 'first', '2'), ('issue', 'second', '3')), ('ok', 'ok'))
        self.assertEqual(self.names(), {'first', 'second'})
        records = read_json(self.records)
        for name, filename in [('first', 'a.json'), ('second', 'b.json')]:
            token = read_json(self.root / 'private' / filename, private=True)['token']
            self.assertEqual(next(r['sha256'] for r in records if r['name'] == name), hashlib.sha256(token.encode()).hexdigest())
        lock = self.root / '.public.json.lock'
        lock_inode = lock.stat().st_ino
        revoke.revoke(self.records, 'first')
        revoke.revoke(self.records, 'second')
        self.assertEqual(read_json(self.records), [])
        self.issue_fixture(name='third', path=self.root / 'private' / 'third.json')
        self.assertEqual(lock.stat().st_ino, lock_inode)
        self.assertEqual(stat.S_IMODE(lock.stat().st_mode), 0o600)

    def test_concurrent_duplicate_name_is_rejected(self):
        self.assertEqual(self.interleave(('issue', 'same', '2'), ('issue', 'same', '3')), ('ok', 'ValueError'))
        self.assertEqual(self.names(), {'same'})
        self.assertFalse((self.root / 'private' / 'b.json').exists())

    def test_issuer_then_revocation_preserves_new_and_removes_old(self):
        self.issue_fixture()
        self.assertEqual(self.interleave(('issue', 'new', '2'), ('revoke', 'old', '3')), ('ok', 'ok'))
        self.assertEqual(self.names(), {'new'})

    def test_revocation_then_issuer_cannot_resurrect_old(self):
        self.issue_fixture()
        self.assertEqual(self.interleave(('revoke', 'old', '2'), ('issue', 'new', '3')), ('ok', 'ok'))
        self.assertEqual(self.names(), {'new'})
        self.assertEqual(revoke.revoke(self.records, 'old'), {'name': 'old', 'removed': False})

    def test_changed_destination_aborts_without_resurrecting_revocation(self):
        self.issue_fixture()
        original_write = issue.write_new_private
        new_secret = self.root / 'private' / 'new.json'

        def revoke_outside_lock(*args):
            original_write(*args)
            self.records.write_text('[]\n')

        with patch('issue.write_new_private', revoke_outside_lock):
            with self.assertRaisesRegex(ValueError, 'changed outside'):
                self.issue_fixture(name='new', character='2', path=new_secret)
        self.assertEqual(read_json(self.records), [])
        self.assertFalse(new_secret.exists())
        self.assertEqual(list(self.root.glob('.finance-readonly-public-*')), [])

    def test_replaced_removed_or_unsafe_destination_aborts(self):
        for mutation in ('same-bytes-replacement', 'removed', 'symlink', 'hardlink', 'unsafe-mode', 'lock-replacement'):
            with self.subTest(mutation=mutation):
                self.records.write_text('[]\n')
                os.chmod(self.records, 0o644)
                with PublicRecords(self.records) as public:
                    other = self.root / 'other.json'
                    if mutation in ('same-bytes-replacement', 'symlink', 'hardlink'):
                        other.write_text('[]\n')
                        os.chmod(other, 0o644)
                        self.records.unlink()
                        if mutation == 'symlink':
                            self.records.symlink_to(other)
                        elif mutation == 'hardlink':
                            os.link(other, self.records)
                        else:
                            os.replace(other, self.records)
                    elif mutation == 'removed':
                        self.records.unlink()
                    elif mutation == 'unsafe-mode':
                        os.chmod(self.records, 0o666)
                    else:
                        lock = self.root / '.public.json.lock'
                        lock.unlink()
                        lock.write_text('')
                        os.chmod(lock, 0o600)
                    with self.assertRaises((ValueError, OSError)):
                        public.install([])
                    self.assertFalse(public.install_attempted)
                self.records.unlink(missing_ok=True)
                other.unlink(missing_ok=True)
                self.assertEqual(list(self.root.glob('.finance-readonly-public-*')), [])

    def test_lock_symlink_hardlink_and_unsafe_modes_are_rejected(self):
        lock = self.root / '.public.json.lock'
        other = self.root / 'other'
        other.write_text('')
        os.chmod(other, 0o600)
        for mutation in ('symlink', 'hardlink', 'unsafe-mode'):
            with self.subTest(mutation=mutation):
                if mutation == 'symlink':
                    lock.symlink_to(other)
                elif mutation == 'hardlink':
                    os.link(other, lock)
                else:
                    lock.write_text('')
                    os.chmod(lock, 0o644)
                with self.assertRaises((ValueError, OSError)):
                    self.issue_fixture()
                self.assertFalse(self.private.exists())
                lock.unlink()

    def test_replaced_public_parent_path_is_detected(self):
        directory = self.root / 'public-parent'
        directory.mkdir(mode=0o700)
        records = directory / 'public.json'
        records.write_text('[]\n')
        os.chmod(records, 0o644)
        moved = self.root / 'moved-parent'
        with PublicRecords(records) as public:
            directory.rename(moved)
            directory.mkdir(mode=0o700)
            records.write_text('[]\n')
            os.chmod(records, 0o644)
            with self.assertRaisesRegex(ValueError, 'parent changed'):
                public.install([])
            self.assertFalse(public.install_attempted)
        self.assertEqual((moved / 'public.json').read_text(), '[]\n')
        self.assertEqual(records.read_text(), '[]\n')
        self.assertEqual(list(moved.glob('.finance-readonly-public-*')), [])

    def test_private_public_and_parent_sync_order_and_output_durability(self):
        events = []
        original_sync, original_replace = os.fsync, os.replace

        def sync(fd):
            events.append(('sync', os.readlink('/proc/self/fd/' + str(fd))))
            return original_sync(fd)

        def replace(*args, **kwargs):
            events.append(('replace', str(args[1])))
            return original_replace(*args, **kwargs)

        with patch('os.fsync', sync), patch('os.replace', replace):
            self.issue_fixture()
        installed = next(i for i, event in enumerate(events) if event[0] == 'replace')
        self.assertIn(('sync', str(self.private)), events[:installed])
        self.assertIn(('sync', str(self.private.parent)), events[:installed])
        self.assertTrue(any(event[0] == 'sync' and '.finance-readonly-public-' in event[1] for event in events[:installed]))
        self.assertEqual(events[installed + 1], ('sync', str(self.root)))
        events.clear()
        output = self.root / 'results' / 'nested' / 'result.json'
        with patch('os.fsync', sync):
            check.write_result(output, {'success': True})
        self.assertEqual(events[-2:], [('sync', str(output)), ('sync', str(output.parent))])
        self.assertIn(('sync', str(output.parent.parent)), events)
        self.assertIn(('sync', str(self.root)), events)

    def test_injected_failures_before_install_cleanup_and_sync_removal(self):
        for stage in ('private-file-sync', 'private-directory-sync', 'public-temp-sync', 'changed-check'):
            with self.subTest(stage=stage):
                original_sync, original_unlink = os.fsync, os.unlink
                removed = []
                sync_after_removal = []
                failed = False

                def sync(fd):
                    nonlocal failed
                    path = os.readlink('/proc/self/fd/' + str(fd))
                    if removed:
                        sync_after_removal.append(path)
                    match = ((stage == 'private-file-sync' and path == str(self.private))
                             or (stage == 'private-directory-sync' and path == str(self.private.parent))
                             or (stage == 'public-temp-sync' and '.finance-readonly-public-' in path))
                    if match and not failed:
                        failed = True
                        raise OSError('injected fsync failure')
                    return original_sync(fd)

                def unlink(name, **kwargs):
                    removed.append(str(name))
                    return original_unlink(name, **kwargs)

                with patch('os.fsync', sync), patch('os.unlink', unlink):
                    if stage == 'changed-check':
                        with patch.object(PublicRecords, 'check_unchanged', side_effect=ValueError('injected check failure')):
                            with self.assertRaises(ValueError):
                                self.issue_fixture()
                    else:
                        with self.assertRaises(OSError):
                            self.issue_fixture()
                self.assertFalse(self.private.exists())
                self.assertEqual(read_json(self.records), [])
                self.assertIn(self.private.name, removed)
                self.assertIn(str(self.private.parent), sync_after_removal)
                self.assertEqual(list(self.root.glob('.finance-readonly-public-*')), [])

    def test_post_replace_sync_failure_retains_private_and_installed_public(self):
        original_replace, original_sync = os.replace, os.fsync
        installed = False

        def replace(*args, **kwargs):
            nonlocal installed
            result = original_replace(*args, **kwargs)
            installed = True
            return result

        def sync(fd):
            if installed and os.readlink('/proc/self/fd/' + str(fd)) == str(self.root):
                raise OSError('injected post-replace sync failure')
            return original_sync(fd)

        with patch('os.replace', replace), patch('os.fsync', sync):
            with self.assertRaises(OSError):
                self.issue_fixture()
        self.assertEqual(self.names(), {'old'})
        self.assertTrue(self.private.exists())
        self.assertEqual(list(self.root.glob('.finance-readonly-public-*')), [])

    def test_interrupted_replace_after_effect_retains_pair_without_rollback(self):
        original_replace = os.replace

        def replace(*args, **kwargs):
            original_replace(*args, **kwargs)
            raise OSError('injected interrupted return')

        with patch('os.replace', replace):
            with self.assertRaises(OSError):
                self.issue_fixture()
        self.assertEqual(self.names(), {'old'})
        self.assertTrue(self.private.exists())
        self.assertEqual(list(self.root.glob('.finance-readonly-public-*')), [])

    def test_revocation_failure_does_not_restore_removed_digest(self):
        self.issue_fixture()
        original_replace = os.replace

        def replace(*args, **kwargs):
            original_replace(*args, **kwargs)
            raise OSError('injected interrupted return')

        with patch('os.replace', replace):
            with self.assertRaises(OSError):
                revoke.revoke(self.records, 'old')
        self.assertEqual(read_json(self.records), [])
        self.assertTrue(self.private.exists()) # deployment/disposal is a separate operator action

    def test_failed_private_cleanup_reports_error_and_leaves_inspectable_orphan(self):
        original_unlink = os.unlink

        def unlink(name, **kwargs):
            if name == self.private.name:
                raise OSError('injected cleanup failure')
            return original_unlink(name, **kwargs)

        with patch.object(PublicRecords, 'check_unchanged', side_effect=ValueError('injected check failure')):
            with patch('os.unlink', unlink):
                with self.assertRaisesRegex(OSError, 'cleanup failure'):
                    self.issue_fixture()
        self.assertEqual(read_json(self.records), [])
        self.assertTrue(self.private.exists())
        self.assertEqual(list(self.root.glob('.finance-readonly-public-*')), [])

    def test_result_directory_sync_failure_cleans_and_syncs_removal(self):
        output = self.root / 'results' / 'result.json'
        original_sync = os.fsync
        failed = False
        events = []

        def sync(fd):
            nonlocal failed
            path = os.readlink('/proc/self/fd/' + str(fd))
            events.append(path)
            if path == str(output.parent) and not failed:
                failed = True
                raise OSError('injected result-parent sync failure')
            return original_sync(fd)

        with patch('os.fsync', sync):
            with self.assertRaises(OSError):
                check.write_result(output, {'success': True})
        self.assertFalse(output.exists())
        self.assertEqual(events[-2:], [str(output.parent), str(output.parent)])

    def test_private_ancestor_creation_sync_failure_prevents_public_install(self):
        original_sync = os.fsync

        def sync(fd):
            # The lock's root sync is before the private parent exists. Fail
            # exactly when mkdir has created that entry and syncs its parent.
            if os.readlink('/proc/self/fd/' + str(fd)) == str(self.root) and self.private.parent.exists():
                raise OSError('injected mkdir-parent sync failure')
            return original_sync(fd)

        with patch('os.fsync', sync):
            with self.assertRaises(OSError):
                self.issue_fixture()
        self.assertFalse(self.private.exists())
        self.assertEqual(read_json(self.records), [])
