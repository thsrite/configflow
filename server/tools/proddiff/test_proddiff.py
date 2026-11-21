"""Synthetic regressions for the acceptance gate (run on the test server)."""
import importlib.util
import contextlib
import hashlib
import io
import os
import json
import tempfile
from types import SimpleNamespace
import zipfile
from email.message import Message
from pathlib import Path
from unittest import mock
import unittest
import sys
sys.path.insert(0, str(Path(__file__).parent))

spec = importlib.util.spec_from_file_location('proddiff', Path(__file__).with_name('run.py'))
pd = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pd)

JSON = 'application/json'


def reply(body, content_type=JSON, status=200, headers=None):
    return (status, content_type, body if isinstance(body, bytes) else json.dumps(body).encode(), headers or {})


class ComparatorTests(unittest.TestCase):
    def test_o9_bootstrap_and_security_headers_compose_exactly(self):
        token = 'x' * 43
        before = b'services:\n'
        after = before + ('      - AGENT_MIHOMO_REGISTRATION_TOKEN=' + token + '\n').encode()
        left = reply(before, 'text/plain; charset=utf-8', headers={
            'access-control-allow-origin': '*', 'vary': 'Origin, Accept-Encoding'})
        headers = {'cache-control': 'no-store', 'x-content-type-options': 'nosniff',
                   'vary': 'Accept-Encoding'}
        route = '/api/agents/docker-agent-compose'
        self.assertEqual([], pd.response_diffs(left, reply(after, left[1], headers=headers), route, []))
        for changed in ({**headers, 'cache-control': 'private'},
                        {**headers, 'x-content-type-options': 'unsafe'},
                        {**headers, 'access-control-allow-origin': '*'}):
            self.assertTrue(pd.response_diffs(left, reply(after, left[1], headers=changed), route, []))
        self.assertTrue(pd.response_diffs(left, reply(after + b'changed', left[1], headers=headers), route, []))

    def test_o9_security_header_delta_is_exact(self):
        source = {'access-control-allow-origin': '*', 'access-control-allow-methods': 'GET, POST',
                  'access-control-allow-headers': 'Authorization', 'vary': 'Origin, Accept-Encoding',
                  'cache-control': 'no-cache'}
        target = {'vary': 'Accept-Encoding', 'cache-control': 'no-cache', 'x-content-type-options': 'nosniff'}
        left = reply(b'rule text', 'text/plain; charset=utf-8', headers=source)
        right = reply(left[2], left[1], headers=target)
        self.assertEqual([], pd.response_diffs(left, right, '/api/rules/local/<name>', []))
        for name, value in [('access-control-allow-origin', '*'), ('x-content-type-options', 'unsafe'),
                            ('cache-control', 'private'), ('vary', 'Accept-Encoding, Cookie')]:
            with self.subTest(name=name):
                bad = reply(left[2], left[1], headers={**target, name: value})
                self.assertTrue(pd.response_diffs(left, bad, '/api/rules/local/<name>', []))
        self.assertTrue(pd.response_diffs(left, reply(b'wrong', left[1], headers=target), '/api/rules/local/<name>', []))
        self.assertTrue(pd.response_diffs(left, reply(left[2], left[1], status=403, headers=target), '/api/rules/local/<name>', []))
    def test_o9_bootstrap_installer_fields_are_exact(self):
        token = 'x' * 43
        for route, before, after in [
            ('/api/agents/docker-agent-compose', b'services:\n',
             ('services:\n      - AGENT_MIHOMO_REGISTRATION_TOKEN=' + token + '\n').encode()),
            ('/api/agents/docker-agent-run', b'docker run\n',
             ('docker run\n  -e AGENT_MIHOMO_REGISTRATION_TOKEN=' + token + ' ' + chr(92) + '\n').encode()),
        ]:
            left = reply(before, 'text/plain; charset=utf-8')
            right = reply(after, 'text/plain; charset=utf-8', headers={'cache-control': 'no-store'})
            self.assertEqual([], pd.response_diffs(left, right, route, []))
            self.assertTrue(pd.response_diffs(left, right, '/api/other', []))
            self.assertTrue(pd.response_diffs(left, reply(after.replace(token.encode(), b'short'), 'text/plain; charset=utf-8'), route, []))
            self.assertTrue(pd.response_diffs(left, reply(after.replace(b'REGISTRATION_TOKEN', b'UNKNOWN_TOKEN'), 'text/plain; charset=utf-8'), route, []))
            self.assertTrue(pd.response_diffs(left, reply(after + b'changed', 'text/plain; charset=utf-8'), route, []))

    def test_o9_core_redactor_delta_preserves_existing_no_store(self):
        before = b'token|installation_token|secret|password'
        after = b'token|installation_token|registration_token|registration_key|secret|password'
        left = reply(before, 'text/plain; charset=utf-8', headers={'cache-control': 'no-store'})
        right = reply(after, 'text/plain; charset=utf-8', headers={'cache-control': 'no-store'})
        self.assertEqual([], pd.response_diffs(left, right, '/api/agents/core-install-script', []))
        self.assertTrue(pd.response_diffs(left, right, '/api/other', []))
        self.assertTrue(pd.response_diffs(left, reply(after, 'text/plain; charset=utf-8'), '/api/agents/core-install-script', []))

    def test_public_generated_file_schema_names_are_exact(self):
        for name in ('ETag', 'Last-Modified', 'Accept-Ranges'):
            self.assertEqual(name, pd.safe_header(name.lower()))
        self.assertEqual('config.yaml', pd.safe_key('config.yaml'))
        self.assertEqual('<key>', pd.safe_key('synthetic-private-file'))
        self.assertEqual('<key>', pd.safe_header('synthetic-private-header'))

    def test_c07_readiness_exception_is_exact_and_preserves_headers(self):
        for route, field, message in [('/api/tasks', 'tasks', '任务模块尚未就绪：'),
                                       ('/api/notifications', 'items', '通知模块尚未就绪：')]:
            before = {'success': True, field: []}
            if field == 'items':
                before['checked_at'] = '2026-11-18T12:00:00+00:00'
            after = {'success': False, 'message': message + 'router_rule_sync、agent',
                     'missing_slots': ['router_rule_sync', 'agent']}
            left = reply(before, headers={'cache-control': 'no-store'})
            right = reply(after, status=503, headers={'cache-control': 'no-store'})
            self.assertEqual([], pd.response_diffs(left, right, route, []))
            self.assertTrue(pd.response_diffs(left, reply(after, status=503), route, []))
            for mutation in [dict(after, success=True), dict(after, message='wrong'),
                             dict(after, missing_slots=[]), dict(after, future='extra'),
                             dict(after, missing_slots=['router_rule_sync', 'agent', 'metrics_history'])]:
                self.assertTrue(pd.response_diffs(left, reply(mutation, status=503), route, []))
            self.assertTrue(pd.response_diffs(left, right, '/api/other', []))
            self.assertTrue(pd.response_diffs(reply(before, status=500), right, route, []))

    def test_installer_rollback_delta_is_fixed_and_other_script_bytes_stay_strict(self):
        approved = json.loads((pd.ROOT / 'server/internal/installer/installertest/l1-rollback-deltas.json').read_text())
        first, trap = approved['deltas'][:2]
        suffix = 'echo unchanged\n'
        original = '#!/bin/sh\n' + first['before'] + trap['before'] + suffix
        changed = ('#!/bin/sh\n' + first['after'].replace('{agent_install_rollback}', approved['module'])
                   + trap['after'] + suffix)
        route = '/api/agents/install-script'
        left = reply(original.encode(), 'text/plain; charset=utf-8')
        right = reply(changed.encode(), left[1])
        self.assertEqual([], pd.response_diffs(left, right, route, []))
        for mutation in [changed.replace(suffix, 'echo wrong\n'),
                         changed.replace('AGENT_ROLLBACK_KEEP=0', 'AGENT_ROLLBACK_KEEP=1')]:
            self.assertTrue(pd.response_diffs(left, reply(mutation.encode(), left[1]), route, []))
        self.assertTrue(pd.response_diffs(left, right, '/different-script', []))
        self.assertTrue(pd.response_diffs(left, reply(right[2], left[1], 500), route, []))

    def test_installer_compensation_wait_is_reversed_only_with_the_rollback_module(self):
        approved = json.loads((pd.ROOT / 'server/internal/installer/installertest/l1-rollback-deltas.json').read_text())
        wrapper = approved['rendered'][0]
        first = approved['deltas'][0]
        route = '/api/agents/install-script'
        original = '#!/bin/sh\n' + wrapper['before'] + first['before'] + ')\n'
        changed = ('#!/bin/sh\n' + wrapper['after'] + first['after'].replace('{agent_install_rollback}', approved['module'])
                   + ')\n')
        left = reply(original.encode(), 'text/plain; charset=utf-8')
        self.assertEqual([], pd.response_diffs(left, reply(changed.encode(), left[1]), route, []))
        for hook in approved['rendered'][1:]:
            with self.subTest(hook=hook['before']):
                self.assertEqual([], pd.response_diffs(reply((original + hook['before']).encode(), left[1]),
                                                       reply((changed + hook['after']).encode(), left[1]), route, []))
        # Without the D3 module (another installer) the wait trap stays a difference.
        bare = reply(('#!/bin/sh\n' + wrapper['after'] + ')\n').encode(), left[1])
        self.assertTrue(pd.response_diffs(reply(('#!/bin/sh\n' + wrapper['before'] + ')\n').encode(), left[1]), bare, route, []))

    def test_agent_pull_yaml_contract_and_mutations(self):
        route = '/api/agents/<agent_id>/config'
        left = reply({'content': 'plugins:\n- tag: example\n  type: sequence\n'})
        reformatted = reply({'content': 'plugins: [{tag: example, type: sequence}]\n'})
        self.assertEqual([], pd.response_diffs(left, reformatted, route, []))
        changed = reply({'content': 'plugins: [{tag: changed, type: sequence}]\n'})
        deleted = reply({'content': 'plugins: [{tag: example}]\n'})
        self.assertTrue(pd.response_diffs(left, changed, route, []))
        self.assertTrue(pd.response_diffs(left, deleted, route, []))
        # Scope: another route retains the previous text policy for this shape.
        other_left = reply({'content': 'plugins:\n- {tag: example}\n'})
        other_right = reply({'content': 'plugins: [{tag: example}]\n'})
        self.assertTrue(pd.response_diffs(other_left, other_right, '/api/other', []))
        surge = reply({'content': '[General]\nloglevel = warning\n'})
        changed_surge = reply({'content': '[General]\nloglevel = info\n'})
        self.assertTrue(pd.response_diffs(surge, changed_surge, route, []))

    def test_agent_pull_digest_is_verified_not_ignored(self):
        route = '/api/agents/<agent_id>/config'
        def payload(content):
            digest = hashlib.md5(content.encode()).hexdigest()
            return {'content': content, 'md5': digest, 'version': digest[:8]}
        a = payload('plugins:\n- tag: example\n')
        b = payload('plugins: [{tag: example}]\n')
        self.assertEqual([], pd.response_diffs(reply(a), reply(b), route, []))
        for field in ('md5', 'version'):
            broken = dict(b, **{field: 'wrong'})
            self.assertTrue(pd.response_diffs(reply(a), reply(broken), route, []))
        changed = payload('plugins: [{tag: changed}]\n')
        removed = payload('plugins: []\n')
        self.assertTrue(pd.response_diffs(reply(a), reply(changed), route, []))
        self.assertTrue(pd.response_diffs(reply(a), reply(removed), route, []))

    def test_final_generation_native_transport_requires_same_target(self):
        route = '/api/agents/<agent_id>/generate-config'
        py = reply({'success': False, 'message': "HTTPConnectionPool(host='agent.example.com', port=8080): Max retries exceeded with url: /api/capabilities (connection refused)"}, status=400)
        native = reply({'success': False, 'message': 'Get "http://agent.example.com:8080/api/capabilities": dial tcp: connection refused'}, status=400)
        self.assertEqual([], pd.response_diffs(py, native, route, []))
        for message in ('Get "http://other.example.com:8080/api/capabilities": dial tcp: refused',
                        'Get "http://agent.example.com:8080/api/other": dial tcp: refused',
                        'deploy: DNS HA pair publication is not available (A18)',
                        '目标设备配置已改变，请重新生成最终配置后推送。'):
            changed = reply({'success': False, 'message': message}, status=400)
            self.assertTrue(pd.response_diffs(py, changed, route, []))
        self.assertTrue(pd.response_diffs(py, native, '/api/other', []))
        wrong_status = reply({'success': False, 'message': json.loads(native[2])['message']}, status=500)
        self.assertTrue(pd.response_diffs(py, wrong_status, route, []))

    def test_a18_ha_refusal_is_its_own_difference_type(self):
        route = '/api/agents/<agent_id>/generate-config'
        py = reply({'success': False, 'message': "HTTPConnectionPool(host='agent.example.com', port=8080): Max retries exceeded"}, status=400)
        refusal = reply({'success': False, 'message': pd.A18_HA_REFUSAL}, status=400)
        self.assertEqual([('body.message', 'a18-ha-refusal')], pd.response_diffs(py, refusal, route, []))
        # Any other message, field or status difference stays an ordinary difference.
        other = reply({'success': False, 'message': '目标设备配置已改变，请重新生成最终配置后推送。'}, status=400)
        self.assertEqual([('body.message', 'value')], pd.response_diffs(py, other, route, []))
        extra = reply({'success': False, 'message': pd.A18_HA_REFUSAL, 'code': 'x'}, status=400)
        self.assertIn(('body.code', 'extra-in-go'), pd.response_diffs(py, extra, route, []))
        self.assertIn(('status', 'value'), pd.response_diffs(py, reply({'success': False, 'message': pd.A18_HA_REFUSAL}, status=500), route, []))
        self.assertEqual([('body.message', 'value')], pd.response_diffs(py, refusal, '/api/agents/<agent_id>/push-config', []))

    def test_baseline_migration_patches_only_private_identity_without_revision(self):
        import tempfile
        import copy
        import sqlite3
        for tamper in (False, True):
            with self.subTest(tamper=tamper), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                go = root / 'copies-synthetic' / 'go'
                go.mkdir(parents=True)
                database = go / 'configflow.sqlite3'
                original = [{'id': 'synthetic', 'host': '192.0.2.1', 'config_version': 'a'*64,
                             'applied_content': {'fingerprint': 'old', 'files': {'config.yaml': 'old'}, 'profile_id': 'default'}}]
                with sqlite3.connect(database) as connection:
                    connection.executescript('CREATE TABLE fields(scope,owner,field,position,value,revision); CREATE TABLE meta(key,value);')
                    connection.execute('INSERT INTO fields VALUES(?,?,?,?,?,?)', ('system', '', 'agents', 0, json.dumps(original), 7))
                    connection.execute('INSERT INTO fields VALUES(?,?,?,?,?,?)', ('system', '', '_revision', 1, '11', 2))
                    connection.execute('INSERT INTO meta VALUES(?,?)', ('generation', '19'))
                def worker(command, **kwargs):
                    updated = copy.deepcopy(original)
                    updated[0]['applied_content'].update(fingerprint='new', files={'config.yaml': 'new'})
                    if tamper:
                        updated[0]['host'] = '192.0.2.2'
                    (Path(command[2]) / 'agents-after.json').write_text(json.dumps(updated))
                with mock.patch.dict(os.environ, {'PRODDIFF_BASELINE_TOOL': '/synthetic/baseline'}), mock.patch.object(pd.subprocess, 'run', side_effect=worker):
                    if tamper:
                        with self.assertRaises(RuntimeError):
                            pd.migrate_baselines(go, root)
                    else:
                        pd.migrate_baselines(go, root)
                with sqlite3.connect(database) as connection:
                    value, revision = connection.execute("SELECT value,revision FROM fields WHERE field='agents'").fetchone()
                    self.assertEqual(7, revision)
                    self.assertEqual([('generation', '19')], list(connection.execute('SELECT key,value FROM meta')))
                    actual = json.loads(value)
                    self.assertEqual('old' if tamper else 'new', actual[0]['applied_content']['fingerprint'])
                    self.assertEqual('192.0.2.1', actual[0]['host'])

    def test_agent_comparison_identity_pairing_has_proofs(self):
        identities = pd.AgentComparisonIdentities()
        route = '/api/agents/<agent_id>/push-diff'
        a = {'status': 'changed', 'checked_at': '2026-01-01T00:00:00.000001Z', 'version': 'a'*16, 'files': []}
        b = dict(a, checked_at='2026-01-01T00:00:00.000002Z', version='b'*16)
        compare = lambda left, right: pd.response_diffs(reply(left), reply(right), route, [], identities)
        self.assertEqual([], compare(a, b))
        self.assertEqual([], compare(a, b))
        for changed in (dict(b, version='c'*16), dict(b, version='bad'), dict(b, checked_at='bad'), dict(b, status='synced'), dict(b, files=[{'path': 'missing'}])):
            self.assertTrue(compare(a, changed))
        missing = dict(b); missing.pop('checked_at')
        self.assertTrue(compare(a, missing))
        collision = dict(a, version='d'*16)
        self.assertTrue(compare(collision, b))
        self.assertTrue(pd.response_diffs(reply(a), reply(b), '/api/other', [], identities))
        for public_route, left, right in (('/api/agents', [{'content_sync': a}], [{'content_sync': b}]), ('/api/agents/<agent_id>', {'content_sync': a}, {'content_sync': b})):
            self.assertEqual([], pd.response_diffs(reply(left), reply(right), public_route, [], identities))
        self.assertTrue(pd.response_diffs(reply(a), reply(b), '/api/agents/<agent_id>', [], identities))

    def test_short_link_plain_error_and_attachment_exception_are_strict(self):
        route = '/api/agents/i/<token>'
        for status, message in [(404, '安装链接不存在或已过期，请在面板重新生成。\n'),
                                (503, '安装脚本暂时不可用，请稍后重试。\n')]:
            headers = {'cache-control': 'no-store',
                       'content-disposition': 'attachment; filename="configflow-install.sh"'}
            source = reply(message.encode(), 'text/plain; charset=utf-8', status, headers)
            target = reply(message.encode(), source[1], status, {'cache-control': 'no-store'})
            self.assertTrue(pd.plain_install_error(source, target, route))
            self.assertEqual([], pd.response_diffs(source, target, route, []))
            for changed in [reply({'success': False, 'message': message}, status=status),
                            reply(b'different', source[1], status, target[3]),
                            reply(target[2], source[1], 400, target[3]),
                            reply(target[2], 'text/html', status, target[3]),
                            reply(target[2], source[1], status, {'content-disposition': 'other'})]:
                with self.subTest(status=status, changed=changed):
                    self.assertFalse(pd.plain_install_error(source, changed, route))
                    self.assertTrue(pd.response_diffs(source, changed, route, []))
            self.assertTrue(pd.response_diffs(source, target, '/other', []))
            self.assertIn(('header.Cache-Control', 'missing-in-go'),
                          pd.response_diffs(source, reply(target[2], source[1], status), route, []))

    def test_mutations_are_detected(self):
        good = reply(b'{"success":true,"data":{"count":3,"name":"a","items":["x","y"],"note":""}}')
        tests = [
            ('status', reply(good[2], status=500), ('status', 'value')),
            ('content-type', reply(good[2], 'text/plain'), ('Content-Type', 'value')),
            ('value', reply(good[2].replace(b'3', b'4')), ('body.data.count', 'value')),
            ('missing key', reply(good[2].replace(b'"count":3,', b'')), ('body.data.count', 'missing-in-go')),
            ('extra key', reply(good[2].replace(b'"count":3', b'"count":3,"success":1')), ('body.data.success', 'extra-in-go')),
            ('type bool', reply(good[2].replace(b'3', b'true')), ('body.data.count', 'type')),
            ('int to string', reply(good[2].replace(b'3', b'"3"')), ('body.data.count', 'type')),
            ('int to float', reply(good[2].replace(b'3', b'3.0')), ('body.data.count', 'int-float')),
            ('list order', reply(good[2].replace(b'["x","y"]', b'["y","x"]')), ('body.data.items[0]', 'value')),
            ('list length', reply(good[2].replace(b'["x","y"]', b'["x"]')), ('body.data.items', 'length')),
            ('empty vs missing', reply(good[2].replace(b',"note":""', b'')), ('body.data.note', 'missing-in-go')),
            ('empty vs null', reply(good[2].replace(b'"note":""', b'"note":null')), ('body.data.note', 'type')),
            ('duplicate key', reply(good[2].replace(b'"count":3', b'"count":3,"count":3')), ('body', 'parse-go')),
            ('one-sided transport', (0, '', b'', {}), ('status', 'transport-one-sided')),
        ]
        for name, bad, expected in tests:
            with self.subTest(name=name):
                self.assertIn(expected, pd.response_diffs(good, bad, '/synthetic', []))

    def test_both_sided_transport_failure_is_a_difference(self):
        failed = (0, '', b'', {})
        self.assertEqual([('status', 'transport-both')], pd.response_diffs(failed, failed, '/synthetic', []))

    def test_discovery_store_state_exemption_is_exact(self):
        base_agent = {'id': 'gw', 'name': 'GW', 'status': 'running', 'enabled': True}
        for route in ('/api/domain-discovery/agents', '/api/domain-discovery/domains'):
            py = reply({'success': True, 'agents': [dict(base_agent)]})
            go_ok = reply({'success': True, 'agents': [{**base_agent, 'store_state': 'ok', 'store_message': ''}]})
            go_damaged = reply({'success': True, 'agents': [{**base_agent, 'store_state': 'damaged', 'store_message': '已损坏'}]})
            self.assertEqual([], pd.response_diffs(py, go_ok, route, []))
            # Only the healthy baseline is exempt; damaged on real data must surface.
            self.assertTrue(pd.response_diffs(py, go_damaged, route, []))
            # Invalid state/message combinations or other field drifts must still fail.
            for bad_fields in (
                {'store_state': 'unknown', 'store_message': ''},
                {'store_state': 'ok', 'store_message': 'unexpected'},
                {'store_state': 'damaged', 'store_message': ''},
                {'store_state': 'ok', 'store_message': '', 'status': 'no_data'},
            ):
                bad = reply({'success': True, 'agents': [{**base_agent, **bad_fields}]})
                self.assertTrue(pd.response_diffs(py, bad, route, []))
            self.assertTrue(pd.response_diffs(py, go_ok, '/api/agents', []))

    def test_agent_release_exemption_is_exact(self):
        root = Path(__file__).resolve().parents[3]
        python = (root / 'backend/agents/version.py').read_text()
        go = (root / 'server/internal/agents/version_changelog.go').read_text()
        self.assertIn(f'LATEST_AGENT_VERSION = "{pd.PYTHON_AGENT_LATEST}"', python)
        self.assertIn(f'const LatestAgentVersion = "{pd.GO_AGENT_LATEST}"', go)
        latest = '/api/agents/latest-version'
        self.assertEqual([], pd.response_diffs(reply({'success': True, 'version': '1.8.61-go'}),
                                               reply({'success': True, 'version': '1.9.1-go'}), latest, []))
        for wrong in ('1.9.0-go', '1.9.2-go'):
            self.assertTrue(pd.response_diffs(reply({'success': True, 'version': '1.8.61-go'}),
                                              reply({'success': True, 'version': wrong}), latest, []))
        agent = {'id': 'a1', 'version': '1.8.61-go', 'has_update': False}
        for route, wrap in (('/api/agents', lambda x: [x]), ('/api/agents/<agent_id>', lambda x: x)):
            self.assertEqual([], pd.response_diffs(reply(wrap(agent)), reply(wrap({**agent, 'has_update': True})), route, []))
            between = {**agent, 'version': '1.9.0-go'}
            self.assertEqual([], pd.response_diffs(reply(wrap(between)), reply(wrap({**between, 'has_update': True})), route, []))
            for left, right in (
                ({**agent, 'version': '1.8.60-go'}, {**agent, 'version': '1.8.60-go', 'has_update': True}),
                ({**agent, 'version': '1.9.1-go'}, {**agent, 'version': '1.9.1-go', 'has_update': True}),
                (agent, {**agent, 'version': '1.8.62-go', 'has_update': True}),
                ({**agent, 'has_update': True}, agent),
                (agent, {**agent, 'has_update': True, 'name': 'other'}),
            ):
                self.assertTrue(pd.response_diffs(reply(wrap(left)), reply(wrap(right)), route, []), (route, left, right))
        self.assertTrue(pd.response_diffs(reply([agent]), reply([{**agent, 'has_update': True}]), '/api/other', []))

    def test_headers_are_compared(self):
        base = {'x-configflow-profile': 'one', 'etag': '"a"', 'date': 'd1', 'server': 'waitress', 'content-length': '2'}
        left = reply(b'{}', headers=base)
        tests = [
            ('value', dict(base, **{'x-configflow-profile': 'two'}), ('header.X-ConfigFlow-Profile', 'value')),
            ('missing', {k: v for k, v in base.items() if k != 'etag'}, ('header.ETag', 'missing-in-go')),
            ('extra', dict(base, **{'content-disposition': 'attachment'}), ('header.Content-Disposition', 'extra-in-go')),
        ]
        for name, headers, expected in tests:
            with self.subTest(name=name):
                self.assertIn(expected, pd.response_diffs(left, reply(b'{}', headers=headers), '/synthetic', []))
        transport_only = {'x-configflow-profile': 'one', 'etag': '"a"', 'date': 'd2', 'server': 'go', 'content-length': '3'}
        self.assertEqual([], pd.response_diffs(left, reply(b'{}', headers=transport_only), '/synthetic', []))

    def test_provider_stale_header_and_body_are_still_compared(self):
        stale = reply(b'proxies: [{name: One}, {name: Two}]\n', 'text/yaml',
                      headers={'x-configflow-provider': 'last-good'})
        fresh = reply(b'proxies: [{name: One}]\n', 'text/yaml')
        for route in ('/api/aggregations/<agg_id>/provider',
                      '/api/profiles/<profile_id>/aggregations/<agg_id>/provider'):
            differences = pd.response_diffs(stale, fresh, route, [])
            self.assertIn(('header.X-ConfigFlow-Provider', 'missing-in-go'), differences)
            self.assertIn(('body.proxies', 'length'), differences)

    def test_structural_formats(self):
        cases = [
            (reply(b'{"b":2,"a":1}'), reply(b'{"a":1,"b":2}'), '/synthetic'),
            (reply(b'a: 1\nb: [2, 3]\n', 'text/yaml'), reply(b'b:\n- 2\n- 3\na: 1\n', 'text/yaml'), '/synthetic'),
            (reply(b'a\nb\n\n', 'text/plain'), reply(b'a\nb', 'text/plain'), '/synthetic'),
            (reply(b'{"content":"a: 1\\nb: 2\\n"}'), reply(b'{"content":"b: 2\\na: 1\\n"}'), '/api/generate/mihomo/preview'),
            (reply(b'base: &b {x: 1}\nitem:\n  <<: *b\n  x: 2\n', 'text/yaml'), reply(b'base: {x: 1}\nitem: {x: 2}\n', 'text/yaml'), '/synthetic'),
        ]
        for left, right, route in cases:
            with self.subTest(route=route, content_type=left[1], body=left[2]):
                self.assertEqual([], pd.response_diffs(left, right, route, []))

    def test_line_endings_and_duplicate_yaml_keys(self):
        self.assertEqual([('body', 'line-endings')],
                         pd.response_diffs(reply(b'#!/bin/sh\necho\n', 'text/plain'), reply(b'#!/bin/sh\r\necho\r\n', 'text/plain'), '/synthetic', []))
        self.assertIn(('body.content', 'line-endings'),
                      pd.response_diffs(reply({'content': 'a\nb\n'}), reply({'content': 'a\r\nb\r\n'}), '/synthetic', []))
        self.assertIn(('body', 'parse-go'),
                      pd.response_diffs(reply(b'dns: {a: 1}\n', 'text/yaml'), reply(b'dns: {a: 1}\ndns: {a: 1}\n', 'text/yaml'), '/synthetic', []))
        self.assertIn(('body', 'parse-go'),
                      pd.response_diffs(reply(b'a: 1\n', 'text/yaml'), reply(b'a: 1\nb: {c: 1, c: 2}\n', 'text/yaml'), '/synthetic', []))

    def test_invalid_documents_cannot_pass(self):
        bad = reply(b'{')
        self.assertEqual([('body', 'parse-python'), ('body', 'parse-go')], pd.response_diffs(bad, bad, '/synthetic', []))

    def test_zip_members_use_their_own_format(self):
        def archive(content, mode=0o644, crlf=False):
            buffer = io.BytesIO()
            with zipfile.ZipFile(buffer, 'w') as output:
                output.writestr('config.yaml', 'a: 1\n')
                info = zipfile.ZipInfo('install.sh')
                info.external_attr = mode << 16
                output.writestr(info, content.replace('\n', '\r\n') if crlf else content)
            return reply(buffer.getvalue(), 'application/zip')
        self.assertEqual([], pd.response_diffs(archive('a\nb\n'), archive('a\nb'), '/api/generate/oxidns', []))
        self.assertTrue(pd.response_diffs(archive('a\nb'), archive('a\nc'), '/api/generate/oxidns', []))
        self.assertTrue(pd.response_diffs(archive('a\nb', 0o755), archive('a\nb', 0o644), '/api/generate/oxidns', []))
        self.assertTrue(pd.response_diffs(archive('a\nb\n'), archive('a\nb\n', crlf=True), '/api/generate/oxidns', []))

    def test_normalization_is_route_and_path_specific(self):
        a = reply(b'{"size":1,"data":{"size":2}}')
        b = reply(b'{"size":3,"data":{"size":4}}')
        rule = [{'route': '/synthetic', 'path': 'size', 'reason': 'test'}]
        self.assertEqual([('body.data.size', 'value')], pd.response_diffs(a, b, '/synthetic', rule))
        self.assertEqual(2, len(pd.response_diffs(a, b, '/other', rule)))

    def test_batch7_request_clocks_keep_exact_fields_and_negative_controls(self):
        configured = json.loads((pd.PARITY / 'proddiff-normalize.json').read_text())
        expected = {
            ('/api/agents/<agent_id>/ipv6-readiness', 'refresh_error.at'),
            ('/api/agents/<agent_id>/upstream-routers', 'checked_at'),
            ('/api/client-devices/discovery', 'checked_at'),
            ('/api/notifications', 'checked_at'),
            ('/api/routers/<router_id>/policy', 'generated_at'),
        }
        selected = [rule for rule in configured if (rule['route'], rule.get('path')) in expected]
        self.assertEqual(5, len(selected))
        for rule in selected:
            with self.subTest(route=rule['route']):
                self.assertEqual(120, rule['tolerance'])
                self.assertTrue(rule['reason'])
                parts = rule['path'].split('.')
                def body(value):
                    result = {'success': True, 'payload': {'keep': 'value'}}
                    node = result
                    for part in parts[:-1]:
                        node[part] = {}
                        node = node[part]
                    node[parts[-1]] = value
                    return result
                self.assertEqual('request-clock', rule['timestamp_format'])
                def stamp(seconds):
                    suffix = ('+00:00' if rule['route'] != '/api/routers/<router_id>/policy' else '')
                    micros = '.000000' if rule['route'] == '/api/notifications' else ''
                    return '2026-01-01T00:' + seconds + micros + suffix
                first, second = stamp('00:00'), stamp('00:01')
                source, target = body(first), body(second)
                self.assertEqual([], pd.response_diffs(reply(source), reply(target), rule['route'], [rule]))
                for invalid in ['bad', 1, '2026-01-01', '2026-01-01T00:00:00Z', stamp('02:01')]:
                    self.assertTrue(pd.response_diffs(reply(source), reply(body(invalid)), rule['route'], [rule]))
                self.assertTrue(pd.response_diffs(reply(body(1)), reply(body(2)), rule['route'], [rule]))
                missing = body(second)
                node = missing
                for part in parts[:-1]:
                    node = node[part]
                del node[parts[-1]]
                self.assertTrue(pd.response_diffs(reply(source), reply(missing), rule['route'], [rule]))
                changed = body(second)
                changed['payload']['keep'] = 'changed'
                self.assertTrue(pd.response_diffs(reply(source), reply(changed), rule['route'], [rule]))
                self.assertTrue(pd.response_diffs(reply(source), reply(target), '/other', [rule]))
                rows = [('GET', route, '') for route in {r['route'] for r in configured}]
                for mutation in [dict(rule, route='/other'), dict(rule, path='other'), dict(rule, tolerance=121), dict(rule, timestamp_format='other')]:
                    with mock.patch.object(pd.Path, 'read_text', return_value=json.dumps([mutation])):
                        with self.assertRaises(RuntimeError):
                            pd.load_rules(rows)

    def test_discovery_comparison_aligns_cold_and_hit_without_masking_cache_state(self):
        route = '/api/client-devices/discovery'
        cases = [('GET', '/ordinary', '/ordinary', None, False),
                 ('POST', '/artifact', '/artifact', {}, False),
                 ('GET', '/api/logs/info', '/api/logs/info', None, False),
                 ('GET', route, route + '?profile_id=synthetic', None, False),
                 ('GET', route, route, None, False)]
        calls = []
        seen = 0
        def compare(case):
            nonlocal seen
            calls.append(case[2])
            if case[1] == route:
                result = {'cached': seen != 0, 'stale': False, 'refresh_error': '', 'devices': []}
                seen += 1
                return result
            return case[1]
        answers = list(pd.comparison_results(cases, compare))
        self.assertEqual([route + '?profile_id=synthetic', route], calls[:2])
        self.assertEqual('/api/logs/info', calls[-1])
        self.assertFalse(answers[0]['cached'])
        self.assertTrue(answers[1]['cached'])
        rules = [r for r in json.loads((pd.PARITY / 'proddiff-normalize.json').read_text()) if r['route'] == route]
        base = {'cached': True, 'stale': False, 'refresh_error': '', 'devices': [],
                'checked_at': '2026-01-01T00:00:00+00:00'}
        for changed in [dict(base, cached=False), dict(base, stale=True),
                        dict(base, refresh_error='failed'), dict(base, devices=[{'ip': '192.0.2.1'}])]:
            with self.subTest(changed=changed):
                self.assertTrue(pd.response_diffs(reply(base), reply(changed), route, rules))
        self.assertEqual(0, pd.phase(('GET', '/api/client-devices', '/', None, False)))
        self.assertEqual(1, pd.phase(('POST', route, '/', {}, False)))

    def test_normalization_preserves_type_changes(self):
        rule = [{'route': '/synthetic', 'path': 'size', 'reason': 'test'}]
        self.assertEqual([('body.size', 'type')], pd.response_diffs(reply(b'{"size":1}'), reply(b'{"size":{}}'), '/synthetic', rule))
        self.assertEqual([('body.size', 'missing-in-go')], pd.response_diffs(reply(b'{"size":1}'), reply(b'{}'), '/synthetic', rule))

    def test_pd1_http_date_tolerance_is_exact_and_keeps_negative_controls(self):
        rules = [{'route': '/api/generate/mihomo', 'path': 'header.last-modified',
                  'timestamp_format': 'http-date', 'tolerance': 600, 'reason': 'PD-1'}]
        stamp = 'Thu, 01 Jan 2026 00:00:00 GMT'
        left = reply({}, headers={'last-modified': stamp, 'etag': 'unchanged'})
        boundary = reply({}, headers={'last-modified': 'Thu, 01 Jan 2026 00:10:00 GMT', 'etag': 'unchanged'})
        self.assertEqual([], pd.response_diffs(left, boundary, '/api/generate/mihomo', rules))
        for changed, expected in [
            ({'etag': 'unchanged'}, ('header.Last-Modified', 'missing-in-go')),
            ({'last-modified': 'invalid', 'etag': 'unchanged'}, ('header.Last-Modified', 'value')),
            ({'last-modified': '2026-01-01T00:00:30Z', 'etag': 'unchanged'}, ('header.Last-Modified', 'value')),
            ({'last-modified': 123, 'etag': 'unchanged'}, ('header.Last-Modified', 'value')),
            ({'last-modified': 'Thu, 01 Jan 2026 00:10:01 GMT', 'etag': 'unchanged'}, ('header.Last-Modified', 'value')),
            ({'last-modified': 'Thu, 01 Jan 2026 00:01:00 GMT', 'etag': 'changed'}, ('header.ETag', 'value')),
        ]:
            with self.subTest(expected=expected, changed=changed):
                self.assertIn(expected, pd.response_diffs(left, reply({}, headers=changed), '/api/generate/mihomo', rules))
        self.assertTrue(pd.response_diffs(left, boundary, '/api/generate/surge', rules))
        with mock.patch.object(Path, 'read_text', return_value=json.dumps(rules)):
            self.assertEqual(rules, pd.load_rules([['POST', '/api/generate/mihomo']]))
        for mutation in [dict(rules[0], timestamp_format='other'), dict(rules[0], tolerance=601),
                         dict(rules[0], path='header.etag'), dict(rules[0], route='/api/other')]:
            with mock.patch.object(Path, 'read_text', return_value=json.dumps([mutation])):
                with self.assertRaises(RuntimeError):
                    pd.load_rules([['POST', mutation['route']]])

    def test_pattern_and_tolerance_normalization_stay_narrow(self):
        prefix = [{'route': '/r', 'path': 'message', 'pattern': '(?s)(?<=^Connection error: ).+', 'reason': 't'}]
        same = pd.response_diffs(reply({'message': 'Connection error: a'}), reply({'message': 'Connection error: b'}), '/r', prefix)
        self.assertEqual([], same)
        other = pd.response_diffs(reply({'message': 'Connection error: a'}), reply({'message': 'Agent not found'}), '/r', prefix)
        self.assertEqual([('body.message', 'value')], other)
        lines = [{'route': '/r', 'path': 'logs[]', 'pattern': r'^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d,\d{3}', 'reason': 't'}]
        a = reply({'logs': ['2026-01-01 00:00:00,000 - x - ERROR - a', '2026-01-01 00:00:00,000 - x - ERROR - b']})
        b = reply({'logs': ['2026-01-02 01:00:00,111 - x - ERROR - a', '2026-01-02 01:00:00,111 - x - ERROR - c']})
        self.assertEqual([('body.logs[1]', 'value')], pd.response_diffs(a, b, '/r', lines))
        unordered = lines + [{'route': '/r', 'path': 'logs', 'unordered': True, 'reason': 't'}]
        swapped = reply({'logs': ['2026-01-02 01:00:00,111 - x - ERROR - b', '2026-01-02 01:00:00,111 - x - ERROR - a']})
        self.assertEqual([], pd.response_diffs(a, swapped, '/r', unordered))
        self.assertTrue(pd.response_diffs(a, b, '/r', unordered))
        self.assertIn(('body.logs', 'length'), pd.response_diffs(a, reply({'logs': ['2026-01-02 01:00:00,111 - x - ERROR - a']}), '/r', unordered))
        # Lists of different length are still masked element by element (no positional pairing needed).
        longer = reply({'logs': ['2026-01-03 00:00:00,000 - a.b - ERROR - z', 'continuation without timestamp',
                                 '2026-01-03 00:00:00,001 - x - ERROR - a', '2026-01-03 00:00:00,002 - x - ERROR - b']})
        self.assertEqual({('body.logs', 'length'), ('body.logs[0]', 'value'), ('body.logs[1]', 'value')},
                         set(pd.response_diffs(a, longer, '/r', unordered)))
        clock = [{'route': '/r', 'path': 'start', 'tolerance': 60, 'reason': 't'}]
        tests = [
            ({'start': 1000.5}, {'start': 1030.25}, []),
            ({'start': 1000.5}, {'start': 2000.5}, [('body.start', 'value')]),
            ({'start': '2026-01-01T00:00:00.000001'}, {'start': '2026-01-01T00:00:30.000002'}, []),
            ({'start': '2026-01-01T00:00:00.000001'}, {'start': '2026-01-01T00:00:30Z'}, [('body.start', 'value')]),
            ({'start': '2026-01-01T00:00:00'}, {'start': 'not a time'}, [('body.start', 'value')]),
        ]
        for left, right, expected in tests:
            with self.subTest(left=left, right=right):
                self.assertEqual(expected, pd.response_diffs(reply(left), reply(right), '/r', clock))
        header = [{'route': '/r', 'path': 'header.content-disposition', 'pattern': r'\d{8}_\d{6}', 'reason': 't'}]
        left = reply(b'{}', headers={'content-disposition': 'inline; filename="p_20260101_000000.yaml"'})
        self.assertEqual([], pd.response_diffs(left, reply(b'{}', headers={'content-disposition': 'inline; filename="p_20260101_000001.yaml"'}), '/r', header))
        self.assertTrue(pd.response_diffs(left, reply(b'{}', headers={'content-disposition': 'attachment; filename="p_20260101_000001.yaml"'}), '/r', header))

    def test_rule_file_validation(self):
        rows = [['GET', '/r']]
        bad_rules = [
            [{'route': '/r', 'path': 'a'}],
            [{'route': '/missing', 'path': 'a', 'reason': 't'}],
            [{'route': '/r', 'path': 'a*', 'reason': 't'}],
            [{'route': '/r', 'path': 'a', 'bracket': True, 'reason': 't'}],
            [{'route': '/r', 'reason': 't'}],
            [{'route': '/r', 'path': 'a', 'reason': 't', 'ignore': True}],
            [{'route': '/r', 'path': 'a', 'reason': 't', 'pattern': 'x', 'tolerance': 1}],
            [{'route': '/r', 'path': 'a', 'reason': 't', 'pattern': 'x', 'unordered': True}],
        ]
        for rules in bad_rules:
            with self.subTest(rules=rules), mock.patch.object(pd, 'PARITY', Path('/nonexistent')), \
                    mock.patch.object(Path, 'read_text', return_value=json.dumps(rules)):
                with self.assertRaises(RuntimeError):
                    pd.load_rules(rows)
        with mock.patch.object(Path, 'read_text', return_value=json.dumps([{'route': '/r', 'bracket': True, 'reason': 't'}])):
            self.assertEqual(1, len(pd.load_rules(rows)))

    def compare_http(self, python, go, bracket=False):
        senders = [mock.Mock(side_effect=python), mock.Mock(side_effect=go)]
        result = pd.compare_http(*senders, lambda a, b: pd.response_diffs(a, b, '/synthetic', []), bracket=bracket)
        return result, senders

    def test_one_sided_transport_retries_only_failed_side(self):
        good = reply({'data': 2})
        # Exercise send() and comparison together: two timeouts, then a real response shape.
        for side in (0, 1):
            with self.subTest(side=side):
                peers = [[good], [good]]
                peers[side] = [TimeoutError(), TimeoutError(), good]
                def open_request(request, timeout):
                    value = peers[pd.urllib.parse.urlsplit(request.full_url).port - 15001].pop(0)
                    if isinstance(value, Exception):
                        raise value
                    response = io.BytesIO(value[2])
                    response.code = value[0]
                    response.headers = Message()
                    response.headers['Content-Type'] = value[1]
                    return response
                with mock.patch.object(pd.urllib.request, 'build_opener') as build:
                    send = build.return_value.open
                    send.side_effect = open_request
                    result = pd.compare_http(lambda timeout: pd.send(15001, 'GET', '/', timeout=timeout),
                                             lambda timeout: pd.send(15002, 'GET', '/', timeout=timeout),
                                             lambda a, b: pd.response_diffs(a, b, '/synthetic', []))
                self.assertEqual([], pd.response_diffs(*result[:2], '/synthetic', []))
                self.assertEqual([2 if index == side else 0 for index in (0, 1)], result[3])
                ports = [pd.urllib.parse.urlsplit(call.args[0].full_url).port for call in send.call_args_list]
                self.assertEqual([20, 40, 80], [call.kwargs['timeout'] for call, port in zip(send.call_args_list, ports)
                                               if port == 15001 + side])
                self.assertEqual(1, sum(port != 15001 + side for port in ports))

    def test_exhausted_transport_retries_are_a_difference(self):
        good, failed = reply({}), (0, '', b'', {})
        for side in (0, 1):
            with self.subTest(side=side):
                peers = [[good], [good]]
                peers[side] = [failed] * 4
                result, senders = self.compare_http(*peers, bracket=True)
                self.assertEqual([('status', 'transport-one-sided')], pd.response_diffs(*result[:2], '/synthetic', []))
                self.assertEqual([mock.call(timeout=t) for t in (20, 40, 80, 160)], senders[side].call_args_list)
                self.assertEqual(1, senders[1 - side].call_count)
                self.assertEqual(0, result[2])

    def test_http_status_difference_never_retries(self):
        for bracket in (False, True):
            for side in (0, 1):
                with self.subTest(bracket=bracket, side=side):
                    peers = [[reply({})], [reply({})]]
                    peers[side] = [reply({}, status=500)]
                    result, senders = self.compare_http(*peers, bracket=bracket)
                    self.assertEqual([('status', 'value')], pd.response_diffs(*result[:2], '/synthetic', []))
                    self.assertEqual([1, 1], [sender.call_count for sender in senders])
                    self.assertEqual([0, 0], result[3])

    def test_both_sided_transport_does_not_retry(self):
        failed = (0, '', b'', {})
        for bracket in (False, True):
            with self.subTest(bracket=bracket):
                result, senders = self.compare_http([failed], [failed], bracket=bracket)
                self.assertEqual([('status', 'transport-both')], pd.response_diffs(*result[:2], '/synthetic', []))
                self.assertEqual([1, 1], [sender.call_count for sender in senders])

    def test_http_error_after_transport_recovery_is_not_retried(self):
        result, senders = self.compare_http([(0, '', b'', {}), reply({}, status=500)], [reply({})], bracket=True)
        self.assertEqual([('status', 'value')], pd.response_diffs(*result[:2], '/synthetic', []))
        self.assertEqual([2, 1], [sender.call_count for sender in senders])
        self.assertEqual([1, 0], result[3])

    def test_bracket_stops_on_a_new_http_status_difference(self):
        result, senders = self.compare_http([reply({'data': 1}), reply({'data': 2}, status=500)],
                                           [reply({'data': 2})], bracket=True)
        self.assertEqual([('status', 'value')], pd.response_diffs(*result[:2], '/synthetic', []))
        self.assertEqual([2, 1], [sender.call_count for sender in senders])
        self.assertEqual((0, [0, 0]), result[2:])

    def test_bracketing_accepts_only_an_adjacent_python_state(self):
        def compare(python, go):
            result, _ = self.compare_http([reply({'data': n}) for n in python],
                                          [reply({'data': n}) for n in go], bracket=True)
            return json.loads(result[0][2])['data'], json.loads(result[1][2])['data'], result[2]
        # The window moved between the first Python reply and Go: the next Python reply agrees.
        self.assertEqual((2, 2, 1), compare([1, 2], [2]))
        # Go answered before the window moved, Python after: the next Go reply agrees.
        self.assertEqual((2, 2, 1), compare([1, 2], [0, 2]))
        # A Go reply that never equals any Python state is reported, however often it is asked.
        left, right, retries = compare([1, 1, 1, 1], [9, 9, 9, 9])
        self.assertEqual((0, 1, 9), (retries, left, right))

    def test_bracketing_alternates_after_python_transport_recovery(self):
        failed = (0, '', b'', {})
        calls = []
        python = iter([failed, reply({'data': 2}), reply({'data': 3})])
        go = iter([reply({'data': 1}), reply({'data': 3})])
        def ask(label, replies, timeout):
            calls.append((label, timeout))
            return next(replies)
        result = pd.compare_http(lambda timeout: ask('python', python, timeout),
                                 lambda timeout: ask('go', go, timeout),
                                 lambda a, b: pd.response_diffs(a, b, '/synthetic', []), bracket=True)
        self.assertEqual([], pd.response_diffs(*result[:2], '/synthetic', []))
        self.assertEqual([('python', 20), ('go', 20), ('python', 40), ('go', 20), ('python', 20)], calls)
        self.assertEqual([1, 0], result[3])

    def test_bracket_transport_retry_budget_is_per_case(self):
        failed = (0, '', b'', {})
        result, senders = self.compare_http([failed, reply({'data': 1}), failed, failed, failed],
                                           [reply({'data': 2}), reply({'data': 2})], bracket=True)
        self.assertEqual([('status', 'transport-one-sided')], pd.response_diffs(*result[:2], '/synthetic', []))
        self.assertEqual([3, 0], result[3])
        self.assertEqual([mock.call(timeout=t) for t in (20, 40, 20, 80, 160)], senders[0].call_args_list)

    def test_non_bracket_http_body_differences_never_retry(self):
        result, senders = self.compare_http([reply({'data': 1})], [reply({'data': 2})])
        self.assertEqual([('body.data', 'value')], pd.response_diffs(*result[:2], '/synthetic', []))
        self.assertEqual([1, 1], [sender.call_count for sender in senders])

    def test_send_distinguishes_transport_failures_from_http_errors(self):
        for error in (TimeoutError(), ConnectionRefusedError(), ConnectionResetError(),
                      pd.urllib.error.URLError(TimeoutError())):
            with self.subTest(error=type(error).__name__), mock.patch.object(pd.urllib.request, 'build_opener') as build:
                build.return_value.open.side_effect = error
                self.assertEqual((0, '', b'', {}), pd.send(15001, 'GET', '/', timeout=80))
                self.assertEqual(80, build.return_value.open.call_args.kwargs['timeout'])
        with mock.patch.object(pd.urllib.request, 'build_opener') as build:
            headers = Message()
            headers['Content-Type'] = JSON
            build.return_value.open.side_effect = pd.urllib.error.HTTPError('http://example.com', 500, 'error', headers, io.BytesIO(b'{}'))
            self.assertEqual(500, pd.send(15001, 'GET', '/')[0])

    def test_transport_diagnostics_preserve_reply_and_timeout(self):
        errors = []
        failure = TimeoutError('synthetic transport failure')
        with mock.patch.object(pd.urllib.request, 'build_opener') as build:
            build.return_value.open.side_effect = failure
            result = pd.send(15002, 'GET', '/synthetic', timeout=40,
                             transport_error=errors.append)
            self.assertEqual((0, '', b'', {}), result)
            self.assertEqual([failure], errors)
            self.assertEqual(40, build.return_value.open.call_args.kwargs['timeout'])

    def test_phases_put_log_readers_last(self):
        self.assertEqual(0, pd.phase(('GET', '/api/agents', '', None, False)))
        self.assertEqual(1, pd.phase(('POST', '/api/generate/mihomo', '', None, False)))
        self.assertEqual(2, pd.phase(('GET', '/api/logs/tail', '', None, False)))

    def test_unknown_keys_never_escape(self):
        self.assertEqual([('body.<key>', 'value')], pd.differences({'private.example': 1}, {'private.example': 2}))
        self.assertEqual([('header.<key>', 'value')], pd.response_diffs(
            reply(b'{}', headers={'x-private-example': 'a'}), reply(b'{}', headers={'x-private-example': 'b'}), '/r', []))

    def test_scrub_replaces_only_the_own_data_copy(self):
        own = Path('/runs/run-a/copies-b/go')
        scrubbed = pd.scrub((200, JSON, b'{"path":"/runs/run-a/copies-b/go/app.log","other":"/runs/run-a/copies-b/python"}',
                             {'x-file': '/runs/run-a/copies-b/go/x'}), own)
        self.assertEqual(b'{"path":"<DATA_DIR>/app.log","other":"/runs/run-a/copies-b/python"}', scrubbed[2])
        self.assertEqual({'x-file': '<DATA_DIR>/x'}, scrubbed[3])

    def test_enumerates_every_snapshot_entity(self):
        doc = {'system': {'agents': [{'id': 'agent-a'}, {'id': 'agent-b'}]}, 'shared': {
            'subscriptions': [{'id': 'sub-a'}, {'id': 'sub-b'}], 'rule_library': []},
            'profiles': {'one': {'routers': {'items': [{'id': 'router-a'}]}}, 'two': {'routers': {'items': []}}}}
        rows = [['GET', route] for route in ('/api/agents/<agent_id>', '/api/subscriptions/<sub_id>/nodes', '/api/routers/<router_id>/routes')]
        result = list(pd.cases(rows, doc, Path('/nonexistent')))
        # 2 profiles x entities, plus one default-profile (no selector) request per entity of the first profile.
        self.assertEqual(15, len(result))
        self.assertEqual(1, sum(case[-1] for case in result))
        self.assertTrue(any('/router-a/' in case[2] for case in result))
        self.assertEqual(5, sum('profile_id=' not in case[2] for case in result))

    def test_self_check_uses_real_samples_and_rejects_blind_comparators(self):
        from generators import self_check
        samples = {'yaml': (200, 'text/yaml', b'dns:\n  upstreams:\n  - port: 53\nmode: rule\n'),
                   'text': (200, 'text/plain', b'[General]\nloglevel=info\n[Rule]\nFINAL,DIRECT\n'),
                   'headers': reply(b'{}', headers={'x-configflow-profile': 'one', 'date': 'x'}),
                   'http-json': reply({'success': True, 'data': {'items': [{'id': 'a', 'port': 1}, {'id': 'b', 'port': 2}]}})}
        real = reply(b'{"auth_enabled":true}', headers={'date': 'x'})
        checks = self_check(real, samples, pd.response_diffs, pd.safe_key, pd.safe_header)
        for name in ('status', 'header-value', 'header-missing', 'json-real-leaf', 'json-missing-key', 'json-extra-key',
                     'json-null-vs-value', 'json-real-list-order', 'json-int-float', 'json-duplicate-key', 'yaml-leaf',
                     'yaml-duplicate-key', 'control-yaml-redumped', 'text-line', 'text-insert', 'text-line-endings',
                     'transport-both', 'control-json-reencoded'):
            self.assertEqual('PASS', checks.get(name), name)
        for blind in (lambda *args: [], lambda *args: [('body', 'value')]):
            with self.subTest(blind=blind), self.assertRaises(RuntimeError):
                self_check(real, samples, blind, pd.safe_key, pd.safe_header)
        for missing in samples:
            with self.subTest(missing=missing), self.assertRaises(RuntimeError):
                self_check(real, {k: v for k, v in samples.items() if k != missing}, pd.response_diffs, pd.safe_key, pd.safe_header)

    def test_yaml_redump_control_keeps_real_response_metadata_and_rejects_loss(self):
        from generators import self_check
        import yaml
        samples = {'text': reply(b'first\nsecond\n', 'text/plain'),
                   'headers': reply({}, headers={'cache-control': 'no-store'}),
                   'http-json': reply({'items': [{'id': 'a'}, {'id': 'b'}]})}
        real = reply({'auth_enabled': True})
        for content_type, status in [('text/yaml', 200), ('application/yaml; charset=utf-8', 200),
                                     ('application/x-yaml', 201)]:
            with self.subTest(content_type=content_type, status=status):
                sample = reply(b'dns: {port: 53}\nenabled: true\n', content_type, status)
                checks = self_check(real, dict(samples, yaml=sample), pd.response_diffs, pd.safe_key, pd.safe_header)
                self.assertEqual('PASS', checks['control-yaml-redumped'])
                self.assertEqual('PASS', checks['yaml-leaf'])
                self.assertEqual('PASS', checks['yaml-duplicate-key'])
        # Even valid YAML need not survive this transform: PyYAML re-dumps an
        # ordered mapping as plain nested sequences, losing the tagged type.
        ordered = reply(b'ordered: !!omap\n- alpha: 1\n- beta: 2\n', 'application/yaml')
        redumped = reply(yaml.safe_dump(yaml.safe_load(ordered[2]), sort_keys=True).encode(), ordered[1])
        self.assertIn(('body.ordered[0]', 'type'), pd.response_diffs(ordered, redumped, '/self-check', []))
        with self.assertRaisesRegex(RuntimeError, 'control-yaml-redumped'):
            self_check(real, dict(samples, yaml=ordered), pd.response_diffs, pd.safe_key, pd.safe_header)
        # PyYAML's non-strict loader also drops duplicate entries during a re-dump.
        # This is an actual lossy transform, not a formatting-only control:
        # a consumer-rejected document becomes accepted and one value is lost.
        lossy = reply(b'dns: {port: 53, port: 54}\n', 'application/yaml')
        redumped = reply(yaml.safe_dump(yaml.safe_load(lossy[2]), sort_keys=True).encode(), lossy[1])
        self.assertIn(('body', 'parse-python'), pd.response_diffs(lossy, redumped, '/self-check', []))
        with self.assertRaisesRegex(RuntimeError, 'control-yaml-redumped'):
            self_check(real, dict(samples, yaml=lossy), pd.response_diffs, pd.safe_key, pd.safe_header)
        # Fixing the control's metadata must not make real metadata changes equal.
        self.assertIn(('Content-Type', 'value'), pd.response_diffs(
            reply(b'dns: {port: 53}\n', 'application/yaml'),
            reply(b'dns: {port: 53}\n', 'text/yaml'), '/self-check', []))

    def test_generator_inventory_and_http_takeover(self):
        from generators import inventory
        doc = {'system': {'system_config': {'default_profile_id': 'one'},
                          'agents': [{'id': 'agent-a', 'service_type': 'mihomo'}]},
               'shared': {'subscriptions': [{'id': 'sub-a'}]},
               'profiles': {'one': {'routers': {'items': []}}, 'two': {'routers': {'items': []}}}}
        jobs, coverage = inventory(doc, set())
        self.assertEqual(2, coverage['GENERATOR mihomo']['cases'])
        self.assertEqual(2, coverage['GENERATOR subscription-loon']['cases'])
        self.assertEqual(1, coverage['GENERATOR agent-mihomo']['cases'])
        self.assertEqual(list(range(len(jobs))), [job['index'] for job in jobs])
        jobs, coverage = inventory(doc, {('GET', '/api/config/<profile_id>/mihomo')})
        self.assertEqual('http', coverage['GENERATOR mihomo']['mode'])
        self.assertFalse(any(job['kind'] == 'mihomo' for job in jobs))

    def test_successful_output_floor_survives_http_takeover(self):
        from generators import successful_output_regressions
        previous = {'GENERATOR mihomo': {'success': 2, 'mode': 'generator'}}
        self.assertEqual(set(), successful_output_regressions(previous, {'GENERATOR mihomo': {'success': 2, 'mode': 'http'}}))
        self.assertEqual({'GENERATOR mihomo successful-output-count'}, successful_output_regressions(previous, {'GENERATOR mihomo': {'success': 1}}))

    def test_a11_previous_partial_coverage_is_promoted_without_losing_output(self):
        from generators import inventory, successful_output_regressions
        doc={'system': {'agents':[{'id':'agent-a','service_type':'mihomo'}]},'shared':{},'profiles':{'default':{}}}
        jobs,coverage=inventory(doc,set())
        self.assertEqual(1,coverage['GENERATOR agent-deployment-bundle']['cases'])
        self.assertEqual('generator',coverage['GENERATOR aggregation-provider']['mode'])
        key='GENERATOR agent-deployment-bundle'
        previous={key:{'success':2,'partial':1}}
        self.assertEqual(set(),successful_output_regressions(previous,{key:{'success':3,'partial':0}}))
        self.assertEqual({key+' packaging-output-count'},successful_output_regressions(previous,{key:{'success':2,'partial':0}}))
        self.assertIn(key+' successful-output-count',successful_output_regressions(previous,{key:{'success':1,'partial':2}}))

    def test_http_subscription_takeover_requests_all_formats(self):
        doc = {'system': {'agents': []}, 'shared': {'subscriptions': [{'id': 'sub-a'}]},
               'profiles': {'one': {'routers': {'items': []}}}}
        cases = list(pd.cases([['GET', '/api/profiles/<profile_id>/subscriptions/<sub_id>/proxies']], doc, Path('/nonexistent')))
        self.assertEqual(3, len(cases))
        self.assertEqual({'mihomo', 'surge', 'loon'}, {pd.urllib.parse.parse_qs(pd.urllib.parse.urlsplit(case[2]).query)['format'][0] for case in cases})


