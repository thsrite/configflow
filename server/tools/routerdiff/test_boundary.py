"""Native public-process prerequisites; never product implementation imports."""
import base64
import hashlib
import json
import sqlite3
import os
from pathlib import Path
import unittest

from harness import Pair
from seed import DEVICE_PASSWORD, router
from fixtures import agent_peers, agent_options
import returns


@unittest.skipUnless(os.environ.get('ROUTERDIFF_BINARY'), 'requires .32 native public Go binary')
class BoundaryTests(unittest.TestCase):
    def test_routeros_refuses_runtime_fields_and_connected_row_writes(self):
        with Pair(Path(os.environ['ROUTERDIFF_BINARY'])) as pair:
            side = pair.python
            headers = {'Authorization': 'Basic ' + base64.b64encode(('test-admin:' + DEVICE_PASSWORD).encode()).decode(),
                       'Content-Type': 'application/json'}
            row = {'.id': '*D0', 'dst-address': '10.44.0.0/24', 'gateway': 'bridge-lan',
                   'connect': 'true', 'active': 'true'}
            side.rpc('fake', action='seed', ros={'/ip/route': [row]})
            before = side.rpc('snapshot')['fake']['ros']
            for method, path, payload in (
                ('PUT', '/rest/ip/route', {'dst-address': '10.55.0.0/24', '.id': '*D1'}),
                ('PUT', '/rest/ip/route', {'dst-address': '10.55.0.0/24', 'active': 'true'}),
                ('PUT', '/rest/ip/firewall/nat', {'chain': 'srcnat', 'action': 'accept', 'bytes': '42'}),
                ('PUT', '/rest/ip/dhcp-server/lease', {'address': '10.44.0.21', 'status': 'bound'}),
                ('PATCH', '/rest/ip/route/*D0', {'disabled': 'true'}),
                ('DELETE', '/rest/ip/route/*D0', None),
            ):
                response = side.rpc('request', port=18081, method=method, path=path, headers=headers,
                    body=base64.b64encode(json.dumps(payload).encode()).decode() if payload is not None else None)
                self.assertEqual(response['status'], 400)
                self.assertEqual(side.rpc('snapshot')['fake']['ros'], before)
            self.assertEqual(side.rpc('snapshot')['fake']['errors'], [])

    def test_stored_legacy_missing_profile_is_rejected_before_handlers(self):
        with Pair(Path(os.environ['ROUTERDIFF_BINARY']), agent_options()) as pair:
            side = pair.python
            side.rpc('stop')
            # Owned synthetic bytes only. This models a legacy/corrupt stored
            # row without bypassing validation or patching application helpers.
            with sqlite3.connect(side.directory / 'data/configflow.sqlite3') as database:
                raw = database.execute("SELECT value FROM fields WHERE scope='system' AND owner='' AND field='agents'").fetchone()[0]
                agents = json.loads(raw)
                agents[0].pop('profile_id')
                database.execute("UPDATE fields SET value=? WHERE scope='system' AND owner='' AND field='agents'",
                                 (json.dumps(agents, ensure_ascii=False),))
            with self.assertRaisesRegex(RuntimeError, 'python exited'):
                side.rpc('start')
            self.assertIn('绑定的配置不存在', side.rpc('logs', limit=6000))

    def test_agent_peer_requires_own_bearer_and_persists_its_own_state(self):
        with Pair(Path(os.environ['ROUTERDIFF_BINARY'])) as pair:
            for side in pair.sides.values():
                side.rpc('fake', action='seed', agent=agent_peers())
            def call(side, method, path, token):
                raw = b'{}' if method == 'POST' else None
                reply = side.rpc('request', port=18083, method=method, path=path,
                    headers={'Host': '10.44.0.2:18083', 'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'},
                    body=base64.b64encode(raw).decode() if raw is not None else None)
                return reply, json.loads(base64.b64decode(reply['body']))
            token = agent_peers()['10.44.0.2:18083']['token']
            self.assertEqual(call(pair.python, 'GET', '/api/linux-gateway/status', 'wrong-token')[0]['status'], 401)
            self.assertEqual(call(pair.python, 'POST', '/api/linux-gateway/forwarding', token)[1]['ipv4_forwarding']['current'], '1')
            self.assertTrue(call(pair.python, 'GET', '/api/linux-gateway/status', token)[1]['ipv4_forwarding']['persisted'])
            self.assertEqual(call(pair.go, 'GET', '/api/linux-gateway/status', token)[1]['ipv4_forwarding']['current'], '0')

    def test_return_peer_negotiates_allocation_and_preserves_foreign_resources(self):
        with Pair(Path(os.environ['ROUTERDIFF_BINARY'])) as pair:
            for side in pair.sides.values():
                side.rpc('fake', **returns.peer(returns.status()))
            side = pair.python
            headers = {'Host': '10.44.0.2:18083', 'Authorization': 'Bearer ' + returns.peer(returns.status())['agent']['10.44.0.2:18083']['token'],
                       'Content-Type': 'application/json'}
            def call(method, path='/api/return-routing', document=None):
                reply = side.rpc('request', port=18083, method=method, path=path, headers=headers,
                    body=base64.b64encode(json.dumps(document).encode()).decode() if document is not None else None)
                return reply['status'], json.loads(base64.b64decode(reply['body']))
            spec = {'router_ip': '10.44.0.1', 'table': 104, 'priority': 105, 'mark': '0x400000', 'safety_version': 2}
            self.assertEqual(call('PUT', document={**spec, 'table': 106})[0], 409)
            self.assertIsNone(call('GET')[1]['managed'])
            self.assertEqual(call('PUT', document=spec)[0], 200)
            after = call('GET')[1]
            self.assertEqual(after['managed']['spec'], {'proxy_dst': None, **spec})
            self.assertTrue(after['managed']['active'])
            self.assertEqual(after['return_tables'][0], returns.FOREIGN_TABLE)
            self.assertEqual(after['fwmark_rules'][0], returns.FOREIGN_RULE)
            self.assertEqual(after['fwmark_rules'][1], {'priority': '105', 'table': '104', 'mark': '0x400000/0x400000'})
            self.assertEqual(call('DELETE', '/api/return-routing?restore_legacy=1')[0], 409)
            self.assertEqual(call('GET')[1], after)
            self.assertEqual(call('DELETE')[0], 200)
            cleaned = call('GET')[1]
            self.assertFalse(cleaned['managed']['active'])
            self.assertEqual(cleaned['return_tables'], [returns.FOREIGN_TABLE])
            self.assertEqual(cleaned['fwmark_rules'], [returns.FOREIGN_RULE])
            self.assertIsNone(pair.go.rpc('snapshot')['fake']['agent']['10.44.0.2:18083']['endpoints']['/api/return-routing']['managed'])

    def test_real_startup_independent_stateful_peers_and_transcript_gate(self):
        options = {'alpha': {'routers': {'items': [router()]}}}
        with Pair(Path(os.environ['ROUTERDIFF_BINARY']), options) as pair:
            # Only the production entry's observer, never routes.tsv's stale todo.
            manifest = pair.go.rpc('manifest')
            self.assertIn('GET\t/api/auth/status', manifest)
            sides = [pair.python]
            if 'POST\t/api/routers/<router_id>/probe\n' in manifest:
                sides.append(pair.go)
            for side in sides:
                self.assertEqual(side.request('GET', '/api/routers')['status'], 200)
                side.rpc('fake', action='fault', match={'peer': 'ros', 'method': 'GET',
                    'path': '/rest/system/resource'}, gate='probe-started', status=200,
                    body={'version': '7.24.5', 'board-name': 'synthetic-gated-router'})
                side.rpc('async', ident='probe', method='POST', path='/api/routers/r-ros/probe',
                    headers={'Authorization': 'Bearer ' + side.token}, body=None)
                side.rpc('fake', action='wait', gate='probe-started')
                busy = side.request('PUT', '/api/routers/r-ros', document=[])
                self.assertEqual(busy['status'], 409, busy['content'])
                side.rpc('fake', action='release', gate='probe-started')
                probe = side.rpc('join', ident='probe')
                self.assertEqual(probe['status'], 200, base64.b64decode(probe['body']))
                snapshot = side.rpc('snapshot')
                self.assertEqual(snapshot['fake']['errors'], [])
                self.assertGreaterEqual(len(snapshot['fake']['transcript']), 4)
            # Mutating one fake cannot change the other fake's table.
            pair.python.rpc('fake', action='seed', ros={'/ip/route': [{'.id': '*FF', 'comment': 'foreign-python-only'}]})
            self.assertEqual(pair.go.rpc('snapshot')['fake']['ros']['/ip/route'], [])

    def test_fake_vendor_state_session_rejection_and_lost_acknowledgement(self):
        with Pair(Path(os.environ['ROUTERDIFF_BINARY'])) as pair:
            for side in pair.sides.values():
                def call(port, path, document, headers=None, method='POST'):
                    reply = side.rpc('request', port=port, method=method, path=path,
                        headers={'Content-Type': 'application/json', **(headers or {})},
                        body=base64.b64encode(json.dumps(document).encode()).decode())
                    return json.loads(base64.b64decode(reply['body']))
                basic = {'Authorization': 'Basic ' + base64.b64encode(('test-admin:' + DEVICE_PASSWORD).encode()).decode()}
                side.rpc('fake', action='fault', match={'peer': 'ros', 'path': '/rest/system/resource'}, status=200, body=None)
                self.assertIsNone(call(18081, '/rest/system/resource', None, basic, 'GET'))
                row = call(18081, '/rest/ip/route', {'dst-address': '10.55.0.0/24',
                    'gateway': '10.44.0.2', 'comment': 'foreign🧪'}, basic, 'PUT')
                self.assertRegex(row['.id'], r'^\*[0-9A-F]+$')
                self.assertEqual(call(18081, '/rest/ip/route/' + row['.id'], None, basic, 'GET')['comment'], 'foreign🧪')
                call(18081, '/rest/ip/route/' + row['.id'], {'comment': 'updated'}, basic, 'PATCH')
                self.assertEqual(side.rpc('snapshot')['fake']['ros']['/ip/route'][0]['comment'], 'updated')
                first = call(18081, '/rest/ip/firewall/nat', {'chain': 'srcnat', 'action': 'accept',
                             'comment': 'foreign first'}, basic, 'PUT')
                second = call(18081, '/rest/ip/firewall/nat', {'chain': 'srcnat', 'action': 'accept',
                              'comment': 'new before foreign', 'place-before': first['.id']}, basic, 'PUT')
                ordered = side.rpc('snapshot')['fake']['ros']['/ip/firewall/nat']
                self.assertEqual([item['.id'] for item in ordered], [second['.id'], first['.id']])
                self.assertNotIn('place-before', ordered[0])
                side.rpc('fake', action='fault', match={'peer': 'ros', 'method': 'PUT', 'path': '/rest/ip/route'},
                         gate='actual-add-ack', apply=True, passthrough=True)
                side.rpc('async', ident='actual-add', port=18081, method='PUT', path='/rest/ip/route',
                         headers={**basic, 'Content-Type': 'application/json'},
                         body=base64.b64encode(json.dumps({'dst-address': '10.57.0.0/24',
                              'gateway': '10.44.0.2', 'comment': 'real gated acknowledgement'}).encode()).decode())
                side.rpc('fake', action='wait', gate='actual-add-ack')
                side.rpc('fake', action='release', gate='actual-add-ack')
                actual = side.rpc('join', ident='actual-add')
                self.assertEqual(actual['status'], 201)
                actual = json.loads(base64.b64decode(actual['body']))
                self.assertRegex(actual['.id'], r'^\*[0-9A-F]+$')
                self.assertEqual(actual['dst-address'], '10.57.0.0/24')
                login = {'username': 'test-admin', 'passwd': hashlib.md5(DEVICE_PASSWORD.encode()).hexdigest(),
                         'pass': base64.b64encode(('salt_11' + DEVICE_PASSWORD).encode()).decode()}
                self.assertEqual(call(18082, '/Action/login', login)['code'], 0)
                form = {'func_name': 'static_rt', 'action': 'add', 'param': {'tagname': 'synthetic-tag', 'enabled': 'yes'}}
                self.assertEqual(call(18082, '/Action/call', form)['code'], 1008)
                self.assertEqual(side.rpc('snapshot')['fake']['ikuai'].get('static_rt', []), [])
                cookie = {'Cookie': 'sess_key=synthetic-session-1'}
                self.assertEqual(call(18082, '/Action/call', form, cookie)['code'], 0)
                self.assertEqual(call(18082, '/Action/call', form, cookie)['code'], 3001)
                self.assertEqual(len(side.rpc('snapshot')['fake']['ikuai']['static_rt']), 1)
                side.rpc('fake', action='fault', match={'peer': 'ikuai', 'path': '/Action/call'},
                    body={'Result': 10000, 'code': 1008}, status=200)
                form['param']['tagname'] = 'refused-tag'
                self.assertEqual(call(18082, '/Action/call', form, cookie)['code'], 1008)
                self.assertEqual(len(side.rpc('snapshot')['fake']['ikuai']['static_rt']), 1)
                side.rpc('fake', action='fault', match={'peer': 'ikuai', 'path': '/Action/call'},
                    apply=True, raw='', status=200)
                reply = side.rpc('request', port=18082, method='POST', path='/Action/call',
                    headers={'Cookie': cookie['Cookie'], 'Content-Type': 'application/json'},
                    body=base64.b64encode(json.dumps(form).encode()).decode())
                self.assertEqual(base64.b64decode(reply['body']), b'')
                self.assertEqual(len(side.rpc('snapshot')['fake']['ikuai']['static_rt']), 2)
                side.rpc('fake', action='seed', ikuai={'dhcp_static': [{'id': 70, 'comment': 'owned synthetic row'}]})
                delete = {'func_name': 'dhcp_static', 'action': 'del', 'param': {'id': 70}}
                self.assertEqual(call(18082, '/Action/call', delete, cookie)['code'], 3001)
                delete['param']['id'] = '70'
                self.assertEqual(call(18082, '/Action/call', delete, cookie)['code'], 0)
                self.assertEqual(side.rpc('snapshot')['fake']['ikuai']['dhcp_static'], [])


if __name__ == '__main__':
    unittest.main()
