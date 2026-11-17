package routers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"

	"configflow/internal/routers/routerfake"
)

func r07NewSync() NATListSync {
	sources := []string{}
	for _, p := range ClientSourceNetworks(netip.MustParseAddr("192.0.2.3")) {
		sources = append(sources, p.String())
	}
	return NATListSync{Kind: "nat_list_sync", Comment: NATListComment, SrcList: NATSrcList, DstList: NATDstList, Sources: sources, Destinations: []string{"198.51.100.0/24"}, SrcAdd: sources, DstAdd: []string{"198.51.100.0/24"}, SrcRemove: []ROSResource{}, DstRemove: []ROSResource{}, Legacy: []ROSResource{}, Stale: []ROSResource{}}
}
func r07FakeState() routerfake.State {
	return routerfake.State{Tables: map[string][]routerfake.Row{rosNAT: {{".id": "*3", "chain": "srcnat", "action": "masquerade", "disabled": "false"}}}}
}

func TestNATListWriteAndRollbackFinishAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := routerfake.New(r07FakeState().Tables)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.ServeHTTP(w, r)
		if r.URL.Path == "/rest/execute" {
			cancel()
		}
	}))
	defer srv.Close()
	d := NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	undo, err := d.ApplyNATLists(ctx, r07NewSync())
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("test did not cancel during the batch")
	}
	if len(f.Snapshot().Tables[rosNAT]) != 2 {
		t.Fatal("accepted update abandoned")
	}
	if err = d.RestoreNATLists(ctx, *undo); err != nil {
		t.Fatal(err)
	}
	state := f.Snapshot()
	if len(state.Tables[rosNAT]) != 1 || len(state.Tables[rosAddresses]) != 0 {
		t.Fatal("cancelled rollback left device changes")
	}
}

func TestAddressDeleteGuardsAllMembersBeforeAnyWrite(t *testing.T) {
	d, f := newFakeRouterOS(t, r07FakeState())
	journal := []NATStep{}
	created, err := d.CreateAddresses(context.Background(), NATEntries(NATDstList, []string{"198.51.100.1/32", "198.51.100.2/32", "198.51.100.3/32"}), &journal, ROSIDs{})
	if err != nil {
		t.Fatal(err)
	}
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{rosAddresses: {created[1].ID: {"comment": "external"}}}}})
	before := f.Snapshot()
	journal = nil
	err = d.DeleteAddresses(context.Background(), created, &journal)
	if err == nil || len(journal) != 0 {
		t.Fatalf("unguarded delete: %v %#v", err, journal)
	}
	if !reflect.DeepEqual(before.Tables, f.Snapshot().Tables) {
		t.Fatal("changed member did not stop the whole delete")
	}
}

func TestAddressBatchLimitsUseUTF8Bytes(t *testing.T) {
	d, f := newFakeRouterOS(t, r07FakeState())
	entries := []Row{}
	for i := 0; i < 450; i++ {
		entries = append(entries, Row{"list": "L", "comment": strings.Repeat("中", 4800) + `"$` + "\n", "disabled": "false", "address": fmt.Sprintf("198.18.%d.%d/32", i/256, i%256)})
	}
	_, err := d.CreateAddresses(context.Background(), entries, &[]NATStep{}, ROSIDs{})
	if err != nil {
		t.Fatal(err)
	}
	scripts := 0
	for _, call := range f.Calls() {
		if call.Path != "/execute" {
			continue
		}
		body := Row(call.Body.(routerfake.Row))
		script := body.text("script")
		if strings.Contains(script, "address-list/add") {
			scripts++
			if len(script) > AddressScriptBytes {
				t.Fatalf("script has %d bytes", len(script))
			}
			if strings.Count(script, `";"`)+1 > AddressBatchMembers {
				t.Fatal("member cap exceeded")
			}
		}
	}
	if scripts <= 3 {
		t.Fatal("byte cap did not split long UTF-8 scripts")
	}
}

func TestAddressProofCacheOwnsDeepCopies(t *testing.T) {
	d, _ := newFakeRouterOS(t, r07FakeState())
	rows := []Row{{".id": "*1", "list": "L", "address": "198.51.100.1", "tags": []any{map[string]any{"name": "original"}}}}
	d.RememberAddresses("L", rows)
	rows[0]["tags"].([]any)[0].(map[string]any)["name"] = "external"
	cached := d.knownAddresses()["L"]
	if reflect.DeepEqual(cached, rows) {
		t.Fatal("cache shares mutable nested rows with caller")
	}
}

