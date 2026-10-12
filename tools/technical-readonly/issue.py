#!/usr/bin/env python3
"""Issue locally after review; public SHA-256 only, private bearer outside Git."""
import argparse
from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import secrets

from private_files import origin, parent_fd, remove_file, write_new_private
from public_records import PublicRecords, NAME_RE

PUBLIC_RECORDS = Path(__file__).resolve().parents[2] / 'go/internal/auth/technical_readonly_tokens.json'


def issue(record_path, secret_path, subject_email, name, api_origin, days=30):
    api_origin = origin(api_origin)
    if (type(days) is not int or not 1 <= days <= 90
            or not NAME_RE.fullmatch(name)
            or not re.fullmatch(r'[^\s@\x00]+@[^\s@\x00]+', subject_email)
            or len(subject_email) > 254):
        raise ValueError('Invalid token metadata')
    with PublicRecords(record_path) as public:
        if len(public.records) >= 64 or any(record['name'] == name for record in public.records):
            raise ValueError('Public record limit or duplicate name')
        secret_dir = None
        secret_created = False
        try:
            secret_dir, secret_name, private_path = parent_fd(secret_path, private=True, create=True)
            if private_path == public.absolute:
                raise ValueError('Public and private destinations must differ')
            token = 'finance_ro_' + secrets.token_hex(32)
            expires = (datetime.now(timezone.utc) + timedelta(days=days)).strftime('%Y-%m-%dT%H:%M:%SZ')
            # Private file and its name must be durable BEFORE installing a digest.
            write_new_private(secret_dir, secret_name, {'token': token, 'name': name,
                              'expires_at': expires, 'api_origin': api_origin})
            secret_created = True
            records = public.records + [{'sha256': hashlib.sha256(token.encode()).hexdigest(), 'name': name,
                                        'subject_email': subject_email, 'expires_at': expires}]
            public.install(records)
            return {'credential_file': str(private_path), 'expires_at': expires, 'name': name}
        except BaseException:
            if secret_created and not public.install_attempted:
                remove_file(secret_dir, secret_name)
            raise
        finally:
            if secret_dir is not None:
                os.close(secret_dir)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--subject-email', required=True)
    parser.add_argument('--name', required=True)
    parser.add_argument('--origin', required=True, help='Operator-confirmed exact HTTPS Go API origin; no default')
    parser.add_argument('--secret-file', type=Path, required=True)
    parser.add_argument('--days', type=int, default=30)
    args = parser.parse_args()
    try:
        print(json.dumps(issue(PUBLIC_RECORDS, args.secret_file, args.subject_email, args.name, args.origin, args.days)))
    except (ValueError, OSError, TypeError):
        parser.exit(1, 'Issuance failed; inspect public/private state per README recovery before retrying. No credential was printed.\n')
