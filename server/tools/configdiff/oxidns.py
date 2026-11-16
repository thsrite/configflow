"""Public OxiDNS ETag/publish-guard invariants against observable SQLite rows."""
import hashlib
import json

from compare import Difference, database
from harness import DesignError


def etag(value):
    return '"' + hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False,
                                         separators=(',', ':')).encode()).hexdigest() + '"'


def check(pair, profile='default'):
    def reference(reply):
        rows = database(pair.dirs[0])['fields']
        stored = rows['profile/' + profile + '/oxidns']['value']
        if reply.headers.get('etag') != etag(stored):
            raise DesignError('Python OxiDNS ETag is not the exact stored-object SHA256')
        value = reply.json()
        if 'publish_guard' in value:
            intended = {'expected_profile_id': profile, 'expected_service_type': 'oxidns',
                        'expected_config_revision': rows['profile/' + profile + '/_revision']['value'] + rows['shared//_revision']['value']}
            if value['publish_guard'] != intended:
                raise DesignError('Python publish guard does not name committed rows')
    return reference


def capture(pair, profile='default'):
    def observe(replies, policy):
        out = []
        for index, (reply, directory) in enumerate(zip(replies, pair.dirs)):
            if reply.status != 200:
                continue  # Ordinary response comparison reports status/body differences.
            rows = database(directory)['fields']
            stored = rows['profile/' + profile + '/oxidns']['value']
            actual = reply.headers.get('etag')
            if actual != etag(stored):
                if index == 0:
                    raise DesignError('Python ETag contract failed after save')
                out.append(Difference('headers.etag', 'not-Python-stored-object-SHA', etag(stored), actual))
            value = reply.json()
            if 'publish_guard' in value:
                expected = rows['profile/' + profile + '/_revision']['value'] + rows['shared//_revision']['value']
                guard = value['publish_guard']
                intended = {'expected_profile_id': profile, 'expected_service_type': 'oxidns', 'expected_config_revision': expected}
                if guard != intended:
                    if index == 0:
                        raise DesignError('Python publish_guard does not describe committed snapshot')
                    out.append(Difference('body.publish_guard', 'not-committed-revision', intended, guard))
        return out
    return observe
