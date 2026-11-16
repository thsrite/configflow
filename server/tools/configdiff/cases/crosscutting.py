"""O6 first: X01/X02/X03/X08, three auth environments and T29."""
import json
import re
import time
from urllib.parse import quote

import jwt

from compare import Policy, Difference, database, database_diffs, file_diffs
from harness import Pair, DesignError, USERNAME, PASSWORD, SECRET, json_body
from seed import CURRENT, RETIRED, AGENT, PULL
from inventory import routes

MATRIX = routes()
PULL_ROUTES = [(route, rid) for route, rid in MATRIX.items()
               if re.fullmatch(r'GET /api/config/(?:<profile_id>/)?(?:mihomo|surge|loon|mosdns|oxidns)(?:/<filename>)?', route)]


def pull_path(route, profile='default'):
    return route.split(' ', 1)[1].replace('<profile_id>', profile).replace('<filename>', 'synthetic.lcf')


def pull_filename(route):
    if '/surge' in route:
        return 'surge.conf'
    if '/loon' in route:
        return 'loon.lcf'
    return 'config.yaml'

# Each representative uses the matrix's real method/template, even if the
# current Go manifest has not wired it. Missing business objects are separate
# from missing routes and never inferred from a response status.
SCOPED = [
    ('R173', 'GET /api/rules'),
    ('R171', 'GET /api/rule-sets'),
    ('R166', 'GET /api/proxy-groups'),
    ('R034', 'GET /api/custom-config/mihomo'),
    ('R078', 'GET /api/mosdns/api-settings'),
    ('R114', 'GET /api/oxidns/settings'),
    ('R064', 'POST /api/generate/loon'),
    ('R223', 'GET /api/stats/overview'),
    ('R047', 'GET /api/domain-discovery/domains'),
    ('R030', 'GET /api/config/mihomo'),
    ('R148', 'GET /api/profiles/<profile_id>/rules'),
]
GLOBAL = [
    ('R196', 'GET /api/server-domain'), ('R095', 'GET /api/nodes'),
    ('R213', 'GET /api/subscriptions'), ('R157', 'GET /api/rule-library'),
    ('R115', 'GET /api/profiles'), ('R225', 'GET /api/tasks'),
    ('R104', 'GET /api/notifications'), ('R075', 'GET /api/logs/info'),
]


SCOPED = [(MATRIX[route], route) for _, route in SCOPED]
GLOBAL = [(MATRIX[route], route) for _, route in GLOBAL]

def precedence(binary, recorder):
    with Pair(binary, recorder) as pair:
        for rid, route in SCOPED:
            template = route.split(' ', 1)[1]
            for missing, expected in [('deleted-synthetic', 404), ('bad!id', 400)]:
                if '<profile_id>' in template:
                    pair.request('X02', route, template.replace('<profile_id>', quote(missing)), auth='',
                                 variant=rid + '/path/' + missing, expect=expected)
                else:
                    for selector in ('profile', 'profile_id', 'header'):
                        path = template + ('?' + selector + '=' + quote(missing) if selector != 'header' else '')
                        headers = {'X-ConfigFlow-Profile': missing} if selector == 'header' else {}
                        pair.request('X02', route, path, auth='', headers=headers,
                                     variant=rid + '/' + selector + '/' + missing, expect=expected)
            path = template.replace('<profile_id>', pair.default_profile)
            pair.request('X02', route, path, auth='', variant=rid + '/existing', expect=401)
        for rid, route in GLOBAL:
            pair.request('X02', route, auth='', headers={'X-ConfigFlow-Profile': 'deleted-synthetic'},
                         variant=rid + '/global', expect=401)


