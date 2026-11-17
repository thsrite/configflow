"""Build the real command with ephemeral, test-only lifecycle/manifest overlays."""
import json
import pathlib
import sys

# One-shot startup work that Python performs synchronously at import (initialize_app).
STARTUP_ONCE = ('subscription-cache-prune',)

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
        allowed = ' || '.join(f'l.name == {json.dumps(name)}' for name in STARTUP_ONCE)
        text = text[:start] + f'''
 // proddiff: never launch registered background loops; run Python's synchronous
 // startup work once, in the caller, before the ready marker is written.
 mu.Lock()
 var once []loop
 for _, l := range loops {{
  if {allowed} {{
   once = append(once, l)
  }}
 }}
 mu.Unlock()
 for _, l := range once {{
  l.run(ctx)
 }}
}}''' + text[end:]
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
''')
        marker = '\tlifecycle.Start(ctx)\n'
        assert text.count(marker) == 1
        # Loops are listed after Start: lazily wired loops are registered by then.
        text = text.replace(marker, marker + '''
 if err := os.WriteFile(os.Getenv("PRODDIFF_ROUTES")+".loops", []byte(strings.Join(lifecycle.Names(), "\\n")), 0600); err != nil { return err }
 if err := os.WriteFile(os.Getenv("PRODDIFF_ROUTES")+".ready", nil, 0600); err != nil { return err }
''')
    text = '//go:build configflow_proddiff\n\n' + text
    destination = out / source.name
    destination.write_text(text)
    replacements[str(source)] = str(destination)
(out / 'overlay.json').write_text(json.dumps({'Replace': replacements}))