func TestNATRestoreStopsDeletingAfterOwnershipFailure(t *testing.T) {
	d, f := newFakeRouterOS(t, r07FakeState())
	undo, err := d.ApplyNATLists(context.Background(), r07NewSync())
	if err != nil {
		t.Fatal(err)
	}
	state := f.Snapshot()
	rule := state.Tables[rosNAT][0]
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{PatchRows: map[string]map[string]routerfake.Row{rosNAT: {fmt.Sprint(rule[".id"]): {"protocol": "tcp"}}}}})
	err = d.RestoreNATLists(context.Background(), *undo)
	if err == nil {
		t.Fatal("externally changed rule was treated as owned")
	}
	after := f.Snapshot()
	if len(after.Tables[rosAddresses]) != len(state.Tables[rosAddresses]) || len(after.Tables[rosNAT]) != 2 {
		t.Fatal("removed members still referenced by the externally changed rule")
	}
}

func TestAddressSweepRefusesAmbiguousOrDynamicIDs(t *testing.T) {
	for _, variant := range []string{"twins", "dynamic", "duplicate_id"} {
		t.Run(variant, func(t *testing.T) {
			d, f := newFakeRouterOS(t, r07FakeState())
			row := routerfake.Row{".id": "*AAA", "list": NATDstList, "address": "198.51.100.1", "comment": NATListComment, "disabled": "false"}
			rows := []routerfake.Row{row}
			if variant == "dynamic" {
				row["dynamic"] = "true"
			} else {
				copy := routerfake.Row{}
				for k, v := range row {
					copy[k] = v
				}
				if variant == "twins" {
					copy[".id"] = "*AAB"
				}
				rows = append(rows, copy)
			}
			f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{rosAddresses: rows}}})
			step := NATStep{Action: "sweep", Path: rosAddresses, List: NATDstList, Shared: Row{"list": NATDstList, "comment": NATListComment, "disabled": "false"}, Addresses: []string{"198.51.100.1/32"}, Before: []string{}}
			if err := d.restoreAddressSweep(context.Background(), step); err != nil {
				t.Fatal(err)
			}
			if len(f.Snapshot().Tables[rosAddresses]) != len(rows) {
				t.Fatal("sweep guessed ownership from an ambiguous address")
			}
		})
	}
}

func TestDeviceDiagnosticsRedactPassword(t *testing.T) {
	for _, action := range []string{"http", "console"} {
		t.Run(action, func(t *testing.T) {
			state := r07FakeState()
			if action == "http" {
				state.Faults = []routerfake.Fault{{Method: "GET", Path: rosNAT, Status: 400, Body: routerfake.Row{"detail": "echo synthetic-password"}}}
			} else {
				state.Faults = []routerfake.Fault{{Method: "POST", Path: "/execute", Count: 1, AfterCommit: true, Status: 200, Body: routerfake.Row{"ret": "error synthetic-password"}}}
			}
			d, _ := newFakeRouterOS(t, state)
			var err error
			if action == "http" {
				_, err = d.ReadState(context.Background())
			} else {
				_, err = d.CreateAddresses(context.Background(), NATEntries(NATDstList, []string{"198.51.100.1"}), &[]NATStep{}, ROSIDs{})
			}
			if err == nil || strings.Contains(err.Error(), "synthetic-password") || !strings.Contains(err.Error(), "[REDACTED]") {
				t.Fatalf("unsafe diagnostic: %v", err)
			}
		})
	}
}

func TestInvalidNATUndoCannotWriteOtherTables(t *testing.T) {
	d, f := newFakeRouterOS(t, r07FakeState())
	err := d.RestoreNATLists(context.Background(), NATListUndo{Steps: []NATStep{{Action: "delete", Path: "/system/script", ID: "*3", Config: Row{}}}})
	if err == nil || len(f.Calls()) > 0 {
		t.Fatal("wrong-table undo reached device")
	}
}

