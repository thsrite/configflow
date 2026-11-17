"""Concrete HTTP recipes derived only from the reviewed matrix and Python.

Each design ID is attached to explicit variants. Unattached entries remain
pending in the ledger; parsing the matrix never marks a case tested.
"""
from dataclasses import dataclass, field
import time
from urllib.parse import urlencode

import jwt

from inventory import load
from seed import AGENT_TOKEN, JWT_SECRET


@dataclass(frozen=True)
class Case:
    ident: str
    route: str
    method: str
    path: str
    expected: int
    entries: tuple = ()
    document: object = None
    raw: bytes | None = None
    headers: dict = field(default_factory=dict)
    auth: bool = True
    unchanged: bool = True
    reason: str = ''
    message: str | None = None


def concrete(path, router_id='r-ros', agent_id='missing-agent'):
    return path.replace('<router_id>', router_id).replace('<agent_id>', agent_id)


def authentication():
    routes, _ = load()
    now = int(time.time())
    credentials = [None,
        jwt.encode({'username': 'synthetic', 'iat': now, 'exp': now + 3600}, 'wrong-synthetic-secret-0123456789'),
        jwt.encode({'username': 'synthetic', 'iat': now - 120, 'exp': now - 60}, JWT_SECRET),
        jwt.encode({}, JWT_SECRET), ' ' + jwt.encode({'username': 'synthetic', 'exp': now + 3600}, JWT_SECRET),
        'x' * 8193, AGENT_TOKEN]
    for route in routes.values():
        for index, credential in enumerate(credentials, 1):
            ident = route.ident + '-F' + format(index, '02')
            headers = {} if credential is None else {'Authorization': 'Bearer ' + credential}
            yield Case(ident + '/A' + str(index), route.ident, route.method, concrete(route.path),
                       401, (ident,), headers=headers, auth=False,
                       reason='A1–A7 must reject before any handler/device mutation')


def profile_selection_failures():
    routes, _ = load()
    for route in routes.values():
        path = concrete(route.path)
        for suffix, profile, status in (('F08', 'missing-profile', 404), ('F09', 'bad!profile', 400)):
            if route.ident == 'R001':
                # Global agents route checks explicit profile after authentication.
                yield Case(route.ident + '-' + suffix + '/explicit', route.ident, route.method,
                           path + '?' + urlencode({'profile': profile}), status,
                           (route.ident + '-' + suffix,), reason='R001 explicit scope only')
                continue
            # All three selectors must reach profile validation before auth.
            for selector in ('profile', 'profile_id', 'header'):
                headers = {'X-ConfigFlow-Profile': profile} if selector == 'header' else {}
                query = '' if selector == 'header' else '?' + urlencode({selector: profile})
                yield Case(route.ident + '-' + suffix + '/' + selector, route.ident, route.method,
                           path + query, status, (route.ident + '-' + suffix,), headers=headers,
                           reason='X02 profile validation precedes authentication')
                yield Case(route.ident + '-F90/' + suffix + '/' + selector, route.ident, route.method,
                           path + query, status, (route.ident + '-F90',), headers=headers, auth=False,
                           reason='O6 no bearer still yields profile error')
    yield Case('R001-F90/no-bearer-missing-profile', 'R001', 'GET',
               '/api/agents/missing-agent/upstream-routers?profile=missing-profile', 401,
               ('R001-F90',), auth=False, reason='global blueprint authenticates before explicit scope')


def body_guards():
    # Precise wrapper shape guards; no claim to cover downstream field guards.
    definitions = [('R003', '/api/client-devices', 'PUT', 'F10'),
        ('R005', '/api/client-devices/fixed-ip/apply', 'POST', 'F10'),
        ('R006', '/api/client-devices/fixed-ip/preview', 'POST', 'F10'),
        ('R008', '/api/routers', 'POST', 'F10'),
        ('R015', '/api/routers/r-ros/policy-settings', 'PUT', 'F13'),
        ('R016', '/api/routers/r-ros/policy-settings/preview', 'POST', 'F10'),
        ('R024', '/api/routers/r-ros/rule-sync', 'PUT', 'F13'),
        ('R026', '/api/routers/gateway-forwarding', 'POST', 'F10')]
    for route, path, method, entry in definitions:
        for variant, raw, content_type in [('none', None, None), ('null', b'null', 'application/json'),
            ('array', b'[]', 'application/json'), ('text', b'"wrong"', 'application/json'),
            ('bool', b'true', 'application/json'), ('number', b'1', 'application/json'),
            ('malformed', b'{', 'application/json'), ('wrong-content-type', b'{}', 'text/plain')]:
            headers = {'Content-Type': content_type} if content_type else {}
            yield Case(route + '-' + entry + '/' + variant, route, method, path, 400,
                (route + '-' + entry,), raw=raw, headers=headers,
                reason='request.get_json(silent=True) and exact outer shape guard')
