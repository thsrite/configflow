//go:build configflow_proddiff

package main

import (
	"fmt"
	"os"
	"strings"

	"configflow/internal/httpx"
)

func writeTestManifest(app *httpx.App) error {
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