class AggregationInventoryTests(unittest.TestCase):
    def test_generator_and_http_catalog_selectors_use_shared_aggregation_key(self):
        from generators import inventory
        document = {'shared': {'subscription_aggregations': [{'id': 'one'}, {'id': 'two'}]},
                    'profiles': {'default': {}, 'other': {}}, 'system': {}}
        jobs, coverage = inventory(document, set())
        aggregates = [job for job in jobs if job['kind'] == 'aggregation-provider']
        self.assertEqual(4, len(aggregates))
        self.assertEqual({'one', 'two'}, {job['agg_id'] for job in aggregates})
        self.assertEqual(4, coverage['GENERATOR aggregation-provider']['cases'])
        _, http_coverage = inventory(document, {('GET', '/api/profiles/<profile_id>/aggregations/<agg_id>/provider')})
        self.assertEqual('http', http_coverage['GENERATOR aggregation-provider']['mode'])
        rows = [['GET', '/api/aggregations/<agg_id>/count'],
                ['GET', '/api/aggregations/<resource_id>/health-impact']]
        cases = list(pd.cases(rows, document, Path('/nonexistent')))
        self.assertEqual(12, len(cases))
        self.assertFalse(any('proddiff-missing' in case[2] for case in cases))

    def test_unfrozen_provider_document_still_detects_changed_content(self):
        document = {'files': [{'path': 'providers/a.yaml', 'role': 'provider',
                    'document': {'proxies': [{'name': 'Node', 'port': 80}]}}]}
        changed = json.loads(json.dumps(document))
        changed['files'][0]['document']['proxies'][0]['port'] = 81
        differences = pd.response_diffs(reply(document), reply(changed),
                                       '/generator/agent-deployment-bundle', [])
        self.assertIn(('body.files[0].document.proxies[0].port', 'value'), differences)


