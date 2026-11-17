"""Build the real public Go command, adding only a route-manifest observer.

The overlay replaces the public entry's no-op manifest hook. No business file or
lifecycle file is read or changed; normal startup and background loops remain.
"""
import argparse
import json
from pathlib import Path
import subprocess

from inventory import ROOT

OBSERVER = '''//go:build !configflow_proddiff

package main

import (
    "fmt"
    "os"
    "strings"
    "configflow/internal/httpx"
)

func writeTestManifest(app *httpx.App) error {
    var output strings.Builder
    for _, route := range app.Router.Routes() {
        for _, method := range route.Methods {
            fmt.Fprintf(&output, "%s\\t%s\\n", method, route.Path)
        }
    }
    return os.WriteFile(os.Getenv("ROUTERDIFF_MANIFEST"), []byte(output.String()), 0600)
}
'''


def build(directory, parallelism=4):
    if not 1 <= parallelism <= 64:
        raise ValueError('build parallelism must be between 1 and 64')
    directory = Path(directory).resolve()
    directory.mkdir(parents=True, exist_ok=True)
    original = ROOT / 'server/cmd/configflow/manifest_default.go'
    # Fail rather than instrument an implementation or silently change startup.
    if 'func writeTestManifest(*httpx.App) error { return nil }' not in original.read_text():
        raise RuntimeError('public manifest hook changed; review the test observer')
    observer = directory / 'manifest.go'
    observer.write_text(OBSERVER)
    overlay = directory / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {str(original): str(observer)}}))
    binary = directory / 'configflow'
    subprocess.run(['go', 'build', '-p', str(parallelism), '-overlay', str(overlay), '-o', str(binary), './cmd/configflow'],
                   cwd=ROOT / 'server', check=True)
    return binary


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('output', type=Path)
    parser.add_argument('--parallel', type=int, default=4)
    args = parser.parse_args()
    print(build(args.output, args.parallel))
