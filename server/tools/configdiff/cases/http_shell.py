"""X06 actual HEAD/OPTIONS, CORS, slash and escaped-segment routing."""
from harness import Pair
from compare import database, Difference
import re


def heads(binary, recorder):
    representatives = ['/api/auth/status', '/api/nodes', '/api/subscriptions', '/api/aggregations',
        '/api/rule-library', '/api/profiles', '/api/proxy-groups', '/api/rules', '/api/rule-sets',
        '/api/custom-config/mihomo', '/api/mosdns/api-settings', '/api/oxidns/settings',
        '/api/stats/overview', '/api/domain-discovery/history', '/api/tasks', '/api/notifications', '/api/logs/info']
    for path in representatives:
        with Pair(binary, recorder) as pair:
            pair.policy.times.add('files.profiles/default/stats_history.json.content.samples[0].at')
            strict_get_post = {'/api/nodes', '/api/subscriptions', '/api/aggregations', '/api/rule-library',
                '/api/proxy-groups', '/api/rules', '/api/rule-sets', '/api/custom-config/mihomo', '/api/mosdns/api-settings'}
            expected = 500 if path in strict_get_post else 201 if path == '/api/profiles' else 400 if path == '/api/oxidns/settings' else 200
            def capture(_replies, policy):
                if path != '/api/profiles':
                    return []
                rows = [database(d) for d in pair.dirs]
                ids = [[r[1] for r in value['sections'] if r[0] == 'profile' and r[1] not in ('default', 'other')] for value in rows]
                if not all(len(v) == 1 and re.fullmatch('profile_[a-f0-9]{12}', v[0]) for v in ids):
                    return [Difference('head.profile_creation', 'created-profile-set', ids[0], ids[1])]
                a, b = ids[0][0], ids[1][0]
                policy.bind(a, b, r'profile_[a-f0-9]{12}', 'HEAD-created-profile')
                for key in rows[0]['fields']:
                    if key.startswith('profile/' + a + '/'):
                        policy.bind(key, key.replace(a, b, 1), r'profile/profile_[a-f0-9]{12}/[A-Za-z0-9_]+', 'HEAD-profile-row')
                for field in ('created_at', 'updated_at'):
                    policy.times.add('db.fields.profile/' + a + '/' + field + '.value')
                return []
            pair.request('X06', 'GET ' + path, method='HEAD', variant='head/' + path, expect=expected, writes=True, capture=capture)
    for service in ('mihomo', 'surge', 'loon', 'mosdns', 'oxidns'):
        with Pair(binary, recorder) as pair:
            for template in ['/api/config/' + service, '/api/config/<profile_id>/' + service]:
                pair.request('X06', 'GET ' + template, template.replace('<profile_id>', 'default'),
                             method='HEAD', variant='head-pull/' + template, expect=200, writes=True)


def options_and_errors(binary, recorder):
    for mode in ('login', 'open'):
        with Pair(binary, recorder, mode=mode) as pair:
            for path in ('/api/nodes', '/api/auth/status'):
                for credential in ('', 'admin'):
                    for origin in ('', 'https://browser.example'):
                        headers = {'Origin': origin, 'Access-Control-Request-Method': 'GET',
                                   'Access-Control-Request-Headers': 'Authorization,X-ConfigFlow-Profile'} if origin else {}
                        expected = 401 if mode == 'login' and path == '/api/nodes' and not credential else 200
                        pair.request('X06', 'GET ' + path, method='OPTIONS', auth=credential,
                                     headers=headers, expect=expected, writes=True,
                                     variant='/'.join(('options', mode, path, credential, str(bool(origin)))))
            for name, path, auth, status in [('trailing-slash', '/api/subscriptions/', 'admin', 404),
                                            ('unknown-admin', '/api/synthetic-missing-route', 'admin', 404),
                                            ('unknown-anonymous', '/api/synthetic-missing-route', '', 401 if mode == 'login' else 404)]:
                pair.request('X06', 'GET /api/subscriptions', path, auth=auth, expect=status,
                             headers={'Origin': 'https://browser.example'}, variant=mode + '/' + name, writes=True)
    with Pair(binary, recorder) as pair:
        escaped = [('GET /api/rules/local/<name>', '/api/rules/local/one%2Ftwo'),
                   ('GET /api/rule-library/content/<rule_id>', '/api/rule-library/content/one%2Ftwo'),
                   ('GET /api/profiles/<profile_id>/rules/local/<name>', '/api/profiles/default/rules/local/one%2Ftwo'),
                   ('GET /api/profiles/<profile_id>/rule-library/content/<rule_id>', '/api/profiles/default/rule-library/content/one%2Ftwo'),
                   ('GET /api/generate/artifacts/<preview_id>/<action>', '/api/generate/artifacts/' + '0'*32 + '/one%2Ftwo')]
        for route, path in escaped:
            pair.request('X06', route, path, expect=404, variant='encoded-slash/' + route,
                         headers={'Origin': 'https://browser.example'}, writes=True)


GROUPS = {'http-head': heads, 'http-options-routing': options_and_errors}
