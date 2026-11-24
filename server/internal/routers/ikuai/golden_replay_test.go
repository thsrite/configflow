package ikuai_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/routerfake"
	"configflow/internal/testgolden"
)

func goldenMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestIkuaiGoldenReplay records and replays deterministic Go-native boundary
// outputs for R05 (iKuai static route apply + undo).
func TestIkuaiGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	t.Run("R05_ikuai_static_roundtrip", func(t *testing.T) {
		f := routerfake.NewIkuai(routerfake.IkuaiState{})
		srv := httptest.NewServer(f)
		defer srv.Close()
		d := ikuai.NewClient(srv.URL, routerfake.IkuaiUser, routerfake.IkuaiPassword, false)
		defer d.Close()

		addOp := goldenMarshal(t, map[string]any{
			"kind":    "route_add",
			"cidr":    "198.51.100.0/24",
			"gateway": "192.0.2.4",
			"comment": "cf-r05-golden",
		})
		undo, err := d.Apply(context.Background(), addOp)
		if err != nil {
			t.Fatal(err)
		}
		afterAdd := f.Snapshot().Tables["static_rt"]
		if _, err := d.Apply(context.Background(), undo); err != nil {
			t.Fatal(err)
		}
		afterUndo := f.Snapshot().Tables["static_rt"]
		ref.Assert(t, []byte(`{"module":"R05","case":"static_roundtrip"}`), goldenMarshal(t, map[string]any{
			"after_add":  afterAdd,
			"after_undo": afterUndo,
		}))
	})
}
