"""Exact D2 provider paths / R5 hook inputs for the frozen Python replay.

The real backend lowerer supplies every digest preimage. Go results are never
used, and no original archive is rewritten.
"""
import ast
import copy
import gzip
import hashlib
import json
import re
from pathlib import Path
from types import SimpleNamespace
import yaml
from backend.utils import group_candidates as gc

root = Path(__file__).parent
archive = root / 'mihomo_recorded.jsonl.gz'
lines = gzip.decompress(archive.read_bytes()).splitlines()
real_sha256 = hashlib.sha256
references = {}
for index, line in enumerate(lines):
    row = json.loads(line)
    if row['fn'] != 'generate' or row.get('err') or row.get('patched'):
        continue
    profile = row['args']['profile']
    document = yaml.safe_load(row['out'])
    providers = document.get('proxy-providers', {})
    private = {name: provider for name, provider in providers.items()
               if re.fullmatch(r'\./providers/group_filter_[a-f0-9]{20}\.yaml', str(provider.get('path', '')))}
    paths = {}
    if private:
        # Restore only the already-lowered use references, keeping all other
        # recorded data. The actual Python lowerer then emits its original keys.
        before = copy.deepcopy(document)
        originals = [name for name in providers if name not in private]
        aliases = {}
        for name in private:
            matches = [original for original in originals
                       if any(name == f"{group['name']}_{original}_筛选" or
                              re.fullmatch(re.escape(f"{group['name']}_{original}_筛选") + r' \([0-9]+\)', name)
                              for group in profile.get('proxy_groups', []))]
            assert len(matches) == 1, ('ambiguous original provider', index)
            aliases[name] = matches[0]
            del before['proxy-providers'][name]
        for group in before.get('proxy-groups', []):
            if 'use' in group:
                group['use'] = [aliases.get(name, name) for name in group['use']]
        def capture(data):
            key = ast.literal_eval(data.decode())
            assert isinstance(key, tuple) and len(key) == 3 and all(isinstance(v, str) for v in key)
            old = './providers/group_filter_' + real_sha256(data).hexdigest()[:20] + '.yaml'
            native = json.dumps(key, ensure_ascii=False, separators=(',', ':'))
            for char, escaped in [('&', '\\u0026'), ('<', '\\u003c'), ('>', '\\u003e'),
                                  ('\u2028', '\\u2028'), ('\u2029', '\\u2029')]:
                native = native.replace(char, escaped)
            paths[old] = './providers/group_filter_' + real_sha256(native.encode()).hexdigest()[:20] + '.yaml'
            return real_sha256(data)
        previous = gc.hashlib
        gc.hashlib = SimpleNamespace(sha256=capture)
        try:
            gc.lower_group_provider_filters(copy.deepcopy(profile), before)
        finally:
            gc.hashlib = previous
        assert set(paths) == {item['path'] for item in private.values()}, ('lowerer did not bind original paths', index)
    patches = []
    if 'test_legacy_falsy_filter_generation[0-' in row['test']:
        projected = copy.deepcopy(profile)
        for group in projected['proxy_groups']:
            for field in ('regex', 'aggregation_regex'):
                if type(group.get(field)) is int and group[field] == 0:
                    group[field] = '0'  # shared model.String R5 input contract
        groups = {group['id']: group for group in projected['proxy_groups']}
        by_name = {group['name']: group for group in projected['proxy_groups']}
        for call_index, call in enumerate(row['env']['gateway']):
            for group_index, group in enumerate(call['before']['proxy-groups']):
                record = by_name[group['name']]
                source = groups.get(record.get('follow_group'), record)
                value = gc.render_members(source, projected)['filter']
                assert value == '(0)|(0)' and 'filter' not in group
                patches.append({'call': call_index, 'group': group_index, 'name': group['name'], 'filter': value})
    if paths or patches:
        references[str(index)] = {'test': row['test'], 'sha256': real_sha256(line).hexdigest(),
                                  'paths': paths, 'gateway_filters': patches}
output = {'archive_sha256': real_sha256(archive.read_bytes()).hexdigest(), 'rows': references}
(root / 'n89_recorded_provider_paths.json').write_text(json.dumps(output, ensure_ascii=False, indent=2) + '\n')
print('RECORDED PROVIDER REFERENCE:', sum(bool(r['paths']) for r in references.values()),
      'exact D2 rows;', sum(bool(r['gateway_filters']) for r in references.values()), 'exact R5 hook rows')
