package gateway

// SetAuthEnabled replaces the panel login check for a test and returns a
// function that restores it. Tests that use it must not run in parallel.
func SetAuthEnabled(enabled bool) (restore func()) {
	previous := isAuthEnabled
	isAuthEnabled = func() bool { return enabled }
	return func() { isAuthEnabled = previous }
}

// BaseYAMLInvalidMessage is the original base-reader context for external tests.
const BaseYAMLInvalidMessage = msgBaseYAMLInvalid
