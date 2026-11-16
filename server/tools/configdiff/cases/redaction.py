"""X01 expanded public blueprints, keys/depth and native/editor exemptions."""
import json
from urllib.parse import quote, quote_plus

from compare import Policy, Difference
from harness import Pair, DesignError, json_body
from seed import CURRENT, RETIRED, AGENT


def assert_redacted(reply):
    value = reply.json()
    def walk(item):
        if isinstance(item, dict):
            if {'rule_proxy_token', 'retired_rule_proxy_tokens', 'rule_fetch_proxy'} & item.keys():
                raise DesignError('X01 Python sensitive key survived ordinary JSON hook')
            for v in item.values():
                walk(v)
        elif isinstance(item, list):
            for v in item:
                walk(v)
    walk(value)
    if '[REDACTED]' not in reply.body.decode():
        raise DesignError('X01 ordinary JSON fixture did not expose redaction')


def blueprint_text(binary, recorder):
    routes = {'node': 'GET /api/nodes', 'subscription': 'GET /api/subscriptions',
              'rule': 'GET /api/rules', 'group': 'GET /api/proxy-groups'}
    encodings = {'raw': lambda v: v, 'percent': lambda v: quote(v, safe=''),
                 'double-percent': lambda v: quote(quote(v, safe=''), safe=''), 'plus': quote_plus}
    for label, encode in encodings.items():
        payload = '合成🧪-' + '-'.join(encode(v) for v in (CURRENT, RETIRED, AGENT))
        for location, route in routes.items():
            with Pair(binary, recorder, options={'x01_location': location, 'x01_payload': payload}) as pair:
                pair.request('X01', route, variant=location + '/' + label, expect=200, check=assert_redacted)


def depth_keys(binary, recorder):
    for depth in (0, 126, 127, 128, 129):
        with Pair(binary, recorder, options={'x01_location': 'node', 'x01_payload': CURRENT,
                                            'depth': depth}) as pair:
            pair.request('X01', 'GET /api/nodes', variant='sensitive-keys/depth-' + str(depth),
                         expect=200, check=assert_redacted)
            # No redaction for a nonsecret sibling: a sanitizer that replaces params
            # wholesale must fail even when both HTTP replies contain REDACTED.
            from harness import send
            reference = send(pair.bases[0], 'GET', '/api/nodes', headers={'Authorization': 'Bearer ' + pair.tokens[0]})
            if reference.json()[0]['params']['nested'] != {'keep': 'visible'}:
                raise DesignError('X01 sibling fixture was not preserved')


def native_exemptions(binary, recorder):
    with Pair(binary, recorder, options={'x01_native': True}) as pair:
        def native_check(reply):
            value = reply.json()
            text = json.dumps(value, ensure_ascii=False)
            if CURRENT not in text or AGENT not in text:
                raise DesignError('X01 native exemption did not retain observable API credentials')
        # Both aliases are standalone matrix entries and must remain testable when wired.
        for prefix, scoped in [('/api/oxidns', False),
                               ('/api/profiles/' + pair.default_profile + '/oxidns', True)]:
            template = '/api/profiles/<profile_id>/oxidns' if scoped else '/api/oxidns'
            replies = pair.request('X01', 'GET ' + template + '/settings', prefix + '/settings',
                                   variant=template + '/settings-native', expect=200, check=native_check)
            settings = replies[0].json()
            for suffix, extras in [('render', {}), ('editor', {}), ('common-settings', {}),
                                   ('common-settings/apply', {'changes': {'api.username': AGENT, 'api.password': CURRENT}}),
                                   ('native-settings/apply', {'native_config': settings['native_config'], 'renames': {}}),
                                   ('template-preview', {'template_settings': settings})]:
                pair.request('X01', 'POST ' + template + '/' + suffix, prefix + '/' + suffix,
                             body=json_body({'settings': settings, 'base_url': 'http://panel.example', **extras}),
                             variant=template + '/' + suffix, expect=200,
                             check=native_check if suffix != 'template-preview' else None)
            policy = Policy(start=pair.policy.start)
            for name in ('default', 'other'):
                policy.times.update({'db.fields.profile/' + name + '/updated_at.value',
                                     'db.fields.profile/' + name + '/created_at.value'})
            pair.request('X01', 'POST ' + template + '/settings', prefix + '/settings',
                         body=json_body(settings), variant=template + '/save-native', expect=200,
                         check=native_check, writes=True, policy=policy)