def jwt_compatibility(binary, recorder):
    with Pair(binary, recorder) as pair:
        route = 'GET /api/auth/verify'
        now = int(time.time())
        variants = {
            'no-exp': jwt.encode({'username': USERNAME}, SECRET, algorithm='HS256'),
            'string-exp-iat': jwt.encode({'username': USERNAME, 'exp': str(now + 3600), 'iat': str(now)}, SECRET, algorithm='HS256'),
            'expired': jwt.encode({'username': USERNAME, 'exp': now - 60}, SECRET, algorithm='HS256'),
            'future-nbf': jwt.encode({'username': USERNAME, 'nbf': now + 3600}, SECRET, algorithm='HS256'),
            'future-iat': jwt.encode({'username': USERNAME, 'iat': now + 3600}, SECRET, algorithm='HS256'),
            'empty-claims': jwt.encode({}, SECRET, algorithm='HS256'),
            'HS512': jwt.encode({'username': USERNAME}, SECRET, algorithm='HS512'),
            'wrong-key': jwt.encode({'username': USERNAME}, 'synthetic-wrong-key', algorithm='HS256'),
            'RS256': 'eyJhbGciOiJSUzI1NiJ9.eyJ1c2VybmFtZSI6ImRpZmYtYWRtaW4ifQ.c3ludGhldGljLXNpZ25hdHVyZQ',
            'none': jwt.encode({'username': USERNAME}, key='', algorithm='none'),
        }
        for name, token in variants.items():
            pair.request('T29', route, auth=token, variant=name, expect=200 if name in ('no-exp', 'string-exp-iat') else 401)
        # Both directions send exactly the issuer's token to both processes.
        for issuer, token in zip(('python', 'go'), pair.tokens):
            reply = pair.request('R013-B90', route, auth=token, variant=issuer + '-issued', expect=200)
            if not reply[0].json().get('success'):
                raise DesignError('T29 issuer token did not verify on Python')


    with Pair(binary, recorder, options={'secret_key': None}) as pair:
        old_tokens = list(pair.tokens)
        pair.restart()
        pair.request('T29', 'GET /api/auth/verify', auth=old_tokens, variant='random-secret-restart', expect=401)

def auth_environments(binary, recorder):
    for mode in ('login', 'token', 'open'):
        with Pair(binary, recorder, mode=mode) as pair:
            for route, rid in PULL_ROUTES:
                path = pull_path(route, pair.default_profile)
                expired = jwt.encode({'username': USERNAME, 'exp': 1}, SECRET, algorithm='HS256')
                requests = [('', '', 1), ('?token=wrong-synthetic', '', 2), ('', expired, 3),
                            ('?token=' + quote(CURRENT, safe=''), '', 4),
                            ('?token=' + quote(RETIRED, safe=''), '', 5),
                            ('', 'Bearer  malformed', 6), ('?token=' + 'x' * 8193, '', 7)]
                for query, auth, number in requests:
                    pair.request(f'{rid}-F{number:02}', route, path + query, auth=auth,
                                 headers={'Host': 'panel.example'}, writes=True, filename=pull_filename(route),
                                 variant=mode + '/D' + str(number), expect=200 if mode == 'open' and number not in (4, 5) else 401)
                suffix = ('S03' if mode == 'login' else 'S04' if mode == 'token' else 'S01') if '/oxidns' in route else ('S01' if mode == 'login' else 'S02' if mode == 'token' else 'S03')
                pair.request(rid + '-' + suffix, route, path + '?token=' + PULL if mode == 'token' else path,
                             auth='admin' if mode == 'login' else '', variant=mode + '/authorized', expect=200,
                             headers={'Host': 'panel.example'}, writes=True, filename=pull_filename(route))
                for credential in (CURRENT, RETIRED):
                    pair.request('D8', route, path + '?token=' + quote(credential, safe=''),
                                 headers={'X-ConfigFlow-Internal': 'synthetic-spoof'},
                                 auth='admin' if mode == 'login' else '', variant=mode + '/internal-with-admin/' + ('current' if credential == CURRENT else 'retired'), expect=401,
                                 writes=True)
            for internal in ('', 'synthetic-guess', 'x' * 8193):
                pair.request('X03', 'GET /api/server-domain', auth='',
                             headers={'X-ConfigFlow-Internal': internal}, variant=mode + '/spoof-' + str(len(internal)),
                             expect=401 if mode == 'login' else 200)