class BaselineExitTests(unittest.TestCase):
    def test_a13_deferrals_are_exact_or_removed_together_by_a14(self):
        rows = [line.split('\t', 3) for line in (pd.PARITY / 'proddiff-baseline.txt').read_text().splitlines()
                if 'deferred → A14' in line and line.split('\t', 1)[0] in
                {'GET ' + route for route in pd.AGENT_COMPARISON_ROUTES}]
        if not rows:
            return  # A14 removes all twenty-four after wiring real preparation.
        self.assertEqual(24, len(rows))
        fixture = Path(__file__).parents[2] / 'internal/api/agents/content_proof_test.go'
        approved = fixture.read_text().split('const a13DeferredFields = `', 1)[1].split('`', 1)[0]
        expected = {('GET ' + route, path, kind) for route, path, kind in
                    (line.split('\t') for line in approved.splitlines())}
        self.assertEqual(expected, {tuple(row[:3]) for row in rows})
        for row in rows:
            self.assertNotIn('*', row[0] + row[1])
            self.assertEqual('deferred → A14：PrepareFunc 未接线，接线后必须消失', row[3])

    def test_real_main_rejects_stale_but_accepts_matching_and_recorded_baselines(self):
        # Exercise main's actual baseline/report/exit path. Only process and
        # network boundaries are replaced; all data belongs to this temp tree.
        difference = ('GET /synthetic', 'body.value', 'value')
        obsolete = ('GET /synthetic', 'body.old', 'missing-in-go')
        for name, baseline, record, expected in (
            ('matching', {difference}, False, 0),
            ('stale-only', {difference, obsolete}, False, 1),
            ('new-only', set(), False, 1),
            ('explicit-record', {obsolete}, True, 0),
            ('agent-identities', set(), False, 0),
            ('agent-content-mutation', set(), False, 1),
        ):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                snapshot, parity = root / 'snapshot', root / 'parity'
                snapshot.mkdir()
                parity.mkdir()
                (root / 'cf-prod-snapshot/runs').mkdir(parents=True)
                agent_case = name.startswith('agent-')
                agent_routes = sorted(pd.AGENT_COMPARISON_ROUTES) if agent_case else []
                route_manifest = ''.join('GET\t' + route + '\n' for route in agent_routes)
                (parity / 'routes.tsv').write_text(route_manifest)
                (parity / 'proddiff-coverage.txt').write_text('')
                (parity / 'proddiff-generator-coverage.json').write_text('{}')
                path = parity / 'proddiff-baseline.txt'
                path.write_text(''.join('\t'.join(row) + '\tSynthetic accepted difference\n' for row in baseline))

                def launch(_command, *, env, **_kwargs):
                    for key in ('PRODDIFF_ROUTES', 'PRODDIFF_PY_ROUTES', 'PRODDIFF_PY_LOOPS'):
                        Path(env[key]).write_text('' if key == 'PRODDIFF_PY_LOOPS' else route_manifest)
                    manifest = env['PRODDIFF_ROUTES']
                    for suffix in ('.ready', '.loops', '.startups'):
                        Path(manifest + suffix).write_text('')
                    return mock.Mock(poll=lambda: None)

                def send(port, method, path, *_args, **_kwargs):
                    if method == 'POST':
                        return reply({'token': 'synthetic-login'})
                    value = {'status': 'changed', 'checked_at': '2026-01-01T00:00:00.00000' + ('1Z' if port == 15001 else '2Z'), 'version': ('a' if port == 15001 else 'b') * 16, 'files': []}
                    if path == '/api/agents/<agent_id>/push-diff':
                        if name == 'agent-content-mutation' and port == 15002:
                            value['files'] = [{'path': 'synthetic.yaml'}]
                        return reply(value)
                    public = {'content_sync': {k: v for k, v in value.items() if k not in ('version', 'files')}}
                    return reply([public] if path == '/api/agents' else public)

                argv = ['run.py', str(snapshot), '/synthetic/configflow'] + (['--record-baseline'] if record else [])
                with contextlib.ExitStack() as patches:
                    for patch in (
                        mock.patch.object(pd, 'PARITY', parity),
                        mock.patch.object(pd, 'PRIVATE_REPORT', None),
                        mock.patch.object(pd.Path, 'home', return_value=root),
                        mock.patch.object(pd, 'assert_no_egress'),
                        mock.patch.object(pd, 'digest_tree', return_value='immutable'),
                        mock.patch.object(pd, 'lock_runs'),
                        mock.patch.object(pd, 'load_rules', return_value=[]),
                        mock.patch.object(pd, 'document', return_value={'profiles': {'default': {}}}),
                        mock.patch.object(pd, 'run_workers', return_value=(set() if agent_case else {difference}, {}, {})),
                        mock.patch.object(pd, 'cases', return_value=[('GET', route, route, None, False) for route in agent_routes] * 2),
                        mock.patch.object(pd, 'self_check', return_value={}),
                        mock.patch.object(pd, 'stop_processes'),
                        mock.patch.object(pd.os, 'statvfs', return_value=SimpleNamespace(f_flag=os.ST_RDONLY)),
                        mock.patch.object(pd.subprocess, 'Popen', side_effect=launch),
                        mock.patch.object(pd.signal, 'signal'),
                        mock.patch.object(pd, 'send', side_effect=send),
                        mock.patch.object(pd.sys, 'argv', argv),
                        mock.patch.dict(pd.os.environ, PRODDIFF_GENERATOR='/synthetic/generator'),
                        mock.patch('sys.stdout', new_callable=io.StringIO),
                    ):
                        patches.enter_context(patch)
                    self.assertEqual(expected, pd.main())
                summary_path = next((root / 'cf-prod-snapshot/runs').glob('run-*/summary.json'))
                summary = json.loads(summary_path.read_text())
                self.assertEqual(int(name == 'stale-only'), summary['stale'])
                self.assertEqual(int(name in ('new-only', 'agent-content-mutation')), summary['new'])
                if record:
                    self.assertNotIn('body.old', path.read_text())
                    self.assertIn('body.value', path.read_text())


