#!/usr/bin/env python3
"""Generate review.jsonl: edge cases added by the C01 adversarial review.

Same record format as corpus.jsonl (kind=value / kind=error, see gen.py),
from the production interpreter and Flask:

    ~/cf-test/.venv/bin/python server/internal/jsonx/testdata/review_gen.py
"""

import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from gen import error_case, value_case  # noqa: E402

# Exactly halfway between 0 and the smallest subnormal (2**-1075), written
# out in full (> 800 significant digits): ties-to-even gives 0.0; one more
# digit anywhere after it gives 5e-324.
HALF = '0.' + str(5 ** 1075).rjust(1075, '0')  # 2**-1075 exactly
assert len(HALF) > 800

VALUES = [
    HALF, HALF + '0001', HALF[:-1] + '4' + '9' * 50, '0.' + '9' * 1000, '1' + '0' * 400 + '.0', '-' + '1' * 309 + '.5',
    '123456789012345678901234567890.5', '1e-320', '2.2250738585072011e-308', '2.2250738585072012e-308',
    '9007199254740993.0', '9007199254740995.0', '0.1e1', '1e23', '8.98846567431158e307', '1.7976931348623158e308',
    '-0.0e0', '0E-0', '1E+0', '-1e-0', '1e0000000000000000000400', '1e-99999999999999999999',
    '0.0000000000000000000000000000001e31', '1e16', '1e-4', '1e-5', '0.1', '[0.1,0.2,0.30000000000000004]',
    '{"\\uffff":1,"\\ud800\\udc00":2,"\\u00e9":3,"e\\u0301":4,"Z":5,"a":6," ":7,"\\u2028":8,"\\u0000":9,"":10,"\\u007f":11}',
    '{"k\\u2028":"v\\u2029","\\u3000":"全角\\uff11\\uff12","emoji":"\\ud83d\\ude80\\ud83c\\udde8\\ud83c\\uddf3"}',
    '"\\u0000\\u001f\\u0020\\u007e\\u007f\\u0080\\u00ad\\u061c\\u200b\\u200e\\u2028\\u2029\\ufeff\\ufffe\\uffff"',
    '"\\udbff\\udfff\\ud800\\udc00\\ud83d\\ude00"', '"１２３．５"', '["１", "٣", "߀", "𝟘"]',
    '{"a":1,"b":{"a":2,"a":3},"a":{"z":[]}}', '{"dup":1,"x":2,"dup":3,"dup":{"inner":1,"inner":2}}',
    '[' + ','.join(str(i * 7919 % 100003 - 50000) + ('.5' if i % 3 == 0 else '') for i in range(600)) + "]",
    '{' + ','.join('"k%05d":%d' % ((i * 7919) % 400, i) for i in range(400)) + '}',
]

DEEP = ['[' * 600 + ']' * 600, '{"a":' * 300 + '[]' + '}' * 300, '[{"k":' * 200 + '"\u00e9"' + '}]' * 200]

ERRORS = [
    '[' * 600 + 'x', '"\\ud800\\u"', '"\\ud800\\"', '"\\ud800\\u12"', '"\\ud800\\u12G4"', '"\\ud800\\uDC0"',
    '{\r\n"a":\r\n x}', '["😀😀", x]', ' [1]', '[1] ', '1\x00', '[1]﻿', ' ', '　[]',
    '[" ", \x7f]', '{"例": 1, "例" 2}', '"😀\t"', '[-0x]', '-Infinityx', 'NaN1', '[NaN,nan]', '1e+-5', '1.5.5',
    '０', '[1,2]]', '{"a":1}{', '"\\U0001F600"', '"\\u00e9\\x"', '\n' * 3 + '  ]',
    '-' + '9' * 4301, '9' * 4301 + '.', '[' + '1' * 4301 + ']',
]


def main():
    cases = [value_case(f'review-{index}', raw) for index, raw in enumerate(VALUES)]
    for index, raw in enumerate(DEEP):  # no indent outputs: they would be megabytes of spaces
        cases.append(value_case(f'review-deep-{index}', raw, only=('compact', 'sorted', 'default', 'response', 'truthy')))
    for raw in ERRORS:
        case = error_case(raw)
        assert case is not None, raw
        cases.append(case)
    target = Path(__file__).with_name('review.jsonl')
    with open(target, 'w', encoding='ascii', newline='\n') as handle:
        for case in cases:
            handle.write(json.dumps(case, ensure_ascii=True, separators=(',', ':')) + '\n')
    print(f'{target.name}: {len(cases)} cases, python {sys.version.split()[0]}')


if __name__ == '__main__':
    main()
