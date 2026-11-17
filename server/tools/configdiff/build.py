"""Build production servers and a separate public route-only probe.

The proddiff tag changes its waiting policy, so its process must never serve
configdiff requests. No Go business source is opened, copied or overlaid.
"""
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
from functools import lru_cache


def route_manifest(binary):
    """Validate the untagged binary and the matching exported route sidecars."""
    binary = Path(binary).resolve()
    metadata = json.loads(Path(str(binary) + '.build.json').read_text())
    manifest = Path(str(binary) + '.routes.tsv').read_bytes()
    if (metadata.get('build_tags') != [] or metadata.get('manifest_build_tags') != ['configflow_proddiff']
            or metadata.get('binary_sha256') != hashlib.sha256(binary.read_bytes()).hexdigest()
            or metadata.get('manifest_sha256') != hashlib.sha256(manifest).hexdigest()):
        raise ValueError('configdiff requires its verified untagged production binary and matching route sidecars')
    rows = [line.split('\t') for line in manifest.decode().splitlines()]
    if not rows or any(len(row) != 2 for row in rows) or ['GET', '/api/auth/status'] not in rows:
        raise ValueError('public route probe did not export a complete usable manifest')
    return {(method, path) for method, path in rows}


@lru_cache(maxsize=8)
def _verified_manifest(binary, identities):
    return frozenset(route_manifest(binary))


def manifest_for_server(binary):
    """Reuse verified immutable build data, invalidating on any sidecar change."""
    binary = str(Path(binary).resolve())
    identities = tuple((path.stat().st_mtime_ns, path.stat().st_size, path.stat().st_ino)
        for path in (Path(binary), Path(binary + '.build.json'), Path(binary + '.routes.tsv')))
    return _verified_manifest(binary, identities)


def export_routes(root, tagged):
    """Run one isolated route-export process, without test HTTP traffic."""
    from harness import Pair, Recorder, PYTHON, free_port
    probe = Pair(tagged, Recorder())
    seed = probe.root / 'seed'
    seed.mkdir()
    port = free_port()
    probe.port_leases.append(port)
    process = None
    try:
        subprocess.run([PYTHON, str(root / 'server/tools/configdiff/seed.py'), str(seed), '{}'],
            cwd=root, env=probe.environment(seed, 0), check=True, capture_output=True)
        with (probe.root / 'route-probe.log').open('wb') as output:
            process = subprocess.Popen([str(tagged)], cwd=root, env=probe.environment(seed, port),
                stdout=output, stderr=output, start_new_session=True)
            deadline = time.monotonic() + 60
            manifest = probe.root / 'go-routes.tsv'
            while not manifest.exists():
                if process.poll() is not None:
                    raise RuntimeError('public route probe exited: ' + (probe.root / 'route-probe.log').read_text()[-3000:])
                if time.monotonic() >= deadline:
                    raise RuntimeError('public route probe failed to export its manifest in 60 seconds')
                time.sleep(.05)
            return manifest.read_bytes()
    finally:
        if process is not None and process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
        probe.close()
        probe.temp.cleanup()


def build(root, output):
    root, output = Path(root).resolve(), Path(output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    tagged, binary = output / 'configflow-route-probe', output / 'configflow'
    subprocess.run(['go', 'build', '-tags=configflow_proddiff', '-o', str(tagged), './cmd/configflow'],
        cwd=root / 'server', check=True)
    manifest = export_routes(root, tagged)
    subprocess.run(['go', 'build', '-o', str(binary), './cmd/configflow'], cwd=root / 'server', check=True)
    Path(str(binary) + '.routes.tsv').write_bytes(manifest)
    Path(str(binary) + '.build.json').write_text(json.dumps({'build_tags': [],
        'manifest_build_tags': ['configflow_proddiff'], 'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
        'manifest_sha256': hashlib.sha256(manifest).hexdigest()}))
    route_manifest(binary)
    return binary


if __name__ == '__main__':
    print(build(*sys.argv[1:]))