class ContentObserverTests(unittest.TestCase):
    def content_app(self, delay, endpoint, shape, timeout=1):
        from flask import Flask
        from content_policy import wait_for_content
        app = Flask(__name__)
        calls, clock = [0], [0.0]

        def read():
            calls[0] += 1
            status = 'checking' if calls[0] <= delay else 'changed'
            value = {'status': status}
            return value if shape == 'diff' else {'content_sync': value} if shape == 'detail' else [{'content_sync': value}]

        app.add_url_rule('/content', endpoint=endpoint, view_func=read)
        wait_for_content(app, timeout=timeout, clock=lambda: clock[0], sleep=lambda seconds: clock.__setitem__(0, clock[0] + seconds))
        return app.test_client(), calls

    def test_both_slow_checkers_settle_before_any_comparison(self):
        for endpoint, shape in (('agents.agent_push_diff', 'diff'), ('agents.handle_agent_item', 'detail'), ('agents.handle_agents', 'list')):
            with self.subTest(shape=shape):
                python, python_calls = self.content_app(2, endpoint, shape)
                go, go_calls = self.content_app(5, endpoint, shape)

                def send(client):
                    def request(*, timeout):
                        response = client.get('/content')
                        return reply(response.data, response.content_type, response.status_code,
                                     {key.lower(): value for key, value in response.headers.items()})
                    return request

                differences = mock.Mock(side_effect=lambda a, b: pd.response_diffs(a, b, '/content', []))
                left, right, _, _ = pd.compare_http(send(python), send(go), differences, bracket=True)
                self.assertEqual([], pd.response_diffs(left, right, '/content', []))
                self.assertEqual((3, 6), (python_calls[0], go_calls[0]))
                self.assertEqual(1, differences.call_count)
                self.assertNotIn(b'checking', left[2] + right[2])

    def test_timeout_is_harness_error_without_new_or_stale_classification(self):
        from content_policy import HEADER
        client, _ = self.content_app(1000, 'agents.agent_push_diff', 'diff', timeout=.2)
        response = client.get('/content')
        self.assertEqual(503, response.status_code)
        self.assertEqual('content-sync-timeout', response.headers.get(HEADER))
        failed = reply(response.data, response.content_type, response.status_code, {HEADER: 'content-sync-timeout'})
        for side in (0, 1):
            with self.subTest(side=side):
                peers = [lambda **_: reply({'status': 'changed'}), lambda **_: reply({'status': 'changed'})]
                peers[side] = lambda **_: failed
                differences = mock.Mock(return_value=[])
                with self.assertRaisesRegex(RuntimeError, 'observer timed out'):
                    pd.compare_http(*peers, differences)
                differences.assert_not_called()



