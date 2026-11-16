"""Build the public Go command; overlay only adds a registered-route manifest.

Business implementations and lifecycle sources are neither opened nor patched.
Both production servers retain their real startup/background behavior (X08).
"""
import json
import pathlib
import subprocess
import sys


def build(root, output):
    root, output = pathlib.Path(root).resolve(), pathlib.Path(output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    original = root / 'server/cmd/configflow/main.go'
    source = original.read_text()
    marker = '\tapp.RegisterLocal()'
    if source.count(marker) != 1:
        raise RuntimeError('public command manifest insertion point changed')
    source = source.replace(marker, marker + '''
 var configDiffRoutes strings.Builder
 for _, route := range app.Router.Routes() {
  for _, method := range route.Methods {
   fmt.Fprintf(&configDiffRoutes, "%s\\t%s\\n", method, route.Path)
  }
 }
 if err := os.WriteFile(os.Getenv("CONFIGDIFF_ROUTES"), []byte(configDiffRoutes.String()), 0600); err != nil { return err }
''')
    overlay_main = output / 'main.go'
    overlay_main.write_text('//go:build configflow_configdiff\n\n' + source)
    overlay = output / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {str(original): str(overlay_main)}}))
    subprocess.run(['go', 'build', '-tags=configflow_configdiff', '-overlay', str(overlay),
                    '-o', str(output / 'configflow'), './cmd/configflow'], cwd=root / 'server', check=True)
    return output / 'configflow'


if __name__ == '__main__':
    print(build(*sys.argv[1:]))
