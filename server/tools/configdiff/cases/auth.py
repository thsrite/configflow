"""Complete route-file batch backend/routes/auth.py (R011–R013)."""
import time
import jwt

from compare import Policy, Difference, differences
from harness import Pair, DesignError, USERNAME, PASSWORD, SECRET, json_body


def bind_jwt(replies, policy):
    tokens, claims = [], []
    for index, reply in enumerate(replies):
        try:
            token = reply.json()['token']
            claim = jwt.decode(token, SECRET, algorithms=['HS256'])
            assert claim['username'] == USERNAME
            assert 86398 <= int(claim['exp']) - int(claim['iat']) <= 86401
            assert abs(claim['iat'] - time.time()) < 10
            tokens.append(token)
            claims.append(claim)
        except (KeyError, ValueError, AssertionError, jwt.PyJWTError):
            if index == 0:
                raise DesignError('R011-S01 Python login JWT contract failed')
            return [Difference('body.token', 'invalid-Go-JWT', '<valid-HS256>', '<invalid>')]
    policy.bind(*tokens, r'[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+', 'login-jwt')
    # username and duration were checked; issue/expiry timestamps are independently
    # sampled within 10 seconds, not ignored along with arbitrary other claims.
    if set(claims[0]) != set(claims[1]):
        return [Difference('body.token.claims', 'claim-keys', sorted(claims[0]), sorted(claims[1]))]
    for key in set(claims[0]) - {'iat', 'exp'}:
        if claims[0][key] != claims[1][key]:
            return [Difference('body.token.' + key, 'claim', claims[0][key], claims[1][key])]
    return []


def auth_routes(binary, recorder):
    with Pair(binary, recorder) as pair:
        pair.request('R011-S01', 'POST /api/auth/login', auth='',
                     body=json_body({'username': USERNAME, 'password': PASSWORD}), expect=200, capture=bind_jwt)
        invalid = [({'username': 'wrong', 'password': PASSWORD}, 'F02'),
                   ({'username': USERNAME, 'password': 'wrong'}, 'F03'),
                   ({'password': PASSWORD}, 'F04'), ({'username': USERNAME, 'password': ''}, 'F05')]
        for body, suffix in invalid:
            pair.request('R011-' + suffix, 'POST /api/auth/login', body=json_body(body), auth='', expect=401)
        for body, content_type, suffix in [('[1]', 'application/json', 'F06'),
                                            ('{"username":', 'application/json', 'F07'),
                                            ('{}', 'text/plain', 'F08')]:
            policy = Policy(internal_errors={'body.message': '登录失败。原始错误：'},
                            start=pair.policy.start)
            pair.request('R011-' + suffix, 'POST /api/auth/login', auth='', body=body,
                         headers={'Content-Type': content_type}, expect=500, policy=policy)
        pair.request('R012-S01', 'GET /api/auth/status', auth='', expect=200)
        pair.request('R012-F01', 'GET /api/auth/status', method='POST', auth='', expect=405)
        pair.request('R012-B01', 'GET /api/auth/status', auth='wrong.jwt.synthetic', expect=200)
        pair.request('R013-S01', 'GET /api/auth/verify', expect=200)
        expired = jwt.encode({'username': USERNAME, 'exp': 1}, SECRET, algorithm='HS256')
        forged = jwt.encode({'username': USERNAME}, 'synthetic-wrong-key', algorithm='HS256')
        empty = jwt.encode({}, SECRET, algorithm='HS256')
        for number, token, query in [(1, '', ''), (2, forged, ''), (3, expired, ''),
                                     (4, 'Bearer  malformed', ''), (5, 'x' * 8193, ''),
                                     (6, empty, ''), (7, '', '?token=synthetic-public-pull-token')]:
            pair.request(f'R013-F{number:02}', 'GET /api/auth/verify', '/api/auth/verify' + query,
                         auth=token, expect=401)
    with Pair(binary, recorder, mode='open') as pair:
        pair.request('R011-F01', 'POST /api/auth/login', auth='',
                     body=json_body({'username': USERNAME, 'password': PASSWORD}), expect=400)
        pair.request('R013-B01', 'GET /api/auth/verify', auth='wrong.jwt.synthetic', expect=200)
    for missing in ('admin_username', 'admin_password'):
        with Pair(binary, recorder, options={missing: ''}) as pair:
            pair.request('R012-S02', 'GET /api/auth/status', auth='', variant=missing, expect=200,
                         check=lambda reply: require_disabled(reply))
    for options, case_id, variant in [({'admin_username': '账号🧪', 'admin_password': '密码🧪'}, 'R011-B01', 'unicode'),
                                      ({'admin_password': 'p' * 8192}, 'R011-B02', '8192-characters')]:
        with Pair(binary, recorder, options=options) as pair:
            def capture(replies, policy):
                a, b = [reply.json().get('token', '') for reply in replies]
                if not a:
                    raise DesignError('Python special credential login failed')
                if not b:
                    return [Difference('body.token', 'missing', '<token>', None)]
                ca, cb = [jwt.decode(token, SECRET, algorithms=['HS256']) for token in (a, b)]
                if ca['username'] != options.get('admin_username', USERNAME) or cb['username'] != ca['username']:
                    return [Difference('body.token.username', 'value', ca['username'], cb.get('username'))]
                policy.bind(a, b, r'[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+', 'special-login')
                return []
            pair.request(case_id, 'POST /api/auth/login', auth='', variant=variant,
                         body=json_body({'username': options.get('admin_username', USERNAME),
                                         'password': options['admin_password']}), expect=200, capture=capture)


def require_disabled(reply):
    if reply.json() != {'authEnabled': False}:
        raise DesignError('Python should disable authentication when either credential is empty')


GROUPS = {'auth-routes': auth_routes}
