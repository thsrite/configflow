"""Runner control-flow self-checks, not substitutes for HTTP product recipes."""
import base64
import copy
import json
from types import SimpleNamespace
import unittest

from cases import Case, Ref
from inventory import load
from run import execute, shape


class ObservationSide:
    def __init__(self, status=200, before=None, after=None):
        self.observations, self.pending, self.missing_effects, self.requests = {}, {}, [], []
        self.status, self.snapshots = status, 0
        self.before = before or {'database': {'fields': {'columns': ['scope', 'owner', 'field', 'position', 'value', 'revision'],
            'rows': []}}, 'files': {}, 'fake': {'ros': {}, 'ikuai': {'static_rt': []}, 'agent': {}, 'transcript': [], 'errors': []}}
        self.after = after or self.before

    def rpc(self, operation, **values):
        if operation != 'snapshot':
            raise AssertionError('runner self-check unexpectedly calls ' + operation)
        value = self.before if self.snapshots == 0 else self.after
        self.snapshots += 1
        return copy.deepcopy(value)

    def request(self, **values):
        self.requests.append(values)
        content = {'success': self.status < 400}
        raw = json.dumps(content).encode()
        return {'status': self.status, 'headers': [('Content-Type', 'application/json'), ('Content-Length', str(len(raw)))],
                'body': base64.b64encode(raw).decode(), 'content': content, 'kind': 'json'}


def pair(python=None, go=None):
    sides = {'python': python or ObservationSide(), 'go': go or ObservationSide()}
    return SimpleNamespace(sides=sides, **sides)


class RunnerTests(unittest.TestCase):
    def test_transaction_order_precondition_rejects_forward_undo(self):
        original = ObservationSide().before
        for actions, valid in ((['add-one', 'add-two', 'undo-two', 'undo-one'], True),
                               (['add-one', 'add-two', 'undo-one', 'undo-two'], False)):
            with self.subTest(actions=actions):
                after = copy.deepcopy(original)
                after['fake']['transcript'] = [{'method': 'POST', 'path': action, 'body': {}, 'headers': {}, 'raw': ''}
                                              for action in actions]
                world = pair(ObservationSide(200, original, after))
                case = Case('rollback-order', 'R005', 'POST', '/api/client-devices/fixed-ip/apply', 200,
                            unchanged=False, request_order=tuple({'path': action} for action in
                            ('add-one', 'add-two', 'undo-two', 'undo-one')))
                if valid:
                    self.assertEqual(execute(world, case, set(), load()[0])['status'], 'go-unimplemented')
                else:
                    with self.assertRaisesRegex(RuntimeError, 'transaction order mismatch'):
                        execute(world, case, set(), load()[0])

    def test_missing_go_response_reference_never_falls_back_to_python(self):
        routes, _ = load()
        world = pair()
        world.python.observations['previous'] = {'body': {'token': 'python-only'}}
        case = Case('dependent', 'R001', 'GET', '/api/agents/test/upstream-routers', 200,
                    document={'token': Ref('previous', '/body/token')})
        result = execute(world, case, {('GET', shape(routes['R001'].path))}, routes)
        self.assertEqual(result['status'], 'blocked-by-difference')
        self.assertEqual(world.python.requests[0]['document']['token'], 'python-only')
        self.assertEqual(world.go.requests, [])

    def test_unimplemented_write_cannot_poison_a_later_implemented_read(self):
        routes, _ = load()
        world = pair()
        available = {('GET', shape(routes['R007'].path))}
        write = Case('missing-write', 'R020', 'POST', '/api/routers/test/routes/apply', 200,
                     unchanged=False, effect='device-only')
        self.assertEqual(execute(world, write, available, routes)['status'], 'go-unimplemented')
        read = Case('later-read', 'R007', 'GET', '/api/routers', 200)
        result = execute(world, read, available, routes)
        self.assertEqual(result['status'], 'go-unimplemented')
        self.assertEqual(result['unimplemented_reason']['prior_missing_mutations'], ['missing-write'])
        self.assertEqual(world.go.requests, [])

    def test_reproducing_python_residue_is_a_go_safety_failure(self):
        routes, _ = load()
        original = ObservationSide().before
        residue = copy.deepcopy(original)
        residue['fake']['ikuai']['static_rt'] = [{'id': 1, 'dst_addr': '198.18.0.0'}]
        world = pair(ObservationSide(502, original, residue), ObservationSide(502, original, residue))
        case = Case('suffix-bug', 'R011', 'POST', '/api/routers/test/apply', 502,
                    unchanged=False, effect='device-only', python_bug='IKUAI_NETMASK_SUFFIX_ROLLBACK')
        result = execute(world, case, {('POST', shape(routes['R011'].path))}, routes)
        self.assertEqual(result['status'], 'different')
        self.assertTrue(any(item['category'] == 'safety' for item in result['differences']))

    def test_clean_failure_on_python_bug_is_recorded_for_review_not_passed(self):
        routes, _ = load()
        original = ObservationSide().before
        residue = copy.deepcopy(original)
        residue['fake']['ikuai']['static_rt'] = [{'id': 1, 'dst_addr': '198.18.0.0'}]
        world = pair(ObservationSide(502, original, residue), ObservationSide(502, original, original))
        case = Case('suffix-clean', 'R011', 'POST', '/api/routers/test/apply', 502,
                    unchanged=False, effect='device-only', python_bug='IKUAI_NETMASK_SUFFIX_ROLLBACK')
        result = execute(world, case, {('POST', shape(routes['R011'].path))}, routes)
        self.assertEqual(result['status'], 'reference-bug-observation')
        self.assertEqual(result['go_bug_outcome']['created_static_rows_remaining'], 0)
        self.assertTrue(result['differences'])


if __name__ == '__main__':
    unittest.main()
