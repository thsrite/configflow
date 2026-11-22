package repo

import (
	"encoding/json"
	"testing"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestO10ProfileListingSkipsAgentHistory: listing profiles reads each Agent's
// binding members only; its cost does not grow with deployment history. The
// encoded-registry read it replaced is the negative control.
func TestO10ProfileListingSkipsAgentHistory(t *testing.T) {
	small, large := o10StoreSystem(t, 1), o10StoreSystem(t, 500)
	small.SetViewGuard(nil)
	large.SetViewGuard(nil)
	list := func(r *Repository) func() error {
		return func() error { _, err := r.ListProfileRecords(bg); return err }
	}
	smallBytes, largeBytes := o10ReadBytes(t, list(small)), o10ReadBytes(t, list(large))
	if largeBytes > smallBytes+64*1024 {
		t.Fatalf("ListProfileRecords allocates %d bytes with 500 deployments (%d with 1)", largeBytes, smallBytes)
	}
}
