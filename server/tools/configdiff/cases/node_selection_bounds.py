"""Source-key selection, complete batch limits and damaged history files."""
import copy
import json

from compare import database
from harness import Pair, json_body
from probe import Exchange
from substore import SubStore, NodeBatchExchange
from cases.nodes import NODE
from cases.probes import policy_for
from cases.setup import Stable
from cases.subscriptions import require

ROUTE = 'POST /api/nodes/latency'


def damaged_history(binary, recorder):
    for variant, raw in [('invalid-JSON', b'{broken'), ('array-root', b'[]'),
                         ('old-method-filtered', b'{"legacy":{"method":"tcp_v1","latency":7}}')]:
        with Pair(binary, recorder) as pair:
            for directory in pair.dirs:
                (directory / 'shared/node_latency.json').write_bytes(raw)
            pair.request('R097-B01', 'GET /api/nodes/latency', expect=200, writes=True,
                interaction=Stable(pair), check=lambda reply: require(reply.json() == {'success': True, 'results': {}},
                    'bad/obsolete result history returns empty without repairing or overwriting the original file'), variant=variant)


def reference(pair, keys, count):
    before = database(pair.dirs[0])
    def check(reply):
        value = reply.json()
        require(value['success'] is True and value['missing'] == [] and set(value['results']) == set(keys),
                'every requested source key remains independently represented even if display names coincide')
        require(all(row['latency'] == 7 and row['history'] == [7] and row['method'] == 'proxy_http_v2'
                    for row in value['results'].values()), 'complete fresh latency and one-element history for every selected key')
        saved = json.loads((pair.dirs[0] / 'shared/node_latency.json').read_text())
        require(saved == value['results'] and len(saved) == count and database(pair.dirs[0]) == before,
                'persisted results equal public full records, with exact all database rows unchanged')
    return check


def selected(binary, recorder):
    for count, case, variant in [(2, 'R098-B01', 'same-unicode-display-name-distinct-source-keys'),
                                  (20, 'R098-B02', 'exact-twenty-structured-node-boundary')]:
        nodes = [{**copy.deepcopy(NODE), 'id': 'bound-' + str(i),
                  'name': '同名中文🧪' if count == 2 else '边界中文🧪-' + str(i)} for i in range(count)]
        keys = [json.dumps(['node', node['id']], ensure_ascii=False, separators=(',', ':')) for node in nodes]
        with Pair(binary, recorder, options={'shared': {'nodes': nodes}, 'probe_case': 'valid'}) as pair:
            policy_for(pair, keys)
            pair.request(case, ROUTE, body=json_body({'keys': keys}), expect=200, writes=True,
                interaction=Exchange(pair), check=reference(pair, keys, count), variant=variant)


class ConvertedBatch(Exchange):
    def __init__(self, pair, store, contents):
        super().__init__(pair)
        self.conversions = NodeBatchExchange(store, contents)

    def before(self, index):
        super().before(index)
        self.conversions.before(index)

    def after(self, index, reply):
        super().after(index, reply)
        self.conversions.after(index, reply)

    def compare(self, policy):
        return super().compare(policy) + self.conversions.compare(policy)


def five_conversions(binary, recorder):
    contents = ['Synthetic-' + str(i) + ' = snell, 192.0.2.1, 443, psk=synthetic, version=4' for i in range(5)]
    nodes = [{'id': 'opaque-' + str(i), 'name': '转换中文🧪-' + str(i), 'proxy_string': raw}
             for i, raw in enumerate(contents)]
    keys = [json.dumps(['node', node['id']], separators=(',', ':')) for node in nodes]
    with SubStore() as store, Pair(binary, recorder, options={'shared': {'nodes': nodes}, 'probe_case': 'valid',
            'environment': {'SUB_STORE_URL': store.url}}) as pair:
        policy_for(pair, keys)
        pair.request('R098-B02', ROUTE, body=json_body({'keys': keys}), expect=200, writes=True,
            interaction=ConvertedBatch(pair, store, contents), check=reference(pair, keys, 5),
            variant='exact-five-concurrent-Sub-Store-conversions-boundary')


GROUPS = {'node-history-corruption': damaged_history, 'node-selection-bounds': selected,
          'node-five-conversion-boundary': five_conversions}
