"""Compile four purpose-broken variants; each boundary comparison must fail.

Run on a test server from the repository root. Sources are restored in finally.
"""
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parents[4]
server = root / 'server'
variants = [
    ('result', 'internal/routers/dns_guard_plan.go', 'p.Status = "pending"', 'p.Status = "synced"', 'first_enable_synced'),
    ('journal', 'internal/routers/dns_guard_write.go', 'slices.Reverse(undo)', 'undo = []ROSChange{}', 'first_enable_synced'),
    ('request', 'internal/routers/dns_guard_spec.go', '"dst-port": "53"', '"dst-port": "54"', 'first_enable_synced'),
    # Deliberately corrupt the firmware table after accepting a correct PUT.
    ('table', 'internal/routers/routerfake/fake.go',
     'status, result, closeReply := f.serveLocked(r.Method, path, query, body, fault)',
     'status, result, closeReply := f.serveLocked(r.Method, path, query, body, fault)\n\tif r.Method == "PUT" && path == "/ip/firewall/address-list" && r.UserAgent() == "ConfigFlow" && status == 201 { f.tables[path][len(f.tables[path])-1]["address"] = "203.0.113.99/32" }', 'first_enable_synced'),
]
for name, path, old, new, case in variants:
    file = server / path
    original = file.read_text()
    assert old in original, (name, 'mutation target disappeared')
    try:
        file.write_text(original.replace(old, new, 1))
        run = subprocess.run(['go', 'test', '-count=1', '-run', '^TestR09PythonBoundary/' + case + '$', './internal/routers'], cwd=server, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=120)
        if run.returncode == 0 or 'Python/Go differ' not in run.stdout:
            raise RuntimeError(name + ' did not fail the boundary comparison\n' + run.stdout[-6000:])
        print('MUTATION_DETECTED ' + name, flush=True)
    finally:
        file.write_text(original)
