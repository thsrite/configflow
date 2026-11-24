//go:build !configflow_difftest && !configflow_proddiff

package main

import (
	"configflow/internal/repo"
	"configflow/internal/server"
	"strings"
	"testing"
)

func TestProductionHasNoFaultRoutes(t *testing.T) {
	t.Setenv("CONFIGFLOW_FAULT_DIR", t.TempDir())
	r, err := repo.OpenDefaultWithOptions(t.TempDir(), server.RepoOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	srv, err := server.New(t.Context(), server.Options{Repo: r})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Agents.Close)
	app := srv.HTTP
	if err := writeTestManifest(app, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, route := range app.Router.Routes() {
		if strings.HasPrefix(route.Path, "/__faults/") {
			t.Fatal("production registered a fault route", route.Path)
		}
	}
}
