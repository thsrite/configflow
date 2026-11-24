package testgolden

import "testing"

// Lines replays a committed JSON-lines golden transcript.
type Lines struct {
	reference *Reference
}

// NewLines opens a compressed golden transcript for the current test.
func NewLines(t *testing.T) *Lines {
	t.Helper()
	return &Lines{reference: New(t, Spec{Compress: true})}
}

// Call returns the exact recorded response for input. keyInput, when non-nil,
// substitutes only the transcript key (for an allocated loopback fixture port).
func (l *Lines) Call(t *testing.T, input, keyInput []byte) []byte {
	t.Helper()
	if keyInput == nil {
		keyInput = input
	}
	return l.reference.Call(t, keyInput)
}
