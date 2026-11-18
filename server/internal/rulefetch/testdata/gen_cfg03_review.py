"""CFG-03 independent review oracle: real Python normalize_mihomo_yaml, synthetic sources only."""
import json
from pathlib import Path
from backend.utils.ruleset_content import normalize_mihomo_yaml
cases = {
    'block-payload-quoted-multiline': 'payload:\n  - "a.example.com\n    b"\n',
    'block-payload-plain-multiline': 'payload:\n  - a.example.com\n    b\n',
    'block-payload-single-multiline': "payload:\n  - 'a.example.com\n    b'\n",
    'block-root-flow-payload-multiline-item': 'payload: [+.example.com, "a\n b"]\n',
    'block-root-flow-payload-other-multiline': 'payload: [+.example.com]\nextra: "a\n b"\n',
    'multiline-key': '{payload: [+.example.com], "multi\n key": v}\n',
    'nel-in-quoted': '{payload: [+.example.com], extra: "first\u0085second"}\n',
    'ls-in-quoted': '{payload: [+.example.com], extra: "first second"}\n',
    'ps-in-plain': '{payload: [+.example.com], extra: first second}\n',
    'nested-sequence': '{payload: [+.example.com], extra: ["a\n b"]}\n',
    'cjk-multiline': '{payload: [+.例子.com], 名: "甲\n 乙"}\n',
    'cr-only': '{payload: [+.example.com], extra: "first\r second"}\r',
    'tagged-plain-multiline': '{payload: [+.example.com], extra: !!str first\n second}\n',
    'verbatim-tag-multiline': '{payload: [+.example.com], extra: !<tag:yaml.org,2002:str> "a\n b"}\n',
    'valid-escaped-backslash-end': '{payload: [+.example.com], extra: "a\\\\"}\n',
    'valid-doubled-single-quote': "{payload: [+.example.com], extra: 'it''s'}\n",
    'valid-verbatim-tag': '{payload: [+.example.com], extra: !<tag:yaml.org,2002:str> "a b"}\n',
    'valid-multiline-layout': '{\n  payload: [+.example.com, a.example.com],\n  extra: "x"\n}\n',
    'valid-block-flow-payload': 'payload: [+.example.com, "a b"]\nextra: 1\n',
    'valid-comment-quote': '{payload: [+.example.com], extra: "x # y"} # "\n',
}
rows = []
for name, source in cases.items():
    out, warnings = normalize_mihomo_yaml(source)
    rows.append(dict(name=name, source=source, output=out, warnings=warnings))
    print(name, 'preserved=', out == source, 'warnings=', warnings, flush=True)
Path(__file__).with_name('cfg03_review.json').write_text(json.dumps(rows, ensure_ascii=False, indent=2) + '\n')
