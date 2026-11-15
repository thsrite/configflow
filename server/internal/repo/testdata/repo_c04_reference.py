#!/usr/bin/env python3
"""Python reference values for the C04 review (see review_c04_test.go).

    CONFIGFLOW_REPO=<repo root> repo_c04_reference.py OUT
"""

import json
import os
import random
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, os.environ['CONFIGFLOW_REPO'])

from backend.common.config_repository import ProfileRepository  # noqa: E402


def read_json_cases():
    """_read_json of files with these bytes: (hex, outcome)."""
    interesting = [0x00, 0x0a, 0x0d, 0x22, 0x31, 0x7b, 0x7d, 0x7f, 0x80, 0x8f, 0x90, 0x9f, 0xa0, 0xbf, 0xc0,
                   0xc1, 0xc2, 0xdf, 0xe0, 0xe1, 0xec, 0xed, 0xee, 0xef, 0xf0, 0xf1, 0xf3, 0xf4, 0xf5, 0xff]
    rng = random.Random(4)
    samples = [b'', b'{}', b'\xef\xbb\xbf{}', b'{\r\n"a": }', b'{\r"a":\r\n}', b'[1]', b'null', b'{"a": NaN}',
               b'{"a":1,"a":2}', b'{"\xe4\xb8\xad": "\xf0\x9f\x9a\x80"}', b'{"a": "\r"}', b'\r\n\r\n{', b'{"a": 1}\r\nx']
    for _ in range(600):
        length = rng.randint(1, 7)
        prefix = b'{"k": "' if rng.random() < 0.5 else b''
        samples.append(prefix + bytes(rng.choice(interesting) for _ in range(length)))
    result = []
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / 'case.json'
        for data in samples:
            path.write_bytes(data)
            try:
                value = ProfileRepository._read_json(None, path)
                outcome = {'value': value}
            except Exception as error:  # class and message are compared
                outcome = {'class': type(error).__name__, 'message': str(error)}
            result.append({'hex': data.hex(), 'outcome': outcome})
    return result


def main():
    result = {'read_json': read_json_cases()}
    Path(sys.argv[1]).write_text(json.dumps(result, ensure_ascii=False, indent=1) + '\n', encoding='utf-8')


if __name__ == '__main__':
    main()
