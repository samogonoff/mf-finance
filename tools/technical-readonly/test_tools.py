import hashlib
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import HTTPError

import issue
import check
from private_files import origin, read_json

FIXTURE = 'finance_ro_' + '1' * 64  # deterministic local test fixture; never a live credential


class Response:
    status = 200
    def __init__(self, raw=b'{"saved_only":true}', content_type=None):
        self.raw = raw
        self.content_type = content_type
        self.headers = self
    def get_content_type(self):
        return self.content_type
    def __enter__(self):
        return self
    def __exit__(self, *args):
        pass
    def read(self, size):
        return self.raw[:size]


class FakeOpener:
    def __init__(self, response=None):
        self.request = None
        self.response = response or Response()
    def open(self, request, timeout):
        self.request = request
        if isinstance(self.response, Exception):
            raise self.response
        return self.response


class ToolsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.records = self.root / 'public.json'
        self.records.write_text('[]\n')
        os.chmod(self.records, 0o644)
        self.private = self.root / 'private' / 'credential.json'
    def tearDown(self):
        self.tmp.cleanup()
    def issue(self, **overrides):
        fixture_character = overrides.pop('fixture_character', '1')
        args = dict(record_path=self.records, secret_path=self.private,
                    subject_email='reader@example.test', name='test', api_origin='https://api.example.test')
        args.update(overrides)
        with patch('issue.secrets.token_hex', return_value=fixture_character * 64):
            return issue.issue(**args)
    def test_digest_only_and_private_expiry(self):
        result = self.issue()
        public = json.loads(self.records.read_text())
        private = read_json(self.private, private=True)
        self.assertEqual(public[0]['sha256'], hashlib.sha256(FIXTURE.encode()).hexdigest())
        self.assertEqual(set(public[0]), {'sha256', 'name', 'subject_email', 'expires_at'})
        self.assertNotIn(FIXTURE, json.dumps(result))
        self.assertNotIn(FIXTURE, self.records.read_text())
        self.assertEqual(stat.S_IMODE(self.private.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(self.private.parent.stat().st_mode), 0o700)
        self.assertEqual(check.credentials(self.private), ('https://api.example.test', FIXTURE))
        from datetime import datetime, timezone
        days = (datetime.fromisoformat(private['expires_at'].replace('Z', '+00:00')) - datetime.now(timezone.utc)).days
        self.assertEqual(days, 29)
    def test_no_overwrite_no_git_no_symlinks(self):
        self.issue()
        original = self.private.read_text()
        with self.assertRaises((ValueError, OSError)):
            self.issue(name='second')
        self.assertEqual(self.private.read_text(), original)
        repo = self.root / 'repo'
        repo.mkdir(mode=0o700)
        (repo / '.git').write_text('gitdir: irrelevant')
        with self.assertRaises(ValueError):
            self.issue(secret_path=repo / 'sub' / 'secret.json', name='git')
        link = self.root / 'link'
        link.symlink_to(self.private.parent, target_is_directory=True)
        with self.assertRaises(OSError):
            self.issue(secret_path=link / 'new.json', name='symlink')
        self.records.unlink()
        self.records.symlink_to(self.private)
        with self.assertRaises(OSError):
            self.issue(name='public-symlink')
    def test_metadata_and_permissions_fail_before_issuance(self):
        for days in (0, 91, True):
            with self.assertRaises(ValueError):
                self.issue(days=days)
        with self.assertRaises(ValueError):
            self.issue(subject_email='bad\n@example.test')
        self.private.parent.mkdir(mode=0o755)
        with self.assertRaises(ValueError):
            self.issue()
        self.assertFalse(self.private.exists())
        os.chmod(self.private.parent, 0o700)
        os.chmod(self.records, 0o666)
        with self.assertRaises(ValueError):
            self.issue()
        self.assertEqual(self.records.read_text(), '[]\n')
    def test_expired_unsafe_and_symlink_credentials(self):
        self.issue()
        os.chmod(self.private, 0o644)
        with self.assertRaises(ValueError):
            check.credentials(self.private)
        os.chmod(self.private, 0o600)
        value = json.loads(self.private.read_text())
        value['expires_at'] = '2020-01-01T00:00:00Z'
        self.private.write_text(json.dumps(value))
        with self.assertRaises(ValueError):
            check.credentials(self.private)
        link = self.private.parent / 'linked.json'
        link.symlink_to(self.private)
        with self.assertRaises(OSError):
            check.credentials(link)
    def test_origin_pin_and_safe_paths(self):
        self.issue()
        opener = FakeOpener()
        result = check.read('/api/reports/debt/report?date_from=2026-01-01&date_to=2026-01-31', self.private, opener)
        self.assertTrue(result['success'])
        self.assertEqual(opener.request.full_url, 'https://api.example.test/api/reports/debt/report?date_from=2026-01-01&date_to=2026-01-31')
        self.assertEqual(opener.request.get_method(), 'GET')
        for path in (
                '/api/plans/cfo-exp/123/form?code_cfo=31&limit=100',
                '/api/plans/cfo-exp/123/validate',
                '/api/plans/cfo-exp/123/export?dir=desc&sort=code_pl',
                '/api/plans/cfo-exp/123/registry/travelBY'):
            self.assertEqual(check.canonical_path(path), path)
        for bad in ('http://api.example.test', 'https://api.example.test/', 'https://api.example.test@evil.test',
                    'https://api.example.test#x', 'https://api.example.test?x', 'https://api.example.test:99999', '', None):
            with self.assertRaises((ValueError, TypeError)):
                origin(bad)
        for path in ('https://evil.test/api/nav', '//evil.test/api/nav', '/api//nav', '/api/x/../nav',
                     '/api/%6eav', '/api/nav#x', '/api/nav?force=1', '/api/auth/me?force=1', '/v1/test',
                     '/api/chz/warehouse', '/api/reports/debt/report?date_from=1&date_from=2',
                     '/api/reports/debt/report?date_from=%0A', '/api/reports/debt/report?',
                     '/api/reports/debt/report?date_from=%ZZ'):
            with self.assertRaises(ValueError, msg=path):
                check.read(path, self.private, FakeOpener())
    def test_redirect_failure_size_and_echo_redaction(self):
        self.issue()
        error = HTTPError('https://api.example.test/api/reports/debt/report', 302, 'redirect', {}, None)
        self.assertEqual(check.read('/api/reports/debt/report', self.private, FakeOpener(error)), {'status': 302, 'success': False})
        self.assertIsNone(check.NoRedirect().redirect_request(None, None, 302, 'x', {}, 'https://evil.test'))
        with self.assertRaises(ValueError):
            check.read('/api/reports/debt/filter-options', self.private, FakeOpener(Response(b'x' * (check.MAX_BYTES + 1))))
        result = check.read('/api/reports/debt/filter-options', self.private, FakeOpener(Response(json.dumps({'echo': FIXTURE}).encode())))
        self.assertNotIn(FIXTURE, json.dumps(result))
        escaped = json.dumps({'nested': [FIXTURE], FIXTURE: FIXTURE}).replace('1', '\\u0031').encode()
        result = check.read('/api/reports/debt/filter-options', self.private, FakeOpener(Response(escaped)))
        self.assertNotIn(FIXTURE, json.dumps(result))
        binary = check.read('/api/plans/cfo-exp/123/export', self.private,
                            FakeOpener(Response(bytes([0x50, 0x4b, 0x03, 0x04]) + b'binary',
                                                   'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet')))
        self.assertTrue(binary['success'])
        self.assertEqual(binary['content_type'], 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet')
        self.assertEqual(binary['bytes'], 10)
        self.assertNotIn('PK', json.dumps(binary))
        output = self.private.parent / 'result.json'
        check.write_result(output, result)
        self.assertEqual(stat.S_IMODE(output.stat().st_mode), 0o600)
        with self.assertRaises(FileExistsError):
            check.write_result(output, result)
        unsafe = self.root / 'unsafe'
        unsafe.mkdir(mode=0o755)
        with self.assertRaises(ValueError):
            check.write_result(unsafe / 'output.json', result)

    def test_max_expiry_and_ambiguous_install_retains_private(self):
        result = self.issue(days=90)
        from datetime import datetime, timezone
        expires = datetime.fromisoformat(result['expires_at'].replace('Z', '+00:00'))
        self.assertEqual((expires - datetime.now(timezone.utc)).days, 89)
        other = self.private.parent / 'other.json'
        before = self.records.read_text()
        with patch('issue.os.replace', side_effect=OSError('simulated installation failure')):
            with self.assertRaises(OSError):
                self.issue(secret_path=other, name='rollback', fixture_character='2')
        # Once replace is attempted, retain the durable secret for recovery even
        # when this injected failure happened before rename. Never claim success.
        self.assertTrue(other.exists())
        self.assertEqual(before, self.records.read_text())

    def test_unknown_routes_rejected_before_credentials_or_network(self):
        opener = FakeOpener()
        with self.assertRaises(ValueError):
            check.read('/api/reports/debt/unknown', self.root / 'missing-credential.json', opener)
        self.assertIsNone(opener.request)
        self.assertEqual(check.canonical_path('/api/reports/debt/filter-options'), '/api/reports/debt/filter-options')
        self.assertEqual(check.canonical_path('/api/reports/debt/report?date_to=2026-01-31&date_from=2026-01-01'),
                         '/api/reports/debt/report?date_from=2026-01-01&date_to=2026-01-31')

    def test_hardlink_and_writable_ancestor_refusal(self):
        self.issue()
        hardlink = self.private.parent / 'hardlinked.json'
        os.link(self.private, hardlink)
        with self.assertRaises(ValueError):
            check.credentials(self.private)
        hardlink.unlink()
        unsafe = self.root / 'shared'
        unsafe.mkdir(mode=0o777)
        os.chmod(unsafe, 0o777)
        with self.assertRaises(ValueError):
            self.issue(secret_path=unsafe / 'private' / 'credential.json', name='unsafe-parent')
