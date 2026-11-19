package metrics

// Frozen Python references keep their old helper name only in test code.
func pyLower(value string) string { return lowerTrafficDomain(value) }

// The frozen rollback oracle uses its historical private helper names.
func (c *trafficConn) rollback() error { return c.rollbackContext(bg) }
