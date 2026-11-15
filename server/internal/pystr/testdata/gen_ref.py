#!/usr/bin/env python3
"""Generate ref.json: CPython 3.11 results of str.lower(), str.strip(), int(s, base) and
float(s) on edge inputs (synthetic values only).

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/pystr/testdata/gen_ref.py
"""
import json
import random
import sys
from pathlib import Path

assert sys.version_info[:2] == (3, 11), sys.version
R = random.Random(20261115)

LOWER = ['', 'ABC', 'İstanbul', 'ΑΣ', 'ΣΑΣ', 'ΑΣΑ', 'Σ', 'AΣ', 'AΣ.', "A'Σ", 'AΣ́', 'AΣ́b', 'ⒶΣ', 'Ⓐ', 'ǅ', 'ẞ', 'ﬃ', 'K', 'Å',
         'ΣΣ', 'Σ.Σ', '1Σ', 'aΣ1', 'ʰΣ', 'ͅΣ', 'ͅΣ', 'xΣͅ', 'aΣ­', 'Ꞌ', '\U0001E900', '\U00010400', 'Ա', 'Ⴀ', 'Ᏸ',
         'Ꟁ', 'Ꟑ', 'Ꟗ', '\U00010570', '\U0001E030', 'Ɤ', 'Ꟛ', '\U00016E40', '\U0001E922']
for _ in range(400):
    LOWER.append(''.join(chr(R.choice([R.randrange(0x41, 0x5B), R.randrange(0x370, 0x400), 0x3A3, 0x3A3, R.randrange(0x300, 0x370),
                                       R.randrange(0x1E00, 0x1F00), R.randrange(0x2000, 0x2070), R.randrange(0x24B6, 0x24EA),
                                       R.randrange(0x10400, 0x10450), R.randrange(0x1E900, 0x1E950), 0x27, 0x2E, 0x20])) for _ in range(R.randrange(1, 6))))

STRIP = ['', ' a ', '\x1c a \x1f', '　a ', '\x85a\xa0', '​a​', '᠎a', '﻿a', '   a   ']

INT_TEXTS = ['0', '00', '000', '0_0', '0__0', '_0', '0_', '010', '0010', '1', '-1', '+1', ' 1 ', '\t1\n', '\x0b1\x0c', '\x1c1', '1\x1f',
             '　1　', ' 1', '\x851', '1_000', '1__000', '_1', '1_', '+_1', '-0', '--1', '+-1', '', ' ', '+', '-', '_',
             '0x1f', '0X1F', '0x_1f', '0x__1f', '0x', '0x_', '0o17', '0O17', '0o_7', '0b101', '0B1', '0b_1', '0b', '0b2', '0o8',
             '0x-1F', '0x+1', '-0x1f', '+0x1f', ' 0x1f ', '0x 1f', '0 x1f', '1f', 'ff', 'FF', 'z', 'Z', '10', '0z', 'zz',
             '١٢٣', '١x', '０１２', '𝟙𝟙', '٣', '\U00011F50', '\U0001E4F0', '๑๒', '¹', '²', '½', 'Ⅻ', '1e3', '1.0', '1 2',
             '12\x0034', '\x00', 'abcé', 'é', '1é', '0b1é', 'x' * 250, '1' * 4300, '1' * 4301, '1_' * 4300 + '1', 'f' * 5000,
             '9' * 4301 + 'x', '0' * 4301, '-' + '1' * 4301, '07', '08', '0_7', '00x1', '0x0x1', '0b0b1', '0o0o1', '0b11_', '0o_', '١٠']
BASES = [0, 2, 8, 10, 16, 36]

FLOAT_TEXTS = ['0', '1', '1.5', '-1.5', '+1.5', '.5', '5.', '.', '', ' ', '1e5', '1E5', '1e+5', '1e-5', '1e', '1e+', 'e5', '.e5', '1.e5',
               '1_0', '1__0', '_1', '1_', '1_.5', '1._5', '1.5_', '1e1_0', '1e_10', '1_e10', 'inf', 'Inf', 'INF', 'infinity', 'Infinity',
               '-inf', '+inf', 'nan', 'NaN', '-nan', '+nan', 'in', 'infinit', 'infinityy', 'nana', ' 1.5 ', '\t1\n', '　1　',
               '\x1c1', '١.٥', '０.５', '𝟙e𝟚', '1e400', '-1e400', '1e-400', '0x1p3', '0x10', '1,5', '1 5', '++1', '+-1', '--1',
               '1.5.5', '١', 'é', '1é', ' 1', '\U00011F51', 'x' * 300, '.inf', '.nan', '1e3_0', '0_0.0_0', '1.0e-0_1', 'i_nf']

out = {'lower': [[s, s.lower()] for s in LOWER],
       'strip': [[s, s.strip()] for s in STRIP],
       'int': [], 'float': []}
for text in INT_TEXTS:
    for base in BASES:
        try:
            out['int'].append([text, base, hex(int(text, base)), None])
        except ValueError as exc:
            out['int'].append([text, base, None, str(exc)])
for text in FLOAT_TEXTS:
    try:
        out['float'].append([text, repr(float(text)), None])
    except ValueError as exc:
        out['float'].append([text, None, str(exc)])
Path(__file__).with_name('ref.json').write_text(json.dumps(out, ensure_ascii=True, indent=0), encoding='ascii')
