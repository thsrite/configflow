package ros_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"configflow/internal/routers/ros"
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

// TestROSGoldenReplay records and replays deterministic Go-native boundary
// outputs for R04 (RouterOS static routes) and R07 (address-list digest/fetch).
func TestROSGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	// R04: RouterOS static route apply + undo + state snapshot.
	t.Run("R04_routeros_static_roundtrip", func(t *testing.T) {
		f := routerfake.New(nil)
		srv := httptest.NewServer(f)
		defer srv.Close()
		d := ros.NewClient(srv.URL, "synthetic-user", "synthetic-pass", false)
		defer d.Close()

		addOp := goldenMarshal(t, map[string]any{
			"kind":    "route_add",
			"cidr":    "198.51.100.0/24",
			"gateway": "192.0.2.4",
			"comment": "cf-r04-golden",
		})
		undo, err := d.ApplyBase(context.Background(), addOp)
		if err != nil {
			t.Fatal(err)
		}
		afterAdd := f.Snapshot().Tables["/ip/route"]
		if _, err := d.ApplyBase(context.Background(), undo); err != nil {
			t.Fatal(err)
		}
		afterUndo := f.Snapshot().Tables["/ip/route"]
		ref.Assert(t, []byte(`{"module":"R04","case":"static_roundtrip"}`), goldenMarshal(t, map[string]any{
			"after_add":  afterAdd,
			"after_undo": afterUndo,
		}))
	})

	// R07: RouterOS address-list digest & fetch across boundary counts.
	t.Run("R07_address_list_digest_and_fetch", func(t *testing.T) {
		f := routerfake.New(nil)
		srv := httptest.NewServer(f)
		defer srv.Close()
		for _, count := range []int{0, 1, 5, 501} {
			rows := make([]routerfake.Row, 0, count)
			converted := make([]ros.Row, 0, count)
			for i := 0; i < count; i++ {
				row := routerfake.Row{
					".id":           fmt.Sprintf("*%X", i+1),
					"list":          "cf-golden",
					"address":       fmt.Sprintf("198.51.%d.%d", i/256%256, i%256),
					"comment":       "c",
					"disabled":      "false",
					"creation-time": "synthetic",
				}
				rows = append(rows, row)
				var r ros.Row
				_ = json.Unmarshal(goldenMarshal(t, row), &r)
				converted = append(converted, r)
			}
			f.Reset(routerfake.State{Tables: map[string][]routerfake.Row{"/ip/firewall/address-list": rows}})
			d := ros.NewClient(srv.URL, "synthetic-user", "synthetic-pass", false)
			fetched, err := d.FetchAddresses(context.Background(), "cf-golden")
			d.Close()
			if err != nil {
				t.Fatal(err)
			}
			digest := ros.DigestAddresses(converted)
			ref.Assert(t, goldenMarshal(t, map[string]any{"module": "R07", "count": count}), goldenMarshal(t, map[string]any{
				"digest_count":  digest.Count,
				"digest_md5":    digest.MD5,
				"fetched_count": len(fetched),
			}))
		}
	})
}
