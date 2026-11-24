//go:build configflow_proddiff

package main

import (
	"fmt"
	"os"
	"strings"

	apiagents "configflow/internal/api/agents"
	"configflow/internal/httpx"
	"configflow/internal/provider"
)

func writeTestManifest(app *httpx.App, pv *provider.Service) error {
	// Compare completed outputs, not whichever old snapshot a loaded worker
	// happened to serve after its 12-second client deadline. Flask's launcher
	// selects the same wait policy; production binaries retain the deadline.
	if pv != nil {
		pv.SetResponseSeconds(-1)
	}
	apiagents.InstallProddiffContentObserver(app)
	var manifest strings.Builder
	for _, route := range app.Router.Routes() {
		for _, method := range route.Methods {
			fmt.Fprintf(&manifest, "%s\t%s\n", method, route.Path)
		}
	}
	if err := os.WriteFile(os.Getenv("PRODDIFF_ROUTES"), []byte(manifest.String()), 0o600); err != nil {
		return fmt.Errorf("write test route manifest: %w", err)
	}
	return nil
}
