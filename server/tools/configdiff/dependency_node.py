"""Real Node syntax checker via stdin, with deterministic diagnostic origin."""
import json
import shutil

from harness import DesignError, PYTHON


def events(pair):
    path = pair.root / 'node-check-events.jsonl'
    return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []


def install(pair):
    binary = shutil.which('node')
    if not binary:
        raise DesignError('real Node is required for dependency syntax-check fixture')
    executable = pair.root / 'tools/node'
    executable.write_text('#!' + PYTHON + '\n' + 'REAL_NODE = ' + repr(binary) + '\n' + '''import hashlib, json, os, subprocess, sys
from pathlib import Path
args = sys.argv[1:]
if args == ['--version']:
    sys.exit(subprocess.run([REAL_NODE, *args], timeout=10).returncode)
root = Path(os.environ['DATA_DIR']).resolve()
valid = len(args) == 2 and args[0] == '--check' and Path(args[1]).resolve() == root / 'sub-store/runtime/sub-store.download.js'
row = {'args': ['--check', 'sub-store/runtime/sub-store.download.js'] if valid else args}
if valid:
    payload = Path(args[1]).read_bytes()
    result = subprocess.run([REAL_NODE, '--check'], input=payload, capture_output=True, timeout=10)
    row.update(stdin_sha256=hashlib.sha256(payload).hexdigest(), returncode=result.returncode)
else:
    result = None
fd = os.open(os.environ['CONFIGDIFF_NODE_LOG'], os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
try:
    os.write(fd, (json.dumps(row) + '\\n').encode())
finally:
    os.close(fd)
if result is None:
    print('synthetic Node fixture refuses unexpected arguments', file=sys.stderr)
    sys.exit(97)
sys.stdout.buffer.write(result.stdout)
sys.stderr.buffer.write(result.stderr)
sys.exit(result.returncode)
''')
    executable.chmod(0o755)
    return binary
