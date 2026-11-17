"""Mutation tests for every comparison class; run on the test server only."""
import copy
import json
import io
import zipfile
import tarfile
from pathlib import Path
import sqlite3
import tempfile
import unittest
from types import SimpleNamespace
import time

from compare import (Policy, differences, structure, response_diffs,
                     database_diffs, file_diffs)
from harness import Reply


class Comparators(unittest.TestCase):
    def test_cfg08_closure_requires_actual_equal_status_and_exact_absolute_query_identity(self):
        from config_finding_resolutions import annotate
        for case, route in [('R087-B91', 'GET /api/mosdns/rule-proxy'),
                ('R138-B91', 'GET /api/profiles/<profile_id>/mosdns/rule-proxy')]:
            row = {'case': case, 'route': route, 'group': 'rule-proxy-local-cache',
                'variant': 'absolute-query-dispatches-internal-content-endpoint', 'status': 'closed',
                'python_status': 200, 'go_status': 200,
                'differences': [{'path': path} for path in ('status', 'headers.content-type', 'body')]}
            self.assertEqual(annotate([copy.deepcopy(row)])[0]['fix_commits'], ['4786d7f86'])
            for mutation in ('open', 'blocked', 'route', 'variant', 'group', 'status', 'SQL'):
                bad = copy.deepcopy(row)
                if mutation in ('open', 'blocked'): bad['status'] = mutation
                elif mutation in ('route', 'variant', 'group'): bad[mutation] += '-wrong'
                elif mutation == 'status': bad['go_status'] = 502
                else: bad['differences'].append({'path': 'db.fields.unrelated.revision'})
                self.assertNotIn('4786d7f86', annotate([bad])[0]['fix_commits'], mutation)

    def test_protocol_closure_attribution_requires_an_actual_comparison_and_exact_identity(self):
        from config_finding_resolutions import annotate
        vectors = json.loads((Path(__file__).parent / 'testdata/cfg-harness-repairs.json').read_text())
        for vector in vectors:
            row = {key: vector[key] for key in ('case', 'group', 'variant', 'route')}
            row.update(status='closed', differences=[{'path': path} for path in vector['original_paths']])
            self.assertEqual(annotate([copy.deepcopy(row)])[0]['fix_commits'], ['2896b8d9e'])
            for mutation in ('open', 'blocked', 'wrong-case', 'SQL-path'):
                bad = copy.deepcopy(row)
                if mutation in ('open', 'blocked'): bad['status'] = mutation
                elif mutation == 'wrong-case': bad['case'] += '-wrong'
                else: bad['differences'].append({'path': 'db.fields.unrelated.revision'})
                self.assertNotIn('2896b8d9e', annotate([bad])[0]['fix_commits'], mutation)

    def test_propfind_equivalence_preserves_auth_depth_methods_objects_and_invalid_xml(self):
        from protocol_compare import dav_differences
        source = {'requests': [{'method': 'PROPFIND', 'path': '/backup/', 'depth': '1',
            'authorization': 'Basic synthetic', 'body': ''}], 'objects': {'kept': 'unchanged'}}
        target = copy.deepcopy(source)
        xml = '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>'
        for body in (xml, '<d:propfind xmlns:d="DAV:">\n<d:allprop />\n</d:propfind>'):
            target['requests'][0]['body'] = body
            self.assertFalse(dav_differences(source, target, Policy()))
            self.assertEqual(target['requests'][0]['body'], body, 'raw observation is retained')
        for body in (' ', '<propfind xmlns="DAV:"><propname/></propfind>',
                '<propfind xmlns="DAV:"><prop><getetag/></prop></propfind>',
                '<propfind xmlns="wrong:"><allprop/></propfind>',
                '<propfind xmlns="DAV:"><allprop/><include/></propfind>',
                '<propfind xmlns="DAV:" extra="1"><allprop/></propfind>',
                '<propfind xmlns="DAV:">unexpected<allprop/></propfind>',
                '<propfind xmlns="DAV:"><allprop extra="1"/></propfind>',
                '<propfind xmlns="DAV:"><allprop>unexpected</allprop></propfind>',
                '<!DOCTYPE propfind><propfind xmlns="DAV:"><allprop/></propfind>',
                '<propfind xmlns="DAV:"><!-- hidden --><allprop/></propfind>',
                '<?extra instruction?><propfind xmlns="DAV:"><allprop/></propfind>',
                '<propfind xmlns="DAV:"><allprop>', None, {}, 'x' * 4097):
            target['requests'][0]['body'] = body
            self.assertTrue(dav_differences(source, target, Policy()), repr(body)[:100])
        target['requests'][0]['body'] = xml
        for field, wrong in [('authorization', 'Basic wrong'), ('depth', '0'), ('path', '/other/'), ('method', 'GET')]:
            bad = copy.deepcopy(target); bad['requests'][0][field] = wrong
            self.assertTrue(dav_differences(source, bad, Policy()), field)
        for bad in ({**target, 'objects': {}}, {**target, 'requests': target['requests'] * 2}):
            self.assertTrue(dav_differences(source, bad, Policy()))
        put = copy.deepcopy(source); put['requests'][0]['method'] = 'PUT'
        other = copy.deepcopy(put); other['requests'][0]['body'] = xml
        self.assertTrue(dav_differences(put, other, Policy()), 'non-PROPFIND bytes remain strict')

    def test_connect_equivalence_retains_target_auth_body_order_count_and_get_headers(self):
        from protocol_compare import source_differences
        source = [{'method': 'CONNECT', 'path': 'origin.example:443', 'authorization': '',
            'proxy_authorization': 'Basic synthetic', 'request_line': 'CONNECT origin.example:443 HTTP/1.0',
            'host': '', 'user_agent': ''},
            {'method': 'GET', 'path': 'http://mirror.example/rules', 'authorization': '', 'user_agent': 'expected'}]
        target = copy.deepcopy(source)
        target[0].update(request_line='CONNECT origin.example:443 HTTP/1.1', host='ORIGIN.EXAMPLE:443',
            user_agent='Go-http-client/1.1')
        self.assertFalse(source_differences(source, target, Policy()))
        self.assertEqual(target[0]['host'], 'ORIGIN.EXAMPLE:443', 'raw wire data must not mutate')
        for address in ('[::1]:443', '[fe80::1%25scopeA]:443', 'legacy_name.example:443'):
            a, b = copy.deepcopy(source), copy.deepcopy(target)
            a[0].update(path=address, request_line='CONNECT ' + address + ' HTTP/1.0')
            b[0].update(path=address, request_line='CONNECT ' + address + ' HTTP/1.1', host=address)
            self.assertFalse(source_differences(a, b, Policy()), address)
        address = '[fe80::1%25scopeA]:443'
        a[0].update(path=address, request_line='CONNECT ' + address + ' HTTP/1.0')
        b[0].update(path=address, request_line='CONNECT ' + address + ' HTTP/1.1', host='[fe80::1%25scopeB]:443')
        self.assertTrue(source_differences(a, b, Policy()), 'different IPv6 scope/authority is not equivalent')
        for field, value in [('path', 'other.example:443'), ('host', 'other.example:443'), ('host', ''),
                ('proxy_authorization', 'Basic wrong'), ('authorization', 'Bearer unexpected'),
                ('request_line', 'CONNECT other.example:443 HTTP/1.1'),
                ('request_line', 'CONNECT origin.example:443 HTTP/2.0'),
                ('request_line', 'GET origin.example:443 HTTP/1.1'),
                ('user_agent', 'unexpected-secret'), ('body_hex', '77726f6e67'), ('extra', True),
                ('path', 'origin.example:0'), ('path', 'origin.example:' + '9' * 5000)]:
            bad = copy.deepcopy(target); bad[0][field] = value
            self.assertTrue(source_differences(source, bad, Policy()), field)
        for bad in (target[::-1], target + [target[1]], target[:1]):
            self.assertTrue(source_differences(source, bad, Policy()))
        bad = copy.deepcopy(target); bad[1]['user_agent'] = 'Go-http-client/1.1'
        self.assertTrue(source_differences(source, bad, Policy()), 'GET header is not CONNECT metadata')
        bad = copy.deepcopy(source); bad[0]['request_line'] = 'malformed'
        self.assertTrue(source_differences(bad, bad, Policy()), 'invalid metadata cannot certify itself')

    def test_explicit_pending_dependency_skips_only_go_without_hiding_normal_requests(self):
        from unittest.mock import patch
        from harness import Pair, Recorder, DesignError
        route = 'GET /api/rules/dns'
        pair = SimpleNamespace(policy=Policy(), unimplemented_mutations=set(),
            route_available=lambda _: True, dirs=['python-dir', 'go-dir'], bases=['python', 'go'],
            tokens=['source-token', 'go-token'], default_profile='default', recorder=Recorder())
        good = Reply(200, {'content-type': 'application/json'}, b'{"ok":true}')
        wrong = Reply(200, good.headers, b'{"ok":false}')
        with patch('harness.send', side_effect=[good, good, wrong]) as send, \
                patch('harness.database', return_value={'unchanged': True}), \
                patch('harness.stored_files', return_value={}):
            replies = Pair.request(pair, 'H911', route, writes=True, expect=200,
                pending_dependency='CFG-04 / A17 incomplete binding and background owner')
            self.assertEqual(send.call_count, 1)
            self.assertEqual(replies[1].status, 0)
            row = pair.recorder.results[-1]
            self.assertEqual(row['state'], 'go-unimplemented-dependency')
            self.assertIn('A17', row['blocked_by'][0])
            self.assertFalse(pair.unimplemented_mutations, 'deferral does not silently block the next request')
            Pair.request(pair, 'R188-S01', route, expect=200)
            self.assertEqual(send.call_count, 3)
            self.assertEqual(pair.recorder.results[-1]['state'], 'different', 'ordinary Go body mismatch remains visible')
        from compare import Difference
        with patch('harness.send', return_value=good) as send, \
                patch('harness.database_diffs', return_value=[Difference('db.fields.kept.revision', 'value', 1, 2)]), \
                patch('harness.file_diffs', return_value=[Difference('files.kept', 'value', 'old', 'wrong')]):
            Pair.request(pair, 'R188-S01', route, expect=200, writes=True)
            self.assertEqual(send.call_count, 2)
            self.assertEqual(pair.recorder.results[-1]['state'], 'different')
            self.assertEqual(len(pair.recorder.results[-1]['differences']), 2, 'SQL and files remain strict after a deferred request')
        with patch('harness.send', return_value=wrong), patch('harness.database', return_value={}), \
                patch('harness.stored_files', return_value={}):
            with self.assertRaises(DesignError):
                Pair.request(pair, 'H911', route, expect=400, pending_dependency='A17')
        from finding_reviews import reviews, markdown
        old = {'case': 'H911', 'variant': '', 'route': route, 'group': 'dns', 'state': 'different', 'differences': []}
        deferred = {**old, **row, 'group': 'dns'}
        review = reviews([old], [deferred], baseline='synthetic')[0]
        self.assertEqual(review['status'], 'blocked')
        self.assertIn('A17', '\n'.join(markdown([review])))
        resumed = reviews([], [{**old, 'state': 'different'}], [review], baseline='restored')[0]
        self.assertEqual(resumed['status'], 'open')
        self.assertNotIn('blocked_by', resumed)

    def test_long_flow_audit_does_not_turn_mapping_into_observed_execution(self):
        from coverage_audit import long_flows
        text = '- **T01 first**\n- **T02 second**\n| T02 | partial steps | real process missing |\n'
        rows = [{'case': 'T01'}, {'case': 'R001-S01'}]
        value = long_flows(text, rows)
        self.assertEqual(value['long_flow_observed_labels'], ['T01'])
        self.assertEqual(value['long_flow_partial_mapped_labels'], ['T02'])
        self.assertFalse(value['long_flow_unaccounted'])
        self.assertEqual(long_flows(text.split('|')[0], rows)['long_flow_unaccounted'], ['T02'])
        self.assertEqual(long_flows(text, rows + [{'case': 'T99'}])['long_flow_unknown'], ['T99'])
        with self.assertRaises(ValueError):
            long_flows(text + '| T02 | duplicate | wrong |\n', rows)

    def test_c29_finite_approvals_preserve_every_unapproved_difference(self):
        from config_finding_resolutions import annotate, c29_vectors
        for vector in c29_vectors()['layouts']:
            record = {key: vector[key] for key in ('case', 'group', 'route', 'variant')}
            record.update(status='open', python_status=vector['code'], go_status=vector['code'], differences=[],
                remaining_differences=[{'path': path, 'kind': 'value', 'python': 'plugins: []\n',
                    'go': 'plugins: [ ]\n'} for path in vector['paths']])
            self.assertEqual(annotate([copy.deepcopy(record)])[0]['status'], 'accepted')
            for mutate in ('extra-SQL', 'wrong-type', 'changed-value', 'missing-field', 'unknown-identity'):
                bad = copy.deepcopy(record)
                if mutate == 'extra-SQL': bad['remaining_differences'].append(
                    {'path': 'db.fields.profile/default/oxidns.revision', 'kind': 'value', 'python': 1, 'go': 2})
                elif mutate == 'wrong-type': bad['remaining_differences'][0]['go'] = 'plugins: null\n'
                elif mutate == 'changed-value': bad['remaining_differences'][0]['go'] = 'plugins: [changed]\n'
                elif mutate == 'missing-field': bad['remaining_differences'][0]['go'] = '{}\n'
                else: bad['variant'] += '-unexpected'
                self.assertEqual(annotate([bad])[0]['status'], 'open', mutate)
        for vector in c29_vectors()['messages']:
            record = {key: vector[key] for key in ('case', 'group', 'route', 'variant')}
            record.update(status='open', python_status=400, go_status=400, differences=[], remaining_differences=[
                {'path': 'body.message', 'kind': 'value', 'python': vector['python'], 'go': vector['go']}])
            self.assertEqual(annotate([copy.deepcopy(record)])[0]['status'], 'accepted')
            for mutate in ('message', 'status', 'extra-field', 'identity'):
                bad = copy.deepcopy(record)
                if mutate == 'message': bad['remaining_differences'][0]['go'] += 'wrong'
                elif mutate == 'status': bad['go_status'] = 500
                elif mutate == 'extra-field': bad['remaining_differences'].append(
                    {'path': 'body.unexpected', 'kind': 'extra-in-go', 'python': None, 'go': True})
                else: bad['variant'] += '-unexpected'
                self.assertEqual(annotate([bad])[0]['status'], 'open', mutate)

    def test_go_generated_log_off_projection_cannot_hide_false_or_explicit_bool(self):
        from cases.oxidns_controls import draft_projection
        original = {'config': "log:\n  level: 'off'\n"}
        expected = draft_projection(original, 0)
        plain = {'config': 'log:\n  level: off\n'}
        self.assertEqual(draft_projection(plain, 1), expected)
        self.assertNotEqual(draft_projection(plain, 0), expected, 'Python YAML 1.1 oracle remains unchanged')
        for text in ('log:\n  level: false\n', 'log:\n  level: !!bool off\n',
                'log:\n  level: OFF\n', 'log:\n  level: off\nextra: false\n'):
            self.assertNotEqual(draft_projection({'config': text}, 1), expected)
        self.assertEqual(original['config'], "log:\n  level: 'off'\n")
        preview = {'content': "log:\n  level: 'off'\n", 'service': 'oxidns'}
        expected_preview = draft_projection(preview, 0, 'content')
        self.assertEqual(draft_projection({'content': plain['config'], 'service': 'oxidns'}, 1, 'content'), expected_preview)
        for text in ('log:\n  level: false\n', 'log:\n  level: !!bool off\n'):
            self.assertNotEqual(draft_projection({'content': text, 'service': 'oxidns'}, 1, 'content'), expected_preview)

    def test_head_control_uses_get_metadata_and_detects_wrong_wire_or_storage(self):
        from unittest.mock import patch
        from cases.http_shell import oxidns_head_control, OXIDNS_HEAD_ETAG
        fixture = SimpleNamespace(bases=['python', 'go'], dirs=['python-dir', 'go-dir'], tokens=['a', 'b'])
        headers = {'content-type': 'application/json', 'etag': OXIDNS_HEAD_ETAG}
        source_get = Reply(200, headers, b'{"settings":true}')
        go_get = Reply(200, headers, b'{"settings":true}')
        heads = [Reply(400, {}, b''), Reply(200, headers, b'')]
        rows = {'fields': {'profile/default/oxidns': {'value': {}}}}
        def observe(replies, target=go_get, files=None):
            with patch('cases.http_shell.send', side_effect=[source_get, target]), \
                    patch('cases.http_shell.database', return_value=rows), \
                    patch('cases.http_shell.stored_files', side_effect=files or [{}, {}, {}, {}]), \
                    patch('cases.http_shell.etag', return_value=OXIDNS_HEAD_ETAG):
                return oxidns_head_control(fixture, replies, Policy())
        self.assertFalse(observe(heads), 'legitimate GET body is not mistaken for a nonempty HEAD')
        for reply in (Reply(500, headers, b''), Reply(200, headers, b'wrong HEAD body'),
                Reply(200, {**headers, 'etag': 'wrong-etag'}, b'')):
            self.assertTrue(observe([heads[0], reply]))
        self.assertTrue(observe(heads, target=Reply(500, headers, b'{}')))
        self.assertTrue(observe(heads, target=Reply(200, {**headers, 'etag': 'wrong'}, b'{}')))
        self.assertTrue(observe(heads, files=[{}, {}, {}, {'unexpected': 'write'}]))

    def test_c29_head_approval_requires_source_get_etag_and_exact_changes(self):
        from config_finding_resolutions import annotate
        from cases.http_shell import OXIDNS_HEAD_ETAG
        record = {'case': 'X06', 'group': 'http-head', 'route': 'GET /api/oxidns/settings',
            'variant': 'head//api/oxidns/settings', 'status': 'open', 'python_status': 400, 'go_status': 200,
            'differences': [], 'remaining_differences': [
                {'path': 'status', 'kind': 'value', 'python': 400, 'go': 200},
                {'path': 'headers.etag', 'kind': 'value', 'python': '', 'go': OXIDNS_HEAD_ETAG}]}
        self.assertEqual(annotate([copy.deepcopy(record)])[0]['status'], 'accepted')
        for mutate in ('etag', 'storage', 'GET-status', 'route'):
            bad = copy.deepcopy(record)
            if mutate == 'etag': bad['remaining_differences'][1]['go'] = '\"' + '0' * 64 + '\"'
            elif mutate == 'storage': bad['remaining_differences'].append(
                {'path': 'db.fields.profile/default/oxidns.revision', 'kind': 'value', 'python': 1, 'go': 2})
            elif mutate == 'GET-status': bad['remaining_differences'].append(
                {'path': 'status', 'kind': 'value', 'python': 500, 'go': 200})
            else: bad['route'] += '-unexpected'
            self.assertEqual(annotate([bad])[0]['status'], 'open', mutate)

    def test_native_yaml_layout_approval_keeps_typed_nodes_duplicates_order_and_storage(self):
        from config_finding_resolutions import annotate
        source = 'plugins:\n- tag: kept\n  args:\n    values: [1, 2]\n'
        target = 'plugins:\n  - tag: "kept"\n    args:\n      values:\n        - 1\n        - 2\n'
        record = {'case': 'R189-S01', 'group': 'rule-dns-choices', 'route': 'PUT /api/rules/dns',
            'variant': 'native/direct', 'status': 'open', 'python_status': 200, 'go_status': 200,
            'differences': [], 'remaining_differences': [{'path': 'db.fields.profile/default/oxidns.value.native_config',
                'kind': 'value', 'python': source, 'go': target}]}
        self.assertEqual(annotate([copy.deepcopy(record)])[0]['status'], 'accepted')
        for bad_text in (target.replace('"kept"', 'changed'), target.replace('- 1', '- "1"'),
                target.replace('- 1\n        - 2', '- 2\n        - 1'),
                target + 'extra: true\n', target + '# unexpected comment\n', target.replace('\n', '\r\n'),
                target.replace('  - tag:', ' - tag:'), 'plugins: []\n', source + 'plugins: []\n',
                'plugins: !unknown []\n', 'plugins: &cycle [*cycle]\n', '#' * 262145):
            bad = copy.deepcopy(record)
            bad['remaining_differences'][0]['go'] = bad_text
            self.assertEqual(annotate([bad])[0]['status'], 'open', bad_text[:80])
        for mutation in ('route', 'group', 'variant', 'status', 'extra-SQL', 'duplicate'):
            bad = copy.deepcopy(record)
            if mutation in ('route', 'group', 'variant'): bad[mutation] += '-unexpected'
            elif mutation == 'status': bad['go_status'] = 400
            elif mutation == 'extra-SQL': bad['remaining_differences'].append(
                {'path': 'db.fields.profile/default/oxidns.revision', 'kind': 'value', 'python': 1, 'go': 2})
            else: bad['remaining_differences'].append(copy.deepcopy(bad['remaining_differences'][0]))
            self.assertEqual(annotate([bad])[0]['status'], 'open', mutation)

    def test_rules_boundary_approvals_keep_exact_envelope_and_all_storage_differences(self):
        from config_finding_resolutions import annotate
        head = {'case': 'X06', 'group': 'http-head', 'route': 'GET /api/rules', 'variant': 'head//api/rules',
            'status': 'open', 'python_status': 500, 'go_status': 200, 'differences': [],
            'remaining_differences': [
                {'path': 'status', 'kind': 'value', 'python': 500, 'go': 200},
                {'path': 'headers.content-type', 'kind': 'value', 'python': 'text/html; charset=utf-8', 'go': 'application/json'}]}
        message = 'Rule not found'
        missing = {'case': 'R185-F16', 'group': 'rules-update', 'route': 'PUT /api/rules/<rule_id>', 'variant': '',
            'status': 'open', 'python_status': 404, 'go_status': 404, 'differences': [],
            'remaining_differences': [
                {'path': 'headers.content-type', 'kind': 'value', 'python': 'text/html; charset=utf-8', 'go': 'application/json'},
                {'path': 'body', 'kind': 'type', 'python': ['<!doctype html>', '<html lang=en>', '<title>404 Not Found</title>',
                    '<h1>Not Found</h1>', '<p>Rule not found</p>'], 'go': {'message': message, 'success': False}}]}
        for record in (head, missing):
            self.assertEqual(annotate([copy.deepcopy(record)])[0]['status'], 'accepted')
            for mutation in ('route', 'group', 'variant', 'status', 'extra-SQL', 'duplicate', 'message-or-header'):
                bad = copy.deepcopy(record)
                if mutation in ('route', 'group', 'variant'): bad[mutation] += '-unexpected'
                elif mutation == 'status': bad['go_status'] = 403
                elif mutation == 'extra-SQL': bad['remaining_differences'].append(
                    {'path': 'db.fields.profile/default/updated_at.revision', 'kind': 'value', 'python': 1, 'go': 2})
                elif mutation == 'duplicate': bad['remaining_differences'].append(copy.deepcopy(bad['remaining_differences'][0]))
                else: bad['remaining_differences'][-1]['go'] = 'wrong-response'
                self.assertEqual(annotate([bad])[0]['status'], 'open', mutation)

    def test_rules_timestamp_closure_requires_real_recheck_and_exact_original_paths(self):
        from config_finding_resolutions import annotate
        record = {'case': 'R183-S01', 'variant': '', 'route': 'POST /api/rules', 'group': 'rules-create',
            'status': 'closed', 'differences': [{'path': 'db.fields.profile/default/updated_at.value',
                'kind': 'value', 'python': 'sample-a', 'go': 'sample-b'}]}
        self.assertEqual(annotate([copy.deepcopy(record)])[0]['fix_commits'], ['fd6355ce0'])
        for status in ('open', 'pending', 'blocked'):
            bad = copy.deepcopy(record)
            bad['status'] = status
            self.assertNotIn('fd6355ce0', annotate([bad])[0]['fix_commits'])
        for field, value in [('group', 'unrelated-group'), ('differences', [
                {'path': 'db.fields.profile/default/updated_at.revision', 'kind': 'value', 'python': 1, 'go': 2}])]:
            bad = copy.deepcopy(record)
            bad[field] = value
            self.assertNotIn('fd6355ce0', annotate([bad])[0]['fix_commits'])

    def test_profile_write_fixture_validates_metadata_times_without_hiding_other_rows(self):
        from datetime import datetime, timezone
        from unittest.mock import patch
        from cases.helpers import profile_pair
        now = 1760000000.25
        original = Policy(start=now - 1, end=now + 1, internal_errors={'body.message': 'known'})
        fixture = SimpleNamespace(policy=original)
        class Context:
            def __enter__(self): return fixture
            def __exit__(self, *args): pass
        with patch('harness.Pair', return_value=Context()) as factory:
            with profile_pair('binary', 'recorder', options={'profile': {}}) as pair:
                factory.assert_called_once_with('binary', 'recorder', options={'profile': {}})
                self.assertIs(pair, fixture)
                self.assertEqual(pair.policy.internal_errors, original.internal_errors)
                self.assertFalse(original.times, 'profile fixture does not mutate another policy')
                left = datetime.fromtimestamp(now, timezone.utc).isoformat()
                right = datetime.fromtimestamp(now + .001, timezone.utc).isoformat()
                path = 'db.fields.profile/default/updated_at.value'
                self.assertFalse(differences(left, right, path, pair.policy))
                for bad in ('1990-01-01T00:00:00+00:00', 'invalid', right.replace('+00:00', 'Z')):
                    self.assertTrue(differences(left, bad, path, pair.policy), bad)
                self.assertTrue(differences(left, right, 'db.fields.profile/default/unrelated.value', pair.policy))
                self.assertTrue(differences(3, 4, 'db.fields.profile/default/updated_at.revision', pair.policy))
                self.assertTrue(differences({'dns': 'direct'}, {'dns': 'proxy'},
                    'db.fields.profile/default/oxidns.value', pair.policy))

    def test_automatic_backup_compares_complete_snapshots_wire_objects_auth_and_storage(self):
        from datetime import datetime, timezone
        from cases.automatic_backup import snapshots_diffs
        now = time.time()
        doc = {'system': {'setting': 'kept'}, 'shared': {'nodes': []},
            'profiles': {'default': {'updated_at': datetime.fromtimestamp(now, timezone.utc).isoformat(), 'unknown': ['中文🧪']}}}
        with tempfile.TemporaryDirectory() as root:
            dirs = [Path(root) / name for name in ('python', 'go')]
            for directory in dirs:
                directory.mkdir()
                with sqlite3.connect(directory / 'configflow.sqlite3') as db:
                    db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                        'CREATE TABLE fields(scope,owner,field,position,value,revision);')
            snapshots = []
            for offset in (0, .001):
                path = '/backup/config_backup_' + datetime.fromtimestamp(now + offset, timezone.utc).strftime('%Y%m%d_%H%M%S_%f') + '.json'
                snapshots.append({'requests': [{'method': 'PUT', 'path': path, 'authorization': 'Basic synthetic',
                    'depth': '', 'body': copy.deepcopy(doc)}], 'directories': ['/', '/backup/'], 'objects': {path: copy.deepcopy(doc)}})
            fakes = [SimpleNamespace(basic='Basic synthetic')] * 2
            def compare(observed, expected=None):
                pair = SimpleNamespace(dirs=dirs, policy=Policy(start=now - 2, end=now + 2))
                return snapshots_diffs(pair, observed, expected or [[doc], [doc]], fakes)
            self.assertFalse(compare(snapshots))
            for mutation in ('lost-upload', 'wrong-auth', 'wrong-body', 'extra-body-field', 'extra-object', 'wrong-directory', 'invalid-calendar-name'):
                bad = copy.deepcopy(snapshots)
                row = bad[1]['requests'][0]
                if mutation == 'lost-upload': bad[1]['requests'] = []
                elif mutation == 'wrong-auth': row['authorization'] = 'Bearer secret'
                elif mutation == 'wrong-body': row['body']['system']['setting'] = 'lost'
                elif mutation == 'extra-body-field': row['body']['extra'] = True
                elif mutation == 'extra-object': bad[1]['objects']['/unexpected.json'] = {'extra': True}
                elif mutation == 'wrong-directory': bad[1]['directories'] = ['/unexpected/']
                else: row['path'] = '/backup/config_backup_20261399_990000_000000.json'
                self.assertTrue(compare(bad), mutation)
            # Even a wrong Go body that agrees with its wrong own SQL proof
            # must still fail the complete cross-implementation payload check.
            bad = copy.deepcopy(snapshots)
            wrong = copy.deepcopy(doc)
            wrong['system']['setting'] = 'lost'
            bad[1]['requests'][0]['body'] = wrong
            self.assertTrue(compare(bad, [[doc], [wrong]]))
            with sqlite3.connect(dirs[1] / 'configflow.sqlite3') as db:
                db.execute('INSERT INTO fields VALUES (?,?,?,?,?,?)', ('root', '', 'unexpected', 0, 'true', 1))
            self.assertTrue(compare(snapshots), 'unexpected SQL row')
            with sqlite3.connect(dirs[1] / 'configflow.sqlite3') as db: db.execute('DELETE FROM fields')
            (dirs[1] / 'unexpected-file').write_bytes(b'changed')
            self.assertTrue(compare(snapshots), 'unexpected persisted file')

    def test_automatic_backup_SQL_document_reader_preserves_all_rows_and_is_readonly(self):
        from cases.automatic_backup import document
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / 'configflow.sqlite3'
            with sqlite3.connect(path) as db:
                db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                    'CREATE TABLE fields(scope,owner,field,position,value,revision);')
                db.execute('INSERT INTO meta VALUES (?,?)', ('root_order', json.dumps(['schema_version', 'system', 'shared', 'profiles', 'unknown'])))
                db.executemany('INSERT INTO sections VALUES (?,?,?)', [('root', '', 0), ('system', '', 0), ('shared', '', 0), ('profile', 'default', 0)])
                for scope, owner, field, value in [('root', '', 'schema_version', 5), ('root', '', 'unknown', {'keep': [1, '🧪']}),
                        ('system', '', 'backup', {'enabled': True}), ('shared', '', 'nodes', []), ('profile', 'default', 'name', '合成')]:
                    db.execute('INSERT INTO fields VALUES (?,?,?,?,?,?)', (scope, owner, field, 0, json.dumps(value), 7))
            before = path.read_bytes()
            self.assertEqual(document(Path(root)), {'schema_version': 5, 'system': {'backup': {'enabled': True}},
                'shared': {'nodes': []}, 'profiles': {'default': {'name': '合成'}}, 'unknown': {'keep': [1, '🧪']}})
            self.assertEqual(path.read_bytes(), before)

    def test_automatic_backup_timeout_is_source_premise_only_for_Python(self):
        from unittest.mock import patch
        from cases.automatic_backup import wait_for
        from harness import DesignError
        with patch('cases.automatic_backup.time.monotonic', side_effect=[0, 20]):
            self.assertFalse(wait_for(1, lambda: False, 'synthetic missing Go effect'))
        with patch('cases.automatic_backup.time.monotonic', side_effect=[0, 20]):
            with self.assertRaises(DesignError): wait_for(0, lambda: False, 'synthetic invalid source fixture')

    def test_N8_login_approvals_require_exact_error_envelopes_and_valid_source_JWT(self):
        import jwt
        from harness import SECRET, USERNAME
        from config_finding_resolutions import native_binding_basis
        bad = 'The browser (or proxy) sent a request that this server could not understand.'
        def diff(path, kind, python, go):
            return {'path': path, 'kind': kind, 'python': python, 'go': go}
        value = {'case': 'R011-F06', 'group': 'auth-routes', 'variant': '', 'route': 'POST /api/auth/login',
            'status': 'open', 'python_status': 500, 'go_status': 400, 'remaining_differences': [
                diff('status', 'value', 500, 400), diff('body.message', 'internal-error-prefix',
                    "登录失败。原始错误：'list' object has no attribute 'get'", bad)]}
        self.assertTrue(native_binding_basis(value))
        token = jwt.encode({'username': USERNAME, 'iat': 1700000000, 'exp': 1700086400}, SECRET, algorithm='HS256')
        finite = {**value, 'case': 'X04', 'group': 'json-decoder', 'variant': 'login/ignored-extra/NaN', 'python_status': 200,
            'remaining_differences': [diff('body.token', 'invalid-Go-JWT', '<valid-HS256>', '<invalid>'),
                diff('status', 'value', 200, 400), diff('body.expiresIn', 'missing-in-go', 86400, None),
                diff('body.message', 'extra-in-go', None, bad), diff('body.success', 'value', True, False),
                diff('body.token', 'missing-in-go', token, None), diff('body.username', 'missing-in-go', USERNAME, None)]}
        self.assertTrue(native_binding_basis(finite))
        for original in (value, finite):
            for mutation in ('extra-SQL', 'wrong-status', 'wrong-route', 'wrong-group', 'changed-message', 'duplicate-difference'):
                wrong = copy.deepcopy(original)
                if mutation == 'extra-SQL': wrong['remaining_differences'].append(diff('db.fields.secret', 'value', 'kept', 'lost'))
                elif mutation == 'wrong-status': wrong['go_status'] = 415
                elif mutation == 'wrong-route': wrong['route'] = 'POST /api/unrelated'
                elif mutation == 'wrong-group': wrong['group'] = 'unrelated'
                elif mutation == 'changed-message':
                    next(row for row in wrong['remaining_differences'] if row['path'] == 'body.message')['go'] += 'changed'
                else: wrong['remaining_differences'].append(copy.deepcopy(wrong['remaining_differences'][0]))
                self.assertFalse(native_binding_basis(wrong), mutation)
        for claims in ({'username': 'wrong', 'iat': 1700000000, 'exp': 1700086400},
                {'username': USERNAME, 'iat': 1700000000, 'exp': 1700086401},
                {'username': USERNAME, 'iat': 1700000000, 'exp': 1700086400, 'extra': True}):
            wrong = copy.deepcopy(finite)
            next(row for row in wrong['remaining_differences'] if row['kind'] == 'missing-in-go' and row['path'] == 'body.token')['python'] = jwt.encode(claims, SECRET, algorithm='HS256')
            self.assertFalse(native_binding_basis(wrong), claims)

    def test_aborted_source_effects_compare_every_SQL_value_file_byte_and_mode(self):
        from cases.source_write_disconnect import effects, bind_composition, control_policy
        with tempfile.TemporaryDirectory() as root:
            dirs = [Path(root) / part for part in ('control', 'aborted')]
            for index, directory in enumerate(dirs):
                directory.mkdir()
                with sqlite3.connect(directory / 'configflow.sqlite3') as db:
                    db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                        'CREATE TABLE fields(scope,owner,field,position,value,revision);')
                    db.execute('INSERT INTO fields VALUES (?,?,?,?,?,?)', ('shared', '', 'rule_library', 0, '[{"id":"fixed"}]', 1))
                    db.execute('INSERT INTO meta VALUES (?,?)', ('commit_id', json.dumps(('a' if index == 0 else 'b') * 32)))
                    db.execute('INSERT INTO meta VALUES (?,?)', ('generation', '1'))
                (directory / 'complete.list').write_bytes(b'unchanged\n')
                (directory / 'complete.list').chmod(0o600)
            self.assertEqual(effects(*dirs, 'create', Policy()), [])
            previous = Policy()
            previous.bind('c' * 32, 'b' * 32, '[a-f0-9]{32}', 'previous-cross-backend-pair')
            self.assertTrue(effects(*dirs, 'create', copy.deepcopy(previous)))
            self.assertFalse(effects(*dirs, 'create', control_policy(previous)))
            self.assertEqual(previous.identities, {'c' * 32: 'b' * 32})
            with sqlite3.connect(dirs[1] / 'configflow.sqlite3') as db:
                db.execute("UPDATE meta SET value='2' WHERE key='generation'")
            self.assertTrue(effects(*dirs, 'create', control_policy(previous)), 'extra commit must remain visible')
            with sqlite3.connect(dirs[1] / 'configflow.sqlite3') as db:
                db.execute("UPDATE meta SET value='1' WHERE key='generation'")
            with sqlite3.connect(dirs[1] / 'configflow.sqlite3') as db:
                db.execute('UPDATE fields SET revision=2')
            self.assertTrue(effects(*dirs, 'create', Policy()))
            with sqlite3.connect(dirs[1] / 'configflow.sqlite3') as db:
                db.execute('UPDATE fields SET revision=1')
            path = dirs[1] / 'complete.list'
            path.write_bytes(b'changed\n')
            self.assertTrue(effects(*dirs, 'create', Policy()))
            path.write_bytes(b'unchanged\n')
            path.chmod(0o644)
            self.assertTrue(effects(*dirs, 'create', Policy()))
            path.chmod(0o600)
            (dirs[1] / 'unexpected').write_text('extra')
            self.assertTrue(effects(*dirs, 'create', Policy()))
        library = {'id': 'lib_' + 'a' * 12, 'source_type': 'url', 'url': 'http://source.example/rules', 'behavior': 'domain'}
        rule = {'id': 'ruleset_' + 'b' * 12, 'itemType': 'ruleset', 'library_rule_id': library['id']}
        right = ([{**library, 'id': 'lib_' + 'c' * 12}], [{**rule, 'id': 'ruleset_' + 'd' * 12}])
        self.assertFalse(bind_composition(([library], [rule]), right, Policy()))
        for value in ([], {}, None, 'invalid'):
            bad = copy.deepcopy(right)
            bad[0][0]['id'] = value
            self.assertTrue(bind_composition(([library], [rule]), bad, Policy()))
        self.assertTrue(bind_composition(([library], [rule]), ([library, library], [rule]), Policy()))
        self.assertTrue(bind_composition(([library], [rule]), ([None], [rule]), Policy()))

    def test_internal_redirect_observer_detects_extra_destination_and_changed_transport_path(self):
        from cases.rule_fetch_edges import InternalRedirectBoundary
        observer = InternalRedirectBoundary.__new__(InternalRedirectBoundary)
        observer.before_values, observer.after_values = [{}, {}], [{}, {}]
        observer.exchange = SimpleNamespace(compare=lambda policy: [])
        observer.proof = [{'destination_requests': [], 'source_paths': ['/redirect']},
                          {'destination_requests': [], 'source_paths': []}]
        self.assertEqual(observer.compare(Policy()), [])
        observer.proof[1]['destination_requests'] = [{'method': 'GET', 'path': '/landing'}]
        self.assertTrue(observer.compare(Policy()))
        observer.proof[1]['destination_requests'] = []
        observer.proof[1]['source_paths'] = ['/redirect']
        self.assertTrue(observer.compare(Policy()))  # Earlier refusal must be exact.
        observer.proof[1]['source_paths'].append('/landing')
        self.assertTrue(observer.compare(Policy()))
        observer.proof[1]['source_paths'] = []
        observer.proof[0]['source_paths'] = ['/changed-reference-path']
        self.assertTrue(observer.compare(Policy()))
        observer.proof[0]['source_paths'] = ['/redirect']
        observer.before_values[1] = {'changed': True}
        self.assertTrue(observer.compare(Policy()))  # Storage isolation still checked.

    def test_coverage_accounting_never_turns_dispositions_or_Go_skips_into_passes(self):
        from coverage_audit import evaluate, dispositions
        expected = {'R001-S01': {'route': 'GET /synthetic'}, 'R001-F01': {'route': 'GET /synthetic'},
            'H001': {'route': 'GET /synthetic'}}
        rows = [{'case': 'R001-S01', 'helper_cases': ['H001'], 'state': 'go-unimplemented'}]
        declared = {'R001-F01': {'kind': 'partial-race', 'reason': 'requires a real storage barrier'}}
        value = evaluate(expected, rows, declared)
        self.assertEqual(value['exercised_reference_recipes'], 2)
        self.assertEqual(value['not_exercised'], {'R001-F01': {**expected['R001-F01'], **declared['R001-F01']}})
        self.assertFalse(value['unaccounted'])
        self.assertEqual(evaluate(expected, rows, {})['unaccounted'], ['R001-F01'])
        self.assertEqual(evaluate(expected, [], declared)['unaccounted'], ['H001', 'R001-S01'])
        self.assertEqual(evaluate(expected, rows, {'H999': declared['R001-F01']})['unknown_labels'], ['H999'])
        block = '<!-- CONFIGDIFF_PARTIAL_BEGIN -->\n| H001-H002 | earlier-guard | synthetic proof |\n<!-- CONFIGDIFF_PARTIAL_END -->'
        self.assertEqual(set(dispositions(block)), {'H001', 'H002'})
        with self.assertRaises(ValueError): dispositions(block.replace('<!-- CONFIGDIFF_PARTIAL_END -->', '| H002 | partial | duplicate |\n<!-- CONFIGDIFF_PARTIAL_END -->'))

    def test_route_manifest_rejects_tagged_stale_or_mismatched_binary_sidecars(self):
        import hashlib
        from build import route_manifest, manifest_for_server
        with tempfile.TemporaryDirectory() as root:
            binary = Path(root) / 'synthetic-unexecuted-binary'
            binary.write_bytes(b'production-placeholder')
            manifest = b'GET\t/api/auth/status\nPOST\t/api/auth/login\n'
            Path(str(binary) + '.routes.tsv').write_bytes(manifest)
            metadata = {'build_tags': [], 'manifest_build_tags': ['configflow_proddiff'],
                'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
                'manifest_sha256': hashlib.sha256(manifest).hexdigest()}
            path = Path(str(binary) + '.build.json')
            path.write_text(json.dumps(metadata))
            self.assertEqual(route_manifest(binary), {('GET', '/api/auth/status'), ('POST', '/api/auth/login')})
            self.assertEqual(manifest_for_server(binary), route_manifest(binary))
            for field, value in [('build_tags', ['configflow_proddiff']), ('manifest_build_tags', []),
                    ('binary_sha256', '0' * 64), ('manifest_sha256', '0' * 64)]:
                path.write_text(json.dumps({**metadata, field: value}))
                with self.assertRaises(ValueError, msg=field): route_manifest(binary)
            path.write_text(json.dumps(metadata))
            binary.write_bytes(b'changed-production-placeholder')
            with self.assertRaises(ValueError): route_manifest(binary)
            with self.assertRaises(ValueError): manifest_for_server(binary)

    def test_recursion_limit_saved_JSON_keeps_all_original_bytes_and_mode(self):
        from compare import files
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / 'web-listener.json'
            content = ('[' * 2000 + ']' * 2000).encode()
            path.write_bytes(content)
            path.chmod(0o600)
            valid = files(root)
            self.assertEqual(valid, {'web-listener.json': {'mode': 0o600,
                'content': {'invalid_structured_file_bytes': content}}})
            path.write_bytes(content + b'changed')
            self.assertTrue(differences(valid, files(root)), 'corrupt data is not dropped or treated as equal')
            path.write_bytes(content)
            path.chmod(0o644)
            self.assertTrue(differences(valid, files(root)), 'corrupt file permissions still compare')

    def test_log_poll_normalization_preserves_extra_fields_counts_events_and_invalid_identities(self):
        from cases.log_sequence import poll_value
        marker = '合成日志：'
        with tempfile.TemporaryDirectory() as root:
            directory = Path(root)
            raw = '2026-01-01 00:00:00,000 - synthetic - WARNING - ' + marker + '完整事件\n'
            (directory / 'app.log').write_text(raw)
            data = {'success': True, 'logs': [raw.rstrip('\n')], 'total_lines': 1,
                'filtered_lines': 1, 'log_file': str(directory / 'app.log')}
            reply = lambda value: Reply(200, {'content-type': 'application/json'}, json.dumps(value).encode())
            valid = poll_value(reply(data), directory, marker)
            self.assertFalse(valid['violations'])
            for kind in ('wrong-file', 'wrong-total', 'wrong-filtered', 'lost-event', 'extra-field', 'wrong-message', 'wrong-level', 'non-string-line'):
                bad = copy.deepcopy(data)
                if kind == 'wrong-file': bad['log_file'] = '/unexpected/app.log'
                elif kind == 'wrong-total': bad['total_lines'] = 3
                elif kind == 'wrong-filtered': bad['filtered_lines'] = 3
                elif kind == 'lost-event': bad['logs'] = []
                elif kind == 'extra-field': bad['unexpected'] = True
                elif kind == 'wrong-message': bad['logs'][0] += '篡改'
                elif kind == 'wrong-level': bad['logs'][0] = bad['logs'][0].replace('WARNING', 'ERROR')
                else: bad['logs'] = [None]
                self.assertTrue(differences(valid, poll_value(reply(bad), directory, marker)), kind)

    def test_real_log_rotation_snapshot_keeps_archive_names_modes_levels_and_every_message(self):
        from cases.log_rotation import rotation_snapshot, rotation_errors, MARKER
        domains = ['synthetic-' + str(i) for i in range(7)]
        with tempfile.TemporaryDirectory() as root:
            for index in range(6):
                path = Path(root) / ('app.log' + ('.' + str(index) if index else ''))
                path.write_text('2026-01-01 00:00:00,000 - synthetic - INFO - ' + MARKER + domains[6 - index] + '\n')
                path.chmod(0o600)
            valid = rotation_snapshot(root)
            self.assertEqual(rotation_errors(valid, domains), [])
            for kind in ('missing', 'extra', 'wrong-order', 'wrong-value', 'wrong-level', 'extra-line'):
                bad = copy.deepcopy(valid)
                if kind == 'missing': del bad['app.log.5']
                elif kind == 'extra': bad['app.log.6'] = copy.deepcopy(bad['app.log.5'])
                elif kind == 'wrong-order': bad['app.log.1']['events'] = copy.deepcopy(bad['app.log.2']['events'])
                elif kind == 'wrong-value': bad['app.log']['events'][0]['message'] += 'changed'
                elif kind == 'wrong-level': bad['app.log']['events'][0]['level'] = 'ERROR'
                else: bad['app.log']['unrecognised_lines'] = 1
                self.assertTrue(rotation_errors(bad, domains), kind)
            bad = copy.deepcopy(valid)
            bad['app.log']['mode'] = 0o644
            self.assertTrue(differences(valid, bad), 'file mode still participates in cross-implementation comparison')

    def test_concurrent_profile_commits_allow_only_exact_independent_serial_orders(self):
        from cases.profile_concurrent_isolation import canonical_profile_writes
        from datetime import datetime, timezone
        now = time.time()
        policy = Policy(start=now - 1, end=now + 1)
        before = {'meta': {'generation': 4, 'commit_id': 'a' * 32}, 'fields': {}, 'sections': {}}
        for pid in ('default', 'other'):
            for field, value in (('_revision', 1), ('updated_at', '2020-01-01T00:00:00Z'), ('rule_configs', []), ('unchanged', 'keep')):
                before['fields']['profile/' + pid + '/' + field] = {'value': value, 'revision': 4, 'position': 0}
        changes = {pid: {'rule_configs': [{'id': pid}]} for pid in ('default', 'other')}
        def committed(order):
            after = copy.deepcopy(before)
            after['meta'].update(generation=6, commit_id='b' * 32)
            for revision, pid in enumerate(order, 5):
                for field, value in {**changes[pid], '_revision': 2, 'updated_at': datetime.fromtimestamp(now, timezone.utc).isoformat()}.items():
                    after['fields']['profile/' + pid + '/' + field].update(value=value, revision=revision)
            return after
        valid = committed(('default', 'other'))
        first, errors = canonical_profile_writes(before, valid, changes, policy)
        second, reverse_errors = canonical_profile_writes(before, committed(('other', 'default')), changes, policy)
        self.assertFalse(errors)
        self.assertFalse(reverse_errors)
        self.assertEqual(first, second)
        for mutation in ('wrong-value', 'wrong-generation', 'duplicate-generation', 'old-commit', 'old-time', 'unrelated-row', 'missing-row', 'extra-row', 'bad-generation-type', 'wrong-position'):
            bad = copy.deepcopy(valid)
            key = 'profile/default/rule_configs'
            if mutation == 'wrong-value': bad['fields'][key]['value'] = []
            elif mutation == 'wrong-generation': bad['meta']['generation'] = 5
            elif mutation == 'duplicate-generation':
                for field in ('rule_configs', '_revision', 'updated_at'): bad['fields']['profile/other/' + field]['revision'] = 5
            elif mutation == 'old-commit': bad['meta']['commit_id'] = before['meta']['commit_id']
            elif mutation == 'old-time': bad['fields']['profile/default/updated_at']['value'] = '2020-01-01T00:00:00Z'
            elif mutation == 'unrelated-row': bad['fields']['profile/other/unchanged']['value'] = 'lost'
            elif mutation == 'missing-row': del bad['fields'][key]
            elif mutation == 'extra-row': bad['fields']['unexpected'] = {'value': 'extra'}
            elif mutation == 'bad-generation-type': bad['fields']['profile/default/_revision']['revision'] = []
            else: bad['fields'][key]['position'] = 1
            self.assertTrue(canonical_profile_writes(before, bad, changes, policy)[1], mutation)

    def test_forwarded_rule_finish_compares_SQL_file_agent_and_source_proofs(self):
        from cases.setup_forwarded_rules import ForwardedRead
        observer = ForwardedRead(SimpleNamespace(), [], None)
        observer.violations = [[], []]
        observer.status.observed = [[], []]
        observer.wire.transcripts = [[], []]
        self.assertEqual(observer.compare(Policy()), [])
        for field in ('SQL', 'files'):
            observer.violations[1] = [{'path': 'forwarded.' + field, 'kind': 'value', 'python': 'kept', 'go': 'changed'}]
            self.assertTrue(observer.compare(Policy()), field)
        observer.violations[1] = []
        observer.status.observed[1] = [{'requests': [{'method': 'POST', 'path': '/unexpected-Agent-write'}]}]
        self.assertTrue(observer.compare(Policy()), 'Agent mutation')
        observer.status.observed[1] = []
        observer.wire.transcripts[1] = [{'method': 'GET', 'path': '/unexpected-source'}]
        self.assertTrue(observer.compare(Policy()), 'extra source read')

    def test_native_reference_oracle_rejects_wrong_message_or_malformed_coordinates(self):
        from cases.oxidns_native_lifecycle_guards import reference_message
        from harness import DesignError
        value = {'success': False, 'message': '合成拒绝（plugins[2].args）',
            'references': [{'path': ['plugins', 2, 'args'], 'tag': 'synthetic', 'reason': '合成原因'}]}
        response = lambda data: Reply(400, {'content-type': 'application/json'}, json.dumps(data).encode())
        check = reference_message('合成拒绝')
        check(response(value))
        for field in ('message', 'path', 'reason', 'missing-references'):
            bad = copy.deepcopy(value)
            if field == 'message':
                bad['message'] = '其他拒绝（plugins[2].args）'
            elif field == 'path':
                bad['references'][0]['path'][1] = True
            elif field == 'reason':
                bad['references'][0]['reason'] = None
            else:
                bad['references'] = []
            with self.assertRaises(DesignError, msg=field):
                check(response(bad))

    def test_helper_annotations_require_the_audited_route_and_specific_guard_input(self):
        from helper_links import attach
        vectors = [('H290', '1', 'POST /api/custom-config/mihomo', 'H289'),
            ('H320', '2001:db8::1', 'POST /api/custom-config/mihomo', 'H319'),
            ('H1016', 'router/1/initial-catalog', 'POST /api/setup/<session_id>/<operation>', 'H969'),
            ('R109-F17', '', 'POST /api/oxidns/native-settings/apply', 'H439'),
            ('G12', 'False/rename-unresolved-custom-text', 'POST /api/oxidns/native-settings/apply', 'H462')]
        for case, variant, route, helper in vectors:
            valid = {'case': case, 'variant': variant, 'route': route}
            wrong_route = {**valid, 'route': 'POST /unrelated'}
            wrong_input = {**valid, 'variant': variant + '/earlier-wrapper'}
            attach([valid, wrong_route, wrong_input])
            self.assertIn(helper, valid['helper_cases'])
            self.assertNotIn('helper_cases', wrong_route)
            self.assertNotIn('helper_cases', wrong_input)

    def test_cancelled_scan_compares_source_and_every_persistence_proof(self):
        from cases.scan_disconnect import CancelledScan
        observer = CancelledScan(SimpleNamespace(), None, {})
        observer.transcripts = [[], []]
        observer.observed = [{'held_source_reached': True, 'socket_closed': True,
            'complete_new_cache_without_invented_backup': True, 'gate_errors': [], 'configuration_unchanged': True,
            'only_exact_rule_caches_written': True} for _ in range(2)]
        self.assertEqual(observer.compare(Policy()), [])
        for field in observer.observed[1]:
            previous = observer.observed[1][field]
            observer.observed[1][field] = ['timeout'] if field == 'gate_errors' else False
            self.assertTrue(observer.compare(Policy()), field)
            observer.observed[1][field] = previous
        observer.transcripts[1] = [{'method': 'POST', 'path': '/unexpected'}]
        self.assertTrue(observer.compare(Policy()), 'unexpected extra download')

    def test_managed_setup_race_retains_secondary_HTTP_and_every_commit_proof(self):
        from cases.setup_managed_router import ChangedDuringInspection
        observer = ChangedDuringInspection(SimpleNamespace(), [], None, set())
        observer.status.observed = [[], []]
        observer.observed = [[], []]
        observer.source_requests = [[], []]
        response = Reply(200, {'content-type': 'application/json'}, b'{"success":true}')
        observer.secondary = [response, response]
        observer.proofs = [{'edit_status': 200, 'exact_single_router_edit': True,
            'new_commit': True, 'actual_edit_time': True} for _ in range(2)]
        self.assertEqual(observer.compare(Policy()), [])
        for field in observer.proofs[1]:
            original = observer.proofs[1][field]
            observer.proofs[1][field] = False
            self.assertTrue(observer.compare(Policy()), field)
            observer.proofs[1][field] = original
        observer.secondary[1] = Reply(409, response.headers, response.body)
        self.assertTrue(observer.compare(Policy()), 'secondary update rejection')

    def test_managed_setup_detects_any_extra_router_or_Agent_interaction(self):
        from cases.setup_managed_router import ManagedExchange
        observer = ManagedExchange(SimpleNamespace(), [], None, set())
        observer.status.observed = [[], []]
        observer.observed = [[], []]
        self.assertEqual(observer.compare(Policy()), [])
        observer.observed[1] = [{'method': 'PATCH', 'path': '/rest/ip/route'}]
        self.assertTrue(observer.compare(Policy()), 'unexpected router write')
        observer.observed[1] = []
        observer.status.observed[1] = [{'requests': [{'method': 'POST', 'path': '/api/unexpected'}]}]
        self.assertTrue(observer.compare(Policy()), 'unexpected Agent operation')
        observer.status.observed[1] = []
        observer.source_requests = [[], [{'method': 'GET', 'path': '/unexpected-source'}]]
        self.assertTrue(observer.compare(Policy()), 'unexpected rule download')

    def test_router_task_completion_keeps_pause_baseline_SQL_and_wire_observable(self):
        from cases.task_router_retry import RouterCompletion
        pair = SimpleNamespace(policy=Policy(), route_available=lambda route: False)
        observer = RouterCompletion(pair, None, {}, 'router-rule-sync/synthetic.json')
        observer.observations = [{'receipt_completed': True}, {'receipt_completed': True}]
        observer.proofs = [{'exact_paused_receipt': True, 'actual_checked_time': True, 'configuration_unchanged': True}
                           for _ in range(2)]
        observer.wire.transcripts = [[], []]
        self.assertEqual(observer.compare(pair.policy), [])
        for key in observer.proofs[1]:
            observer.proofs[1][key] = False
            self.assertTrue(observer.compare(pair.policy), key)
            observer.proofs[1][key] = True
        observer.wire.transcripts[1] = [{'method': 'PUT', 'path': '/unexpected'}]
        self.assertTrue(observer.compare(pair.policy), 'unexpected device write')

    def test_router_publication_busy_comparison_detects_secondary_HTTP_wire_and_storage_changes(self):
        from cases.profile_router_busy import Busy
        observer = Busy(SimpleNamespace(), None)
        observer.before_values = [None, {'SQL': 'kept'}]
        observer.after_values = [None, {'SQL': 'kept'}]
        observer.exchange.transcripts = [[], []]
        response = Reply(502, {'content-type': 'application/json'}, b'{"success":false,"message":"synthetic"}')
        observer.secondary = [response, response]
        self.assertEqual(observer.compare(Policy()), [])
        observer.secondary[1] = Reply(200, response.headers, response.body)
        self.assertTrue(observer.compare(Policy()), 'secondary HTTP status')
        observer.secondary[1] = response
        observer.exchange.transcripts[1] = [{'method': 'PATCH', 'path': '/rest/ip/route'}]
        self.assertTrue(observer.compare(Policy()), 'unexpected device mutation')
        observer.exchange.transcripts[1] = []
        observer.after_values[1]['SQL'] = 'changed'
        self.assertTrue(observer.compare(Policy()), 'primary deletion changed storage')

    def test_fixture_rechecks_attribute_only_exact_old_false_difference_paths(self):
        from config_finding_resolutions import annotate
        rows = [('settings-backup-validation', 'R015-B01', '', 'POST /api/backup/config',
                 'db.fields.system//backup.value.webdav_password'),
                ('aggregation-provider-refresh', 'R008-S05', 'failed-source-valid-cache',
                 'GET /api/aggregations/<agg_id>/provider', 'files.shared/subscribes/sub-a.json.content.updated_at')]
        for group, case, variant, route, path in rows:
            item = {'group': group, 'case': case, 'variant': variant, 'route': route, 'status': 'closed',
                    'differences': [{'path': path}], 'remaining_differences': []}
            closed = annotate([copy.deepcopy(item)])[0]
            self.assertEqual(closed['fix_commits'], ['e4b3fb39f'])
            self.assertIn('不是Go业务修复', closed['closure_note'])
            wrong = copy.deepcopy(item)
            wrong['differences'].append({'path': 'db.genuine-regression'})
            self.assertNotIn('e4b3fb39f', annotate([wrong])[0]['fix_commits'])
            wrong = copy.deepcopy(item)
            wrong['status'] = 'open'
            self.assertNotIn('e4b3fb39f', annotate([wrong])[0]['fix_commits'])

    def test_source_completion_gate_observes_fast_body_before_slow_response(self):
        import threading
        import urllib.request
        from concurrent.futures import ThreadPoolExecutor
        from sources import Source
        with Source(b'actual-complete-body') as source:
            entered, sent = threading.Event(), threading.Event()
            order = []
            def before(path):
                if path == '/slow':
                    entered.set()
                    if not sent.wait(4):
                        raise AssertionError('fast body was not sent')
            def after(path):
                order.append(path)
                if path == '/fast':
                    sent.set()
            source.before_reply, source.after_reply = before, after
            def fetch(path):
                with urllib.request.urlopen(source.url + path, timeout=5) as response:
                    return response.read()
            with ThreadPoolExecutor(max_workers=2) as pool:
                slow = pool.submit(fetch, '/slow')
                self.assertTrue(entered.wait(2))
                fast = pool.submit(fetch, '/fast')
                self.assertEqual(fast.result(timeout=5), source.body)
                self.assertEqual(slow.result(timeout=5), source.body)
            self.assertEqual(order, ['/fast', '/slow'])

    def test_approved_C35_reorder_and_HEAD_changes_keep_all_other_fields_strict(self):
        from config_finding_resolutions import annotate
        common = {'status': 'open', 'differences': [], 'variant': ''}
        samples = [
            {**common, 'case': 'R015-S04', 'group': 'settings-backup-validation', 'route': 'POST /api/backup/config',
             'python_status': 200, 'go_status': 200, 'remaining_differences': [
                 {'path': 'db.fields.system//backup.revision', 'kind': 'value', 'python': 8, 'go': 6},
                 {'path': 'db.fields.system//backup.value.webdav_password', 'kind': 'value', 'python': '', 'go': 'synthetic-pass'}]},
            {**common, 'case': 'R103-F08', 'group': 'nodes-reorder', 'route': 'POST /api/nodes/reorder',
             'python_status': 404, 'go_status': 400, 'remaining_differences': [
                 {'path': 'status', 'kind': 'value', 'python': 404, 'go': 400},
                 {'path': 'body.message', 'kind': 'value', 'python': "以下节点 id 不存在: ['ids 必须是数组']", 'go': 'ids 必须是数组'}]},
            {**common, 'case': 'R223-F12', 'group': 'subscriptions-reorder', 'route': 'POST /api/subscriptions/reorder',
             'python_status': 500, 'go_status': 400, 'remaining_differences': [
                 {'path': 'status', 'kind': 'value', 'python': 500, 'go': 400},
                 {'path': 'body.message', 'kind': 'value', 'python': '订阅操作失败', 'go': '请求体必须是 JSON 对象'}]},
            {**common, 'case': 'X06', 'group': 'http-head', 'route': 'GET /api/nodes', 'variant': 'head//api/nodes',
             'python_status': 500, 'go_status': 500, 'remaining_differences': [
                 {'path': 'headers.content-type', 'kind': 'value', 'python': 'text/html; charset=utf-8', 'go': 'application/json'}]},
        ]
        for item in samples:
            self.assertEqual(annotate([copy.deepcopy(item)])[0]['status'], 'accepted', item['case'])
            for field in ('case', 'group', 'route', 'variant', 'go_status', 'extra-SQL', 'extra-header', 'changed-value'):
                wrong = copy.deepcopy(item)
                if field in ('case', 'group', 'route', 'variant'):
                    wrong[field] += 'wrong'
                elif field == 'go_status':
                    wrong[field] = 503
                elif field == 'changed-value':
                    wrong['remaining_differences'][-1]['go'] = 'wrong'
                else:
                    wrong['remaining_differences'].append({'path': 'db.foreign' if field == 'extra-SQL' else 'headers.allow',
                        'kind': 'value', 'python': 'old', 'go': 'wrong'})
                self.assertEqual(annotate([wrong])[0]['status'], 'open', (item['case'], field))

    def test_real_download_cancellation_requires_every_transport_observation(self):
        from cases.download_disconnect import transfer_errors
        valid = {'status': 200, 'content_length_is_large': True, 'one_byte_read': True,
                 'unread_response_bytes': True, 'receive_buffer_is_small': True, 'socket_closed': True,
                 'length_matches_own_completed_materialization': True}
        self.assertEqual(transfer_errors(valid), [])
        for field in valid:
            broken = dict(valid)
            broken[field] = 500 if field == 'status' else False
            self.assertTrue(transfer_errors(broken), field)
        self.assertTrue(transfer_errors({**valid, 'invented': True}))

    def test_DAV_task_completion_detects_failed_completion_SQL_or_remote_objects(self):
        from cases.task_webdav import DavCompletion
        with tempfile.TemporaryDirectory() as temporary:
            pair = SimpleNamespace(policy=Policy(), route_available=lambda route: False,
                                   dirs=[Path(temporary) / 'python', Path(temporary) / 'go'])
            observer = DavCompletion(pair, None)
            observer.observations = [{'receipt_completed': True}, {'receipt_completed': True}]
            observer.sql_unchanged = [True, True]
            wire = {'requests': [], 'objects': {}}
            observer.wire.observations = [copy.deepcopy(wire), copy.deepcopy(wire)]
            self.assertEqual(observer.compare(pair.policy), [])
            observer.sql_unchanged[1] = False
            self.assertTrue(observer.compare(pair.policy), 'SQL change')
            observer.sql_unchanged[1] = True
            observer.observations[1]['receipt_completed'] = False
            self.assertTrue(observer.compare(pair.policy), 'unfinished task')
            observer.observations[1]['receipt_completed'] = True
            observer.wire.observations[1]['objects']['unexpected.json'] = b'partial'
            self.assertTrue(observer.compare(pair.policy), 'remote object')

    def test_DAV_gate_keeps_recorded_requests_observable_before_reply(self):
        from concurrent.futures import ThreadPoolExecutor
        import threading
        from fakes import WebDAV
        from harness import send
        entered, release = threading.Event(), threading.Event()
        def gate(method, path):
            entered.set()
            release.wait(3)
        with WebDAV() as fake, ThreadPoolExecutor(max_workers=1) as pool:
            fake.before_reply = gate
            future = pool.submit(send, fake.url, 'HEAD', '/backup/')
            try:
                self.assertTrue(entered.wait(2))
                self.assertEqual(fake.snapshot()['requests'][0]['method'], 'HEAD')
                self.assertFalse(future.done())
            finally:
                release.set()
            self.assertEqual(future.result(timeout=3).status, 200)

    def test_closed_DAV_diagnostic_attributes_comparison_fix_without_claiming_a_Go_fix(self):
        from config_finding_resolutions import annotate
        from finding_reviews import markdown
        item = {'case': 'R016-F15', 'variant': 'real-MKCOL-403-before-PUT', 'route': 'POST /api/backup/now',
                'group': 'webdav-named-preflight-and-directory-failure', 'status': 'closed', 'python_status': 400,
                'go_status': 400, 'checked_baseline': 'synthetic', 'differences': [], 'remaining_differences': []}
        result = annotate([copy.deepcopy(item)])[0]
        self.assertEqual(result['fix_commits'], ['0b760d9cb'])
        self.assertIn('不是Go业务修复', result['closure_note'])
        self.assertIn('不是Go业务修复', '\n'.join(markdown([result])))
        for mutation in ({'status': 'open'}, {'variant': 'other'}, {'route': 'other'}):
            wrong = annotate([{**copy.deepcopy(item), **mutation}])[0]
            self.assertEqual(wrong['fix_commits'], [])
            self.assertNotIn('closure_note', wrong)

    def test_finite_english_diagnostic_equivalence_preserves_Chinese_codes_and_other_fields(self):
        from cases.webdav import diagnostic_policy
        for kind, stored in [('space', True), ('mkdir', True), ('connection', False)]:
            pair = SimpleNamespace(policy=Policy())
            diagnostic_policy(pair, kind, stored=stored)
            for path, values in pair.policy.internal_error_values.items():
                self.assertEqual(differences(*values, path, pair.policy), [])
                for wrong in [values[1] + ' extra', values[1].replace('403', '500').replace('507', '500'),
                              values[1].replace('目录', '路径').replace('失败', '成功'), None, {'message': values[1]}]:
                    if wrong == values[1]:
                        continue
                    self.assertTrue(differences(values[0], wrong, path, pair.policy), (kind, path, wrong))
                self.assertTrue(differences(*values, 'db.unrelated-value', pair.policy), 'only exact configured field may differ')
            self.assertTrue(differences(500, 400, 'status', pair.policy))
            self.assertTrue(differences('application/json', 'text/plain', 'headers.content-type', pair.policy))

    def test_incomplete_DAV_checks_real_receipt_and_every_ordinary_state_field(self):
        from cases.webdav_edges import ConfigurationFailure
        observer = ConfigurationFailure(None, None)
        expected = {key: True for key in ('database_unchanged', 'only_failed_receipt_file_added',
                                         'precise_failed_manual_receipt', 'no_network_request')}
        observer.observed = [expected, expected]
        observer.observations = [{'requests': [], 'objects': {}}, {'requests': [], 'objects': {}}]
        observer.replies = []
        # Exercise the exact added field comparison independently of the
        # established wire/receipt time comparator, which is already tested.
        from unittest.mock import patch
        with patch('cases.webdav_edges.Exchange.compare', return_value=[]):
            self.assertEqual(observer.compare(Policy()), [])
            for key in expected:
                observer.observed[1] = {**expected, key: False}
                self.assertTrue(observer.compare(Policy()), key)

    def test_task_interval_race_detects_lost_URL_interval_foreign_row_and_response(self):
        from cases.task_interval_race import expected_errors
        value = {'id': 'sub-a', 'url': 'http://source.example/changed', 'interval': 60}
        before = {'meta': {'generation': 1}, 'fields': {'shared//subscriptions': {'value': []},
            'shared//_revision': {'value': 1}, 'system//config': {'value': {'keep': True}}}, 'sections': []}
        after = copy.deepcopy(before)
        after['meta']['generation'] = after['fields']['shared//_revision']['value'] = 3
        after['fields']['shared//subscriptions']['value'] = [value]
        replies = [Reply(200, {}, b'{}'), Reply(200, {}, json.dumps({'success': True, 'data': value}).encode())]
        self.assertEqual(expected_errors(before, after, replies, value), [])
        for field, wrong in [('url', 'http://source.example/old'), ('interval', 0)]:
            changed = copy.deepcopy(after)
            changed['fields']['shared//subscriptions']['value'][0][field] = wrong
            self.assertTrue(expected_errors(before, changed, replies, value), field)
        changed = copy.deepcopy(after)
        changed['fields']['system//config']['value']['keep'] = False
        self.assertTrue(expected_errors(before, changed, replies, value), 'foreign row')
        changed = copy.deepcopy(after)
        changed['meta']['generation'] = 2
        self.assertTrue(expected_errors(before, changed, replies, value), 'lost commit')
        bad = [replies[0], Reply(200, {}, b'{"success":true}')]
        self.assertTrue(expected_errors(before, after, bad, value), 'secondary response')

    def test_listener_rollback_comparison_requires_every_storage_network_and_fault_invariant(self):
        from cases.web_switch import Rollback
        observer = Rollback(None, None, None, None)
        expected = {key: True for key in ('real_fault_active', 'readonly_mode_retained', 'database_unchanged',
            'all_files_unchanged', 'old_health_and_capability_unchanged', 'old_dual_stack_same_capability',
            'new_port_closed', 'controller_rollback_valid', 'python_config_restored', 'clear_Chinese_failure')}
        observer.observed = [expected, expected]
        self.assertEqual(observer.compare(Policy()), [])
        for key in expected:
            observer.observed[1] = {**expected, key: False}
            self.assertTrue(observer.compare(Policy()), key)

    def test_provider_failure_comment_allows_only_the_proven_english_status_suffix(self):
        from providers import failure_comments
        plain = '# Source: from local cache (fetch error: request_failed (exception_type=SubscriptionFetchError))'
        suffix = plain[:-2] + ' status=403))'
        self.assertEqual(failure_comments([plain], 403), failure_comments([suffix], 403))
        for wrong in [suffix.replace('403', '500'), suffix.replace('local cache', 'remote source'),
                      suffix.replace('SubscriptionFetchError', 'DifferentError'), suffix + ' extra',
                      '# Subscription: changed', '# Proxies: 0']:
            self.assertTrue(differences(failure_comments([plain], 403), failure_comments([wrong], 403)))
        self.assertEqual(failure_comments(['# Subscription: same', suffix], 403), ['# Subscription: same', plain])

    def test_provider_file_failure_approval_keeps_status_payload_and_storage_exact(self):
        from config_finding_resolutions import annotate
        description = ('The server encountered an internal error and was unable to complete your request. '
                       'Either the server is overloaded or there is an error in the application.')
        for case, route in [('R220-F17', 'GET /api/subscriptions/<sub_id>/proxies'),
                            ('R154-F17', 'GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies')]:
            item = {'case': case, 'route': route, 'variant': 'last-good-provider-output-path-is-directory',
                    'group': 'provider-named-graph-and-file-failures', 'status': 'open',
                    'python_status': 500, 'go_status': 500, 'differences': [], 'remaining_differences': [
                        {'path': 'headers.content-type', 'kind': 'value', 'python': 'text/html; charset=utf-8', 'go': 'application/json'},
                        {'path': 'body', 'kind': 'type', 'python': ['<!doctype html>', '<html lang=en>',
                            '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>' + description + '</p>'],
                         'go': {'success': False, 'message': description}}]}
            self.assertEqual(annotate([copy.deepcopy(item)])[0]['status'], 'accepted')
            for label in ('message', 'extra-field', 'status', 'db', 'file', 'header', 'wrong-case', 'wrong-route'):
                wrong = copy.deepcopy(item)
                if label == 'message':
                    wrong['remaining_differences'][1]['go']['message'] = 'wrong'
                elif label == 'extra-field':
                    wrong['remaining_differences'][1]['go']['extra'] = True
                elif label == 'status':
                    wrong['go_status'] = 400
                elif label.startswith('wrong-'):
                    wrong[label[6:]] = 'other'
                else:
                    wrong['remaining_differences'].append({'path': label + '.extra', 'kind': 'value', 'python': 0, 'go': 1})
                self.assertEqual(annotate([wrong])[0]['status'], 'open', label)

    def test_generator_output_permission_observer_detects_chmod_and_persistent_writes(self):
        from cases.generator_edges import ReadonlyOutput
        observer = ReadonlyOutput(None)
        observer.before_values[1] = observer.after_values[1] = {'sql': 'same', 'files': 'same'}
        expected = {'real_fault_active': True, 'readonly_mode_retained': True}
        observer.observed = [expected, expected]
        self.assertEqual(observer.compare(Policy()), [])
        for key in expected:
            observer.observed[1] = {**expected, key: False}
            self.assertTrue(observer.compare(Policy()), key)
        observer.observed[1] = expected
        observer.after_values[1] = {'sql': 'changed', 'files': 'same'}
        self.assertTrue(observer.compare(Policy()), 'committed row')
        observer.after_values[1] = {'sql': 'same', 'files': 'partial-generated-file'}
        self.assertTrue(observer.compare(Policy()), 'partial file')

    def test_artifact_path_redaction_does_not_hide_unredacted_paths_or_other_metadata(self):
        from cases.artifact_redaction import RedactedPath
        path = 'rules/synthetic-secret.txt'
        items = [{'path': 'config.yaml', 'size': 5, 'sha256': 'main-hash'},
                 {'path': path, 'size': 7, 'sha256': 'extra-hash'}]
        manifest = {'config_path': 'config.yaml', 'files': items}
        record = {'id': 'a' * 32, 'deployment_id': 'b' * 32, 'agent_id': 'manual',
                  'profile_id': 'default', 'service_type': 'oxidns', 'expires_at': 12345,
                  'bundle_meta': {'manifest': manifest}}
        checked = {'record': record, 'raw': {'files/config.yaml': b'x: 1\n'}}
        value = {key: record[key] for key in ('id', 'deployment_id', 'agent_id', 'profile_id', 'service_type', 'expires_at')}
        value.update(config_path='config.yaml', content='x: 1\n', files=copy.deepcopy(items), success=True)
        value['files'][1]['path'] = '[REDACTED]'
        observer = RedactedPath(None, path)
        observer.checked = [checked, checked]
        expected = observer.public(value, 0)
        self.assertEqual(differences(expected, observer.public(value, 1)), [])
        for field, wrong in [('path', path), ('path', 'other.txt'), ('size', 8), ('sha256', 'wrong')]:
            changed = copy.deepcopy(value)
            changed['files'][1][field] = wrong
            self.assertTrue(differences(expected, observer.public(changed, 1)), field)
        for field, wrong in [('content', '[REDACTED]'), ('profile_id', 'other'), ('config_path', 'other.yaml')]:
            changed = {**value, field: wrong}
            self.assertTrue(differences(expected, observer.public(changed, 1)), field)
        changed = copy.deepcopy(value)
        changed['files'][0]['path'] = '[REDACTED]'
        self.assertTrue(differences(expected, observer.public(changed, 1)), 'nonsecret sibling')

    def test_large_string_evidence_keeps_full_comparison_and_unequal_digest(self):
        import hashlib
        from harness import Recorder
        left, right = '中🧪' * 60_000, '中🧪' * 59_999 + '中x'
        diffs = differences(left, right, 'body.large')
        self.assertTrue(diffs)
        recorder = Recorder()
        recorder.record('MUTATION-LARGE', '', 'synthetic', 'B', 'different', diffs)
        result = recorder.results[0]
        self.assertEqual(result['state'], 'different')
        a, b = result['differences'][0]['python'], result['differences'][0]['go']
        self.assertEqual(a['utf8_surrogatepass_sha256'], hashlib.sha256(left.encode()).hexdigest())
        self.assertNotEqual(a['utf8_surrogatepass_sha256'], b['utf8_surrogatepass_sha256'])
        self.assertEqual(a['string_characters'], len(left))
        self.assertLess(len(json.dumps(result)), 3000)
        recorder.record('MUTATION-LARGE', 'surrogate', 'synthetic', 'B', 'different',
                        differences('\ud800' + 'x' * 100_001, 'y' * 100_002))
        json.dumps(recorder.results, ensure_ascii=False).encode('utf-8')

    def test_artifact_real_limit_requires_cause_rows_and_exact_failure_files(self):
        from cases.artifact_size import MetadataLimit
        observer = MetadataLimit(None)
        expected = {'real_limit_diagnostic': True, 'all_database_rows_unchanged': True,
                    'preview_root_never_created': True, 'only_actual_generated_backup_added': True}
        observer.observed = [copy.deepcopy(expected), copy.deepcopy(expected)]
        self.assertEqual(observer.compare(Policy()), [])
        for key in expected:
            observer.observed[1] = {**expected, key: False}
            self.assertTrue(observer.compare(Policy()), key)

    def test_artifact_failure_observers_detect_partial_state_and_child_error_changes(self):
        from cases.artifact_failures import PreviewFault, Propagation
        fault = PreviewFault(None)
        expected = {'fault_active': True, 'fault_mode_retained': True, 'old_private_bytes_unchanged': True,
                    'all_database_rows_unchanged': True, 'only_generated_predecessor_backup_added': True}
        fault.observed = [copy.deepcopy(expected), copy.deepcopy(expected)]
        self.assertEqual(fault.compare(Policy()), [])
        for key in expected:
            fault.observed[1] = {**expected, key: False}
            self.assertTrue(fault.compare(Policy()), key)
        child = Propagation(None, '/unused')
        child.observed = [[], []]
        self.assertEqual(child.compare(Policy()), [])
        child.observed[1] = [{'path': 'body.message', 'kind': 'value', 'python': 'original', 'go': 'wrapped'}]
        self.assertTrue(child.compare(Policy()))

    def test_frozen_artifact_read_rejects_rerendering_and_storage_writes(self):
        from cases.artifact_frozen import Frozen
        observer = Frozen(None, [b'old member', b'old member'])
        observer.before_values[1] = observer.after_values[1] = {'db': 'same', 'files': 'same'}
        observer.observed = [{'own_frozen_bytes': True}, {'own_frozen_bytes': True}]
        self.assertEqual(observer.compare(Policy()), [])
        observer.observed[1] = {'own_frozen_bytes': False}
        self.assertTrue(observer.compare(Policy()))
        observer.observed[1] = {'own_frozen_bytes': True}
        observer.after_values[1] = {'db': 'changed', 'files': 'same'}
        self.assertTrue(observer.compare(Policy()))

    def test_discovery_shared_write_isolation_keeps_foreign_rows_revisions_sections_and_metadata_strict(self):
        from cases.discovery_profile_writes import foreign_snapshot
        from cases.setup import Stable
        value = {'fields': {key: {'value': 1, 'revision': 7, 'position': 0} for key in (
            'profile/default/updated_at', 'profile/other/updated_at', 'shared//rule_library',
            'shared//_revision', 'shared//nodes', 'system//system_config')},
            'sections': [['profile', 'other', 0]], 'meta': {'generation': 7, 'commit_id': 'a', 'schema': 5}}
        expected = foreign_snapshot(value, 'default')
        own = copy.deepcopy(value)
        own['fields']['profile/default/updated_at']['value'] = 'changed'
        own['fields']['shared//rule_library']['value'] = ['new']
        own['meta'].update(generation=8, commit_id='b')
        self.assertEqual(expected, foreign_snapshot(own, 'default'))
        for key in ('profile/other/updated_at', 'shared//nodes', 'system//system_config'):
            wrong = copy.deepcopy(own)
            wrong['fields'][key]['value'] = 'corrupted'
            observer = Stable(None)
            observer.before_values[1], observer.after_values[1] = expected, foreign_snapshot(wrong, 'default')
            self.assertTrue(observer.compare(Policy()), key)
        for key in ('sections', 'meta'):
            wrong = copy.deepcopy(own)
            wrong[key] = [] if key == 'sections' else {**wrong['meta'], 'schema': 4}
            self.assertTrue(differences(expected, foreign_snapshot(wrong, 'default')))

    def test_rule_delivery_warning_compares_complete_event_and_stable_storage(self):
        from cases.rule_delivery_yaml import Delivery, WARNING
        observer = Delivery(None)
        observer.before_values[1] = observer.after_values[1] = {'sql': 'same', 'files': 'same'}
        rows = [{'level': 'WARNING', 'message': WARNING}]
        observer.observed = [copy.deepcopy(rows), copy.deepcopy(rows)]
        self.assertEqual(observer.compare(Policy()), [])
        for wrong in ([], rows * 2, [{'level': 'WARN', 'message': WARNING}], [{'level': 'WARNING', 'message': WARNING + ' changed'}]):
            observer.observed[1] = wrong
            self.assertTrue(observer.compare(Policy()))
        observer.observed[1] = copy.deepcopy(rows)
        observer.after_values[1] = {'sql': 'same', 'files': 'rewritten-source'}
        self.assertTrue(observer.compare(Policy()))

    def test_rule_concurrent_observer_detects_lost_commit_rejected_cache_and_second_response_changes(self):
        from cases.rule_concurrency import Race, CONFLICT
        observer = Race(None, SimpleNamespace(url='http://127.0.0.1:12345'), True)
        value = {'gate_reached': True, 'no_gate_timeout': True, 'success_status': 200, 'rejected_status': 409,
                 'success_body': {'success': True}, 'rejected_body': CONFLICT, 'exact_single_commit': True,
                 'new_commit_identity': True, 'exact_files_including_rejected_remote_cache': True, 'source_requests': []}
        reply = Reply(409, {'content-type': 'application/json'}, json.dumps(CONFLICT).encode())
        observer.secondary = [reply, reply]
        observer.observed = [copy.deepcopy(value), copy.deepcopy(value)]
        self.assertEqual(observer.compare(Policy()), [])
        for key, old in value.items():
            changed = False if old is True else (201 if type(old) is int else {'wrong': 'value'})
            observer.observed[1] = {**value, key: changed}
            self.assertTrue(observer.compare(Policy()), key)
        observer.observed[1] = copy.deepcopy(value)
        observer.secondary[1] = Reply(200, reply.headers, reply.body)
        self.assertTrue(observer.compare(Policy()), 'secondary client response must remain observable')

    def test_real_auth_log_tail_keeps_exact_message_level_and_credential_absence(self):
        from cases.pull_logging import Tail, AUTH_MARKER, AUTH_MESSAGE, events
        tail = Tail(None, AUTH_MARKER, AUTH_MESSAGE, ('synthetic-password-secret',))
        a = '2026-01-01 - WARNING - ' + AUTH_MESSAGE
        b = json.dumps({'level': 'WARNING', 'message': AUTH_MESSAGE}, ensure_ascii=False)
        tail.lines = [['source startup', a], ['Go startup', b]]
        make = lambda row: {'success': True, 'logs': [row], 'total_lines': 2, 'filtered_lines': 1}
        expected = tail.project(make(a), 0)
        self.assertEqual(differences(expected, tail.project(make(b), 1)), [])
        self.assertEqual(events((a + '\n').encode(), AUTH_MARKER), [{'level': 'WARNING', 'message': AUTH_MESSAGE}])
        for changed in (b.replace('WARNING', 'WARN'), b.replace('127.0.0.1', '192.0.2.1'),
                        b.replace(AUTH_MARKER, '登录成功：'), b + ' synthetic-password-secret'):
            self.assertTrue(differences(expected, tail.project(make(changed), 1)))

    def test_cache_permission_observer_detects_inactive_fault_chmod_and_overwritten_old_bytes(self):
        from cases.rule_cache_faults import CacheFault
        observer = CacheFault(None)
        value = {'write_fault_active': True, 'fault_modes_retained': True, 'old_cache_bytes_retained': True}
        observer.observed = [copy.deepcopy(value), copy.deepcopy(value)]
        self.assertEqual(observer.compare(Policy()), [])
        for key in value:
            observer.observed[1] = {**value, key: False}
            self.assertTrue(observer.compare(Policy()), key)

    def test_real_tail_projection_validates_own_filter_and_counts_without_hiding_extra_rows(self):
        from cases.pull_logging import Tail, MARKER, WARNING_AGENT
        tail = Tail(None)
        message = MARKER + 'mihomo（配置空间 默认），HTTP 404，来源 127.0.0.1，客户端 ' + WARNING_AGENT
        a = '2026-01-01 - WARNING - ' + message
        b = json.dumps({'level': 'WARNING', 'message': message}, ensure_ascii=False)
        tail.lines = [['source startup', a], ['Go startup one', 'Go startup two', b]]
        make = lambda rows, count: {'success': True, 'logs': rows, 'total_lines': count, 'filtered_lines': 1}
        left, right = tail.project(make([a], 2), 0), tail.project(make([b], 3), 1)
        self.assertEqual(differences(left, right), [])
        for value in (make([], 3), make([b, 'unexpected warning'], 3), make([b], 2),
                      make([b.replace('WARNING', 'WARN')], 3), make([b], True)):
            self.assertTrue(differences(left, tail.project(value, 1)))

    def test_stat_fault_observer_detects_missing_fault_wrong_cause_path_and_storage_write(self):
        from cases.log_stat_fault import StatFault
        observer = StatFault(None, None)
        observer.before_values[1] = observer.after_values[1] = {'db': 'same'}
        value = {'active_stat_fault': True, 'fault_still_denied': True, 'mode_unchanged': True,
                 'permission_error': True, 'assigned_path': True}
        observer.observed = [copy.deepcopy(value), copy.deepcopy(value)]
        self.assertEqual(observer.compare(Policy()), [])
        for key in value:
            observer.observed[1] = {**value, key: False}
            self.assertTrue(observer.compare(Policy()), key)
        observer.observed[1] = copy.deepcopy(value)
        observer.after_values[1] = {'db': 'wrong'}
        self.assertTrue(observer.compare(Policy()), 'failed stat must not update storage')

    def test_pull_log_parser_and_observer_detect_extra_head_log_header_drift_and_capability_leaks(self):
        from cases.pull_logging import events, HeadPull
        message = '配置被拉取：mihomo（配置空间 默认），HTTP 200，来源 127.0.0.1，客户端 configdiff-before-head'
        plain = ('2026-01-01 - INFO - ' + message + '\n').encode()
        structured = (json.dumps({'level': 'INFO', 'message': message, 'time': '2026-01-01'}) + '\n').encode()
        value = [{'level': 'INFO', 'message': message}]
        self.assertEqual(events(plain), value)
        self.assertEqual(events(structured), value)
        self.assertNotEqual(events(plain.replace(b'INFO', b'WARN')), value)
        self.assertNotEqual(events(plain + b'\xff'), value)
        observer = HeadPull(None, None, None)
        observer.before_values[1] = observer.after_values[1] = {'sql': 'same'}
        observed = {'get_statuses': [200, 200], 'head_header_violations': [], 'pull_events': value,
                    'capabilities_absent': True}
        observer.observed = [copy.deepcopy(observed), copy.deepcopy(observed)]
        self.assertEqual(observer.compare(Policy()), [])
        for key, changed in [('get_statuses', [200, 401]), ('head_header_violations', [{'field': 'content-length'}]),
                             ('pull_events', value * 2), ('pull_events', []), ('capabilities_absent', False)]:
            observer.observed[1] = {**observed, key: changed}
            self.assertTrue(observer.compare(Policy()), key)

    def test_listener_switch_observer_detects_false_success_and_controller_mutations(self):
        from cases.web_switch import Switch
        observer = Switch(None, None, None, None)
        value = {'database_unchanged': True, 'new_health_200': True, 'new_token_valid': True,
                 'new_token_rotated': True, 'dual_stack_same_token': True, 'html_exact': True,
                 'old_port_closed': True, 'controller_sequence_valid': True}
        observer.observed = [copy.deepcopy(value), copy.deepcopy(value)]
        self.assertEqual(observer.compare(Policy()), [])
        for key in value:
            observer.observed[1] = {**value, key: False}
            self.assertTrue(observer.compare(Policy()), key)

    def test_listener_observer_detects_health_capability_storage_and_unexpected_requests(self):
        from cases.web_listener import Readonly
        observer = Readonly(None, None)
        observer.before_values[1] = observer.after_values[1] = {'sql': 'unchanged', 'file': 'unchanged'}
        value = {'old_listener': {'status': 200, 'valid_capability': True, 'same_capability': True},
                 'python_config_unchanged': True, 'occupied_requests': []}
        observer.observed = [copy.deepcopy(value), copy.deepcopy(value)]
        self.assertEqual(observer.compare(Policy()), [])
        for key, changed in [('old_listener', {**value['old_listener'], 'status': 500}),
                             ('old_listener', {**value['old_listener'], 'same_capability': False}),
                             ('old_listener', {**value['old_listener'], 'valid_capability': False}),
                             ('python_config_unchanged', False), ('occupied_requests', [{'method': 'GET'}])]:
            observer.observed[1] = {**value, key: changed}
            self.assertTrue(observer.compare(Policy()), key)
        observer.observed[1] = copy.deepcopy(value)
        observer.after_values[1] = {'sql': 'changed', 'file': 'unchanged'}
        self.assertTrue(observer.compare(Policy()), 'rejected listener request writes SQL')
        observer.after_values[1] = {'sql': 'unchanged', 'file': 'changed'}
        self.assertTrue(observer.compare(Policy()), 'rejected listener request writes state')

    def test_dependency_update_observer_detects_terminal_protocol_and_storage_mutations(self):
        from cases.dependency_updates import Update, terminal_diffs
        policy = Policy(start=100, end=110)
        value = {'dependencies': [{'update': {'finished_at': 105.25, 'state': 'success'},
                                   'current_version': '2.42.1', 'lifecycle': {'state': 'running'}}]}
        other = copy.deepcopy(value)
        other['dependencies'][0]['update']['finished_at'] = 106.5
        self.assertEqual(terminal_diffs(value, other, policy), [])
        for mutation in ('old-time', 'string-time', 'nan', 'wrong-state', 'wrong-version', 'extra'):
            other = copy.deepcopy(value)
            row = other['dependencies'][0]
            if mutation == 'old-time':
                row['update']['finished_at'] = 99
            elif mutation == 'string-time':
                row['update']['finished_at'] = '105'
            elif mutation == 'nan':
                row['update']['finished_at'] = float('nan')
            elif mutation == 'wrong-state':
                row['update']['state'] = 'failed'
            elif mutation == 'wrong-version':
                row['current_version'] = '2.40.0'
            else:
                row['extra'] = False
            self.assertTrue(terminal_diffs(value, other, policy), mutation)
        observer = Update(None, None, None)
        protocol = {'database_unchanged': True, 'worker_health_count': 3, 'github': ['release', 'download'],
                    'commands': ['restart'], 'terminal_reached': True, 'installed_bytes': {'VERSION': 'exact-sha'},
                    'first_update_blocked': True, 'rejected_request_readonly': True,
                    'minimum_elapsed_valid': True, 'node_checks': [{'returncode': 1, 'stdin_sha256': 'downloaded-sha'}]}
        observer.observed = [copy.deepcopy(protocol), copy.deepcopy(protocol)]
        observer.terminal = [copy.deepcopy(value), copy.deepcopy(value)]
        self.assertEqual(observer.compare(policy), [])
        for key, changed in [('database_unchanged', False), ('worker_health_count', 4),
                             ('github', ['release']), ('commands', ['restart', 'restart']), ('terminal_reached', False),
                             ('installed_bytes', {'VERSION': 'corrupted-sha'}), ('first_update_blocked', False),
                             ('rejected_request_readonly', False), ('minimum_elapsed_valid', False),
                             ('node_checks', [{'returncode': 0, 'stdin_sha256': 'wrong-sha'}])]:
            observer.observed[1] = {**protocol, key: changed}
            self.assertTrue(observer.compare(policy), key)

    def test_managed_mode_protocol_rejects_mutating_commands_extra_health_reads_and_writes(self):
        from cases.dependency_modes import ModeProtocol, HEALTH
        observer = ModeProtocol(None, None, 1)
        value = {'health': [HEALTH], 'unexpected_commands': [], 'status_count_valid': True}
        observer.observed = [copy.deepcopy(value), copy.deepcopy(value)]
        self.assertEqual(observer.compare(Policy()), [])
        for mutation in ('restart', 'extra-health', 'excess-status'):
            observer.observed[1] = copy.deepcopy(value)
            if mutation == 'restart':
                observer.observed[1]['unexpected_commands'] = [{'args': ['restart', 'sub-store']}]
            elif mutation == 'extra-health':
                observer.observed[1]['health'].append(HEALTH)
            else:
                observer.observed[1]['status_count_valid'] = False
            self.assertTrue(observer.compare(Policy()), mutation)
        observer.observed[1] = copy.deepcopy(value)
        from cases.setup import Stable
        observer.readonly = Stable(None)
        observer.readonly.before_values[1] = {'database': 'same'}
        observer.readonly.after_values[1] = {'database': 'changed'}
        self.assertTrue(observer.compare(Policy()), 'failed control must not persist a mode change')

    def test_dependency_protocol_and_readonly_observer_detect_storage_method_target_and_body_changes(self):
        from cases.dependency_status import DependencyRead
        from dependency_tls import release_request
        observer = DependencyRead(None, None, None, True)
        observer.before_values[1] = {'database': 'unchanged'}
        observer.after_values[1] = {'database': 'unchanged'}
        expected = {'health': [{'method': 'GET', 'path': '/api/utils/env', 'authorization': '', 'body_hex': '', 'transfer_encoding': ''}],
                    'github': release_request()}
        observer.observed = [copy.deepcopy(expected), copy.deepcopy(expected)]
        self.assertEqual(observer.compare(Policy()), [])
        for mutation in ('storage', 'method', 'body', 'target', 'UA', 'call-count'):
            observer.after_values[1] = {'database': 'wrong' if mutation == 'storage' else 'unchanged'}
            observer.observed[1] = copy.deepcopy(expected)
            if mutation == 'method':
                observer.observed[1]['health'][0]['method'] = 'POST'
            elif mutation == 'body':
                observer.observed[1]['health'][0]['body_hex'] = '78'
            elif mutation == 'target':
                observer.observed[1]['github'][0]['target'] = 'wrong.example:443'
            elif mutation == 'UA':
                observer.observed[1]['github'][1]['user_agent'] = 'wrong'
            elif mutation == 'call-count':
                observer.observed[1]['github'] = []
            self.assertTrue(observer.compare(Policy()), mutation)

    def test_optional_source_body_recorder_observes_actual_request_bytes(self):
        from sources import Source
        from harness import send
        with Source(b'ok') as source:
            source.record_body = True
            reply = send(source.url, 'GET', '/synthetic-body', b'\x00\xff')
            self.assertEqual(reply.status, 200)
            self.assertEqual(source.requests[0]['body_hex'], '00ff')

    def test_concurrent_metadata_accepts_only_two_legal_serial_histories(self):
        from cases.profile_concurrency import metadata_batch_errors
        initial = {'id': 'other', 'name': 'old', 'description': 'old-description', 'created_at': 'fixed'}
        current = '2026-01-01T00:00:01Z'
        policy = Policy(start=1767225600, end=1767225603)
        a = {**initial, 'name': '并发改名🧪', 'updated_at': current}
        b = {**initial, 'description': '并发说明🧪', 'updated_at': current}
        final = {**a, **b, 'name': a['name']}
        make = lambda body: Reply(200, {}, json.dumps(body, ensure_ascii=False).encode())
        for bodies in ([a, final], [final, b]):
            self.assertEqual(metadata_batch_errors([make(body) for body in bodies], initial, policy), [])
        for bodies in ([a, b], [final, final], [a, {**final, 'extra': True}],
                       [a, {**final, 'updated_at': 'not-time'}], [a, {**final, 'id': 'wrong'}]):
            self.assertTrue(metadata_batch_errors([make(body) for body in bodies], initial, policy), repr(bodies))
        self.assertTrue(metadata_batch_errors([make(a), Reply(500, {}, b'{}')], initial, policy))

    def test_approved_difference_requires_exact_case_values_and_no_extra_storage_effects(self):
        from config_finding_resolutions import annotate
        path = 'db.fields.profile/default/domain_discovery.value.synthetic_legacy_extension'
        item = {'case': 'R058-B03', 'variant': 'stored-legacy-extension', 'route': 'PUT /api/domain-discovery/settings',
                'group': 'discovery-settings', 'status': 'open', 'python_status': 200, 'go_status': 200,
                'differences': [], 'remaining_differences': [{'path': path, 'kind': 'extra-in-go', 'python': None,
                                                           'go': {'flag': False, 'values': []}}]}
        self.assertEqual(annotate([copy.deepcopy(item)])[0]['status'], 'accepted')
        for mutation in ('route', 'case', 'extra-write', 'wrong-value', 'wrong-type', 'status'):
            wrong = copy.deepcopy(item)
            if mutation in ('route', 'case'):
                wrong[mutation] += '-wrong'
            elif mutation == 'extra-write':
                wrong['remaining_differences'].append({'path': 'db.extra', 'kind': 'value', 'python': 0, 'go': 1})
            elif mutation == 'wrong-value':
                wrong['remaining_differences'][0]['go']['values'] = ['wrong']
            elif mutation == 'wrong-type':
                wrong['remaining_differences'][0]['go']['flag'] = 0
            else:
                wrong['go_status'] = 400
            self.assertEqual(annotate([wrong])[0]['status'], 'open', mutation)
        approved = annotate([copy.deepcopy(item)])[0]
        approved['remaining_differences'].append({'path': 'db.extra', 'kind': 'value', 'python': 0, 'go': 1})
        self.assertEqual(annotate([approved])[0]['status'], 'open', 'approval cannot hide a later regression')

    def test_surrogate_approval_keeps_exact_400_message_route_and_readonly_boundary(self):
        from config_finding_resolutions import annotate
        item = {'case': 'X04', 'variant': 'token/literal/"\\ud800"', 'route': 'POST /api/config-token',
                'group': 'json-settings-intervals', 'status': 'open', 'python_status': 500, 'go_status': 400, 'differences': [],
                'remaining_differences': [{'path': 'status', 'kind': 'value', 'python': 500, 'go': 400},
                    {'path': 'body.message', 'kind': 'non-ascii-internal-error-text',
                     'python': "'utf-8' codec can't encode character '\\ud800' in position 238: surrogates not allowed",
                     'go': '请求内容包含无效的 Unicode 字符'}]}
        self.assertEqual(annotate([copy.deepcopy(item)])[0]['status'], 'accepted')
        for mutation in ('route', 'message', 'stored-replacement'):
            wrong = copy.deepcopy(item)
            if mutation == 'route':
                wrong['route'] = 'POST /api/server-domain'
            elif mutation == 'message':
                wrong['remaining_differences'][1]['go'] = 'wrong'
            else:
                wrong['remaining_differences'].append({'path': 'db.token', 'kind': 'value', 'python': '', 'go': '\ufffd'})
            self.assertEqual(annotate([wrong])[0]['status'], 'open', mutation)

    def test_api_json_migration_accepts_only_the_approved_error_without_extra_fields(self):
        from config_finding_resolutions import annotate
        description = 'The browser (or proxy) sent a request that this server could not understand.'
        item = {'case': 'R218-F10', 'variant': '', 'route': 'POST /api/subscriptions/<sub_id>/fetch',
                'group': 'subscription-fetch-failures', 'status': 'open', 'python_status': 400, 'go_status': 400,
                'differences': [], 'remaining_differences': [
                    {'path': 'headers.content-type', 'kind': 'value', 'python': 'text/html; charset=utf-8', 'go': 'application/json'},
                    {'path': 'body', 'kind': 'type', 'python': ['<!doctype html>', '<html lang=en>', '<title>400 Bad Request</title>',
                        '<h1>Bad Request</h1>', '<p>' + description + '</p>'], 'go': {'success': False, 'message': description}}]}
        self.assertEqual(annotate([copy.deepcopy(item)])[0]['status'], 'accepted')
        shape = copy.deepcopy(item)
        shape.update(case='R218-F11', python_status=500, go_status=400)
        shape['remaining_differences'][1].update(
            python=['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>',
                    '<p>The server encountered an internal error and was unable to complete your request. '
                    'Either the server is overloaded or there is an error in the application.</p>'],
            go={'message': '请求体必须是 JSON 对象', 'success': False})
        shape['remaining_differences'].insert(0, {'path': 'status', 'kind': 'value', 'python': 500, 'go': 400})
        self.assertEqual(annotate([shape])[0]['status'], 'accepted')
        for mutation in ('success', 'extra-field', 'message', 'status'):
            wrong = copy.deepcopy(item)
            if mutation == 'success':
                wrong['remaining_differences'][1]['go']['success'] = 0
            elif mutation == 'extra-field':
                wrong['remaining_differences'][1]['go']['extra'] = True
            elif mutation == 'message':
                wrong['remaining_differences'][1]['go']['message'] = 'wrong'
            else:
                wrong['go_status'] = 500
            self.assertEqual(annotate([wrong])[0]['status'], 'open', mutation)

    def test_helper_labels_require_exact_recorded_case_and_route_without_extra_requests(self):
        from helper_links import attach, auxiliary_cases
        first = {'case': 'R123-F08', 'route': 'POST /api/profiles/<profile_id>/clone', 'state': 'go-unimplemented'}
        wrong = {**first, 'route': 'POST /api/profiles'}
        results = attach([first, wrong, {'case': 'H770', 'route': first['route'], 'state': 'compared'}])
        self.assertEqual(len(results), 3)
        self.assertEqual(first['helper_cases'], ['H770'])
        self.assertNotIn('helper_cases', wrong)
        self.assertEqual(auxiliary_cases(results), {'H770'})
        self.assertEqual(first['state'], 'go-unimplemented')
        valid = {'case': 'R087-F13', 'variant': 'text/F13', 'route': 'GET /api/mosdns/rule-proxy', 'state': 'go-unimplemented'}
        wrong_variant = {**valid, 'variant': 'different-input'}
        wrong_route = {**valid, 'route': 'GET /api/profiles/<profile_id>/mosdns/rule-proxy'}
        variants = attach([valid, wrong_variant, wrong_route])
        self.assertEqual(len(variants), 3)
        self.assertEqual(valid['helper_cases'], ['H124', 'H145'])
        self.assertEqual(valid['state'], 'go-unimplemented')
        self.assertNotIn('helper_cases', wrong_variant)
        self.assertNotIn('helper_cases', wrong_route)
        policy_rows = [{'case': 'H416', 'route': route, 'state': 'go-unimplemented'} for route in (
            'POST /api/oxidns/editor', 'POST /api/profiles/<profile_id>/oxidns/editor', 'POST /api/oxidns/render')]
        attach(policy_rows)
        self.assertEqual([row.get('helper_cases') for row in policy_rows], [['H423'], ['H423'], None])
        self.assertTrue(all(row['state'] == 'go-unimplemented' for row in policy_rows))

    def test_version_405_json_approval_preserves_allow_storage_and_exact_public_payload(self):
        from config_finding_resolutions import annotate
        description = 'The method is not allowed for the requested URL.'
        item = {'case': 'R228-F01', 'variant': '', 'route': 'GET /api/version', 'group': 'settings-sync',
                'status': 'open', 'python_status': 405, 'go_status': 405, 'differences': [], 'remaining_differences': [
                    {'path': 'headers.content-type', 'kind': 'value', 'python': 'text/html; charset=utf-8', 'go': 'application/json'},
                    {'path': 'body', 'kind': 'type', 'python': ['<!doctype html>', '<html lang=en>',
                        '<title>405 Method Not Allowed</title>', '<h1>Method Not Allowed</h1>', '<p>' + description + '</p>'],
                     'go': {'success': False, 'message': description}}]}
        self.assertEqual(annotate([copy.deepcopy(item)])[0]['status'], 'accepted')
        for mutation in ('group', 'case', 'route', 'variant', 'status', 'success', 'message', 'extra-field', 'allow', 'SQL', 'file'):
            wrong = copy.deepcopy(item)
            if mutation in ('group', 'case', 'route', 'variant'):
                wrong[mutation] += 'wrong'
            elif mutation == 'status':
                wrong['go_status'] = 400
            elif mutation == 'success':
                wrong['remaining_differences'][1]['go']['success'] = 0
            elif mutation == 'message':
                wrong['remaining_differences'][1]['go']['message'] = 'wrong'
            elif mutation == 'extra-field':
                wrong['remaining_differences'][1]['go']['extra'] = False
            else:
                path = {'allow': 'headers.allow', 'SQL': 'setup.readonly-go.database', 'file': 'files.web-listener.json'}[mutation]
                wrong['remaining_differences'].append({'path': path, 'kind': 'value', 'python': 'old', 'go': 'wrong'})
            self.assertEqual(annotate([wrong])[0]['status'], 'open', mutation)
        storage = copy.deepcopy(item)
        description = ('The server encountered an internal error and was unable to complete your request. '
                       'Either the server is overloaded or there is an error in the application.')
        storage.update(case='R090-F10', variant='SQLite-trigger-rejects-real-editor-commit', route='POST /api/mosdns/settings',
                       group='mosdns-whole-editor-failures', python_status=500, go_status=500)
        storage['remaining_differences'][1].update(python=['<!doctype html>', '<html lang=en>',
            '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>' + description + '</p>'],
            go={'message': description, 'success': False})
        self.assertEqual(annotate([copy.deepcopy(storage)])[0]['status'], 'accepted')
        for path in ('status', 'db.changed', 'setup.readonly-go.files', 'headers.allow'):
            wrong = copy.deepcopy(storage)
            wrong['remaining_differences'].append({'path': path, 'kind': 'value', 'python': 'old', 'go': 'wrong'})
            self.assertEqual(annotate([wrong])[0]['status'], 'open', path)

    def test_repair_attribution_requires_actual_success_and_keeps_raw_evidence(self):
        from config_finding_resolutions import annotate
        item = {'case': 'G13', 'group': 'delivery-graph', 'status': 'closed', 'differences': [{'path': 'provider.comments'}]}
        original = copy.deepcopy(item['differences'])
        self.assertEqual(annotate([item])[0]['fix_commits'], ['7b904725'])
        self.assertEqual(item['differences'], original)
        for status in ('open', 'pending', 'blocked', 'separate'):
            self.assertEqual(annotate([{**item, 'status': status}])[0]['fix_commits'], [])

    def test_busy_probe_checks_admission_completed_batch_and_concurrent_writes(self):
        from cases.node_concurrency import BusyProbe
        observer = BusyProbe.__new__(BusyProbe)
        observer.inputs = [[], []]
        observer.entered = [True, True]
        observer.violations = [[], []]
        observer.first_replies = [Reply(200, {}, b'{"success":true}'), Reply(200, {}, b'{"success":true}')]
        self.assertEqual(observer.compare(Policy()), [])
        observer.entered[1] = False
        self.assertTrue(observer.compare(Policy()))
        observer.entered[1] = True
        observer.first_replies[1] = Reply(503, {}, b'{"success":false}')
        self.assertTrue(observer.compare(Policy()))
        observer.first_replies[1] = observer.first_replies[0]
        observer.violations[1] = [dict(path='changed-storage')]
        self.assertTrue(observer.compare(Policy()))

    def test_finding_rechecks_close_only_actual_comparisons_and_keep_unrun_pending(self):
        from finding_reviews import reviews
        old = {'case': 'R013-S01', 'variant': 'synthetic', 'route': 'GET /api/auth/verify',
               'group': 'synthetic-group', 'state': 'different', 'differences': [{'path': 'body.success'}]}
        self.assertEqual(reviews([old], [])[0]['status'], 'pending')
        for state, expected in [('compared', 'closed'), ('different', 'open'), ('go-unimplemented', 'blocked'),
                                ('go-unimplemented-dependency', 'blocked'), ('O12-separate', 'separate')]:
            current = {**old, 'state': state, 'python_status': 200, 'go_status': 200 if state == 'compared' else None}
            value = reviews([old], [current], baseline='synthetic-fixed-revision')
            self.assertEqual(value[0]['status'], expected)
            self.assertEqual(value[0]['checked_baseline'], 'synthetic-fixed-revision')
            self.assertEqual(reviews([], [], value, 'unrelated-new-run'), value)
        closed = reviews([old], [{**old, 'state': 'compared'}])
        self.assertEqual(reviews([], [old], closed)[0]['status'], 'open')

    def test_generator_isolation_checks_output_backup_foreign_files_and_database(self):
        from cases.profile_generators import generated_errors
        from compare import ini_structure
        for name in ('config.conf', 'loon.lcf'):
            ini = b'[General]\ndns-server = selector-other.example\n'
            saved = {'database': {}, 'files': {'profiles/other/generated/' + name: {'content': ini_structure(ini.decode())}}}
            self.assertEqual(generated_errors({'database': {}, 'files': {}}, saved, 'other', name, ini), [])
        path = 'profiles/other/generated/config.yaml'
        before = {'database': {'revision': 7}, 'files': {'keep': {'content': 'unchanged'}, path: {'content': {'port': 1}}}}
        after = copy.deepcopy(before)
        after['files'][path] = {'content': {'port': 2}}
        after['files'][path + '.bak'] = {'content': {'port': 1}}
        self.assertEqual(generated_errors(before, after, 'other', 'config.yaml', b'port: 2\n'), [])
        for mutation in ('database', 'foreign-file', 'wrong-output', 'wrong-backup', 'wrong-owner'):
            wrong = copy.deepcopy(after)
            if mutation == 'database':
                wrong['database']['revision'] += 1
            elif mutation == 'foreign-file':
                wrong['files']['keep']['content'] = 'wrong'
            elif mutation == 'wrong-output':
                wrong['files'][path]['content']['port'] = 3
            elif mutation == 'wrong-backup':
                wrong['files'][path + '.bak']['content']['port'] = 3
            else:
                wrong['files']['profiles/default/generated/config.yaml'] = wrong['files'].pop(path)
            self.assertTrue(generated_errors(before, wrong, 'other', 'config.yaml', b'port: 2\n'), mutation)

    def test_node_worker_deadline_rejects_immediate_and_unbounded_timeouts(self):
        from cases.node_helpers import WorkerDeadline
        observer = WorkerDeadline(SimpleNamespace(root=Path('/synthetic-unused')))
        observer.before_values[1] = observer.after_values[1] = {'value': 'unchanged'}
        observer.worker.inputs = [[], []]
        observer.elapsed = [5.5, 5.5]
        self.assertEqual(observer.compare(Policy()), [])
        for wrong in (0, 4.9, 8.1, True, '5.5', None, float('nan'), float('inf')):
            observer.elapsed[1] = wrong
            self.assertTrue(observer.compare(Policy()), repr(wrong))

    def test_node_worker_observer_detects_storage_and_stdin_mutations(self):
        from cases.node_helpers import WorkerStable
        observer = WorkerStable(SimpleNamespace(root=Path('/synthetic-unused')))
        observer.before_values[1] = {'value': 'unchanged'}
        observer.after_values[1] = {'value': 'unchanged'}
        observer.worker.inputs = [[{'nodes': ['synthetic']}], [{'nodes': ['synthetic']}]]
        self.assertEqual(observer.compare(Policy()), [])
        observer.after_values[1]['value'] = 'wrong'
        self.assertTrue(observer.compare(Policy()))
        observer.after_values[1]['value'] = 'unchanged'
        observer.worker.inputs[1][0]['nodes'] = []
        self.assertTrue(observer.compare(Policy()))

    def test_dns_worker_deadline_rejects_immediate_and_unbounded_timeouts(self):
        from cases.dns_protocol import ProbeDeadline
        observer = ProbeDeadline.__new__(ProbeDeadline)
        observer.inputs = [[], []]
        observer.elapsed = [10, 10]
        self.assertEqual(observer.compare(Policy()), [])
        for wrong in (0, 8.9, 13.1, True, '10', None, float('nan'), float('inf')):
            observer.elapsed[1] = wrong
            self.assertTrue(observer.compare(Policy()), repr(wrong))

    def test_pull_read_observer_detects_both_storage_and_source_side_effects(self):
        from cases.pull_routes import SourceStable
        observer = SourceStable(None, None)
        observer.before_values[1] = {'value': 'unchanged'}
        observer.after_values[1] = {'value': 'unchanged'}
        observer.exchange.transcripts = [[], []]
        self.assertEqual(observer.compare(Policy()), [])
        observer.after_values[1]['value'] = 'wrong'
        self.assertTrue(observer.compare(Policy()))
        observer.after_values[1]['value'] = 'unchanged'
        observer.exchange.transcripts[1] = [{'method': 'GET', 'path': '/unnecessary-fetch'}]
        self.assertTrue(observer.compare(Policy()))

    def test_setup_timeout_window_rejects_fast_errors_and_unbounded_waits(self):
        from cases.setup_concurrency import TimedStatus
        observer = TimedStatus(None, [])
        observer.stable = None
        observer.observed = [[], []]
        observer.elapsed = [45.01, 45.02]
        self.assertEqual(observer.compare(Policy()), [])
        for wrong in (0, 30, 43.9, 50.1, True, '45', None, float('nan'), float('inf')):
            observer.elapsed[1] = wrong
            self.assertTrue(observer.compare(Policy()), repr(wrong))
        observer.elapsed[1] = 45
        observer.outbound[1] = [{'method': 'GET', 'path': '/unexpected-rule-refresh'}]
        self.assertTrue(observer.compare(Policy()), 'unsolicited fetch during timeout')

    def test_selected_write_isolation_detects_foreign_row_and_revision_mutations(self):
        from cases.profile_writes import isolation_errors, SelectedWrite
        before = {'meta': {'generation': 10}, 'sections': [['profile', 'default', 0], ['profile', 'other', 1]],
                  'fields': {'profile/default/_revision': {'value': 2}, 'profile/other/_revision': {'value': 4},
                             'profile/default/rule_configs': {'value': []}, 'profile/other/rule_configs': {'value': []},
                             'system//agents': {'value': []}}}
        after = copy.deepcopy(before)
        after['meta']['generation'] += 1
        after['fields']['profile/default/_revision']['value'] += 1
        after['fields']['profile/default/rule_configs']['value'] = [{'id': 'new'}]
        self.assertEqual(isolation_errors(before, after, 'default'), [])
        observer = SelectedWrite(None, 'default')
        observer.observed[1] = []
        self.assertEqual(observer.compare(Policy()), [])
        for field in ('profile/other/rule_configs', 'profile/other/_revision', 'system//agents', 'profile/default/_revision'):
            broken = copy.deepcopy(after)
            broken['fields'][field]['value'] = 'wrong'
            observer.observed[1] = isolation_errors(before, broken, 'default')
            self.assertTrue(observer.compare(Policy()), field)
        for section in ('meta', 'sections'):
            broken = copy.deepcopy(after)
            if section == 'meta':
                broken['meta']['generation'] += 1
            else:
                broken['sections'].reverse()
            observer.observed[1] = isolation_errors(before, broken, 'default')
            self.assertTrue(observer.compare(Policy()), section)

    def test_setup_status_protocol_observer_detects_each_wrong_wire_field(self):
        from cases.setup_completion import GatewayAgent, StatusExchange, TOKENS
        from harness import send
        with GatewayAgent() as agent:
            reply = send('http://127.0.0.1:' + str(agent.port), 'GET', '/api/linux-gateway/status',
                         headers={'Authorization': 'Bearer ' + TOKENS[0], 'Accept': 'application/json'})
            self.assertEqual(reply.status, 200)
            observer = StatusExchange(None, [agent, SimpleNamespace(snapshot=lambda: [], errors=[])])
            observer.offsets = [0, 0]
            observer.after(0, reply)
            observer.observed[1] = copy.deepcopy(observer.observed[0])
            self.assertEqual(observer.compare(Policy()), [])
            baseline = copy.deepcopy(observer.observed[1])
            for field, value in [('method', 'POST'), ('path', '/wrong'), ('body_hex', '7b7d'),
                                  ('authorization', 'Bearer leaked-admin-token'), ('accept', '*/*'),
                                  ('content_type', 'application/json')]:
                observer.observed[1] = copy.deepcopy(baseline)
                observer.observed[1][0]['requests'][0][field] = value
                self.assertTrue(observer.compare(Policy()), field)
            observer.observed[1] = copy.deepcopy(baseline)
            observer.observed[1][1]['requests'] = [copy.deepcopy(agent.snapshot()[0])]
            self.assertTrue(observer.compare(Policy()), 'unsolicited OxiDNS request')

    def test_private_draft_capture_reports_go_identity_faults_without_rebinding(self):
        from cases.setup_lifecycle import capture_draft
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, name) for name in ('python', 'go')]
            for directory in dirs:
                directory.mkdir()
                with sqlite3.connect(directory / 'configflow.sqlite3') as db:
                    db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                                     'CREATE TABLE fields(scope,owner,field,position,value,revision);')
            pair = SimpleNamespace(dirs=dirs)
            capture, policy = capture_draft(pair), Policy()
            def reply(identity):
                return Reply(200, {}, json.dumps({'router_draft': {'id': identity}}).encode())
            left, right = 'router_001122334455', 'router_aabbccddeeff'
            self.assertEqual(capture([reply(left), reply(right)], policy), [])
            for source, broken, kind in [(left, 'router_112233445566', 'unstable-Go-draft-id'),
                                         ('router_667788990011', right, 'reused-Go-draft-id'),
                                         (left, 'wrong', 'not-source-generated-router-id-shape')]:
                result = capture([reply(source), reply(broken)], policy)
                self.assertTrue(any(item.kind == kind for item in result))
                self.assertEqual(policy.identities[left], right)

    def test_port_leases_retire_between_fixtures_but_survive_restart(self):
        from unittest.mock import patch
        import harness
        port = 12345
        class RecycledSocket:
            def __enter__(self):
                return self
            def __exit__(self, *_args):
                pass
            def bind(self, address):
                pass
            def getsockname(self):
                return '127.0.0.1', port
        def fixture():
            value = harness.Pair.__new__(harness.Pair)
            value.processes, value.handles, value.port_leases = [], [], []
            return value
        with patch('harness.socket.socket', side_effect=lambda *_args, **_kwargs: RecycledSocket()):
            try:
                # A kernel with one free recycled candidate must still support
                # more fixture lifetimes than .63's entire 4096-port range.
                for _ in range(5000):
                    pair = fixture()
                    pair.port_leases.append(harness.free_port())
                    pair.close()
                pair = fixture()
                pair.port_leases.append(harness.free_port())
                pair.close(release_ports=False)
                with self.assertRaises(harness.DesignError):
                    harness.free_port()
                pair.close()
                other = fixture()
                other.port_leases.append(harness.free_port())
                pair.close()  # Idempotent cleanup cannot retire another owner.
                with self.assertRaises(harness.DesignError):
                    harness.free_port()
                other.close()
                closing = fixture()
                closing.port_leases.append(harness.free_port())
                closing.processes = [SimpleNamespace(poll=lambda: 0,
                    wait=lambda **_kwargs: self.assertIn(port, harness._used_ports))]
                closing.close()
                self.assertNotIn(port, harness._used_ports)
            finally:
                harness.release_port(port)

    def test_blocking_source_proxy_records_connect_and_every_unexpected_write(self):
        import http.client
        from sources import Source
        with Source(b'synthetic origin blocked') as source:
            source.status = 503
            verbs = ('CONNECT', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS')
            for verb in verbs:
                client = http.client.HTTPConnection('127.0.0.1', source.server.server_port)
                client.request(verb, 'origin.example:443' if verb == 'CONNECT' else '/unexpected')
                reply = client.getresponse()
                self.assertEqual(reply.status, 503)
                reply.read()
                client.close()
            self.assertEqual([r['method'] for r in source.requests], list(verbs))
            self.assertEqual(source.requests[0]['path'], 'origin.example:443')

    def test_setup_readonly_observer_rejects_timestamp_and_commit_mutations(self):
        from cases.setup import Stable
        observer = Stable(None)
        baseline = {'database': {'commit_id': 'a' * 32, 'updated_at': '2026-01-01T00:00:00Z'}, 'files': {'session': 'unchanged'}}
        observer.before_values[1] = baseline
        observer.after_values[1] = copy.deepcopy(baseline)
        self.assertEqual(observer.compare(Policy()), [])
        for section, field, value in [('database', 'commit_id', 'b' * 32), ('database', 'updated_at', '2026-01-01T00:00:01Z'),
                                       ('files', 'session', 'changed')]:
            observer.after_values[1] = copy.deepcopy(baseline)
            observer.after_values[1][section][field] = value
            self.assertTrue(observer.compare(Policy()))

    def test_production_waitress_reconstructs_chunked_wsgi_content_length(self):
        import subprocess
        import sys
        from harness import free_port, release_port, send
        port = free_port()
        script = """import json, sys
from waitress import serve
def app(environ, start):
    length = environ.get('CONTENT_LENGTH')
    body = environ['wsgi.input'].read(int(length or 0))
    value = json.dumps({'length': length, 'bytes': len(body)}).encode()
    start('200 OK', [('Content-Type', 'application/json'), ('Content-Length', str(len(value)))])
    return [value]
serve(app, host='127.0.0.1', port=int(sys.argv[1]))
"""
        process = subprocess.Popen([sys.executable, '-c', script, str(port)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            base = 'http://127.0.0.1:' + str(port)
            deadline = time.monotonic() + 10
            while True:
                try:
                    send(base, 'GET', '/')
                    break
                except OSError:
                    if time.monotonic() >= deadline:
                        raise
                    time.sleep(.02)
            payload = b' ' * 32769 + b'{}'
            reply = send(base, 'POST', '/', payload, {'Transfer-Encoding': 'chunked'})
            self.assertEqual(reply.json(), {'length': str(len(payload)), 'bytes': len(payload)})
        finally:
            process.terminate()
            process.wait(timeout=5)
            release_port(port)

    def test_setup_initial_state_observer_rejects_wrong_profile_and_progress(self):
        from cases.setup import SID, PID, NAME, initial_session_errors
        value = {'id': SID, 'profile_id': PID, 'name': NAME, 'goal': 'router', 'step': 1, 'max_step': 1,
                 'subscription_ids': [], 'node_ids': [], 'prepared': False, 'profile_revision': 0, 'completed': False}
        self.assertEqual(initial_session_errors(value), [])
        for patch in ({'profile_id': 'other'}, {'goal': 'gateway'}, {'step': True}, {'prepared': True}, {'node_ids': ['unexpected']}):
            self.assertTrue(initial_session_errors({**value, **patch}))

    def test_chunked_public_client_sends_complete_large_body_without_content_length(self):
        from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
        import threading
        from harness import send
        observed = []
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass
            def do_POST(self):
                chunks = []
                while True:
                    size = int(self.rfile.readline().strip(), 16)
                    if not size:
                        self.rfile.readline()
                        break
                    chunks.append(self.rfile.read(size))
                    self.rfile.read(2)
                observed.append((self.headers.get('Content-Length'), self.headers.get('Transfer-Encoding'), b''.join(chunks)))
                self.send_response(204)
                self.end_headers()
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        worker = threading.Thread(target=server.serve_forever, daemon=True)
        worker.start()
        try:
            payload = b' ' * 32769 + b'{}'
            self.assertEqual(send('http://127.0.0.1:' + str(server.server_port), 'POST', '/setup', payload,
                                  {'Transfer-Encoding': 'chunked', 'Content-Type': 'application/json'}).status, 204)
            self.assertEqual(observed, [(None, 'chunked', payload)])
        finally:
            server.shutdown()
            server.server_close()
            worker.join()

    def test_render_yaml_projection_keeps_types_and_generated_values(self):
        from cases.oxidns_routes import render_projection
        left = {'config': '# generated\nplugins: []\nruntime: {worker_threads: 2}\n'}
        right = {'config': 'runtime:\n  worker_threads: 2\nplugins: []\n'}
        self.assertEqual(differences(render_projection(left, 0), render_projection(right, 1)), [])
        for value in ('plugins: []\nruntime: {worker_threads: 3}\n', None, False):
            self.assertTrue(differences(render_projection(left, 0), render_projection({'config': value}, 1)))

    def test_background_backup_exception_requires_explicit_loopback_fixture(self):
        from harness import DesignError, verify_backup_fixture
        verify_backup_fixture({'auto_backup': False})
        backup = {'auto_backup': True, 'webdav_url': 'http://127.0.0.1:12345'}
        with self.assertRaises(DesignError):
            verify_backup_fixture(backup)
        verify_backup_fixture(backup, True)
        for endpoint in ('https://127.0.0.1:12345', 'http://dav.example:12345', 'http://127.0.0.1'):
            with self.assertRaises(DesignError):
                verify_backup_fixture({**backup, 'webdav_url': endpoint}, True)

    def test_source_size_framing_is_observable_without_duplicate_length_headers(self):
        import http.client
        from sources import Source
        with Source(b'abc') as source:
            for explicit, omitted, expected in [(None, False, ['3']), ('5242881', False, ['5242881']),
                                                (None, True, [])]:
                source.headers = {} if explicit is None else {'content-length': explicit}
                source.omit_content_length = omitted
                client = http.client.HTTPConnection('127.0.0.1', source.server.server_port)
                client.request('HEAD', '/framing')
                reply = client.getresponse()
                self.assertEqual([v for k, v in reply.getheaders() if k.lower() == 'content-length'], expected)
                reply.read()
                client.close()

    def test_proxy_wire_and_no_backup_observers_detect_intentional_wrong_values(self):
        from cases.rule_proxy import no_backup_errors
        snapshot = {'requests': [], 'objects': {}}
        self.assertEqual(no_backup_errors(snapshot), [])
        for broken in ({'requests': [{'method': 'HEAD'}], 'objects': {}},
                       {'requests': [], 'objects': {'/backup/unexpected.json': {}}}):
            self.assertTrue(no_backup_errors(broken))
        from sources import Source
        import http.client
        with Source(b'example.com') as source:
            source.record_wire = True
            client = http.client.HTTPConnection('127.0.0.1', source.server.server_port)
            client.request('GET', 'http://mirror.example/cache/rules', headers={'Proxy-Authorization': 'Basic synthetic'})
            client.getresponse().read()
            client.close()
            row = source.requests[0]
            self.assertEqual(row['request_line'], 'GET http://mirror.example/cache/rules HTTP/1.1')
            self.assertEqual(row['host'], 'mirror.example')
            self.assertEqual(row['proxy_authorization'], 'Basic synthetic')
            for field in ('request_line', 'host', 'proxy_authorization'):
                self.assertTrue(differences(row, {**row, field: 'wrong'}, 'subscription.source.requests[0]'))

    def test_agent_fake_records_every_write_verb_and_no_request_observer_detects_push(self):
        from agent_fake import Agent, NoRequests
        from harness import send
        with Agent(lambda request, index: (200, {'success': True})) as agent:
            agent.release.set()
            for method in ('GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD'):
                self.assertEqual(send('http://127.0.0.1:' + str(agent.port), method, '/synthetic-push',
                                      body=b'{}' if method != 'HEAD' else None).status, 200)
            self.assertEqual([row['method'] for row in agent.snapshot()], ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD'])
        fakes = [SimpleNamespace(snapshot=lambda: [], errors=[]), SimpleNamespace(snapshot=lambda: [{'method': 'PUT'}], errors=[])]
        observer = NoRequests(fakes, wait_seconds=0)
        observer.after(0, None)
        observer.after(1, None)
        self.assertTrue(observer.compare(Policy()))

    def test_selector_defaults_are_semantic_but_editable_text_and_types_are_preserved(self):
        from cases.profile_reads import project
        parse = project('GET /api/custom-config/mihomo')
        left = {'config': '# keep\r\nmixed-port: 19999\n', 'default_config': 'a: 1\nb: false\n'}
        right = {**left, 'default_config': 'b: false\na:    1\n'}
        self.assertEqual(differences(parse(left, 0), parse(right, 1)), [])
        for patch in ({'config': 'mixed-port: 19999\n'}, {'config': left['config'].replace('\r\n', '\n')},
                      {'default_config': 'a: 2\nb: false\n'}, {'default_config': None}):
            self.assertTrue(differences(parse(left, 0), parse({**right, **patch}, 1)))

    def test_unwired_runtime_job_skips_go_but_wired_wrong_final_job_is_detected(self):
        import threading
        from datetime import datetime, timezone
        from agent_jobs import Running
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, value) for value in ('python', 'go')]
            for directory in dirs:
                directory.mkdir()
                with sqlite3.connect(directory / 'configflow.sqlite3') as db:
                    db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                                     'CREATE TABLE fields(scope,owner,field,position,value,revision);')
            for registered, blocked, verdict in [(False, True, 'go-unimplemented'),
                                                  (True, True, 'go-unimplemented-dependency'), (True, False, 'different')]:
                results, polled = [], []
                now = time.time()
                stamp = datetime.fromtimestamp(now, timezone.utc).replace(tzinfo=None).isoformat(timespec='seconds')
                job = {'id': 'a' * 32, 'kind': 'probe', 'status': 'done', 'total': 0, 'done': 0, 'errors': [],
                       'message': '', 'started_at': stamp, 'finished_at': stamp}
                def record(*args, **kwargs):
                    results.append((args, kwargs))
                pair = SimpleNamespace(policy=Policy(start=now), dirs=dirs, recorder=SimpleNamespace(record=record),
                    route_available=lambda route: registered, unimplemented_mutations={'POST /api/tasks/run'} if blocked else set())
                agents = [SimpleNamespace(release=threading.Event(), snapshot=lambda: [], errors=[]) for _ in range(2)]
                observer = Running(pair, agents, 'probe', 0)
                def poll(index):
                    polled.append(index)
                    return Reply(200, {}, json.dumps({'success': True, 'job': {**job, 'message': 'corrupted' if index else ''}}).encode())
                observer.poll = poll
                observer.finish([], route='POST /api/tasks/run')
                self.assertEqual(results[-1][0][4], verdict)
                self.assertEqual(polled, [0, 1] if verdict == 'different' else [0])
                if verdict == 'different':
                    self.assertTrue(any(d.path == 'job.final.job.message' for d in results[-1][0][5]))

    def test_provider_attachment_identity_is_specific_to_its_subscription(self):
        from providers import filename
        policy = Policy(start=1767225600, end=1767225601)
        reply = Reply(200, {'content-disposition': 'inline; filename="proxies_sub-b_20260101_000000.yaml"'}, b'')
        self.assertEqual(filename(reply, policy, 'sub-b'), reply.headers['content-disposition'])
        with self.assertRaises(ValueError):
            filename(reply, policy)
        with self.assertRaises(ValueError):
            filename(Reply(200, {'content-disposition': 'inline; filename="proxies_sub-b_20250101_000000.yaml"'}, b''), policy, 'sub-b')

    def test_running_jobs_reject_invalid_progress_and_protocol_mutations(self):
        from agent_jobs import job_errors, progress_errors
        from agent_fake import probe_calls
        policy = Policy(start=100, end=104)
        job = {'id': 'a' * 32, 'kind': 'probe', 'status': 'running', 'total': 19, 'done': 0,
               'errors': [], 'message': '', 'started_at': '1970-01-01T00:01:40', 'finished_at': None}
        self.assertEqual(job_errors({'success': True, 'job': job}, policy, 'probe', 19), [])
        terminal = {**job, 'status': 'done', 'done': 19, 'finished_at': '1970-01-01T00:01:41'}
        self.assertEqual(job_errors({'success': True, 'job': terminal}, policy, 'probe', 19, terminal=True), [])
        self.assertTrue(job_errors({'success': True, 'job': {**terminal, 'finished_at': '1970-01-01T00:01:39'}},
                                   policy, 'probe', 19, terminal=True))
        for patch in ({'total': 18}, {'total': True}, {'done': -1}, {'done': 1}, {'id': 'bad'}, {'errors': [{}]},
                      {'status': 'done'}, {'finished_at': '1970-01-01T00:01:41'}, {'unexpected': 0}):
            self.assertTrue(job_errors({'success': True, 'job': {**job, **patch}}, policy, 'probe', 19))
        for previous, current in [({**job, 'done': 2}, {**job, 'done': 1}), (job, {**job, 'total': 18}),
                (job, {**job, 'id': 'b' * 32}), ({**job, 'status': 'done'}, {**job, 'status': 'done', 'errors': [{}]})]:
            self.assertTrue(progress_errors(previous, current))
        trace = probe_calls(['x' + str(i) + '.example' for i in range(19)])
        self.assertEqual([len(call['body']['targets']) for call in trace], [20, 3])
        self.assertTrue(differences([], trace, 'auto.quiet.rpc'))
        for mutate in (lambda t: t[0]['body'].update(attempts=1), lambda t: t[1]['body']['targets'].pop(),
                       lambda t: t[0].update(authorization='wrong'), lambda t: t[0].update(path='/wrong')):
            wrong = copy.deepcopy(trace)
            mutate(wrong)
            self.assertTrue(differences(trace, wrong, 'agent.requests'))

    def test_loopback_port_mapping_checks_exact_assignment_and_path(self):
        path = 'db.fields.system//agents.value[0].port'
        policy = Policy(allocated_ports={path: (12340, 12341)})
        self.assertEqual(differences(12340, 12341, path, policy), [])
        for left, right in [(12340, 12342), (12342, 12341), (True, 12341), (12340, 65536)]:
            self.assertTrue(differences(left, right, path, policy))
        self.assertTrue(differences(12340, 12341, path + '-other', policy))

    def test_async_preflight_checks_errors_before_normalizing_phase_fields(self):
        from discovery_jobs import preflight_errors, acceptance_view
        policy = Policy(start=100, end=104)
        started, finished = '1970-01-01T00:01:40', '1970-01-01T00:01:41'
        value = {'success': True, 'job': {'id': '0' * 32, 'kind': 'probe', 'status': 'running', 'done': 0, 'total': 0,
                 'message': '', 'errors': [], 'started_at': started, 'finished_at': None}}
        failure = copy.deepcopy(value)
        failure['job'].update(status='failed', message='synthetic failure', finished_at=finished)
        self.assertEqual(preflight_errors(value, policy, 'probe', 'synthetic failure'), [])
        self.assertEqual(preflight_errors(failure, policy, 'probe', 'synthetic failure'), [])
        self.assertEqual(acceptance_view(value, True), acceptance_view(failure, True))
        for key, wrong in [('id', 'bad'), ('kind', 'region'), ('done', True), ('total', -1), ('done', 1), ('status', 'done'),
                           ('errors', {}), ('message', 'wrong'), ('finished_at', finished), ('started_at', '2099-01-01T00:00:00')]:
            changed = copy.deepcopy(value)
            changed['job'][key] = wrong
            self.assertTrue(preflight_errors(changed, policy, 'probe', 'synthetic failure'))
            self.assertEqual(acceptance_view(changed, False), changed)
        for key, wrong in [('message', 'wrong'), ('finished_at', None), ('finished_at', '1970-01-01T00:01:39')]:
            changed = copy.deepcopy(failure)
            changed['job'][key] = wrong
            self.assertTrue(preflight_errors(changed, policy, 'probe', 'synthetic failure'))
            self.assertTrue(differences(failure, changed))

    def test_discovery_allocations_require_distinct_lowercase_uuid_shapes(self):
        from discovery import allocation_ids
        library = {'id': 'lib_123456781234423482341234567890ab'}
        rule = {'id': 'ruleset_12345678-1234-4234-8234-1234567890ab'}
        self.assertEqual(len(allocation_ids([library], [rule])), 2)
        self.assertEqual(len(allocation_ids([{'id': 'lib_' + '0' * 32}], [rule])), 2)
        for bad in ('lib_0123456789ab', 'lib_' + 'g' * 32, library['id'].upper(), None):
            with self.assertRaises(ValueError):
                allocation_ids([{'id': bad}], [rule])
        with self.assertRaises(ValueError):
            allocation_ids([library, library], [rule])

    def test_aggregation_created_ids_reject_bad_shape_and_reuse(self):
        from cases.aggregations import created
        def reply(value):
            return Reply(200, {}, json.dumps({'data': {'id': value}}).encode())
        capture = created(None)
        policy = Policy()
        self.assertEqual(capture([reply('agg_01234567'), reply('agg_abcdef01')], policy), [])
        self.assertEqual(policy.identities['agg_01234567'], 'agg_abcdef01')
        for wrong in ('bad-id', 'agg_ABCDEF01', None):
            self.assertTrue(capture([reply('agg_01234567'), reply(wrong)], Policy()))
        self.assertTrue(capture([reply('agg_76543210'), reply('agg_abcdef01')], policy))

    def test_managed_zip_assets_keep_bytes_and_exact_member_closure(self):
        from assets import zip_assets, raw_asset_diffs
        def archive(items):
            target = io.BytesIO()
            with zipfile.ZipFile(target, 'w') as output:
                for name, content in items:
                    output.writestr(name, content)
            return target.getvalue()
        original = [('config.yaml', b'plugins: []\n'), ('rules/native.txt', b'# original\r\ndomain:example.com\r\n'),
                    ('data/opaque.bin', b'\x00\xff\x01')]
        left = zip_assets(archive(original))
        self.assertEqual(raw_asset_diffs(left, zip_assets(archive(original)), Policy()), [])
        for changed in (original[:1] + [('rules/native.txt', original[1][1].replace(b'\r\n', b'\n'))] + original[2:],
                        original[:2], original + [('extra', b'unexpected')]):
            self.assertTrue(raw_asset_diffs(left, zip_assets(archive(changed)), Policy()))
        with self.assertRaises(ValueError):
            zip_assets(archive(original + [original[1]]))

    def test_original_response_and_rule_cache_keep_exact_newline_bytes(self):
        left = Reply(200, {'content-type': 'text/plain; charset=utf-8'}, b'# synthetic\r\nexample.com\n')
        right = Reply(200, dict(left.headers), b'# synthetic\nexample.com\n')
        self.assertEqual(response_diffs(left, right), [])
        self.assertTrue(response_diffs(left, right, raw_body=True))
        self.assertEqual(response_diffs(left, copy.deepcopy(left), raw_body=True), [])
        with tempfile.TemporaryDirectory() as temporary:
            dirs = [Path(temporary, name) for name in ('python', 'go')]
            for directory, value in zip(dirs, (left.body, right.body)):
                path = directory / 'shared/rules/by-id/synthetic.list'
                path.parent.mkdir(parents=True)
                path.write_bytes(value)
            self.assertTrue(file_diffs(*dirs, Policy()))
    def test_node_substore_protocol_requires_uri_query_and_cleanup(self):
        from substore import normalized_node, ProtocolError
        from urllib.parse import quote
        name, content = '_cf_tmp_node_convert_01234567', 'ss://synthetic@example.com#中文🧪+%'
        records = [{'method': 'POST', 'path': '/api/subs', 'body': {'name': name, 'url': 'https://example.com#noFlow', 'noFlow': True}},
                   {'method': 'GET', 'path': '/download/' + name + '/ClashMeta?content=' + quote(content, safe=''), 'body': None},
                   {'method': 'DELETE', 'path': '/api/sub/' + name, 'body': None}]
        self.assertEqual(len(normalized_node(records, content)), 3)
        for changed in (records[:2], [records[0], {**records[1], 'path': '/download/' + name + '/ClashMeta?content=wrong'}, records[2]],
                        [records[0], records[1], {**records[2], 'path': '/api/sub/wrong'}]):
            with self.assertRaises(ProtocolError):
                normalized_node(changed, content)
    def test_concurrent_node_conversions_validate_cycles_before_ignoring_interleaving(self):
        from substore import normalized_node_batch, ProtocolError
        from urllib.parse import quote
        contents = ['snell-A 中文🧪', 'snell-B %+']
        cycles = []
        for i, content in enumerate(contents):
            name = '_cf_tmp_node_convert_' + format(i, '08x')
            cycles.append([{'method': 'POST', 'path': '/api/subs', 'body': {'name': name, 'url': 'https://example.com#noFlow', 'noFlow': True}},
                {'method': 'GET', 'path': '/download/' + name + '/ClashMeta?content=' + quote(content, safe=''), 'body': None},
                {'method': 'DELETE', 'path': '/api/sub/' + name, 'body': None}])
        serial = cycles[0] + cycles[1]
        mixed = [cycles[0][0], cycles[1][0], cycles[1][1], cycles[0][1], cycles[1][2], cycles[0][2]]
        self.assertEqual(normalized_node_batch(serial, contents), normalized_node_batch(mixed, contents))
        bad = [mixed[:-1], mixed + [cycles[0][0]], [cycles[0][1], *mixed],
               cycles[0] + cycles[0], [cycles[0][0], cycles[0][2], cycles[0][1]] + cycles[1]]
        for field, value in [('method', 'POST'), ('path', '/unexpected')]:
            changed = copy.deepcopy(mixed)
            changed[2][field] = value
            bad.append(changed)
        changed = copy.deepcopy(mixed)
        changed[1]['body']['noFlow'] = False
        bad.append(changed)
        changed = copy.deepcopy(mixed)
        changed[1]['body'] = [1]
        bad.append(changed)
        for changed in bad:
            with self.assertRaises(ProtocolError):
                normalized_node_batch(changed, contents)
    def test_elapsed_milliseconds_are_finite_bounded_and_two_decimal_places(self):
        policy = Policy(start=100, end=101, elapsed_ms={'body.elapsed_time'})
        self.assertEqual(differences({'elapsed_time': 1.23}, {'elapsed_time': 900}, policy=policy), [])
        for value in (-1, True, '1.23', float('nan'), float('inf'), 3001, 1.234):
            self.assertTrue(differences({'elapsed_time': 1.23}, {'elapsed_time': value}, policy=policy))
    def test_composition_ids_require_prefix_shape_uniqueness_and_derived_cache(self):
        from composition import identities, capture, cache_name
        good = {'data': {'library': {'id': 'lib_0123456789ab'},
                         'rules': [{'id': 'ruleset_abcdef012345', 'itemType': 'ruleset'}]}}
        self.assertEqual(len(identities(good)), 2)
        for value in ({'data': {'rules': [] , 'library': {'id': 'bad'}}},
                      {'data': {'rules': [{'id': 'rule_0123456789ab'}, {'id': 'rule_0123456789ab'}]}},
                      {'data': {'rules': [{'id': 'rule_ABCDEF012345'}]}}, {'data': {'rules': None}}):
            with self.assertRaises(ValueError):
                identities(value)
        with tempfile.TemporaryDirectory() as temporary:
            import hashlib
            dirs = [Path(temporary, 'python'), Path(temporary, 'go')]
            other = copy.deepcopy(good)
            other['data']['library']['id'] = 'lib_abcdef012345'
            other['data']['rules'][0]['id'] = 'ruleset_0123456789ab'
            for directory, value in zip(dirs, (good, other)):
                path = directory / 'shared/rules/by-id' / (hashlib.sha256(value['data']['library']['id'].encode()).hexdigest() + '.list')
                path.parent.mkdir(parents=True)
                path.write_text('+.example.com\n')
            policy = Policy()
            replies = [Reply(200, {}, json.dumps(value).encode()) for value in (good, other)]
            self.assertEqual(capture(SimpleNamespace(dirs=dirs))(replies, policy), [])
            self.assertEqual(file_diffs(*dirs, policy), [])
            next(dirs[1].rglob('*.list')).write_text('+.wrong.example\n')
            self.assertTrue(file_diffs(*dirs, policy))
        remote = {'id': 'lib_0123456789ab', 'source_type': 'url', 'url': 'http://source.example/中文', 'behavior': 'domain', 'format': 'text'}
        self.assertTrue(cache_name(remote).startswith('shared/rules/by-source/'))
        self.assertNotEqual(cache_name(remote), cache_name({**remote, 'url': remote['url'] + '/changed'}))

    def test_rule_results_align_by_id_without_hiding_loss_or_wrong_result(self):
        from rule_library import by_id
        value = {'results': [{'id': 'b', 'success': True}, {'id': 'a', 'success': False}], 'failed_count': 1}
        reordered = {**value, 'results': list(reversed(value['results']))}
        self.assertEqual(differences(by_id(value), by_id(reordered)), [])
        for rows in ([{'id': 'b', 'success': True}], [{'id': 'b', 'success': True}] * 2,
                     [{'id': 'b', 'success': True}, {'id': 'a', 'success': True}]):
            self.assertTrue(differences(by_id(value), by_id({**value, 'results': rows})))
        from subscriptions import Exchange
        observer = Exchange(None, None, unordered=True)
        first = {'method': 'HEAD', 'path': '/a', 'user_agent': 'synthetic'}
        second = {'method': 'HEAD', 'path': '/b', 'user_agent': 'synthetic'}
        observer.transcripts = [[first, second], [second, first]]
        self.assertEqual(observer.compare(Policy()), [])
        observer.transcripts[1] = [second, {**first, 'method': 'GET'}]
        self.assertTrue(observer.compare(Policy()))

    def test_background_receipt_requires_exact_result_and_count(self):
        from task_lifecycle import receipt_errors
        value = {'run_count': 2, 'last_status': 'failed', 'last_trigger': 'manual', 'last_error': 'synthetic failure',
                 'consecutive_failures': 2, 'failure_count': 2}
        self.assertEqual(receipt_errors(value, count=2, status='failed', error='synthetic failure'), [])
        for field, wrong in [('run_count', 1), ('last_status', 'success'), ('last_trigger', 'schedule'),
                             ('last_error', ''), ('failure_count', 1), ('consecutive_failures', 0)]:
            changed = {**value, field: wrong}
            self.assertTrue(receipt_errors(changed, count=2, status='failed', error='synthetic failure'))

    def test_partial_and_no_body_responses(self):
        a = Reply(206, {'content-type': 'application/json', 'content-range': 'bytes 0-3/20'}, b'{"a"')
        self.assertEqual(response_diffs(a, copy.deepcopy(a)), [])
        b = copy.deepcopy(a)
        b.body = b'{"b"'
        self.assertTrue(response_diffs(a, b))
        b = copy.deepcopy(a)
        b.headers['content-range'] = 'bytes 1-4/20'
        self.assertTrue(response_diffs(a, b))
        for status in (204, 304):
            a = Reply(status, {'content-type': 'application/json'}, b'')
            self.assertEqual(response_diffs(a, copy.deepcopy(a)), [])
            b = copy.deepcopy(a)
            b.body = b'wrong'
            self.assertTrue(response_diffs(a, b))

    def test_http_status_and_headers_mutations(self):
        reply = Reply(200, {'content-type': 'application/json', 'etag': '"fixed"'}, b'{"success":true}')
        self.assertEqual(response_diffs(reply, copy.deepcopy(reply)), [])
        changed = copy.deepcopy(reply)
        changed.status = 201
        self.assertTrue(any(d.path == 'status' for d in response_diffs(reply, changed)))
        changed = copy.deepcopy(reply)
        changed.headers['etag'] = '"broken"'
        self.assertTrue(any(d.path == 'headers.etag' for d in response_diffs(reply, changed)))

    def test_allow_order_does_not_hide_missing_method(self):
        a = Reply(405, {'content-type': 'text/html', 'allow': 'HEAD, GET, OPTIONS'}, b'HTML')
        b = Reply(405, {'content-type': 'text/html', 'allow': 'GET, OPTIONS, HEAD'}, b'HTML')
        self.assertEqual(response_diffs(a, b), [])
        b.headers['allow'] = 'GET, OPTIONS'
        self.assertTrue(response_diffs(a, b))

    def test_internal_exception_keeps_chinese_prefix(self):
        policy = Policy(internal_errors={'body.message': '登录失败。原始错误：'})
        self.assertEqual(differences({'message': '登录失败。原始错误：AttributeError'},
                                    {'message': '登录失败。原始错误：decode error'}, policy=policy), [])
        self.assertTrue(differences({'message': '登录失败。原始错误：AttributeError'},
                                   {'message': 'wrong message'}, policy=policy))
        self.assertTrue(differences({'message': '登录失败。原始错误：AttributeError'},
                                   {'message': '登录失败。原始错误：不同的中文提示'}, policy=policy))
        bare = Policy(internal_errors={'body.message': ''})
        self.assertTrue(differences({'message': '原始中文：1'}, {'message': '原始中文：2'}, policy=bare))
        self.assertTrue(differences({'message': 'TypeError'}, {'message': '中文替代提示'}, policy=bare))
        self.assertEqual(differences({'message': '原始中文：1'}, {'message': '原始中文：1'}, policy=bare), [])

    def test_nested_json_mutation_missing_and_type(self):
        value = {'a': [{'nested': {'number': 3, 'enabled': True}}]}
        changed = copy.deepcopy(value)
        changed['a'][0]['nested']['number'] = 4
        self.assertTrue(differences(value, changed))
        del changed['a'][0]['nested']['number']
        self.assertTrue(differences(value, changed))
        self.assertTrue(differences(True, 1))
        self.assertTrue(differences([1, 2], [2, 1]))

    def test_yaml_and_text_mutations(self):
        a = structure(b'hosts:\n  synthetic.example: 192.0.2.10\n', 'text/yaml')
        b = structure(b'hosts:\n  synthetic.example: 192.0.2.11\n', 'text/yaml')
        self.assertTrue(differences(a, b))
        self.assertTrue(differences(structure(b'[Rule]\nDOMAIN,example.com,DIRECT\n'),
                                    structure(b'[Rule]\nDOMAIN,example.com,REJECT\n')))

    def test_archive_and_binary_mutations(self):
        def zipped(content, directory=False):
            buffer = io.BytesIO()
            with zipfile.ZipFile(buffer, 'w') as archive:
                archive.writestr('rules.txt', content)
                if directory:
                    archive.writestr('empty/', '')
            return structure(buffer.getvalue())
        self.assertTrue(differences(zipped('DOMAIN,example.com,DIRECT'), zipped('DOMAIN,example.com,REJECT')))
        self.assertTrue(differences(zipped('same'), zipped('same', directory=True)))
        def tarred(content):
            buffer = io.BytesIO()
            with tarfile.open(fileobj=buffer, mode='w:gz') as archive:
                member = tarfile.TarInfo('rules.txt')
                member.size = len(content)
                archive.addfile(member, io.BytesIO(content))
            return structure(buffer.getvalue(), filename='bundle.tar.gz')
        self.assertTrue(differences(tarred(b'one'), tarred(b'two')))
        self.assertTrue(differences(structure(b'\xff\x00'), structure(b'\xff\x01')))

    def test_database_row_mutation(self):
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for directory in dirs:
                directory.mkdir()
                with sqlite3.connect(directory / 'configflow.sqlite3') as db:
                    db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                                     'CREATE TABLE fields(scope,owner,field,position,value,revision);')
                    db.execute('INSERT INTO meta VALUES (?,?)', ('generation', '1'))
                    db.execute('INSERT INTO sections VALUES (?,?,?)', ('profile', 'default', 0))
                    db.execute('INSERT INTO fields VALUES (?,?,?,?,?,?)',
                               ('profile', 'default', 'rule_configs', 0, '[{"policy":"DIRECT"}]', 1))
            self.assertEqual(database_diffs(*dirs, Policy()), [])
            with sqlite3.connect(dirs[1] / 'configflow.sqlite3') as db:
                db.execute('UPDATE fields SET value=?', ('[{"policy":"REJECT"}]',))
            self.assertTrue(database_diffs(*dirs, Policy()))
            with sqlite3.connect(dirs[1] / 'configflow.sqlite3') as db:
                db.execute('UPDATE fields SET value=?, revision=2', ('[{"policy":"DIRECT"}]',))
            self.assertTrue(database_diffs(*dirs, Policy()))

    def test_rejected_import_keeps_every_row_and_file_except_one_exact_snapshot(self):
        from cases.repository_retained_guards import rejected_import_errors
        before = {'database': {'meta': {'generation': 7}}, 'files': {'retained': {'mode': 384, 'content': 'exact'}}}
        path = 'migrations/20261118T000000Z-' + 'a' * 32 + '/import/configflow.sqlite3'
        after = copy.deepcopy(before)
        after['files'][path] = {'mode': 384, 'content': copy.deepcopy(before['database'])}
        self.assertEqual(rejected_import_errors(before, after), [])
        for mutation in ('database', 'snapshot', 'retained', 'missing', 'extra', 'wrong-path'):
            bad = copy.deepcopy(after)
            if mutation == 'database':
                bad['database']['meta']['generation'] = 8
            elif mutation == 'snapshot':
                bad['files'][path]['content']['meta']['generation'] = 8
            elif mutation == 'retained':
                bad['files']['retained']['content'] = 'changed'
            elif mutation == 'missing':
                del bad['files'][path]
            elif mutation == 'extra':
                bad['files']['extra'] = {'content': 'unexpected'}
            else:
                bad['files']['migrations/arbitrary/import/configflow.sqlite3'] = bad['files'].pop(path)
            self.assertTrue(rejected_import_errors(before, bad), mutation)

    def test_active_runtime_lease_keeps_primary_and_secondary_protocol_proofs(self):
        from cases.dependency_active_lease import ActiveConversion
        observer = ActiveConversion.__new__(ActiveConversion)
        observer.readonly = None
        observer.conversion_stable = SimpleNamespace(compare=lambda policy: [])
        value = {'health': [], 'unexpected_commands': [], 'status_count_valid': True}
        observer.observed = [copy.deepcopy(value), copy.deepcopy(value)]
        observer.protocol = [[{'method': 'POST', 'path': '/api/subs'}]] * 2
        observer.proofs = [{'lease_gate_reached': True, 'lease_gate_timeout': False, 'temporary_subscriptions_cleaned': True}] * 2
        observer.secondary = [Reply(200, {}, b'{"success":true}'), Reply(200, {}, b'{"success":true}')]
        self.assertEqual(observer.compare(Policy()), [])
        for field in ('mode', 'wire', 'proof', 'response'):
            bad = copy.copy(observer)
            if field == 'mode':
                bad.observed = [value, {**value, 'health': [{'method': 'GET'}]}]
            elif field == 'wire':
                bad.protocol = [observer.protocol[0], []]
            elif field == 'proof':
                bad.proofs = [observer.proofs[0], {**observer.proofs[0], 'temporary_subscriptions_cleaned': False}]
            else:
                bad.secondary = [observer.secondary[0], Reply(200, {}, b'{"success":false}')]
            self.assertTrue(bad.compare(Policy()), field)

    def test_readiness_deadline_retains_all_probes_commands_and_elapsed_proof(self):
        from cases.dependency_modes import NeverReady
        observer = NeverReady(None, None)
        observer.readonly = None
        observer.observed = [{'health': [{'method': 'GET', 'path': '/api/utils/env'}],
            'unexpected_commands': [], 'status_count_valid': True}] * 2
        observer.waits = [{'production_timeout_observed': True}] * 2
        self.assertEqual(observer.compare(Policy()), [])
        observer.observed[1] = {**observer.observed[0], 'health': []}
        self.assertTrue(observer.compare(Policy()))
        observer.observed[1] = {**observer.observed[0], 'unexpected_commands': [{'args': ['stop']}]}
        self.assertTrue(observer.compare(Policy()))
        observer.observed[1] = copy.deepcopy(observer.observed[0])
        observer.waits[1] = {'production_timeout_observed': False}
        self.assertTrue(observer.compare(Policy()))

    def test_environment_owned_frontend_ports_are_distinct_and_absent_from_seed(self):
        from harness import Pair, Recorder
        pair = Pair('/nonexistent-unexecuted-public-binary', Recorder(), options={'web_frontend': True,
            'web_listener_ports': (18080, 18081), 'web_port_environment': True})
        try:
            left = pair.environment(pair.dirs[0], 19080)
            right = pair.environment(pair.dirs[1], 19081)
            seed = pair.environment(pair.root / 'seed', 0)
            self.assertEqual((left['CONFIGFLOW_WEB_PORT'], right['CONFIGFLOW_WEB_PORT']), ('18080', '18081'))
            self.assertEqual((left['CONFIGFLOW_BACKEND_PORT'], right['CONFIGFLOW_BACKEND_PORT']), ('19080', '19081'))
            self.assertNotIn('CONFIGFLOW_WEB_PORT', seed)
            self.assertEqual(left['CONFIGFLOW_FRONTEND_DIR'], '')
            self.assertEqual(right['CONFIGFLOW_FRONTEND_DIR'], str(pair.root / 'frontend'))
        finally:
            pair.temp.cleanup()

    def test_concurrent_rule_creation_reports_every_changed_proof_and_second_response(self):
        from cases.rule_create_race import CreationRace
        observer = CreationRace.__new__(CreationRace)
        value = {'gate_reached': True, 'gate_timeout': False, 'winner_status': 200, 'winner_body': {'success': True},
            'exact_one_commit': True, 'fresh_commit': True, 'exact_only_winner_cache': True,
            'source_requests': [{'method': 'GET', 'path': '/held-create'}]}
        observer.observed = [copy.deepcopy(value), copy.deepcopy(value)]
        observer.secondary = [Reply(200, {}, b'{"success":true}'), Reply(200, {}, b'{"success":true}')]
        self.assertEqual(observer.compare(Policy()), [])
        for key in value:
            observer.observed[1] = {**copy.deepcopy(value), key: 'changed'}
            self.assertTrue(observer.compare(Policy()), key)
        observer.observed[1] = copy.deepcopy(value)
        observer.secondary[1] = Reply(409, {}, b'{"success":false}')
        self.assertTrue(observer.compare(Policy()))

    def test_draft_attachment_conflict_preserves_all_noncheck_state(self):
        from cases.setup_router_draft_conflict import preserved_errors
        from cases.setup import SID
        key = 'setup-sessions/' + SID + '.json'
        before = {'database': {'generation': 1}, 'files': {key: {'mode': 384, 'content': {
            'step': 3, 'router_draft': {'password': 'synthetic'}, 'prepared': True}}, 'another': {'content': 'exact'}}}
        after = copy.deepcopy(before)
        after['files'][key]['content'].update(installation={'id': 'checked'}, updated_at='new', config_revision=4,
            route_instructions={'routes': []})
        self.assertEqual(preserved_errors(before, after), [])
        for field in ('database', 'file', 'step', 'password', 'mode', 'extra'):
            bad = copy.deepcopy(after)
            if field == 'database':
                bad['database']['generation'] += 1
            elif field == 'file':
                bad['files']['another']['content'] = 'changed'
            elif field == 'step':
                bad['files'][key]['content']['step'] = 4
            elif field == 'password':
                bad['files'][key]['content']['router_draft']['password'] = 'changed'
            elif field == 'mode':
                bad['files'][key]['mode'] = 420
            else:
                bad['files'][key]['content']['extra'] = True
            self.assertTrue(preserved_errors(before, bad), field)

    def test_full_config_gate_retains_official_slot_and_both_exit_codes(self):
        import os
        import subprocess
        import shutil
        # This is a tools-only miniature tree: no real Go gate or full suite.
        wrapper = Path(__file__).with_name('gate.sh')
        for go_rc, config_rc in [(0, 0), (1, 0), (0, 2), (1, 2)]:
            with self.subTest(go=go_rc, config=config_rc), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                folder = root / 'server/tools/configdiff'
                folder.mkdir(parents=True)
                shutil.copy2(wrapper, folder / 'gate.sh')
                scripts = root / 'server/scripts'
                scripts.mkdir()
                (scripts / 'gate.sh').write_text('cd "$(dirname "$0")/.."\n'
                    '[ -d tools/configdiff ] || exit 22\n'
                    'exec 8>"$CONFIGDIFF_TEST_SLOT"\nflock -n 8 || exit 20\ngot=1\n'
                    + ('exit 1\n' if go_rc else 'true\n'))
                fake = folder / 'native_network.sh'
                fake.write_text('#!/usr/bin/env bash\n'
                    'if flock -n "$CONFIGDIFF_TEST_SLOT" true; then exit 21; fi\n'
                    'printf "%s\\n" "$*" > "$CONFIGDIFF_TEST_ARGUMENTS"\n'
                    'exit "$CONFIGDIFF_TEST_EXIT"\n')
                fake.chmod(0o755)
                logs = root / 'logs'
                env = {**os.environ, 'CONFIGDIFF_GATE_LOGS': str(logs),
                    'CONFIGDIFF_TEST_SLOT': str(root / 'slot'), 'CONFIGDIFF_TEST_ARGUMENTS': str(root / 'arguments'),
                    'CONFIGDIFF_TEST_EXIT': str(config_rc)}
                if config_rc:
                    env.update(CONFIGDIFF_REPORT='testdata/isolated-result.json', CONFIGDIFF_FINDINGS='testdata/isolated-findings.md')
                result = subprocess.run(['bash', str(folder / 'gate.sh'), 'synthetic-baseline', '-p', '4'],
                    env=env, capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, go_rc or config_rc, result.stdout + result.stderr)
                self.assertEqual((logs / 'go-exit.txt').read_text().strip(), str(go_rc))
                self.assertEqual((logs / 'configdiff-exit.txt').read_text().strip(), str(config_rc))
                self.assertIn('--groups all --baseline synthetic-baseline', (root / 'arguments').read_text())
                if config_rc:
                    self.assertIn('--output testdata/isolated-result.json --findings testdata/isolated-findings.md',
                        (root / 'arguments').read_text())
                self.assertEqual(subprocess.run(['flock', '-n', str(root / 'slot'), 'true']).returncode, 0)

    def test_import_snapshot_must_contain_previous_rows(self):
        from compare import database
        from snapshots import snapshot_errors
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            backup = root / 'migrations/synthetic/import'
            backup.mkdir(parents=True)
            with sqlite3.connect(backup / 'configflow.sqlite3') as db:
                db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                                 'CREATE TABLE fields(scope,owner,field,position,value,revision);')
                db.execute('INSERT INTO meta VALUES (?,?)', ('generation', '1'))
                db.execute('INSERT INTO fields VALUES (?,?,?,?,?,?)',
                           ('profile', 'default', 'rule_configs', 0, '[{"policy":"DIRECT"}]', 1))
            before = database(backup)
            self.assertEqual(snapshot_errors(root, before, set()), [])
            with sqlite3.connect(backup / 'configflow.sqlite3') as db:
                db.execute('UPDATE fields SET revision=2')
            self.assertTrue(snapshot_errors(root, before, set()))
            self.assertTrue(snapshot_errors(root, before, {backup / 'configflow.sqlite3'}))

    def test_publish_guard_uses_profile_and_shared_revisions(self):
        from publish import capture, check
        from harness import DesignError
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for root in dirs:
                root.mkdir()
                with sqlite3.connect(root / 'configflow.sqlite3') as db:
                    db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                                     'CREATE TABLE fields(scope,owner,field,position,value,revision);')
                    for scope, owner, value in [('profile', 'other', 7), ('shared', '', 3)]:
                        db.execute('INSERT INTO fields VALUES (?,?,?,?,?,?)', (scope, owner, '_revision', 0, str(value), 1))
            pair = SimpleNamespace(dirs=dirs)
            correct = {'publish_guard': {'expected_profile_id': 'other', 'expected_service_type': 'mihomo', 'expected_config_revision': 10}}
            a = Reply(200, {}, json.dumps(correct).encode())
            check(pair, 'other', 'mihomo')(a)
            self.assertEqual(capture(pair, 'other', 'mihomo')([a, copy.deepcopy(a)], Policy()), [])
            for key, bad in [('expected_profile_id', 'default'), ('expected_service_type', 'surge'), ('expected_config_revision', 7)]:
                changed = copy.deepcopy(correct)
                changed['publish_guard'][key] = bad
                b = Reply(200, {}, json.dumps(changed).encode())
                self.assertTrue(capture(pair, 'other', 'mihomo')([a, b], Policy()))
                with self.assertRaises(DesignError):
                    check(pair, 'other', 'mihomo')(b)

    def test_subscription_exchange_and_cached_identity_mutation(self):
        from subscriptions import Exchange, capture_ids
        source = SimpleNamespace(requests=[])
        observer = Exchange(None, source)
        observer.transcripts = [[{'method': 'GET', 'path': '/feed', 'user_agent': 'clash.meta'}]] * 2
        self.assertEqual(observer.compare(Policy()), [])
        observer.transcripts[1] = []
        self.assertTrue(observer.compare(Policy()))
        observer.transcripts[1] = [{'method': 'GET', 'path': '/feed', 'user_agent': 'wrong'}]
        self.assertTrue(observer.compare(Policy()))
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for root, ids in zip(dirs, [('node_12345678', 'node_12345679'), ('node_abcdef01', 'node_abcdef02')]):
                cache = root / 'shared/subscribes'
                cache.mkdir(parents=True)
                (cache / 'sub-a.json').write_text(json.dumps({'nodes': [{'id': value} for value in ids]}))
            pair = SimpleNamespace(dirs=dirs)
            self.assertEqual(capture_ids(pair)([], Policy()), [])
            target = dirs[1] / 'shared/subscribes/sub-a.json'
            target.write_text(json.dumps({'nodes': [{'id': 'node_abcdef01'}, {'id': 'node_abcdef01'}]}))
            self.assertTrue(capture_ids(pair)([], Policy()))
            target.write_text(json.dumps({'nodes': [{'id': 'broken-id'}, {'id': 'node_abcdef02'}]}))
            self.assertTrue(capture_ids(pair)([], Policy()))

    def test_database_unilateral_commit_reports_difference(self):
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for root in dirs:
                root.mkdir()
                with sqlite3.connect(root / 'configflow.sqlite3') as db:
                    db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                                     'CREATE TABLE fields(scope,owner,field,position,value,revision);')
                    db.execute('INSERT INTO meta VALUES (?,?)', ('commit_id', json.dumps('a' * 32)))
            policy = Policy()
            self.assertEqual(database_diffs(*dirs, policy), [])
            with sqlite3.connect(dirs[1] / 'configflow.sqlite3') as db:
                db.execute('UPDATE meta SET value=?', (json.dumps('b' * 32),))
            self.assertTrue(any(d.kind == 'unstable-Go-commit' for d in database_diffs(*dirs, policy)))

    def test_blocked_exception_details_preserve_chinese_and_wrong_status(self):
        from subscriptions import blocked_error_projection as project
        a = {'message': '从订阅URL获取失败: request_failed (exception_type=SubscriptionFetchError)，且本地无缓存数据'}
        b = {'message': '从订阅URL获取失败: request_failed (exception_type=SubscriptionFetchError status=403)，且本地无缓存数据'}
        self.assertEqual(project(a, 0), project(b, 1))
        self.assertNotEqual(project(a, 0), project({'message': b['message'].replace('且本地无缓存数据', 'wrong')}, 1))
        self.assertNotEqual(project(a, 0), project({'message': b['message'].replace('403', '500')}, 1))

    def test_task_notification_evidence_uses_real_receipt(self):
        from subscriptions import notice_errors
        task = {'run_count': 1, 'last_run_at': '2026-01-01T00:00:00Z', 'last_status': 'failed', 'consecutive_failures': 1}
        item = {'evidence': '1:2026-01-01T00:00:00Z:failed', 'checked_at': task['last_run_at'],
                'last_seen_at': task['last_run_at'], 'failure_count': 1}
        self.assertEqual(notice_errors(item, task), [])
        for field, bad in [('evidence', '2:2026-01-01T00:00:00Z:failed'), ('failure_count', 0),
                           ('checked_at', '2025-01-01T00:00:00Z'), ('last_seen_at', 'wrong')]:
            changed = copy.deepcopy(item)
            changed[field] = bad
            self.assertTrue(notice_errors(changed, task))

    def test_substore_protocol_cleanup_and_local_payload_mutation(self):
        from substore import normalized, ProtocolError
        name = '_cf_tmp_sub-a_12345678'
        records = [{'method': 'POST', 'path': '/api/subs', 'body': {'name': name, 'url': 'http://source.example/feed#noFlow', 'noFlow': True, 'source': 'local', 'content': 'opaque-body'}},
                   {'method': 'GET', 'path': '/download/' + name + '/ClashMeta?noFlow=true', 'body': None},
                   {'method': 'DELETE', 'path': '/api/sub/' + name, 'body': None}]
        self.assertEqual(len(normalized(records, 'http://source.example/feed', 'opaque-body')), 3)
        with self.assertRaises(ProtocolError):
            normalized(records[:-1], 'http://source.example/feed', 'opaque-body')
        for key, bad in [('noFlow', False), ('content', 'wrong-body'), ('url', 'http://other.example')]:
            changed = copy.deepcopy(records)
            changed[0]['body'][key] = bad
            with self.assertRaises(ProtocolError):
                normalized(changed, 'http://source.example/feed', 'opaque-body')

    def test_provider_local_time_comment_and_filename_mutations(self):
        from datetime import datetime
        from zoneinfo import ZoneInfo
        from providers import comments, filename
        policy = Policy(start=1760000000, end=1760000001, tz='Asia/Shanghai')
        local = datetime.fromtimestamp(policy.start, ZoneInfo(policy.tz)).replace(tzinfo=None)
        raw = ('# Generated: ' + local.isoformat() + 'Z\n# Source: from local cache\nproxies: []\n').encode()
        valid = comments(raw, policy)
        with self.assertRaises(ValueError):
            comments(raw.replace(str(local.year).encode(), b'1990'), policy)
        changed = raw.replace(b'from local cache', b'changed source')
        self.assertTrue(differences(valid, comments(changed, policy)))
        reply = Reply(200, {'content-disposition': 'inline; filename="proxies_sub-a_' + local.strftime('%Y%m%d_%H%M%S') + '.yaml"'}, raw)
        filename(reply, policy)
        with self.assertRaises(ValueError):
            filename(Reply(200, {'content-disposition': reply.headers['content-disposition'].replace('sub-a', 'wrong')}, raw), policy)
        with self.assertRaises(ValueError):
            filename(Reply(200, {'content-disposition': reply.headers['content-disposition'].replace(str(local.year), '1990')}, raw), policy)

    def test_provider_fallback_and_rule_stale_headers_remain_observable(self):
        a = Reply(200, {'content-type': 'text/yaml'}, b'proxies: []')
        for key in ('x-configflow-provider', 'x-configflow-rule-cache', 'x-configflow-cache', 'warning'):
            b = copy.deepcopy(a)
            b.headers[key] = 'unexpected'
            self.assertTrue(response_diffs(a, b))

    def test_provider_fallback_elapsed_and_background_mutation(self):
        from provider_lifecycle import Fallback
        observer = object.__new__(Fallback)
        record = {'elapsed_valid': True, 'elapsed': 12.0, 'requests': [{'path': '/feed'}],
                  'fresh_after_release': True, 'quiescent_after_release': True, 'served_comments': ['# Source: fresh']}
        observer.observed = [record, copy.deepcopy(record)]
        self.assertEqual(observer.compare(Policy()), [])
        observer.observed[1]['elapsed_valid'] = False
        observer.observed[1]['elapsed'] = 14.0
        self.assertTrue(observer.compare(Policy()))
        observer.observed[1] = copy.deepcopy(record)
        observer.observed[1]['fresh_after_release'] = False
        self.assertTrue(observer.compare(Policy()))
        observer.observed[1] = copy.deepcopy(record)
        observer.observed[1]['requests'].append({'path': '/feed'})
        self.assertTrue(observer.compare(Policy()))

    def test_yaml_backup_compares_structure_and_changed_nodes(self):
        a = b'proxies:\n - {name: "\\U0001F9EA", type: socks5, port: 1080}\n'
        b = 'proxies:\n  - name: 🧪\n    type: socks5\n    port: 1080\n'.encode()
        self.assertEqual(structure(a, filename='sub.yaml.bak'), structure(b, filename='sub.yaml.bak'))
        self.assertTrue(differences(structure(a, filename='sub.yaml.bak'), structure(b.replace(b'1080', b'1081'), filename='sub.yaml.bak')))

    def test_all_concurrent_provider_responses_are_compared(self):
        from provider_lifecycle import response_batch_diffs
        a = [Reply(200, {'content-type': 'text/yaml'}, b'proxies: []') for _ in range(20)]
        b = copy.deepcopy(a)
        self.assertEqual(response_batch_diffs(a, b, Policy()), [])
        b[19].status = 401
        self.assertTrue(any(d.path == 'provider.client[19].status' for d in response_batch_diffs(a, b, Policy())))
        self.assertTrue(response_batch_diffs(a, b[:-1], Policy()))

    def test_merged_provider_detail_metadata_mutation(self):
        from datetime import datetime, timezone
        from providers import MergedMetadata
        policy = Policy(start=1760000000, end=1760000001)
        stamp = datetime.fromtimestamp(policy.start, timezone.utc).replace(tzinfo=None).isoformat()
        raw = ('# Generated: ' + stamp + 'Z\n# Subscriptions: 1\n# Total Proxies: 2\n#   - synthetic: 2 proxies (updated: ' + stamp + ')\nproxies: []').encode()
        observer = MergedMetadata(None, [('synthetic', 2)])
        observer.parse(raw, policy)
        for old, new in [(b'synthetic: 2', b'synthetic: 3'), (b'Total Proxies: 2', b'Total Proxies: 3'), (b'updated: 2025', b'updated: 1990')]:
            changed = raw.replace(old, new)
            if changed != raw:
                with self.assertRaises(ValueError):
                    observer.parse(changed, policy)

    def test_future_task_deadline_is_bounded_by_exact_interval(self):
        from datetime import datetime, timezone
        policy = Policy(start=1760000000, end=1760000001, future_times={'body.next': 1800})
        value = datetime.fromtimestamp(1760001800, timezone.utc).isoformat().replace('+00:00', 'Z')
        self.assertEqual(differences({'next': value}, {'next': value}, 'body', policy), [])
        wrong = datetime.fromtimestamp(1760003600, timezone.utc).isoformat().replace('+00:00', 'Z')
        self.assertTrue(differences({'next': value}, {'next': wrong}, 'body', policy))
        self.assertTrue(differences({'next': wrong}, {'next': wrong}, 'body', policy))

    def test_generated_file_mutation(self):
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for directory in dirs:
                directory.mkdir()
                (directory / 'rules.txt').write_text('DOMAIN,example.com,DIRECT\n')
            self.assertEqual(file_diffs(*dirs, Policy()), [])
            (dirs[1] / 'rules.txt').write_text('DOMAIN,example.com,REJECT\n')
            self.assertTrue(file_diffs(*dirs, Policy()))
            (dirs[1] / 'unexpected.txt').write_text('must not disappear')
            self.assertTrue(file_diffs(*dirs, Policy()))

    def test_identity_binding_is_injective_and_does_not_mask_neighbors(self):
        policy = Policy()
        policy.bind('profile_abcdef', 'profile_123456', r'profile_[0-9a-f]{6}', 'profile')
        self.assertEqual(differences({'id': 'profile_abcdef'}, {'id': 'profile_123456'}, policy=policy), [])
        self.assertTrue(differences({'id': 'profile_abcdef', 'name': '甲'},
                                    {'id': 'profile_123456', 'name': '乙'}, policy=policy))
        with self.assertRaises(AssertionError):
            policy.bind('profile_654321', 'profile_123456', r'profile_[0-9a-f]{6}', 'profile')

    def test_time_policy_validates_window_and_format(self):
        policy = Policy(times={'body.at'}, start=1760000000, end=1760000005)
        self.assertTrue(differences({'at': '1990-01-01T00:00:00Z'},
                                    {'at': '1990-01-01T00:00:00Z'}, policy=policy))
        self.assertTrue(differences({'at': 'nonsense'}, {'at': 'nonsense'}, policy=policy))

    def test_duration_is_bounded_and_keeps_other_fields(self):
        policy = Policy(durations={'body.ms'}, start=100, end=102)
        self.assertEqual(differences({'ms': 1}, {'ms': 39}, policy=policy), [])
        for value in (-1, 5000, True, '10'):
            self.assertTrue(differences({'ms': 1}, {'ms': value}, policy=policy))
        self.assertTrue(differences({'ms': 1, 'ok': True}, {'ms': 9, 'ok': False}, policy=policy))

    def test_dav_transcript_and_upload_mutation(self):
        from cases.webdav import Exchange
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for d in dirs:
                d.mkdir()
            exchange = Exchange(None, SimpleNamespace(dirs=dirs, tz='UTC'))
            value = {'requests': [{'method': 'HEAD', 'authorization': 'Basic synthetic',
                                   'path': '/backup/fixed.json', 'body': {'profile': {'rules': ['DIRECT']}}}],
                     'objects': {'/backup/fixed.json': {'profile': {'rules': ['DIRECT']}}}}
            exchange.observations = [copy.deepcopy(value), copy.deepcopy(value)]
            self.assertEqual(exchange.compare(Policy()), [])
            for field, broken in [('method', 'DELETE'), ('authorization', ''), ('body', {'profile': {}})]:
                exchange.observations[1] = copy.deepcopy(value)
                exchange.observations[1]['requests'][0][field] = broken
                self.assertTrue(exchange.compare(Policy()))

    def test_probe_stdin_mutation(self):
        from probe import Exchange
        with tempfile.TemporaryDirectory() as tmp:
            exchange = Exchange(SimpleNamespace(root=Path(tmp)))
            original = {'nodes': [{'name': '["node","node-a"]', 'server': '192.0.2.1'}], 'timeout_ms': 5000}
            exchange.inputs = [[copy.deepcopy(original)], [copy.deepcopy(original)]]
            self.assertEqual(exchange.compare(Policy()), [])
            exchange.inputs[1][0]['nodes'][0]['server'] = '192.0.2.2'
            self.assertTrue(exchange.compare(Policy()))
            exchange.inputs[1] = []
            self.assertTrue(exchange.compare(Policy()))

    def test_artifact_verified_digest_and_content_mutation(self):
        import hashlib
        from artifacts import validate, ArtifactError
        with tempfile.TemporaryDirectory() as tmp:
            def fixture(name, content):
                folder = Path(tmp, name, '.generated-previews', 'a' * 32)
                folder.mkdir(parents=True, exist_ok=True)
                folder.chmod(0o700)
                folder.parent.chmod(0o700)
                digest = hashlib.sha256(content).hexdigest()
                manifest = {'agent_id': 'manual', 'deployment_id': 'b' * 32, 'profile_id': 'default',
                    'service_type': 'mihomo', 'config_path': 'config.yaml', 'files': [
                        {'path': 'config.yaml', 'role': 'config', 'size': len(content), 'sha256': digest}]}
                output = io.BytesIO()
                with tarfile.open(fileobj=output, mode='w:gz') as tar:
                    for path, value in [('manifest.json', json.dumps(manifest).encode()), ('files/config.yaml', content)]:
                        member = tarfile.TarInfo(path)
                        member.size, member.mode = len(value), 0o600
                        tar.addfile(member, io.BytesIO(value))
                archive = output.getvalue()
                record = {'id': 'a' * 32, 'deployment_id': 'b' * 32, 'expires_at': 3700, 'agent_id': 'manual',
                    'profile_id': 'default', 'service_type': 'mihomo', 'bundle_meta': {'manifest': manifest,
                        'sha256': hashlib.sha256(archive).hexdigest(), 'config_version': digest[:8]}}
                (folder / 'record.json').write_text(json.dumps(record))
                (folder / 'bundle.tar.gz').write_bytes(archive)
                return Path(tmp, name), folder
            left, _ = fixture('left', b'port: 8080\n')
            right, folder = fixture('right', b'port: 8080\n')
            self.assertEqual(differences(validate(left, 'a'*32, 100, 100)['projection'],
                                        validate(right, 'a'*32, 100, 100)['projection']), [])
            for private in (folder, folder.parent):
                private.chmod(0o755)
                with self.assertRaises(ArtifactError):
                    validate(right, 'a'*32, 100, 100)
                private.chmod(0o700)
            fixture('right', b'port: 8081\n')
            self.assertTrue(differences(validate(left, 'a'*32, 100, 100)['projection'],
                                       validate(right, 'a'*32, 100, 100)['projection']))
            record = json.loads((folder / 'record.json').read_text())
            record['bundle_meta']['sha256'] = '0' * 64
            (folder / 'record.json').write_text(json.dumps(record))
            with self.assertRaises(ArtifactError):
                validate(right, 'a'*32, 100, 100)

    def test_epoch_window_mutation(self):
        policy = Policy(start=100, end=105, epochs={'body.expires_at': 3600})
        self.assertEqual(differences({'expires_at': 3700}, {'expires_at': 3705}, policy=policy), [])
        for invalid in (1, 3800, True, '3700'):
            self.assertTrue(differences({'expires_at': 3700}, {'expires_at': invalid}, policy=policy))

    def test_damaged_file_and_binary_evidence_mutation(self):
        from harness import Recorder
        from compare import Difference
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for d in dirs:
                d.mkdir()
                (d / 'damaged.json.bak').write_bytes(b'{invalid\xff')
            self.assertEqual(file_diffs(*dirs, Policy()), [])
            (dirs[1] / 'damaged.json.bak').write_bytes(b'{invalid\xfe')
            diffs = file_diffs(*dirs, Policy())
            self.assertTrue(diffs)
            recorder = Recorder()
            recorder.record('self-check', '', 'files', 'self-check', 'different', diffs)
            self.assertIn('bytes_sha256', json.dumps(recorder.results, allow_nan=False))
        self.assertEqual(differences(float('nan'), float('nan')), [])
        self.assertTrue(differences(float('nan'), 1.0))
        recorder = Recorder()
        recorder.record('self-check', '', 'body', 'self-check', 'different',
                        [Difference('body', 'value', '\ud800', float('nan'))])
        json.dumps(recorder.results, ensure_ascii=False, allow_nan=False).encode('utf-8')

    def test_nested_database_backup_row_mutation(self):
        with tempfile.TemporaryDirectory() as tmp:
            roots = [Path(tmp, n) for n in ('python', 'go')]
            for root in roots:
                folder = root / 'migrations/fixed/import'
                folder.mkdir(parents=True)
                with sqlite3.connect(folder / 'configflow.sqlite3') as db:
                    db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                                     'CREATE TABLE fields(scope,owner,field,position,value,revision);')
                    db.execute('INSERT INTO fields VALUES (?,?,?,?,?,?)', ('shared', '', 'nodes', 0, '[{"name":"original"}]', 1))
            self.assertEqual(file_diffs(*roots, Policy()), [])
            with sqlite3.connect(roots[1] / 'migrations/fixed/import/configflow.sqlite3') as db:
                db.execute('UPDATE fields SET value=?', ('[{"name":"changed"}]',))
            self.assertTrue(file_diffs(*roots, Policy()))

    def test_naive_timestamp_uses_selected_server_timezone(self):
        policy = Policy(times={'body.at'}, start=1767225600, end=1767225605, tz='Asia/Shanghai')
        self.assertEqual(differences({'at': '2026-01-01T08:00:00'}, {'at': '2026-01-01T08:00:01'}, policy=policy), [])
        self.assertTrue(differences({'at': '2026-01-01T00:00:00'}, {'at': '2026-01-01T08:00:01'}, policy=policy))

    def test_generated_ini_fields_and_order_mutation(self):
        a = b'[Proxy]\nnode = socks5, 192.0.2.1, 1080\n[Rule]\nDOMAIN,example.com,DIRECT\nFINAL,REJECT\n'
        b = b'[Proxy]\n node=socks5,192.0.2.1,1080\n\n[Rule]\n DOMAIN, example.com, DIRECT\nFINAL, REJECT\n'
        self.assertEqual(structure(a, filename='surge.conf'), structure(b, filename='surge.conf'))
        changed = b.replace(b'DIRECT', b'REJECT')
        self.assertTrue(differences(structure(a, filename='surge.conf'), structure(changed, filename='surge.conf')))
        changed = b.replace(b'DOMAIN, example.com, DIRECT\nFINAL, REJECT', b'FINAL, REJECT\nDOMAIN, example.com, DIRECT')
        self.assertTrue(differences(structure(a, filename='loon.lcf'), structure(changed, filename='loon.lcf')))

    def test_download_headers_describe_actual_bytes(self):
        from downloads import capture_headers
        from email.utils import formatdate
        policy = Policy(start=1767225600, end=1767225601)
        left = Reply(200, {'etag': '"left"', 'last-modified': formatdate(policy.start, usegmt=True), 'content-length': '3'}, b'abc')
        right = copy.deepcopy(left)
        right.headers['etag'] = '"right"'
        self.assertEqual(capture_headers([left, right], policy), [])
        for header, value in [('content-length', '4'), ('etag', 'unquoted'),
                               ('last-modified', 'Wed, 01 Jan 2020 00:00:00 GMT')]:
            broken = copy.deepcopy(right)
            broken.headers[header] = value
            self.assertTrue(capture_headers([left, broken], Policy(start=policy.start, end=policy.end)))

    def test_oxidns_etag_and_committed_guard_mutation(self):
        import hashlib
        from oxidns import etag, capture
        value = {'n': 9007199254740993, 'b': 1.25, 'a': '🧪'}
        literal = '{"a":"🧪","b":1.25,"n":9007199254740993}'
        self.assertEqual(etag(value), '"' + hashlib.sha256(literal.encode()).hexdigest() + '"')
        self.assertNotEqual(etag(value), etag({**value, 'b': 2.0}))
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for directory in dirs:
                directory.mkdir()
                with sqlite3.connect(directory / 'configflow.sqlite3') as db:
                    db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                                     'CREATE TABLE fields(scope,owner,field,position,value,revision);')
                    for scope, owner, field, item in [('profile', 'default', 'oxidns', value),
                                                     ('profile', 'default', '_revision', 2), ('shared', '', '_revision', 3)]:
                        db.execute('INSERT INTO fields VALUES (?,?,?,?,?,?)', (scope, owner, field, 0, json.dumps(item), 1))
            guard = {'expected_profile_id': 'default', 'expected_service_type': 'oxidns', 'expected_config_revision': 5}
            left = Reply(200, {'etag': etag(value)}, json.dumps({'publish_guard': guard}).encode())
            right = copy.deepcopy(left)
            observer = capture(SimpleNamespace(dirs=dirs))
            self.assertEqual(observer([left, right], Policy()), [])
            right.headers['etag'] = '"' + '0' * 64 + '"'
            self.assertTrue(observer([left, right], Policy()))
            right = copy.deepcopy(left)
            right.body = json.dumps({'publish_guard': {**guard, 'expected_config_revision': 4}}).encode()
            self.assertTrue(observer([left, right], Policy()))


if __name__ == '__main__':
    unittest.main()
