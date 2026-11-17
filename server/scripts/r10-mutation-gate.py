#!/usr/bin/env python3
"""Purpose-break R10 boundary results, journals, wire requests and device tables.

Run only on the test server through rt. A compiler failure never counts.
"""
from pathlib import Path
import re
import subprocess
import sys
if sys.platform != 'linux':
    raise SystemExit('Run this gate on the test server through rt.')
server = Path(__file__).resolve().parents[1]
mutations = [
    ('result', 'ipv6_plan.go', r'"missing", messageIPv6FakeMissing', '"synced", messageIPv6FakeMissing'),
    ('journal', 'ipv6_write.go', r'IPv6FakeIPUndo\{"ipv6_fakeip_restore", undo,', 'IPv6FakeIPUndo{"ipv6_fakeip_restore", nil,'),
    ('request', 'ipv6_write.go', r'Values: op.Values\}', 'Values: mergeRows(op.Values, Row{"distance":"2"})}'),
    ('device-table', 'ipv6_boundary_test.go', r'gotState := f.Snapshot\(\)',
     'gotState := f.Snapshot(); gotState.Tables[rosIPv6Routes][0]["gateway"]="fe80::dead%bridge"'),
]
for name, filename, pattern, replacement in mutations:
    path = server / 'internal/routers' / filename
    original = path.read_text()
    changed, count = re.subn(pattern, replacement, original, count=1)
    if count != 1:
        raise SystemExit(f'{name}: mutation anchor missing')
    try:
        path.write_text(changed)
        result = subprocess.run(['go', 'test', '-p', '6', '-count=1', '-run',
            '^TestR10PythonBoundary/fake/publish_idempotent$', './internal/routers'],
            cwd=server, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        detected = any(marker in result.stdout for marker in
                       ('R10 result differs:', 'R10 device tables differ:', 'R10 write sequence differs:'))
        if result.returncode == 0 or not detected or '[build failed]' in result.stdout:
            print(result.stdout[-4000:])
            raise SystemExit(f'{name}: NOT DETECTED')
        print(f'{name}: DETECTED (compiled, real Python boundary mismatch)', flush=True)
    finally:
        path.write_text(original)
print('R10 mutation gate: 4/4 detected; sources restored')
