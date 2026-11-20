package difftest

import "testing"

// Review N8-11 W3 M1: /delete checks the confirmation before the ids shape,
// so a malformed selection without a confirmation reports the confirmation.
func TestReviewN811W3DeletionMessageOrder(t *testing.T) {
	w := newWorld(t, seedGraph(t, "none", false, true), nil)
	for _, body := range []string{`{"ids":"n"}`, `{"ids":[1]}`, `{}`, `{"ids":"n","confirmation":"` + zeros + `"}`} {
		w.call("POST", "/api/nodes/delete", body, true)
		w.call("POST", "/api/nodes/delete-preview", body, true)
	}
}

const zeros = "0000000000000000000000000000000000000000000000000000000000000000"
