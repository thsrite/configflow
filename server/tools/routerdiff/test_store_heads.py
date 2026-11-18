"""Content-bound commit identities, with explicit corruption counterexamples."""
import copy
import unittest

from compare import differences
from store_heads import StoreHeads, head


def snapshot(commit='initial-commit', generation=1, value='initial'):
    return {'database': {
        'meta': {'columns': ['key', 'value'], 'rows': [['store_id', 'synthetic-store'],
            ['generation', generation], ['commit_id', commit], ['root_order', ['profiles', 'system']],
            ['opaque_metadata', 'keep']]},
        'sections': {'columns': ['scope', 'owner', 'position'], 'rows': [['profile', 'alpha', 0]]},
        'fields': {'columns': ['scope', 'owner', 'field', 'position', 'value', 'revision'], 'rows': [
            ['profile', 'alpha', 'value', 0, value, generation]]}},
        'files': {}, 'filesystem': {}, 'fake': {'ros': {}, 'ikuai': {}, 'agent': {}, 'transcript': [], 'errors': []}}


def transition(python=None, go=None):
    before = snapshot()
    return {'python': {'before': copy.deepcopy(before), 'after': python or snapshot('python-next', 2, 'changed')},
            'go': {'before': copy.deepcopy(before), 'after': go or snapshot('go-next', 2, 'changed')}}


def compare(heads, states):
    return heads.pair(states, {side: state['after'] for side, state in states.items()})


