package routersplit

// PythonClass exists only in tests to annotate native errors for the frozen oracle.
func (e *RouterSplitError) PythonClass() string { return "RouterSplitError" }
func (e *ValidationError) PythonClass() string  { return "ValueError" }
