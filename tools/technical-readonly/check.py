#!/usr/bin/env python3
"""GET saved Finance data using an exact origin pinned at issuance; never print bearer."""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
from urllib.error import HTTPError
from urllib.parse import parse_qs, urlencode, urlsplit
from urllib.request import Request, build_opener, HTTPRedirectHandler, ProxyHandler

from private_files import TOKEN_RE, origin, parent_fd, read_json, write_new_private

MAX_BYTES = 16 * 1024 * 1024
ROUTES = Path(__file__).resolve().parents[2] / 'go/internal/auth/technical_readonly_routes.json'


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def credentials(path):
    value = read_json(path, private=True, max_bytes=8192)
    if not isinstance(value, dict) or not isinstance(value.get('token'), str) or not TOKEN_RE.fullmatch(value['token']):
        raise ValueError('Invalid credential')
    if not isinstance(value.get('expires_at'), str):
        raise ValueError('Invalid credential expiry')
    expires = datetime.fromisoformat(value['expires_at'].replace('Z', '+00:00'))
    if expires.tzinfo is None or datetime.now(timezone.utc) >= expires:
        raise ValueError('Expired credential')
    return origin(value.get('api_origin')), value['token']


def canonical_path(path):
    if not isinstance(path, str) or re.search(r'[\x00-\x20\x7f\\#]', path):
        raise ValueError('Unsafe API path')
    parsed = urlsplit(path)
    if (parsed.scheme or parsed.netloc or not path.startswith('/api/') or '%' in parsed.path
            or '//' in parsed.path or any(p in ('.', '..') for p in parsed.path.split('/'))
            or path.endswith('?')):
        raise ValueError('Use a reviewed relative API read path')
    if re.search(r'%(?![0-9A-Fa-f]{2})', parsed.query) or ';' in parsed.query:
        raise ValueError('Invalid query encoding')
    query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
    if len(parsed.query) > 8192 or any(len(v) != 1 for v in query.values()):
        raise ValueError('Ambiguous query')
    for values in query.values():
        if re.search(r'[\x00-\x1f\x7f]', values[0]) or 'finance_ro_' in values[0].lower():
            raise ValueError('Unsafe query')
    for route in read_json(ROUTES):
        if (re.fullmatch(route['path_regex'], parsed.path)
                and parsed.path not in route.get('excluded_paths', [])
                and set(query) <= set(route['query_keys'])):
            # Normalize filters to Go url.Values.Encode order, so the server sees one form.
            encoded = urlencode([(k, query[k][0]) for k in sorted(query)], safe='')
            return parsed.path + ('?' + encoded if encoded else '')
    raise ValueError('Read path is not approved')


def read(path, credential_path, opener=None):
    path = canonical_path(path)
    api_origin, token = credentials(credential_path)
    request = Request(api_origin + path, headers={'Authorization': 'Bearer ' + token, 'Accept': 'application/json'}, method='GET')
    opener = opener or build_opener(ProxyHandler({}), NoRedirect())
    try:
        response = opener.open(request, timeout=45)
    except HTTPError as error:
        error.close()
        return {'status': error.code, 'success': False}
    with response:
        raw = response.read(MAX_BYTES + 1)
        if len(raw) > MAX_BYTES:
            raise ValueError('Response exceeds local limit')
        # Even a compromised/echoing server must not copy the credential to local output.
        raw = raw.replace(token.encode(), b'[redacted]')
        def redact(value):
            if isinstance(value, str):
                return value.replace(token, '[redacted]')
            if isinstance(value, list):
                return [redact(item) for item in value]
            if isinstance(value, dict):
                return {redact(key): redact(item) for key, item in value.items()}
            return value
        return {'status': response.status, 'success': 200 <= response.status < 300,
                'response': redact(json.loads(raw))}


def write_result(path, result):
    directory, name, _ = parent_fd(path, private=True, create=True)
    try:
        write_new_private(directory, name, result)
    finally:
        os.close(directory)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--credential-file', type=Path, required=True)
    parser.add_argument('--path', default='/api/nav')
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    try:
        result = read(args.path, args.credential_file)
        if args.output:
            write_result(args.output, result)
        print(json.dumps({'status': result['status'], 'success': result['success']}))
        raise SystemExit(0 if result['success'] else 1)
    except (OSError, ValueError, TypeError, KeyError, RecursionError):
        parser.exit(1, 'Read-only verification failed; credentials were not printed.\n')
