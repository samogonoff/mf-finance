#!/usr/bin/env python3
"""Remove a named public digest under the same lock as issuance; deploy separately."""
import argparse
import json

from issue import PUBLIC_RECORDS
from public_records import PublicRecords, NAME_RE


def revoke(record_path, name):
    if not isinstance(name, str) or not NAME_RE.fullmatch(name):
        raise ValueError('Invalid public name')
    with PublicRecords(record_path) as public:
        records = [record for record in public.records if record['name'] != name]
        if len(records) == len(public.records):
            return {'name': name, 'removed': False}
        public.install(records)
        return {'name': name, 'removed': True}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--name', required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(revoke(PUBLIC_RECORDS, args.name)))
    except (ValueError, OSError, TypeError):
        parser.exit(1, 'Removal failed; inspect public state per README recovery before retrying.\n')
