"""Build the real command with ephemeral, test-only lifecycle/manifest overlays."""
import json
import pathlib
import sys

root, out = map(pathlib.Path, sys.argv[1:])
replacements = {}
for relative in ('internal/lifecycle/lifecycle.go', 'cmd/configflow/main.go'):
    source = root / 'server' / relative
    text = source.read_text()
    if relative.startswith('internal/'):
        marker = 'func Start(ctx context.Context) {'
        assert text.count(marker) == 1
        start = text.index(marker) + len(marker)
        end, depth = start, 1
        while depth:
            depth += (text[end] == '{') - (text[end] == '}')
            end += 1
        text = text[:start] + '\n // proddiff: never launch registered background loops\n}' + text[end:]
    else:
        marker = '\tapp.RegisterLocal()'
        assert text.count(marker) == 1
        text = text.replace(marker, marker + '''
 var manifest strings.Builder
 for _, route := range app.Router.Routes() {
  for _, method := range route.Methods {
   fmt.Fprintf(&manifest, "%s\\t%s\\n", method, route.Path)
  }
 }
 if err := os.WriteFile(os.Getenv("PRODDIFF_ROUTES"), []byte(manifest.String()), 0600); err != nil { return err }
 if err := os.WriteFile(os.Getenv("PRODDIFF_ROUTES")+".loops", []byte(strings.Join(lifecycle.Names(), "\\n")), 0600); err != nil { return err }
''')
    text = '//go:build configflow_proddiff\n\n' + text
    destination = out / source.name
    destination.write_text(text)
    replacements[str(source)] = str(destination)
(out / 'overlay.json').write_text(json.dumps({'Replace': replacements}))
