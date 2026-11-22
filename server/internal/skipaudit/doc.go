// Package skipaudit holds the core-gate source audit that forbids tests from
// skipping when an interpreter or external tool is missing (a skip like that
// turns the gate silently green on a host without the tool). Existing skips of
// that kind are listed in allowed.txt; they belong to the Python comparison
// tests that the switch commit deletes. The package has no runtime code.
package skipaudit
