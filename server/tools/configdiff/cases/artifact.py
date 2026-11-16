"""Generated artifact/content exemptions and frozen read errors via public HTTP."""
import json
from pathlib import Path
from urllib.parse import quote

from artifacts import Observer
from harness import Pair, json_body, DesignError
from seed import CURRENT


def generated_artifacts(binary, recorder):
    for scoped, rid in [(False, 62), (True, 126)]:
        with Pair(binary, recorder, options={'x01_native': True}) as pair:
            prefix = '/api/profiles/default/generate' if scoped else '/api/generate'
            template = '/api/profiles/<profile_id>/generate/<service>/artifact' if scoped else '/api/generate/<service>/artifact'
            for index, service in enumerate(('mihomo', 'surge', 'loon', 'mosdns', 'oxidns')):
                observer = Observer(pair)
                pair.request(f'R{rid:03}-S{index+1:02}', 'POST ' + template, prefix + '/' + service + '/artifact',
                             body=json_body({'base_url': 'http://panel.example'}), expect=200, writes=True, files=False,
                             check=observer.reference, capture=observer.capture, projection=observer.public)
            observer = Observer(pair)
            pair.request('X01', 'POST ' + template, prefix + '/oxidns/artifact', body='{}',
                         variant='artifact-content/' + str(scoped), expect=200, writes=True, files=False,
                         check=lambda r: observer.reference(r, secret=CURRENT),
                         capture=observer.capture, projection=observer.public)
            pair.request(f'R{rid:03}-F08', 'POST ' + template, prefix + '/unsupported/artifact', body='{}', expect=400)
            for case, profile, expected in [('F12', 'missing-synthetic', 404), ('F13', 'bad!id', 400)]:
                path = ('/api/profiles/' + profile + '/generate/mihomo/artifact' if scoped else prefix + '/mihomo/artifact?profile=' + profile)
                pair.request(f'R{rid:03}-' + case, 'POST ' + template, path, body='{}', expect=expected)


def frozen_reads(binary, recorder):
    with Pair(binary, recorder, options={'x01_native': True}) as pair:
        observer = Observer(pair)
        replies = pair.request('T08', 'POST /api/generate/<service>/artifact', '/api/generate/oxidns/artifact',
                               body='{}', expect=200, writes=True, files=False, variant='prepare-frozen',
                               check=observer.reference, capture=observer.capture, projection=observer.public)
        ids = [r.json()['id'] if r.status else '0' * 32 for r in replies]
        route = 'GET /api/generate/artifacts/<preview_id>/<action>'
        for case, action, query, expected in [('S01', 'download', '', 200), ('S02', 'file', '?path=config.yaml', 200),
                                              ('F08', 'unknown', '', 404), ('F15', 'file', '', 404),
                                              ('F16', 'file', '?path=missing', 404), ('B01', 'file', '?path=../record.json', 404)]:
            pair.request('R063-' + case, route, paths=['/api/generate/artifacts/' + key + '/' + action + query for key in ids],
                         expect=expected, variant='frozen')
        for case, key in [('F09', 'bad-id'), ('F10', '0' * 32)]:
            pair.request('R063-' + case, route, '/api/generate/artifacts/' + key + '/download', expect=409)
        # Only corrupt each existing artifact through its documented on-disk
        # record/archive, restoring it between guards. No business helper called.
        for case in ('F11', 'F12', 'F13', 'F14'):
            backups = []
            for directory, key in zip(pair.dirs, ids):
                folder = directory / '.generated-previews' / key
                if not folder.exists():
                    backups.append(None)
                    continue
                record_path, bundle_path = folder / 'record.json', folder / 'bundle.tar.gz'
                raw, archive = record_path.read_bytes(), bundle_path.read_bytes()
                backups.append((record_path, raw, bundle_path, archive))
                value = json.loads(raw)
                if case == 'F11':
                    value['expires_at'] = 1
                elif case == 'F12':
                    record_path.write_text('{invalid')
                    continue
                elif case == 'F13':
                    bundle_path.write_bytes(archive + b'synthetic corruption')
                else:
                    value['agent_id'] = 'other-synthetic-agent'
                record_path.write_text(json.dumps(value))
            pair.request('R063-' + case, route, paths=['/api/generate/artifacts/' + key + '/download' for key in ids], expect=409)
            for backup in backups:
                if backup:
                    a, raw, b, archive = backup
                    a.write_bytes(raw)
                    b.write_bytes(archive)


GROUPS = {'generated-artifacts': generated_artifacts, 'generated-frozen-reads': frozen_reads}
