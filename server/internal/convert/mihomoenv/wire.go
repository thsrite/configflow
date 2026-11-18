// Package mihomoenv installs the typed Mihomo collaborators without owning
// subscription, rule, provider or delivery business logic.
package mihomoenv

import "configflow/internal/convert"

// Wire registers every production callback used by typed generation.
func Wire() { convert.SetMihomoEnvironment(NativeEnv()) }
