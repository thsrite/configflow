"""Real parse-lines HTTP and the complete loopback Sub-Store protocol."""
from harness import Pair, json_body
from sources import FEED
from substore import SubStore, NodeExchange
from cases.setup import Stable
from cases.subscriptions import require

ROUTE = 'POST /api/nodes/parse-lines'
SURGE = 'SyntheticSnell = snell, 192.0.2.1, 443, psk=synthetic, version=4'
LOON = 'SyntheticLoon = Shadowsocks, 192.0.2.2, 443, aes-128-gcm, "synthetic"'
PROXY = {'name': 'synthetic-node', 'type': 'socks5', 'server': '192.0.2.1', 'port': 1080}
ERROR = {'error': '无法识别的节点格式'}


class Conversion(Stable):
    def __init__(self, pair, store, content):
        super().__init__(pair)
        self.exchange = NodeExchange(store, content)

    def before(self, index):
        super().before(index)
        self.exchange.before(index)

    def after(self, index, reply):
        self.exchange.after(index, reply)
        super().after(index, reply)

    def compare(self, policy):
        return super().compare(policy) + self.exchange.compare(policy)


def converted(binary, recorder):
    scenarios = [('R102-S01', SURGE, FEED, 200, True, 'Surge-snell-through-fake'),
                 ('R102-S02', LOON, FEED, 200, True, 'Loon-through-fake'),
                 ('R102-B03', SURGE.replace('SyntheticSnell', '中文🧪逗号,引号"'), FEED, 200, True, 'unicode-emoji-name'),
                 ('R102-B90', '  ' + SURGE + ' \t', FEED, 200, True, 'trimmed-content-exact-protocol'),
                 ('R102-F13', 'unknown-node-format', b'<html>synthetic invalid conversion</html>', 200, False, 'HTML-result'),
                 ('R102-F13', SURGE, b'proxies: []\n', 200, False, 'empty-proxies-result'),
                 ('R102-F13', SURGE, b'proxies: [\n', 200, False, 'malformed-YAML-result'),
                 ('R102-F13', SURGE, b'synthetic download failed', 503, False, 'download-503-still-cleans-temp')]
    for case, line, body, status, success, variant in scenarios:
        with SubStore(body=body, download_status=status) as store, Pair(binary, recorder,
                options={'environment': {'SUB_STORE_URL': store.url}}) as pair:
            observer = Conversion(pair, store, line.strip())
            expected = {'success': True, 'results': [{'proxy': PROXY} if success else ERROR]}
            pair.request(case, ROUTE, body=json_body({'lines': [line]}), expect=200, writes=True,
                interaction=observer, check=lambda reply: require(reply.json() == expected,
                    'per-line conversion outcome is exact and does not turn an individual failure into HTTP failure'),
                variant=variant)


def unavailable(binary, recorder):
    with Pair(binary, recorder, options={'environment': {'SUB_STORE_URL': 'http://127.0.0.1:1'}}) as pair:
        pair.request('R102-B90', ROUTE, body=json_body({'lines': [SURGE, ' ', LOON]}), expect=200, writes=True,
            interaction=Stable(pair), check=lambda reply: require(reply.json() == {'success': True, 'results': [ERROR] * 3},
                'unavailable runtime returns one error per input, including blank, without persisting anything'),
            variant='unavailable-loopback-runtime-one-error-per-line')


GROUPS = {'nodes-parse-lines-converter': converted, 'nodes-parse-lines-unavailable': unavailable}
