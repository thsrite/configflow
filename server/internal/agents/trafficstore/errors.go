package trafficstore

// TypeError reports invalid report or stored member types.
type TypeError struct{ Message string }

func (e *TypeError) Error() string { return e.Message }

// AttributeError is retained for callers classifying historical stored-data errors.
type AttributeError struct{ Message string }

func (e *AttributeError) Error() string { return e.Message }

// KeyError reports a required stored member that is absent.
type KeyError struct{ Key string }

func (e *KeyError) Error() string { return "'" + e.Key + "'" }
