// Package mihomoenv installs the typed Mihomo collaborators without owning
// subscription, rule, provider or delivery business logic.
package mihomoenv

import (
	"errors"

	"configflow/internal/convert"
	"configflow/internal/provider"
	"configflow/internal/substore"
)

// Wire registers every production callback used by typed generation once.
func Wire() {
	if err := WireWith(nil, nil); err != nil && !errors.Is(err, convert.ErrMihomoEnvironmentAlreadySet) {
		panic(err)
	}
}

// WireWith registers the typed MihomoEnvironment built from sub and pv once.
func WireWith(sub *substore.Service, pv *provider.Service) error {
	return convert.SetMihomoEnvironment(NativeEnvFor(sub, pv))
}