def proxy_editor_exemption(binary, recorder):
    with Pair(binary, recorder) as pair:
        value = 'http://synthetic-user:synthetic-password@proxy.example:7890'
        policy = Policy(start=pair.policy.start)
        pair.request('X01', 'POST /api/settings/rule-fetch-proxy', body=json_body({'rule_fetch_proxy': value}),
                     variant='proxy-editor-save', expect=200, writes=True, policy=policy)
        def check(reply):
            if reply.json().get('rule_fetch_proxy') != value:
                raise DesignError('X01 editor-specific rule_fetch_proxy key exemption absent')
        pair.request('X01', 'GET /api/settings/rule-fetch-proxy', variant='proxy-editor-read', expect=200, check=check)


def other_blueprints(binary, recorder):
    payload = '跨蓝图🧪-' + CURRENT
    locations = {'aggregation': 'GET /api/aggregations', 'library': 'GET /api/rule-library',
                 'ruleset': 'GET /api/rule-sets', 'custom': 'GET /api/custom-config/mihomo',
                 'mosdns': 'GET /api/mosdns/dns-servers', 'domain': 'GET /api/domain-discovery/history',
                 'tasks': 'GET /api/tasks', 'settings': 'GET /api/server-domain',
                 'notifications': 'GET /api/notifications'}
    for location, route in locations.items():
        with Pair(binary, recorder, options={'x01_location': location, 'x01_payload': payload}) as pair:
            policy = Policy(start=pair.policy.start, times={'body.checked_at'}) if location == 'notifications' else pair.policy
            pair.request('X01', route, variant=location + '/metadata', expect=200,
                         check=assert_redacted, policy=policy)
    with Pair(binary, recorder, options={'admin_username': CURRENT}) as pair:
        def capture(replies, policy):
            import jwt
            from harness import SECRET
            tokens = [reply.json().get('token', '') for reply in replies]
            for token in tokens:
                if jwt.decode(token, SECRET, algorithms=['HS256'])['username'] != CURRENT:
                    raise DesignError('X01 login JWT subject changed')
            policy.bind(*tokens, r'[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+', 'x01-login-token')
            return []
        from harness import PASSWORD
        pair.request('X01', 'POST /api/auth/login', auth='',
                     body=json_body({'username': CURRENT, 'password': PASSWORD}),
                     variant='auth/username', expect=200, check=assert_redacted, capture=capture)
    with Pair(binary, recorder) as pair:
        for directory in pair.dirs:
            (directory / 'app.log').write_text('2026-01-01 00:00:00,000 - synthetic - INFO - ' + payload + '\n')
        policy = Policy(start=pair.policy.start)
        policy.bind(*[str(d / 'app.log') for d in pair.dirs], r'/tmp/configdiff-[^/]+/(python|go)/app\.log', 'test-log-path')
        pair.request('X01', 'GET /api/logs/tail', '/api/logs/tail?lines=1',
                     variant='logs/fixed-line', expect=200, check=assert_redacted, policy=policy)
    with Pair(binary, recorder, options={'redaction': payload}) as pair:
        pair.request('X01', 'GET /api/profiles/<profile_id>/export', '/api/profiles/default/export',
                     variant='profile-export', expect=200, check=assert_redacted)


GROUPS = {'redaction-blueprints': blueprint_text, 'redaction-depth': depth_keys,
          'redaction-native': native_exemptions, 'redaction-proxy': proxy_editor_exemption,
          'redaction-other-blueprints': other_blueprints}
