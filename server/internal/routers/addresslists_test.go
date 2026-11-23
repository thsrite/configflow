package routers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

func TestAddressListBoundary(t *testing.T) {
	home, _ := os.UserHomeDir()
	python := filepath.Join(home, "cf-test/.venv/bin/python")
	if _, err := os.Stat(python); err != nil {
		t.Skip("Python reference unavailable")
	}
	working, _ := os.Getwd()
	root := filepath.Clean(filepath.Join(working, "../../.."))
	helper := filepath.Join(working, "testdata/r07_reference.py")
	f := routerfake.New(nil)
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, count := range []int{0, 1, 499, 500, 501, 2000, 2001, 4500} {
		for _, action := range []string{"digest", "fetch", "reuse"} {
			t.Run(fmt.Sprintf("%s/%d", action, count), func(t *testing.T) {
				name := "L\"$\\\n中文"
				rows := []routerfake.Row{}
				for i := 0; i < count; i++ {
					rows = append(rows, routerfake.Row{".id": fmt.Sprintf("*%X", i+1), "list": name, "address": fmt.Sprintf("198.51.%d.%d", i/256%256, i%256), "comment": "c", "disabled": "false", "creation-time": "synthetic"})
				}
				tc := boundaryCase{action: action, state: routerfake.State{Tables: map[string][]routerfake.Row{"/ip/firewall/address-list": rows}}, op: map[string]any{"name": name}}
				py, pyState := runPythonBoundary(t, python, root, helper, srv.URL, f, tc)
				f.Reset(tc.state)
				d := routers.NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
				defer d.Close()
				var result any
				var err error
				switch action {
				case "digest":
					converted := []routers.Row{}
					for _, row := range rows {
						// As the driver decodes the device reply.
						data, _ := json.Marshal(row)
						var decoded routers.Row
						_ = json.Unmarshal(data, &decoded)
						converted = append(converted, decoded)
					}
					v := routers.DigestAddresses(converted)
					result = []any{v.Count, v.MD5}
				case "fetch":
					result, err = d.FetchAddresses(context.Background(), name)
				case "reuse":
					done := d.ReuseAddressReads()
					defer done()
					first, e := d.ReadAddresses(context.Background(), []string{name})
					err = e
					if e == nil {
						d.ExpectAddresses(name, first[name])
						result, err = d.ReadAddresses(context.Background(), []string{name})
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(r07Comparable(t, py.Result), r07Comparable(t, result)) {
					t.Fatalf("result %s", jsonDiff(r07Comparable(t, py.Result), r07Comparable(t, result)))
				}
				if !reflect.DeepEqual(r07Comparable(t, pyState.Calls), r07Comparable(t, f.Calls())) {
					t.Fatalf("calls %s", jsonDiff(r07Comparable(t, pyState.Calls), r07Comparable(t, f.Calls())))
				}
			})
		}
	}
}

func TestAddressDigestMutationIsDetected(t *testing.T) {
	rows := []routers.Row{{".id": "*1", "list": "L", "comment": "中文;x=1"}}
	digest := routers.DigestAddresses(rows)
	rows[0]["comment"] = "changed"
	if routers.DigestAddresses(rows) == digest {
		t.Fatal("content mutation escaped proof")
	}
}
