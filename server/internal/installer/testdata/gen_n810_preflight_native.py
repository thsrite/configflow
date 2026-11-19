"""Independent real-Python report oracle for the approved native machine grammar.

Frozen archives are read only. Original rows and complete old reports are bound
by archive and row SHA-256; native reports run the same Python business parser
with only Go whitespace and ASCII machine-number policy injected.
"""
import copy
import gzip
import hashlib
import json
import re
import sys
from pathlib import Path
from types import SimpleNamespace
from backend.agents import docker_ha_preflight as original

ROOT = Path(__file__).resolve().parent
SPACE = ' \t\n\r\v\f\u0085\u00a0\u1680' + ''.join(map(chr, range(0x2000, 0x200b))) + '\u2028\u2029\u202f\u205f\u3000'
SPACE_CLASS = '[' + re.escape(SPACE) + ']'

class NativeText(str):
    def strip(self, chars=None): return NativeText(super().strip(SPACE if chars is None else chars))
    def split(self, sep=None, maxsplit=-1):
        if sep is not None: return [NativeText(x) for x in super().split(sep, maxsplit)]
        text = self.strip()
        return [NativeText(x) for x in re.split(SPACE_CLASS + '+', text)] if text else []
    def splitlines(self, keepends=False): return [NativeText(x) for x in super().splitlines(keepends)]
    def isdigit(self): return re.fullmatch('[0-9]+', self) is not None

def native_match(pattern, text, *args, **kwargs):
    if pattern == r'^\d+:\s+(\S+)\s+inet\s+(\S+)\s+':
        pattern = r'^[0-9]+:' + SPACE_CLASS + r'+([^' + re.escape(SPACE) + r']+)' + SPACE_CLASS + r'+inet' + SPACE_CLASS + r'+([^' + re.escape(SPACE) + r']+)' + SPACE_CLASS + '+'
    return re.match(pattern, text, *args, **kwargs)

scope = dict(original.__dict__)
exec(compile(Path(original.__file__).read_text(), original.__file__, 'exec'), scope)
real_decode = scope['_decode']
def native_decode(output):
    values, reason = real_decode(output)
    if values is not None:
        values = {key:(code,NativeText(text)) for key,(code,text) in values.items()}
    return values, reason
scope['_decode'] = native_decode
scope['re'] = SimpleNamespace(**{name:getattr(re,name) for name in dir(re) if not name.startswith('__')})
scope['re'].match = native_match

rows = []
for archive_name in ('docker_ha.jsonl.gz','docker_ha_recorded.jsonl.gz','docker_ha_review.jsonl.gz'):
    archive = ROOT / archive_name
    archive_sha = hashlib.sha256(archive.read_bytes()).hexdigest()
    for line in gzip.decompress(archive.read_bytes()).splitlines():
        row = json.loads(line)
        if row.get('kind') == 'preflight':
            args = [row['input'].get('output'),row['input'].get('mode','host')]
            prior = row.get('result')
        elif row.get('fn') == 'parse_report' and 'patched' not in row:
            args = row['args'];prior = row.get('out')
        else: continue
        try:
            old = original.parse_report(*args)
        except (TypeError, ValueError) as exc:
            old = {'class':type(exc).__name__,'message':str(exc),'value_error':isinstance(exc,ValueError)}
            if row.get('kind') == 'preflight': prior = row.get('error')
            else:
                expected = row.get('err')
                prior = {'class':expected[0],'message':expected[1],'value_error':expected[2]} if expected else None
        limit = sys.get_int_max_str_digits()
        try:
            sys.set_int_max_str_digits(0)
            native = scope['parse_report'](*args)
        except (TypeError,ValueError): continue
        finally: sys.set_int_max_str_digits(limit)
        if old != prior: raise RuntimeError('frozen Python report differs from real Python')
        if old == native: continue
        rows.append(dict(archive=archive_name, archive_sha256=archive_sha,
                         row_sha256=hashlib.sha256(line).hexdigest(),
                         original=row, python=old, native=native))
print(json.dumps({'policy':'N8-10 ASCII machine counters and Go whitespace','rows':rows},ensure_ascii=False,separators=(',',':')))
