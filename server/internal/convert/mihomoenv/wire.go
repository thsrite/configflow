// Package mihomoenv installs the typed Mihomo collaborators without owning
// subscription, rule, provider or delivery business logic.
package mihomoenv

import (
	"errors"

	"configflow/internal/convert"
)

// Wire registers every production callback used by typed generation once.
func Wire() {
	if err := convert.SetMihomoEnvironment(NativeEnv()); err != nil && !errors.Is(err, convert.ErrMihomoEnvironmentAlreadySet) {
		panic(err)
	}
}
