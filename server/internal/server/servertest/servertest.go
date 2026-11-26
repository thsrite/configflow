// Package servertest wires the full application for HTTP boundary tests.
package servertest

import (
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/server"
)

// New creates an HTTP fixture using the production route and service assembly.
func New(t *testing.T, options httpxtest.Options) (*httpxtest.Server, *server.App) {
	t.Helper()
	options.RepoOptions = server.RepoOptions()
	s := httpxtest.New(t, options)
	app, err := server.New(s.Context(), server.Options{Repo: s.Repo})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Agents.Close)
	t.Cleanup(func() { app.Runtime.Stop(5 * time.Second) })
	*s.App = *app.HTTP
	return s, app
}
