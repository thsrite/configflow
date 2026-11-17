"""Lossless Mihomo delivery guards reached by public local-rule GET, not proxy."""
from urllib.parse import quote
import time

from compare import differences
from harness import Pair
from cases.rule_library import DOMAIN, content_cache
from cases.pull_logging import events
from cases.setup import Stable
from cases.subscriptions import require

MARKER = "Rule '" + DOMAIN['name'] + "': "
WARNING = MARKER + '1 entries need manual review (lines [0])'


class Delivery(Stable):
    def __init__(self, pair):
        super().__init__(pair)
        self.observed = [None, None]

    def before(self, index):
        super().before(index)
        path = self.pair.dirs[index] / 'app.log'
        self.offset = path.stat().st_size if path.exists() else 0

    def after(self, index, reply):
        super().after(index, reply)
        path = self.pair.dirs[index] / 'app.log'
        deadline = time.monotonic() + 2
        rows = []
        while time.monotonic() < deadline:
            rows = events(path.read_bytes()[self.offset:] if path.exists() else b'', MARKER)
            if rows:
                break
            time.sleep(0.02)
        self.observed[index] = rows
        if index == 0:
            require(rows == [{'level': 'WARNING', 'message': WARNING}],
                    'one real line-zero YAML-delivery warning accompanies original untouched source bytes')

    def compare(self, policy):
        return super().compare(policy) + differences(*self.observed, 'rule.delivery-warning', policy)


def yaml_guards(binary, recorder):
    rows = [('H128', '- +.example.com\n', 'sequence-root'),
            ('H129', 'other: []\n', 'missing-payload'),
            ('H129', 'payload: {}\n', 'mapping-payload'),
            ('H129', 'payload: []\npayload: []\n', 'duplicate-payload'),
            ('H130', 'payload: &kept [+.example.com]\n', 'anchor'),
            ('H130', 'other: &kept [+.example.com]\npayload: *kept\n', 'alias'),
            ('H131', 'payload: [17]\n', 'non-string-entry'),
            ('H131', 'payload: [[+.example.com]]\n', 'nested-entry'),
            ('H131', 'payload:\n  - |\n    +.example.com\n', 'block-entry'),
            ('H132', '{payload: [+.example.com], extra: first, extra: second}\n', 'duplicate-extra-flow-key'),
            ('H133', '{payload: [+.example.com], extra: "first\n second"}\n', 'multiline-extra-flow-value')]
    for case, content, variant in rows:
        for scoped in (False, True):
            rule = {**DOMAIN, 'format': 'yaml', 'content': content}
            with Pair(binary, recorder, options={'shared': {'rule_library': [rule]}}) as pair:
                for directory in pair.dirs:
                    path = content_cache(directory, DOMAIN['id'])
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_bytes(content.encode())
                template = '/api/profiles/<profile_id>/rules/local/<name>' if scoped else '/api/rules/local/<name>'
                path = ('/api/profiles/default/rules/local/' if scoped else '/api/rules/local/') + quote(DOMAIN['name'], safe='')
                pair.request(case, 'GET ' + template, path + '?target=mihomo', auth='', expect=200, writes=True,
                    interaction=Delivery(pair), raw_body=True,
                    check=lambda reply: require(reply.body == content.encode(), 'ambiguous YAML is delivered byte-for-byte, never repaired or truncated'),
                    variant=variant)


GROUPS = {'rule-delivery-yaml-helpers': yaml_guards}
