package difftest

import "testing"

// The legacy repository facade used to reject these shapes. The HTTP binder
// must preserve the same rejection when the typed IDs entry point is selected.
func TestTypedDeletionRequestShapes(t *testing.T) {
	w := newWorld(t, seedGraph(t, "none", false, true), nil)
	for _, body := range []string{`{"ids":"n"}`, `{"ids":[1]}`, `{"ids":[null]}`, `{"ids":[" "]}`} {
		result := w.call("POST", "/api/nodes/delete-preview", body, true)
		if result.Status != 400 {
			t.Fatalf("shape %s: status %d", body, result.Status)
		}
	}
	// Resource records are extensible. Profile metadata's name/description and
	// extra-key restrictions (W1 L1) belong to W5's profile binding, not these rows.
	w.call("POST", "/api/subscriptions", `{"id":"typed","name":42,"url":"https://feed.example/s"}`, true)
	w.call("POST", "/api/nodes", `{"id":"typed-node","name":"Typed","future":{"keep":true}}`, true)
}
