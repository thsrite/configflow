#!/usr/bin/env python3
"""Purpose-break R08 boundary outputs, journal, request and observed device table.

Run through rt from the repository root. Each change is restored in finally;
compiler errors do not count as detection. No devices except loopback fakes.
"""
from pathlib import Path
import re
import subprocess
import sys

if sys.platform != 'linux':
    raise SystemExit('Run this gate on the test server through rt.')
server = Path(__file__).resolve().parents[1]
mutations = [
    ('result', 'rospolicy_plan.go', r'NamingVersion:\s*1', 'NamingVersion: 9'),
    ('journal', 'rospolicy_write.go', r'Steps:\s*journal', 'Steps: nil'),
    ('request', 'rospolicy_plan.go', r'spec.Gateway\s*\+\s*"@main"', 'spec.Gateway + "@other"'),
    ('device-table', 'rospolicy_boundary_test.go', r'gotState\s*:=\s*f.Snapshot\(\)',
     'gotState := f.Snapshot(); gotState.Tables[rosTables][0]["name"] = "deliberate-device-table-mutation"'),
]
for name, filename, pattern, replacement in mutations:
    path = server / 'internal/routers' / filename
    original = path.read_text()
    changed, count = re.subn(pattern, replacement, original, count=1)
    if count != 1:
        raise SystemExit(f'{name}: mutation anchor not found')
    try:
        path.write_text(changed)
        result = subprocess.run(['go', 'test', './internal/routers', '-count=1',
            '-run', '^TestR08PythonBoundary/publish_noop_view$'], cwd=server,
            text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        if result.returncode == 0 or 'differs:' not in result.stdout or '[build failed]' in result.stdout:
            print(result.stdout[-4000:])
            raise SystemExit(f'{name}: NOT DETECTED')
        print(f'{name}: DETECTED (boundary comparison failed)', flush=True)
    finally:
        path.write_text(original)
print('R08 mutation gate: 4/4 detected; sources restored')
