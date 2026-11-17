"""CFG-03 physical scalar delivery oracle: real Python, synthetic sources only."""
import json
from pathlib import Path
from backend.utils.ruleset_content import normalize_mihomo_yaml
cases={
 'H133':'{payload: [+.example.com], extra: "first\n second"}\n',
 'single-quote':"{payload: [+.example.com], extra: 'first\n second'}\n",
 'CRLF':'{payload: [+.example.com], extra: "first\r\n second"}\r\n',
 'BOM-CJK':'\ufeff{payload: [+.example.com], 中文: "首行\n 次行"}\n',
 'nested':'{payload: [+.example.com], extra: [{nested: "first\n second"}]}\n',
 'plain-fold':'{payload: [+.example.com], extra: first\n second}\n',
 'tagged':'{payload: [+.example.com], extra: !!str "first\n second"}\n',
 'escaped-quote':'{payload: [+.example.com], extra: "first \\\"quoted\\\"\n second"}\n',
 'escaped-break':'{payload: [+.example.com], extra: "first\\\n second"}\n',
 'payload-physical':'payload: ["example.\n com"]\n',
 'valid-CJK-BOM':'\ufeff{payload: [+.example.com], 中文: "首行 次行"}\r\n',
 'valid-Unicode-break':'{payload: [+.example.com],\u0085 extra: "first second"}\n',
 'valid-flow':'{payload: [+.example.com], extra: "first second"}\n',
 'valid-tagged':'{payload: [+.example.com], extra: !!str "first second"}\n',
 'valid-quote':'{payload: [+.example.com], extra: "first \\\"quoted\\\" second"}\n',
 'valid-plain':'{payload: [+.example.com], extra: first second}\n',
}
rows=[]
for name,source in cases.items():
 out,warnings=normalize_mihomo_yaml(source)
 rows.append(dict(name=name,source=source,output=out,warnings=warnings))
 print(name,'preserved=',out==source,'warnings=',len(warnings))
Path(__file__).with_name('cfg03.json').write_text(json.dumps(rows,ensure_ascii=False,indent=2)+'\n')
