package gateway

import "gopkg.in/yaml.v3"

// SetAuthEnabled replaces the panel login check for a test and returns a
// function that restores it. Tests that use it must not run in parallel.
func SetAuthEnabled(enabled bool) (restore func()) {
	previous := isAuthEnabled
	isAuthEnabled = func() bool { return enabled }
	return func() { isAuthEnabled = previous }
}

// JSONXNode is the node the jsonx boundary makes of a loaded value.
var JSONXNode = func(v any) *yaml.Node { return (&codec{}).node(v) }
