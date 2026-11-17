"""Proxy normalizer guards and final redirect/mirror failures through HTTP."""
import json
from urllib.parse import urlencode

from harness import Pair, json_body
from sources import Source
from cases.config_backup import message
from cases.pull_routes import SourceStable
from cases.rule_proxy import ROUTES
from cases.subscriptions import require


def proxy_guards(binary, recorder):
    with Source(b'setting validation must not contact proxy') as source, Pair(binary, recorder) as pair:
        vectors = [('H115', value) for value in (None, True, False, 0, 1, 1.5, [], {}, ['synthetic'])]
        vectors += [('H116', source.url + suffix) for suffix in ('/path', '/?query=synthetic', '/#fragment')]
        vectors += [('H117', value) for value in ('socks5://127.0.0.1:9', 'http:///missing',
            'http://127.0.0.1:bad', 'http://127.0.0.1:0', 'http://[::1')]
        for case, value in vectors:
            pair.request(case, 'POST /api/settings/rule-fetch-proxy', body=json_body({'rule_fetch_proxy': value}),
                expect=400, writes=True, interaction=SourceStable(pair, source),
                check=message('规则下载代理须为有效的 HTTP/HTTPS 代理地址，或留空使用现有网络'),
                variant='proxy-normalizer/' + json.dumps(value, ensure_ascii=True, sort_keys=True))


def missing_location(binary, recorder):
    for _rid, route, base in ROUTES:
        with Source(b'synthetic redirect with missing location') as source, Pair(binary, recorder) as pair:
            source.status = 302
            pair.request('H119', route, base + '?' + urlencode({'url': source.url + '/redirect'}), expect=400,
                writes=True, check=message('Invalid remote URL'), interaction=SourceStable(pair, source, expected=1),
                variant='missing-Location/' + base)


def failed_candidates(binary, recorder):
    with Source(b'synthetic proxy and mirror failure') as source:
        source.status = 503
        opts = {'system_config': {'rule_fetch_proxy': source.url, 'github_proxy_domain': 'http://mirror.example/cache'}}
        with Pair(binary, recorder, options=opts) as pair:
            def check(reply):
                require(reply.json()['code'] == 'RULE_SOURCE_UNAVAILABLE' and reply.headers.get('cache-control') == 'no-store',
                        'exhausted original and mirror are a live-source failure, never successful stale content')
            pair.request('H123', 'GET /api/mosdns/rule-proxy', '/api/mosdns/rule-proxy?' + urlencode({
                'url': 'https://raw.githubusercontent.com/synthetic/rules/main/domains.list'}), expect=502, writes=True,
                interaction=SourceStable(pair, source, expected=2), check=check, variant='original-and-mirror-both-fail-through-loopback-proxy')


GROUPS = {'rule-fetch-proxy-normalizer-guards': proxy_guards, 'rule-fetch-redirect-without-location': missing_location,
          'rule-fetch-exhausted-candidates': failed_candidates}
