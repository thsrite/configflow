"""Independent exact D2 refusals for frozen cache presence calls.

Only valid old Python cache bytes with no matching current stored declaration
become unknown. Raw archives and all other expected values/errors stay intact.
"""
import gzip
import hashlib
import json
from pathlib import Path
from backend.common.rule_ip_presence import _declaration
from backend.utils.rule_utils import rule_cache_filename

root = Path(__file__).parent
references = {}
for name in ('r02_recorded.jsonl.gz', 'r02_corpus.jsonl.gz', 'r02_edge.jsonl.gz'):
    archive = root.parent / 'routersplit/testdata' / name
    rows = {}
    for index, line in enumerate(gzip.decompress(archive.read_bytes()).splitlines()):
        row = json.loads(line)
        if row['fn'] != 'classical_ip_lines' or row.get('err') or type(row.get('out')) is not bool:
            continue
        item, config = row['args']['item'], row['args']['config']
        if not isinstance(item, dict) or not isinstance(config, dict):
            continue
        try:
            declaration = _declaration(item, config)  # real Python resolver
        except (AttributeError, TypeError):
            continue
        if declaration.get('source_type', 'url') != 'url' or not isinstance(declaration.get('url'), str) or not declaration['url']:
            continue
        if any(declaration.get(field) is not None and not isinstance(declaration[field], str)
               for field in ('url', 'format', 'behavior', 'source_type')):
            continue
        old = rule_cache_filename(declaration)  # real Python address
        world = row.get('rules', {})
        if old not in world.get('files', {}):
            continue
        stored = world.get('library', [])
        current = next((entry for entry in stored if isinstance(entry, dict) and entry.get('id') == declaration.get('id')), None)
        owned = current is not None and all(current.get(field) == declaration.get(field)
                                           for field in ('url', 'format', 'behavior', 'source_type'))
        if owned:
            continue
        rows[hashlib.sha256(line).hexdigest()] = {'index': index, 'python_output': row['out'], 'native_output': None,
                                                'kind': 'D2 unowned legacy cache'}
    expected = {'r02_recorded.jsonl.gz': 7, 'r02_corpus.jsonl.gz': 237, 'r02_edge.jsonl.gz': 30}
    assert len(rows) == expected[name], ('unexpected exact refusal count', name, len(rows))
    references[name] = {'archive_sha256': hashlib.sha256(archive.read_bytes()).hexdigest(), 'rows': rows}
(root / 'testdata/n89-unowned-cache.json').write_text(json.dumps(references, ensure_ascii=False, indent=2) + '\n')
print('PRESENCE D2 REFERENCES:', {name: len(value['rows']) for name, value in references.items()})
