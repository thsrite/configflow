package refresh

// SetRefreshEnv installs native collaborators on the default service and
// returns a restoration function.
func SetRefreshEnv(value *RefreshEnv) func() { return Default().SetEnv(value) }

func refreshEnv() *RefreshEnv { return Default().currentEnv() }