func TestAddressFetchFallsBackForSeparatorsAndMovingPages(t *testing.T) {
	for _, variant := range []string{"separator", "shrinking", "malformed_digest"} {
		t.Run(variant, func(t *testing.T) {
			state := r07FakeState()
			rows := []routerfake.Row{}
			for i := 0; i < 2001; i++ {
				rows = append(rows, routerfake.Row{".id": fmt.Sprintf("*%X", i+1000), "list": "L", "address": fmt.Sprintf("198.18.%d.%d", i/256, i%256), "disabled": "false"})
			}
			state.Tables[rosAddresses] = rows
			if variant == "separator" {
				rows[0]["comment"] = "x;y"
			} else if variant == "shrinking" {
				state.Faults = []routerfake.Fault{{Method: "POST", Path: "/execute", Count: 1, AfterCommit: true, RemoveIDs: map[string][]string{rosAddresses: {"*3E8"}}}}
			} else {
				state.Faults = []routerfake.Fault{{Method: "POST", Path: "/execute", Count: 1, Status: 200, Body: routerfake.Row{"ret": "cf-digest:x"}}}
			}
			d, f := newFakeRouterOS(t, state)
			if variant == "malformed_digest" {
				d.RememberAddresses("L", []Row{})
				if _, err := d.ReadAddresses(context.Background(), []string{"L"}); err == nil {
					t.Fatal("malformed proof accepted")
				}
				return
			}
			result, err := d.FetchAddresses(context.Background(), "L")
			if err != nil {
				t.Fatal(err)
			}
			expected := len(rows)
			if variant == "shrinking" {
				expected--
			}
			if len(result) != expected {
				t.Fatalf("read %d, expected %d", len(result), expected)
			}
			if !slices.ContainsFunc(f.Calls(), func(c routerfake.Call) bool { return c.Method == "GET" && c.Path == rosAddresses }) {
				t.Fatal("invalid page did not trigger scoped REST fallback")
			}
		})
	}
}

func TestAddressRestoreRetainsPartialNewIDs(t *testing.T) {
	d, f := newFakeRouterOS(t, r07FakeState())
	entries := NATEntries(NATDstList, []string{"198.51.100.1", "198.51.100.2", "198.51.100.3", "198.51.100.4"})
	created, err := d.CreateAddresses(context.Background(), entries, &[]NATStep{}, ROSIDs{})
	if err != nil {
		t.Fatal(err)
	}
	journal := []NATStep{}
	if err = d.DeleteAddresses(context.Background(), created, &journal); err != nil {
		t.Fatal(err)
	}
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{rosAddresses: {{".id": "*FFFF", "list": NATDstList, "address": "198.51.100.4", "comment": NATListComment, "disabled": "false"}}}}})
	remapped := map[[2]string]string{}
	err = d.restoreAddresses(context.Background(), journal, ROSIDs{}, remapped)
	if err == nil {
		t.Fatal("conflicting member should refuse recreation")
	}
	if len(remapped) != 3 {
		t.Fatalf("partial new IDs lost: %#v", remapped)
	}
	for _, item := range created[:3] {
		id := remapped[[2]string{rosAddresses, item.ID}]
		if id == item.ID || id == "" {
			t.Fatal("old ID survived partial recreate")
		}
	}
}

func TestAddressWritesInvalidateScopedProofBeforeBeforeIDs(t *testing.T) {
	d, f := newFakeRouterOS(t, r07FakeState())
	done := d.ReuseAddressReads()
	defer done()
	if _, err := d.ReadAddresses(context.Background(), []string{NATDstList}); err != nil {
		t.Fatal(err)
	}
	f.ApplyExternal(routerfake.ExternalWrite{Fault: routerfake.Fault{ExtraRows: map[string][]routerfake.Row{rosAddresses: {{".id": "*FA", "list": NATDstList, "address": "198.51.100.1", "comment": NATListComment, "disabled": "false"}}}}})
	journal := []NATStep{}
	_, err := d.CreateAddresses(context.Background(), NATEntries(NATDstList, []string{"198.51.100.1"}), &journal, ROSIDs{})
	if err == nil {
		t.Fatal("duplicate unexpectedly succeeded")
	}
	if len(journal) != 0 {
		t.Fatalf("external existing member became owned: %#v", journal)
	}
}

func TestAddressDigestNeverProvesCompositeFields(t *testing.T) {
	rows := []Row{{".id": "*1", "list": "L", "tags": []any{"lan"}}}
	first := DigestAddresses(rows)
	rows[0]["tags"] = []any{"guest"}
	if DigestAddresses(rows) == first {
		t.Fatal("different composite values collapsed into the same proof")
	}
	if digestableAddresses(rows) {
		t.Fatal("array-valued REST fields must be downloaded")
	}
}
