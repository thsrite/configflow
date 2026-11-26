package installer

// Script returns an embedded template by file name (for tests and routes).
func Script(name string) (string, bool) {
	data, err := scriptFiles.ReadFile("scripts/" + name)
	return string(data), err == nil
}
