#!/usr/bin/env python3
"""Compile real R11 mutations and require a boundary failure, then restore source."""
from pathlib import Path
import re
import subprocess
import sys
if sys.platform != 'linux':
    raise SystemExit('Run through rt on the test server.')
server = Path(__file__).resolve().parents[1]
cases = [
 ('result', 'ipv6_pd_plan.go', r'p.Status, p.Message = "pending", messagePDInstall',
  'p.Status, p.Message = "synced", messagePDInstall', '^TestR11PythonBoundary/pd/publish_idempotent$'),
 ('journal', 'ipv6_pd_write.go', r'Kind: "ipv6_pd_guard_restore", Changes: undo,',
  'Kind: "ipv6_pd_guard_restore", Changes: nil,', '^TestR11PythonBoundary/pd/rollback$'),
 ('request', 'ipv6_dns.go', r'Values: Row\{"advertise-dns": "no"\}, Expected: marshalRow\(op.Expected\)',
  'Values: Row{"advertise-dns": "yes"}, Expected: marshalRow(op.Expected)', '^TestR11PythonBoundary/dns/publish_idempotent$'),
 ('device-table', 'r11_boundary_test.go', r'(?<![A-Za-z])state := f.Snapshot\(\)',
  'state := f.Snapshot(); state.Tables[pdScripts][0]["source"]="wrong"', '^TestR11PythonBoundary/pd/publish_idempotent$'),
 ('recovery', 'ipv6_pd_write.go', r'if e != nil && c.Method == "DELETE" && d.pdGone',
  'if false && e != nil && c.Method == "DELETE" && d.pdGone', '^TestR11PDLostCompensationDelete$'),
]
for name, filename, pattern, replacement, target in cases:
    path = server / 'internal/routers' / filename
    original = path.read_text()
    changed, count = re.subn(pattern, replacement, original, count=1)
    if count != 1:
        raise SystemExit(f'{name}: anchor missing')
    try:
        path.write_text(changed)
        result = subprocess.run(['go', 'test', '-p', '6', '-count=1', '-run', target, './internal/routers'],
                                cwd=server, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        markers = ('result:', 'tables:', 'writes:', 'lost DELETE reply should be proven absent')
        if result.returncode == 0 or '[build failed]' in result.stdout or not any(x in result.stdout for x in markers):
            print(result.stdout[-3000:])
            raise SystemExit(f'{name}: NOT DETECTED')
        print(f'{name}: DETECTED (compiled boundary)', flush=True)
    finally:
        path.write_text(original)
print('R11 mutation gate: 5/5 detected; sources restored')
