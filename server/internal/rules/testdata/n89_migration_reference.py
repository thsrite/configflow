"""Real-Python historical filenames: evidence for one-time read migration only."""
import json
from pathlib import Path
from backend.utils.rule_utils import rule_cache_filename
cases = [
 r'{"id":"r1","url":"https://example.com/rules?x=<a>&q=中","behavior":"dom\u0061in","format":"text","source_type":"\u0075rl"}',
 r'{"id":"","name":"Display","url":"https://example.com/rules","behavior":"domain","format":"text"}',
 r'{"id":"line\u2028separator","url":"https://example.com/rules","behavior":"domain","format":"text"}',
 r'{"id":"line\u2029separator","url":"https://example.com/rules","behavior":"domain","format":"text"}',
 r'{"id":"literal\\u2028","url":"https://example.com/rules","behavior":"domain","format":"text"}',
 r'{"id":null,"name":"Name","url":"https://example.com/rules","behavior":null}',
]
rows=[{'input':raw,'filename':rule_cache_filename(json.loads(raw))} for raw in cases]
Path(__file__).with_name('n89_migration_expected.json').write_text(json.dumps(rows,ensure_ascii=False,indent=2)+'\n')
print('historical migration filenames:',len(rows))
