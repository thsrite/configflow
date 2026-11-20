package refresh

import "sync"

var nativeEnvMu sync.RWMutex
var nativeEnv *RefreshEnv

// SetRefreshEnv installs native collaborators and returns a restoration function.
func SetRefreshEnv(value *RefreshEnv) func() {
	nativeEnvMu.Lock()
	old := nativeEnv
	nativeEnv = value
	nativeEnvMu.Unlock()
	return func() { nativeEnvMu.Lock(); nativeEnv = old; nativeEnvMu.Unlock() }
}
func refreshEnv() *RefreshEnv {
	nativeEnvMu.RLock()
	value := nativeEnv
	nativeEnvMu.RUnlock()
	if value != nil {
		return value
	}
	return DefaultRefreshEnv()
}
