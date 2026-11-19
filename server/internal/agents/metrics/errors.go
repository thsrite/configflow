package metrics

// AttributeError reports an invalid stored object shape.
type AttributeError struct{ Message string }

func (e *AttributeError) Error() string { return e.Message }
