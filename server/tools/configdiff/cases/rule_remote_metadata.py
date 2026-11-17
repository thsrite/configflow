"""Retained remote declarations reach the post-download metadata guard."""
from urllib.parse import urlencode

from compare import database
from harness import Pair
from sources import Source
from subscriptions import Exchange, capture_notice
from task_lifecycle import configure
from cases.rule_library import DOMAIN
from cases.rule_proxy import ROUTES, options
from cases.subscriptions import require


def invalid(binary, recorder):
    for field, value, diagnostic in [('behavior', 'retained-unknown', '规则集类型必须是 domain、ipcidr 或 classical。'),
            ('format', 'retained-unknown', '规则集格式必须是 text、yaml 或 mrs。')]:
        with Source(b'example.com\n') as source:
            rule = {**DOMAIN, 'id': 'retained-invalid-metadata', 'source_type': 'url', 'url': source.url + '/rules',
                'content': '', 'behavior': 'domain', 'format': 'text', 'update_interval': 0, field: value}
            with Pair(binary, recorder, options=options([rule])) as pair:
                baseline = database(pair.dirs[0])
                configure(pair, 'rule_source:' + rule['id'])
                for rid, route, path in ROUTES:
                    def check(reply):
                        require(reply.json().get('code') == 'RULE_CONTENT_INVALID' and diagnostic in reply.json().get('message', ''),
                            'retained declaration is rejected by metadata validation after a real source read')
                        require(database(pair.dirs[0]) == baseline, 'downloaded metadata failure preserves every SQL field')
                        require(not list((pair.dirs[0] / 'shared/rules').glob('**/*.list')),
                            'invalid remote metadata never installs a raw rule cache')
                    pair.request('H142', route, path + '?' + urlencode({'url': rule['url'], 'source_id': rule['id']}),
                        expect=422, writes=True, check=check, interaction=Exchange(pair, source),
                        capture=capture_notice(pair, 'rule_source:' + rule['id']), variant=rid + '/' + field)


GROUPS = {'rule-real-downloaded-retained-metadata-errors': invalid}
