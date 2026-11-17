"""Snapshot the approved L1 HA startup source additions for strict comparisons."""
import ast
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parents[4]
source = (root / 'server/internal/installer/docker_ha_health.go').read_text()
module = (root / 'server/internal/installer/ha-startup-checks.sh').read_text()
parts = {}
for fn, variable in [('haPortCheck', 'image'), ('haNewReadiness', 'name')]:
    text = re.search(r'func ' + fn + r'.*?return (.*?)\n}', source, re.S)[1]
    before, after = text.split(' + q(' + variable + ') + ')
    parts[fn] = ast.literal_eval(before) + '{{' + variable.upper() + '}}' + ast.literal_eval(after)
parts['module'] = module
parts['verification'] = (root / 'server/internal/installer/ha-verify.sh').read_text()
parts['migration'] = ast.literal_eval(re.search(r'const haMigrationReadiness = (.*)', source)[1])
Path(__file__).with_name('l1-ha-deltas.json').write_text(json.dumps(parts, ensure_ascii=False, indent=2) + '\n')
