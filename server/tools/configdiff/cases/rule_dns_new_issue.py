"""A real native rewrite refuses newly indexed validation issues atomically."""
from harness import Pair, json_body
from inventory import routes
from oxidns import check as etag_check, capture as etag_capture
from cases.oxidns_editor import doc, text
from cases.oxidns_routes import api, BASE
from cases.rules import options
from cases.setup import Stable
from cases.subscriptions import require


def refused(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        start = api(pair, '/api/oxidns', '/api/oxidns', 'editor', 'S01', {'settings': {'rule_actions': {
            'r-a': {'action': 'upstream', 'source': 'local', 'upstream': {'addr': 'udp://192.0.2.53'}}}},
            'base_url': BASE}, variant='new-native-issue/real-pinned-upstream')['settings']
        candidate = doc(start)
        candidate['plugins'].append({'tag': 'retained_invalid_cache', 'type': 'cache', 'args': {'size': 'retained-wrong-type'}})
        settings = {**start, 'native_config': text(candidate)}
        route = 'POST /api/oxidns/settings'
        pair.request(routes()[route] + '-S02', route, body=json_body(settings), expect=200, writes=True,
            check=etag_check(pair), capture=etag_capture(pair), variant='new-native-issue/real-save-with-retained-invalid-arguments')
        pair.request('H916', 'PUT /api/rules/<rule_id>', '/api/rules/r-a', body=json_body({'dns': 'refused'}),
            expect=409, writes=True, interaction=Stable(pair),
            check=lambda reply: require(reply.json().get('message', '').startswith('写入后 OxiDNS 配置校验失败：'),
                'removing owned pinned helpers shifts a retained issue and actually reaches added-issue rejection'),
            variant='retained-native-issue-moves-after-removing-real-pinned-helpers')


GROUPS = {'rule-DNS-real-new-native-validation-issue': refused}