def redaction(binary, recorder):
    tokens = (CURRENT, RETIRED, AGENT)
    variants = [('raw', lambda value: value), ('percent', lambda value: quote(value, safe='')),
                ('double-percent', lambda value: quote(quote(value, safe=''), safe=''))]
    for name, encode in variants:
        payload = 'synthetic-prefix ' + ' '.join(encode(token) for token in tokens) + ' suffix🧪'
        with Pair(binary, recorder, options={'redaction': payload}) as pair:
            def check(reply):
                if payload in reply.body.decode():
                    raise DesignError('X01 Python did not redact fixture')
                if '[REDACTED]' not in reply.body.decode():
                    raise DesignError('X01 fixture was not observable')
            pair.request('X01', 'GET /api/profiles', variant=name + '/description', expect=200, check=check)
            pair.request('X01', 'GET /api/config/export', '/api/config/export?desensitize=true',
                         variant=name + '/export-desensitized', expect=200, check=check)
            def raw_check(reply):
                if payload not in reply.body.decode():
                    raise DesignError('X01 send_file raw export did not preserve the fixture')
            pair.request('X01', 'GET /api/config/export', '/api/config/export?desensitize=false',
                         variant=name + '/export-raw-passthrough', expect=200, check=raw_check)


def startup(binary, recorder):
    with Pair(binary, recorder, options={'orphan_cache': True}) as pair:
        orphan = 'shared/subscribes/orphan-synthetic.json'
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline and any((d / orphan).exists() for d in pair.dirs):
            time.sleep(.1)
        left, right = [(d / orphan).exists() for d in pair.dirs]
        if left:
            raise DesignError('X08 Python did not prune the synthetic orphan')
        diffs = [] if left == right else [Difference('files.' + orphan, 'startup-prune', left, right)]
        pair.policy.end = time.time()
        diffs += database_diffs(*pair.dirs, pair.policy)
        diffs += file_diffs(*pair.dirs, pair.policy)
        recorder.record('X08', 'production-startup-orphan-prune', 'startup', 'X08',
                        'different' if diffs else 'compared', diffs)


def startup_migration(binary, recorder):
    # O6 explicitly moves this retired migration to O12; record the observed
    # storage difference, never count it as an implemented parity success.
    with Pair(binary, recorder, options={'legacy_dns': True}) as pair:
        left, right = [database(directory) for directory in pair.dirs]
        a = left['fields']['system//system_config']['value']['node_test_dns_config']
        b = right['fields']['system//system_config']['value']['node_test_dns_config']
        if a.get('servers') != ['https://dns.example/dns-query']:
            raise DesignError('X08 legacy migration seed did not reach Python cleanup')
        policy = Policy(start=pair.policy.start, end=time.time(),
                        times={'db.fields.profile/default/updated_at.value'})
        observed = database_diffs(*pair.dirs, policy) + file_diffs(*pair.dirs, policy)
        allowed = ('db.meta.generation', 'db.fields.system//system_config.',
                   'db.fields.system//agents.', 'db.fields.system//_revision.',
                   'db.fields.profile/default/_revision.', 'db.fields.profile/default/updated_at.',
                   'files.notifications.json', 'files.task-status.json')
        retired = [d for d in observed if d.path.startswith(allowed)]
        unexpected = [d for d in observed if not d.path.startswith(allowed)]
        recorder.record('X08', 'retired-node-test-dns-migration', 'startup', 'X08',
                        'O12-separate' if retired else 'compared', retired)
        if unexpected:
            recorder.record('X08', 'retired-migration-unexpected-storage', 'startup', 'X08',
                            'different', unexpected)


GROUPS = {'precedence': precedence, 'jwt': jwt_compatibility, 'auth-environments': auth_environments,
          'redaction': redaction, 'startup': startup, 'startup-migration': startup_migration}
