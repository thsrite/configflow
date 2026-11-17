"""Compile three deliberately broken Go implementations; boundary tests must fail."""
import json
from pathlib import Path
import subprocess
import tempfile

root = Path(__file__).resolve().parents[3]
source = root / 'internal/routers/routeros_routes.go'
text = source.read_text()
mutations = {
    'result': ('&RouteMutation{Changed: true, Entry: entry}', '&RouteMutation{Changed: false, Entry: entry}', 'TestR06PythonBoundary/routeros/add$'),
    'request': ('"gateway": g,', '"gateway": g + "x",', 'TestR06PythonBoundary/routeros/add$'),
    'ownership': ('if !editROSSame(current, after, false) {', 'if false {', 'TestR06PythonBoundary/routeros/add/foreign_after_write$'),
}
for name, (old, new, target) in mutations.items():
    if old not in text:
        raise SystemExit(f'{name}: mutation target missing')
    with tempfile.TemporaryDirectory(prefix='r06-mutation-') as directory:
        directory = Path(directory)
        changed = directory / 'routeros_routes.go'
        changed.write_text(text.replace(old, new, 1))
        overlay = directory / 'overlay.json'
        overlay.write_text(json.dumps({'Replace': {str(source): str(changed)}}))
        result = subprocess.run(['go', 'test', '-count=1', '-overlay', str(overlay), '-run', target, './internal/routers/'], cwd=root, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        if result.returncode != 1 or 'mismatch' not in result.stdout or 'build failed' in result.stdout:
            raise SystemExit(f'{name}: mutation was not caught at boundary\n{result.stdout}')
        print(f'{name}: DETECTED (compiled, real Python boundary mismatch)')
