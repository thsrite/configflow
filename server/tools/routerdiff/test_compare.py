"""Deliberately broken observations must fail the actual case comparator."""
import base64
import copy
import unittest

from compare import Bijection, differences, response_view, logical_snapshot


class CompareTests(unittest.TestCase):
    def test_response_mutations_are_visible(self):
        body = b'{"success":true,"plan":{"fingerprint":"fixed","operations":["one","two"]}}'
        response = {'status': 200, 'headers': [('Content-Type', 'application/json'),
            ('Cache-Control', 'no-store'), ('X-ConfigFlow-Profile', 'alpha'), ('Content-Length', str(len(body)))],
            'body': base64.b64encode(body).decode(), 'kind': 'json',
            'content': {'success': True, 'plan': {'fingerprint': 'fixed', 'operations': ['one', 'two']}}}
        original = response_view(response)
        for mutate in (
            lambda value: value.update(status=502),
            lambda value: value['body'].update(success=1),
            lambda value: value['body']['plan'].update(fingerprint='broken'),
            lambda value: value['body']['plan']['operations'].reverse(),
            lambda value: value['headers'].pop('cache-control'),
            lambda value: value['headers'].update({'x-configflow-profile': ['beta']}),
            lambda value: value['body'].update(message='中文被改🧪'),
        ):
            broken = copy.deepcopy(original)
            mutate(broken)
            self.assertTrue(differences(original, broken))
        self.assertEqual(differences(original, copy.deepcopy(original)), [])

    def test_storage_device_files_and_foreign_objects_are_not_masked(self):
        snapshot = {'database': {'fields': {'columns': ['scope', 'owner', 'field', 'position', 'value', 'revision'],
            'rows': [['profile', 'beta', 'routers', 0, '{"items":[]}', 7]]}},
            'files': {'router-rule-sync/exact-digest.json': 'e30='},
            'fake': {'ros': {'/ip/route': [{'.id': '*A', 'comment': 'foreign'}]}, 'transcript': [], 'errors': []}}
        original = logical_snapshot(snapshot)
        for mutate in (
            lambda value: value['database']['fields']['rows'][0].__setitem__(5, 8),
            lambda value: value['database']['fields']['rows'][0].__setitem__(4, {'items': [{}]}),
            lambda value: value['files'].clear(),
            lambda value: value['fake']['ros']['/ip/route'][0].update(comment='changed'),
        ):
            broken = copy.deepcopy(original)
            mutate(broken)
            self.assertTrue(differences(original, broken))

    def test_allow_order_only_normalization(self):
        def view(value):
            return response_view({'status': 405, 'headers': [('Allow', value)],
                                  'body': '', 'kind': 'text', 'content': ''})
        original = view('GET, HEAD, OPTIONS, POST')
        self.assertEqual(differences(original, view('POST, OPTIONS, GET, HEAD')), [])
        for broken in ('GET, HEAD, OPTIONS', 'get, HEAD, OPTIONS, POST',
                       'GET, GET, HEAD, OPTIONS, POST', 'GET, HEAD, OPTIONS, PUT'):
            self.assertTrue(differences(original, view(broken)))
        with self.assertRaises(ValueError):
            view('GET, , POST')

    def test_bijection_rejects_invalid_or_colliding_ids(self):
        identities = Bijection(r'router_[0-9a-f]{12}')
        identities.pair('router_000000000001', 'router_000000000002')
        identities.pair('router_000000000001', 'router_000000000002')
        for left, right in [('bad', 'router_000000000002'),
                            ('router_000000000003', 'router_000000000002'),
                            ('router_000000000001', 'router_000000000004')]:
            with self.assertRaises(ValueError):
                identities.pair(left, right)


if __name__ == '__main__':
    unittest.main()
