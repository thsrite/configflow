"""generate.py default/scoped downloads and structural previews."""
import copy
import re
import time

from compare import Policy, structure
from downloads import capture_headers, validate_headers
from harness import Pair, DesignError, json_body
from inventory import routes
from cases.nodes import NODE

MATRIX = routes()
FILENAMES = {'mihomo': 'config.yaml', 'surge': 'surge.conf', 'loon': 'loon.lcf', 'mosdns': 'mosdns-config.zip', 'oxidns': 'oxidns-config.zip'}


def previews(binary, recorder):
    for route, rid in MATRIX.items():
        match = re.fullmatch(r'POST /api/(?:profiles/<profile_id>/)?generate/(mihomo|surge|loon|mosdns|oxidns)/preview', route)
        if not match:
            continue
        service = match[1]
        path = route.split(' ', 1)[1].replace('<profile_id>', 'default')
        with Pair(binary, recorder) as pair:
            def project(value, index):
                if isinstance(value, dict) and isinstance(value.get('content'), str):
                    value = copy.deepcopy(value)
                    value['content'] = structure(value['content'].encode(), filename=FILENAMES[service] if service in ('surge', 'loon') else 'config.yaml')
                return value
            for case, body, variant in [('S01', {'base_url': 'http://panel.example'}, ''),
                                         ('S02', {'base_url': 'http://panel.example'}, 'explicit'),
                                         ('S02', {}, 'omitted'), ('B01', {}, 'empty-resources'), ('B03', {}, 'structured-content')]:
                pair.request(rid + '-' + case, route, path, body=json_body(body),
                             headers={'Host': 'panel.example'}, variant=variant, expect=200, writes=True, projection=project)


def downloads(binary, recorder):
    for route, rid in MATRIX.items():
        match = re.fullmatch(r'POST /api/(?:profiles/<profile_id>/)?generate/(mihomo|surge|loon|mosdns|oxidns)', route)
        if not match:
            continue
        service = match[1]
        path = route.split(' ', 1)[1].replace('<profile_id>', 'default')
        with Pair(binary, recorder) as pair:
            for case, body, variant, headers in [('S01', {'base_url': 'http://panel.example'}, '', {}),
                                                 ('S02', {'base_url': 'http://panel.example'}, '', {}),
                                                 ('S03', {}, '', {}), ('B02', {}, 'empty-resources', {}),
                                                 ('B03', {'base_url': 'http://panel.example/a%20b?x=1&y=2'}, '', {}),
                                                 ('B90', {}, 'POST-Range-is-not-GET', {'Range': 'bytes=0-3'}),
                                                 ('B90', {}, 'POST-If-None-Match', {'If-None-Match': '*'})]:
                policy = Policy(start=pair.policy.start)
                def check(reply):
                    errors = validate_headers(reply, policy.start, time.time())
                    if errors:
                        raise DesignError('Python generated download: ' + repr(errors))
                pair.request(rid + '-' + case, route, path, body=json_body(body),
                             headers={'Host': 'panel.example', **headers}, variant=variant, expect=200,
                             writes=True, policy=policy, capture=capture_headers, check=check, filename=FILENAMES[service])
            if service in ('mihomo', 'surge', 'loon', 'mosdns'):
                bad_id = 'F10' if service == 'mosdns' else 'F09'
                policy = Policy(start=pair.policy.start, internal_errors={'body.message': '生成 MosDNS 配置失败：' if service == 'mosdns' else ''})
                pair.request(rid + '-' + bad_id, route, path, body='{bad', expect=500,
                             writes=True, policy=policy)
                if service != 'mosdns':
                    pair.request(rid + '-F10', route, path, body='[1]', expect=500,
                                 writes=True, policy=policy)


def unicode_names(binary, recorder):
    fixture = {'shared': {'nodes': [NODE]}, 'profile': {'proxy_groups': [
        {'id': 'group-unicode', 'name': '策略组🧪', 'type': 'select', 'manual_nodes': ['node-a']}]}}
    for route, rid in MATRIX.items():
        match = re.fullmatch(r'POST /api/(?:profiles/<profile_id>/)?generate/(mihomo|surge|loon|mosdns|oxidns)(/preview)?', route)
        if not match:
            continue
        service, preview = match[1], bool(match[2])
        with Pair(binary, recorder, options=fixture) as pair:
            path = route.split(' ', 1)[1].replace('<profile_id>', 'default')
            def project(value, index):
                if isinstance(value, dict) and isinstance(value.get('content'), str):
                    value = copy.deepcopy(value)
                    value['content'] = structure(value['content'].encode(), filename=FILENAMES[service] if service in ('surge', 'loon') else 'config.yaml')
                return value
            pair.request(rid + ('-B02' if preview else '-B01'), route, path,
                         body=json_body({'base_url': 'http://panel.example'}), headers={'Host': 'panel.example'},
                         expect=200, writes=True, capture=None if preview else capture_headers,
                         projection=project if preview else None, filename=FILENAMES[service] if not preview else '')


GROUPS = {'generate-previews': previews, 'generate-downloads': downloads,
          'generate-unicode': unicode_names}
