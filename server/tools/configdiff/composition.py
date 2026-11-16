"""Validated generated composition IDs and their derived stable cache paths."""
import hashlib
import json
import re

from compare import Difference
from harness import DesignError


def cache_name(library):
    ident = library['id']
    namespace = 'by-id'
    if library.get('source_type', 'url') == 'url' and library.get('url'):
        namespace = 'by-source'
        ident = json.dumps([ident, library['url'], library.get('behavior', 'domain'), library.get('format') or ''], ensure_ascii=False)
    return 'shared/rules/' + namespace + '/' + hashlib.sha256(ident.encode()).hexdigest() + '.list'


def identities(value):
    data = value.get('data') if isinstance(value, dict) else None
    if not isinstance(data, dict) or not isinstance(data.get('rules'), list):
        raise ValueError('composition data.rules must be an array')
    result = []
    library = data.get('library')
    if library is not None:
        if not isinstance(library, dict):
            raise ValueError('composition library must be an object or null')
        result.append(('lib', library.get('id')))
    for row in data['rules']:
        if not isinstance(row, dict):
            raise ValueError('composition rule must be an object')
        result.append(('ruleset' if row.get('itemType') == 'ruleset' else 'rule', row.get('id')))
    ids = [ident for _, ident in result]
    if any(not isinstance(ident, str) or not re.fullmatch(prefix + r'_[0-9a-f]{12}', ident) for prefix, ident in result):
        raise ValueError('composition ID must have its resource prefix and 12 lowercase hex digits')
    if len(ids) != len(set(ids)):
        raise ValueError('composition IDs must be unique')
    return result


def capture(pair):
    def bind(replies, policy):
        try:
            left = identities(replies[0].json())
        except ValueError as error:
            raise DesignError('Python composition identity premise: ' + str(error)) from error
        try:
            right = identities(replies[1].json())
        except ValueError as error:
            return [Difference('composition.identities', 'invalid-Go-identity', left, str(error))]
        if len(left) != len(right):
            return []  # Complete HTTP and SQL comparisons report missing/extra rows.
        out = []
        for (prefix, a), (other, b) in zip(left, right):
            if prefix != other:
                continue  # Type and unmapped ID stay visible in ordinary comparison.
            policy.bind(a, b, prefix + r'_[0-9a-f]{12}', 'composed-' + prefix)
            if prefix == 'lib':
                names = [cache_name(reply.json()['data']['library']) for reply in replies]
                for suffix in ('', '.bak'):
                    paths = [name + suffix for name in names]
                    if all((directory / path).is_file() for directory, path in zip(pair.dirs, paths)):
                        policy.bind(*paths, r'shared/rules/by-(?:id|source)/[0-9a-f]{64}\.list(?:\.bak)?', 'composed-library-cache')
        return out
    return bind
