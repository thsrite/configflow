"""A1–A7 expanded on every matrix route that explicitly declares them.

These are authentication-before-business cases: object IDs deliberately name a
synthetic missing object because a protected route must reject before lookup.
They do not claim valid-business-input or missing-object handler coverage.
"""
import re
import jwt
from urllib.parse import quote

from harness import Pair, USERNAME, SECRET
from inventory import admin_auth_rows
from seed import PULL


def concrete(template, profile):
    values = {'profile_id': profile, 'service': 'mihomo', 'target': 'mihomo',
              'operation': 'check', 'action': 'file', 'key': 'synthetic'}
    return re.sub(r'<(?:[^:>]+:)?([^>]+)>',
                  lambda m: quote(values.get(m[1], 'synthetic-missing'), safe=''), template)


def all_admin_auth(binary, recorder):
    with Pair(binary, recorder, options={'config_token': PULL}) as pair:
        forged = jwt.encode({'username': USERNAME}, 'synthetic-wrong-key', algorithm='HS256')
        expired = jwt.encode({'username': USERNAME, 'exp': 1}, SECRET, algorithm='HS256')
        empty = jwt.encode({}, SECRET, algorithm='HS256')
        credentials = ('', forged, expired, 'Bearer  malformed', 'x' * 8193, empty, '')
        for rid, route, first in admin_auth_rows():
            path = concrete(route.split(' ', 1)[1], pair.default_profile)
            for n, credential in enumerate(credentials):
                actual = path + ('?token=' + PULL if n == 6 else '')
                pair.request(f'{rid}-F{first+n:02}', route, actual, auth=credential,
                             variant='A' + str(n + 1) + '/config-token-set', expect=401)


GROUPS = {'admin-auth-all': all_admin_auth}
