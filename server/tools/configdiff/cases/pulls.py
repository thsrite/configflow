"""Live config pulls: Unicode/long credentials, native OxiDNS and Loon names."""
from urllib.parse import quote

from harness import Pair, DesignError
from compare import Difference
from cases.crosscutting import PULL_ROUTES, pull_path, pull_filename


def credentials(binary, recorder):
    token = '合成🧪-% +/?&'
    with Pair(binary, recorder, mode='token', options={'config_token': token}) as pair:
        for route, rid in PULL_ROUTES:
            def leaked(index):
                text = (pair.dirs[index] / 'app.log').read_text(errors='replace')
                return token in text or quote(token, safe='') in text
            def check(reply):
                if leaked(0):
                    raise DesignError('Python config pull leaked synthetic query token')
            def capture(replies, policy):
                return [Difference('logs.query_token', 'secret-leak', False, True)] if leaked(1) else []
            pair.request(rid + '-B03', route, pull_path(route) + '?token=' + quote(token, safe=''), auth='',
                         headers={'Host': 'panel.example'}, expect=200, writes=True, filename=pull_filename(route),
                         check=check, capture=capture)
    for size in (8192, 8193):
        with Pair(binary, recorder, mode='token', options={'config_token': 'x' * size}) as pair:
            for route, rid in PULL_ROUTES:
                pair.request('T01', route, pull_path(route) + '?token=' + 'x' * size, auth='',
                             headers={'Host': 'panel.example'}, expect=200 if size == 8192 else 401, writes=True,
                             filename=pull_filename(route), variant=rid + '/matching-token-length/' + str(size))
    with Pair(binary, recorder, options={'x01_native': True}) as pair:
        for route, rid in PULL_ROUTES:
            if '/oxidns' in route:
                pair.request(rid + '-S02', route, pull_path(route), headers={'Host': 'panel.example'},
                             expect=200, writes=True, filename='config.yaml')


def spoofed_headers(binary, recorder):
    for mode in ('login', 'token', 'open'):
        with Pair(binary, recorder, mode=mode) as pair:
            for route in ('GET /api/config/mihomo', 'GET /api/mosdns/rule-proxy'):
                for header in ('', 'synthetic-spoof', 'x' * 8193):
                    expected = 200 if mode == 'open' and route.endswith('/mihomo') else 401
                    pair.request('X03', route, auth='', headers={'X-ConfigFlow-Internal': header, 'Host': 'panel.example'},
                                 expect=expected, writes=True, filename='config.yaml',
                                 variant=mode + '/pull-or-rule-proxy/' + route + '/' + str(len(header)))


def loon_names(binary, recorder):
    for name in ('中文 🧪 100%', '.', '\\/:*?"<>|'):
        with Pair(binary, recorder, options={'profile': {'name': name}}) as pair:
            for route, rid in PULL_ROUTES:
                if '/loon' not in route:
                    continue
                pair.request(rid + '-B92', route, pull_path(route), headers={'Host': 'panel.example'},
                             expect=200, writes=True, filename='loon.lcf', variant=name)


GROUPS = {'config-pull-credentials': credentials, 'internal-header-pulls': spoofed_headers,
          'config-loon-names': loon_names}