class HeadTests(unittest.TestCase):
    def test_only_equal_content_and_head_get_a_pair_and_raw_data_remains_intact(self):
        heads, states = StoreHeads(), transition()
        original = copy.deepcopy(states)
        result, found = compare(heads, states)
        self.assertEqual(found, [])
        self.assertEqual(differences(result['python'], result['go']), [])
        self.assertNotEqual(head(states['python']['after'])[2], head(states['go']['after'])[2])
        self.assertEqual(states, original)
        # An unchanged read must retain the established own token/content.
        reads = {side: {'before': value['after'], 'after': value['after']} for side, value in states.items()}
        again, found = compare(heads, reads)
        self.assertEqual(found, [])
        self.assertEqual(again, result)
        self.assertEqual(len(heads.aliases), 1)

    def test_wrong_content_meta_generation_schema_and_unrelated_tokens_never_get_hidden(self):
        for kind in ('field', 'revision', 'opaque-meta', 'store', 'generation', 'schema'):
            states = transition()
            native = states['go']['after']['database']
            if kind == 'field':
                native['fields']['rows'][0][4] = 'wrong'
            elif kind == 'revision':
                native['fields']['rows'][0][5] = 9
            elif kind == 'opaque-meta':
                native['meta']['rows'][-1][1] = 'corrupted'
            elif kind == 'store':
                native['meta']['rows'][0][1] = 'another-store'
            elif kind == 'generation':
                native['meta']['rows'][1][1] = 3
            else:
                native['fields']['columns'].append('unexpected-column')
            projected, found = compare(StoreHeads(), states)
            self.assertTrue(found or differences(projected['python'], projected['go']), kind)
        states = transition()
        for side in states:
            # A coincidentally identical-looking identity inside file bytes is
            # still actual content and cannot earn a generic string replacement.
            states[side]['after']['files']['literal.txt'] = side + '-next'
        projected, found = compare(StoreHeads(), states)
        self.assertEqual(found, [])
        self.assertEqual([item.path for item in differences(projected['python'], projected['go'])], ['/files/literal.txt'])

    def test_invalid_native_heads_are_differences_while_reference_errors_remain_fixtures(self):
        for field, value in (('store_id', None), ('generation', True), ('generation', -1),
                             ('generation', 1), ('commit_id', ''), ('commit_id', 3), ('commit_id', 'initial-commit')):
            states = transition()
            for row in states['go']['after']['database']['meta']['rows']:
                if row[0] == field:
                    row[1] = value
            _, found = compare(StoreHeads(), states)
            self.assertTrue(found, (field, value))
            self.assertEqual(found[0].category, 'store-head')
        states = transition()
        states['go']['after']['database']['meta']['rows'].append(['commit_id', 'duplicate'])
        self.assertTrue(compare(StoreHeads(), states)[1])
        states = transition()
        states['python']['after']['database']['meta']['rows'][1][1] = False
        with self.assertRaises(ValueError):
            compare(StoreHeads(), states)

    def test_reused_token_and_rewritten_generation_are_not_new_baselines(self):
        for change in ('reused-token', 'rotated-token', 'same-generation-content'):
            heads, states = StoreHeads(), transition()
            self.assertEqual(compare(heads, states)[1], [])
            reads = {side: {'before': copy.deepcopy(value['after']), 'after': copy.deepcopy(value['after'])}
                     for side, value in states.items()}
            if change == 'reused-token':
                reads['python']['after'] = snapshot('python-third', 3, 'third')
                reads['go']['after'] = snapshot('go-next', 3, 'third')
            elif change == 'rotated-token':
                for key in ('before', 'after'):
                    reads['go'][key]['database']['meta']['rows'][2][1] = 'rotated-between-requests'
            else:
                for key in ('before', 'after'):
                    reads['go'][key]['database']['fields']['rows'][0][4] = 'corrupted-between-requests'
            _, found = compare(heads, reads)
            self.assertTrue(found, change)

    def test_raw_content_provenance_survives_narrow_clock_or_router_projection(self):
        heads = StoreHeads()
        states = transition(snapshot('python-next', 2, {'known-clock': 'python-fresh-clock'}),
                            snapshot('go-next', 2, {'known-clock': 'go-fresh-clock'}))
        copies = {side: copy.deepcopy(value['after']) for side, value in states.items()}
        for value in copies.values():
            value['database']['fields']['rows'][0][4]['known-clock'] = '<independently validated clock>'
        projected, found = heads.pair(states, copies)
        self.assertEqual(found, [])
        self.assertEqual(differences(projected['python'], projected['go']), [])
        # History binds the original raw content, never a mutable projected copy.
        reads = {side: {'before': state['after'], 'after': state['after']} for side, state in states.items()}
        self.assertEqual(heads.pair(reads, copies)[1], [])
        broken = copy.deepcopy(reads)
        broken['go']['before']['database']['fields']['rows'][0][4]['known-clock'] = 'unexpected'
        self.assertTrue(heads.pair(broken, copies)[1])

    def test_legacy_null_empty_or_missing_commit_is_readable_but_never_aliased(self):
        for kind in ('null', 'empty', 'missing'):
            before = snapshot()
            if kind == 'missing':
                before['database']['meta']['rows'].pop(2)
            else:
                before['database']['meta']['rows'][2][1] = None if kind == 'null' else ''
            states = {side: {'before': copy.deepcopy(before), 'after': copy.deepcopy(before)} for side in ('python', 'go')}
            heads = StoreHeads()
            projected, found = compare(heads, states)
            self.assertEqual(found, [])
            self.assertEqual(projected['python'], before)
            self.assertEqual(heads.aliases, {})
            states = {'python': {'before': before, 'after': snapshot('python-new', 2, 'changed')},
                      'go': {'before': before, 'after': snapshot('go-new', 2, 'changed')}}
            projected, found = compare(heads, states)
            self.assertEqual(found, [])
            self.assertEqual(differences(projected['python'], projected['go']), [])

    def test_metadata_only_transactions_keep_the_real_head_but_meta_stays_compared(self):
        heads = StoreHeads()
        before = snapshot()
        after = copy.deepcopy(before)
        after['database']['meta']['rows'][-1][1] = 'changed metadata only'
        states = {side: {'before': before, 'after': after} for side in ('python', 'go')}
        projected, found = compare(heads, states)
        self.assertEqual(found, [])
        self.assertEqual(differences(projected['python'], projected['go']), [])
        self.assertEqual(head(before)[:3], head(after)[:3])
        self.assertEqual(head(before)[3], head(after)[3])


if __name__ == '__main__':
    unittest.main()