class C33EditorRevisionTests(unittest.TestCase):
    def test_native_editor_revision_normalization_is_exact_and_shape_checked(self):
        route = '/api/rule-library/<rule_id>/draft-content'
        configured = json.loads((pd.PARITY / 'proddiff-normalize.json').read_text())
        selected = [rule for rule in configured if rule['route'] == route]
        self.assertEqual(1, len(selected))
        self.assertEqual('revision', selected[0]['path'])
        self.assertEqual('^[0-9a-f]{64}$', selected[0]['pattern'])
        source = {'success': True, 'revision': 'a' * 64,
                  'rule': {'id': 'synthetic', 'content': 'a.example', '_revision': 2},
                  'usage_count': 1}
        target = {**source, 'revision': 'b' * 64}
        self.assertEqual([], pd.response_diffs(reply(source), reply(target), route, selected))
        for changed in [
            {**target, 'revision': 'b' * 63},
            {**target, 'revision': 'B' * 64},
            {**target, 'revision': 42},
            {key: value for key, value in target.items() if key != 'revision'},
            {**target, 'usage_count': 2},
            {**target, 'rule': {**source['rule'], 'content': 'changed.example'}},
            {**target, 'rule': {**source['rule'], '_revision': 3}},
        ]:
            with self.subTest(changed=changed):
                self.assertTrue(pd.response_diffs(reply(source), reply(changed), route, selected))
        self.assertTrue(pd.response_diffs(reply(source), reply(target), '/api/rule-library', selected))




if __name__ == '__main__':
    unittest.main()
