package gateway

import "configflow/internal/repo"

// WithTestRuleReader binds r and reader onto c for tests.
func (c *Config) WithTestRuleReader(r *repo.Repository, reader RuleSourceReader) *Config {
	c.repo = r
	c.ruleSourceReader = reader
	return c
}

// SetAuthEnabled replaces the panel login check for a test and returns a
// function that restores it. Tests that use it must not run in parallel.
func SetAuthEnabled(enabled bool) (restore func()) {
	previous := isAuthEnabled
	isAuthEnabled = func() bool { return enabled }
	return func() { isAuthEnabled = previous }
}

// BaseYAMLInvalidMessage is the original base-reader context for external tests.
const BaseYAMLInvalidMessage = msgBaseYAMLInvalid
