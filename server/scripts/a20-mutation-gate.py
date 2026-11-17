#!/usr/bin/env python3
"""Purpose-break A20 boundary guarantees; run only through rt on a test server."""
from pathlib import Path
import os
import subprocess

root = Path(__file__).resolve().parents[1]
mutants = [
    ('response', 'internal/gateway/host/host.go', 'return result, nil', 'put(result, "purpose_broken", true)\nreturn result, nil', './internal/api/linuxgateway/difftest', '^TestA20HostFlask$'),
    ('storage', 'internal/gateway/draft/adapter.go', '[]string{"mihomo", "oxidns"}', '[]string{"mihomo"}', './internal/api/linuxgateway/difftest', '^TestA20FlaskHTTPAndStorage$'),
    ('wire-timeout', 'internal/gateway/host/host.go', 'agents.Pair(3, 45)', 'agents.Pair(3, 44)', './internal/gateway/host', '^TestAgentRawWireAndProductionBounds$'),
    ('forwarding-proof', 'internal/gateway/host/host.go', 'else if recordKept(a, status)', 'else if true', './internal/api/linuxgateway/difftest', '^TestA20SplitHTTPGuards$'),
    ('preview-input', 'internal/gateway/draft/draft.go', 'subtle.ConstantTimeCompare([]byte(token), []byte(p.PreviewToken)) != 1', 'subtle.ConstantTimeCompare([]byte(token), []byte(token)) != 1', './internal/gateway/draft', '^TestDraftGuardAtomicityAndUnknownMembers$'),
    ('command-whitelist', 'internal/gateway/host/filter.go', 'if !commandPattern.MatchString(step)', 'if commandPattern.MatchString(step) && false', './internal/api/linuxgateway/difftest', '^TestA20HostFlask$'),
    ('credential-knowledge', 'internal/gateway/draft/draft.go', 'sourceCredentials(config, candidate)', 'sourceCredentials(document{}, document{})', './internal/gateway/draft', '^TestDraftPreviewCollectsCredentialsBeforeProjection$'),
]
passed = 0
for name, relative, before, after, package, test in mutants:
    path = root / relative
    original = path.read_text()
    if before not in original:
        raise SystemExit(f'{name}: mutation target missing')
    try:
        path.write_text(original.replace(before, after, 1))
        env = {**os.environ, 'CONFIGFLOW_DIFF': '1'}
        result = subprocess.run(['go', 'test', '-count=1', '-timeout=5m', '-run', test, package], cwd=root, env=env, capture_output=True, text=True, timeout=360)
        output = result.stdout + result.stderr
        if result.returncode == 0 or '--- FAIL:' not in output or 'build failed' in output:
            print(output[-3000:])
            raise SystemExit(f'{name}: not detected by a running test')
        print(f'{name}: DETECTED', flush=True)
        passed += 1
    finally:
        path.write_text(original)
print(f'A20 mutation gate: {passed}/{len(mutants)} detected; sources restored')
