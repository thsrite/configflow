//go:build !configflow_difftest && !configflow_proddiff

package main

import (
	"configflow/internal/server"
	"strings"
	"testing"
)

func TestProductionHasNoFaultRoutes(t *testing.T) {
	t.Setenv("CONFIGFLOW_FAULT_DIR", t.TempDir())
	app := server.NewApp()
	if err := writeTestManifest(app); err != nil {
		t.Fatal(err)
	}
	for _, route := range app.Router.Routes() {
		if strings.HasPrefix(route.Path, "/__faults/") {
			t.Fatal("production registered a fault route", route.Path)
		}
	}
}
