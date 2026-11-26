// Package mihomoenv installs the typed Mihomo collaborators without owning
// subscription, rule, provider or delivery business logic.
package mihomoenv

import (
	"testing"

	"configflow/internal/convert"
	"configflow/internal/provider"
	"configflow/internal/repo"
	"configflow/internal/substore"
)

// WireWith registers the typed MihomoEnvironment built from r, sub, and pv once.
func WireWith(r *repo.Repository, sub *substore.Service, pv *provider.Service) error {
	return convert.SetMihomoEnvironment(NativeEnvFor(r, sub, pv))
}

// OverrideForTest installs NativeEnvFor(r, sub, pv) for the duration of tb.
func OverrideForTest(tb testing.TB, r *repo.Repository, sub *substore.Service, pv *provider.Service) {
	tb.Helper()
	if sub == nil {
		sub = substore.NewService(substore.NewEnvFor(r))
	}
	tb.Cleanup(convert.OverrideMihomoEnvironmentForTest(tb, NativeEnvFor(r, sub, pv)))
}
