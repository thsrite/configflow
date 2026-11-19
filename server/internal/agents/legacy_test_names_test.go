package agents

// The frozen reference keeps its historical helper name. Production runtime
// ownership uses the single repository resolver, with no private path walker.
func pyResolve(path string) string { return runtimeDirectory(path) }
