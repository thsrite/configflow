"""Regenerate only the coordinator-approved L1 D3 source-delta fixture.

The Python scripts remain untouched historical reference inputs. Run from any
directory; this tool reads/writes source artifacts and does not execute scripts.
"""
import difflib
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parents[4]
scripts = root / 'server/internal/installer/scripts'
module = (scripts / 'agent-install-rollback.sh').read_text()
deltas = []
for name in ['install-go.sh', 'install-go-aio.sh']:
    old = (root / 'backend/agents/scripts' / name).read_text().splitlines(True)
    new = (scripts / name).read_text().splitlines(True)
    for tag, i1, i2, j1, j2 in difflib.SequenceMatcher(None, old, new, autojunk=False).get_opcodes():
        if tag == 'equal':
            continue
        before, after = ''.join(old[i1:i2]), ''.join(new[j1:j2])
        marker = '{core_cleanup_message}'
        if before.startswith(marker) and after.startswith(marker):
            before, after = before[len(marker):], after[len(marker):]
        if after == '        fi\n' and before == '':
            # Anchor the added cleanup branch closer to its unique service
            # restart loop; never remove a generic shell "fi" globally.
            before, after = ''.join(old[i1-4:i2+1]), ''.join(new[j1-4:j2+1])
        assert after and after.strip()
        for text in [before, after.replace('{agent_install_rollback}', module)]:
            assert not re.search(r'(?<!\$)\{[a-z_]+\}', text), text
        deltas.append(dict(file=name, before=before, after=after))

Path(__file__).with_name('l1-rollback-deltas.json').write_text(json.dumps(
    dict(decision='L1 D3: restore old installation on failure', module=module, deltas=deltas),
    ensure_ascii=False, indent=2) + '\n')
