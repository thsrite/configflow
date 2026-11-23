package routers

// Tests from the O4 HR review of R04 (server/parity/review-r04.md). The
// Python-compared cases live in routeros_boundary_test.go (r04ReviewCases);
// these cover behaviour Python cannot be compared on: malformed device
// replies Python crashed on, request cancellation, and Go-only panics.

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/routers/routerfake"
)

func newFakeRouterOS(t *testing.T, state routerfake.State) (*RouterOS, *routerfake.Fake) {
	t.Helper()
	fake := routerfake.New(nil)
	fake.Reset(state)
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	driver := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
	t.Cleanup(driver.Close)
	return driver, fake
}

func routeAddOps(t *testing.T, count int, third int) []json.RawMessage {
	t.Helper()
	ops := make([]json.RawMessage, count)
	for i := range ops {
		ops[i], _ = json.Marshal(map[string]any{"kind": "route_add", "cidr": fmt.Sprintf("45.%d.%d.0/24", third, i),
			"gateway": "192.0.2.4", "comment": "cf-sync"})
	}
	return ops
}

// F1: a console reply reporting more adds than the script holds made Go index
// past its values (a panic, with every written route left unrecorded; Python
// raised IndexError). The proven routes must be recorded so the release can
// remove them, the unknown reported row must stay, and the error must say the
// outcome is uncertain.
func TestRouteBatchExtraReportedIDFailsClosed(t *testing.T) {
	foreign := routerfake.Row{".id": "*FA", "dst-address": "45.40.0.0/24", "gateway": "192.0.2.99",
		"routing-table": "main", "comment": "foreign", "disabled": "false"}
	driver, fake := newFakeRouterOS(t, routerfake.State{Faults: []routerfake.Fault{{Method: "POST", Path: "/execute",
		AfterCommit: true, OutputSuffix: []string{"cf-id:5:*FA"}, ExtraRows: map[string][]routerfake.Row{"/ip/route": {foreign}}}}})
	ops := routeAddOps(t, 5, 40)
	var records []json.RawMessage
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		return driver.ApplyMany(context.Background(), ops, func(_, undo json.RawMessage) { records = append(records, undo) })
	}()
	if device.RouterErrorKind(err) != KindRollback || !device.IsRouterError(err) {
		t.Fatalf("ApplyMany error = %v, want an uncertain-write rollback error", err)
	}
	if want := fmt.Sprintf(device.MessageUncertainRouteWrite, ros.PathRoutes); err.Error() != want {
		t.Fatalf("message = %q, want %q", err.Error(), want)
	}
	if len(records) != 5 {
		t.Fatalf("recorded %d proven routes, want all 5 so the release can remove them", len(records))
	}
	table := fake.Snapshot().Tables[ros.PathRoutes]
	if len(table) != 6 || findFakeRow(table, "*FA") == nil {
		t.Fatalf("device routes = %d (foreign kept: %v), want 5 written + the unknown reported row untouched",
			len(table), findFakeRow(table, "*FA") != nil)
	}
}

func findFakeRow(rows []routerfake.Row, id string) routerfake.Row {
	for _, row := range rows {
		if row[".id"] == id {
			return row
		}
	}
	return nil
}

// cancellingHTTP cancels a context once a request matching after has been
// answered, like a browser that disconnects in the middle of an apply.
type cancellingHTTP struct {
	DeviceHTTP
	after  string
	cancel context.CancelFunc
	once   sync.Once
}

func (c *cancellingHTTP) Do(ctx context.Context, method, rawURL string, body any, timeout time.Duration) (*devhttp.Response, error) {
	response, err := c.DeviceHTTP.Do(ctx, method, rawURL, body, timeout)
	if parsed, _ := url.Parse(rawURL); parsed != nil && method+" "+strings.TrimPrefix(parsed.Path, "/rest") == c.after {
		c.once.Do(c.cancel)
	}
	return response, err
}

func newCancellingRouterOS(t *testing.T, state routerfake.State, after string) (*RouterOS, *routerfake.Fake, context.Context) {
	t.Helper()
	fake := routerfake.New(nil)
	fake.Reset(state)
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client := &cancellingHTTP{DeviceHTTP: devhttp.New(devhttp.Config{BasicAuth: true, Username: "u", Password: "p"}),
		after: after, cancel: cancel}
	driver := NewRouterOSWithHTTP(server.URL, client)
	t.Cleanup(driver.Close)
	return driver, fake, ctx
}

// F3 (rule 4b): a NAT batch accepted by the router must be proven and
// recorded even if the caller's context is cancelled while the driver waits
// for RouterOS to validate the new rules. Before the fix the settle wait
// returned the bare context error: four rules stayed on the router, nothing
// was recorded and the error did not say so.
func TestApplyFinishesNATBatchAfterCancellation(t *testing.T) {
	patch := map[int]routerfake.Row{0: {"invalid": "true"}}
	driver, fake, ctx := newCancellingRouterOS(t, routerfake.State{Faults: []routerfake.Fault{{Method: "POST",
		Path: "/execute", AfterCommit: true, PatchCreated: patch}}}, "POST /execute")
	op, _ := json.Marshal(map[string]any{"kind": "nat_add", "src": []string{"192.0.2.0/24"},
		"dst": []string{"45.50.0.0/24", "45.50.1.0/24", "45.50.2.0/24", "45.50.3.0/24"}, "comment": "cf-mihomo"})
	raw, err := driver.Apply(ctx, op)
	if err != nil {
		t.Fatalf("Apply after cancellation = %v, want the accepted batch proven like Python", err)
	}
	var undo ROSUndo
	if err := json.Unmarshal(raw, &undo); err != nil || len(undo.Changes) != 4 {
		t.Fatalf("undo = %s, want four DELETE records", raw)
	}
	if got := len(fake.Snapshot().Tables[ros.PathNAT]); got != 4 {
		t.Fatalf("NAT rules on the device = %d, want 4", got)
	}
}

// O3 2.5 / PORTING 4b: a client disconnect does not stop a write owner,
// including between batches. Every written route must still be proven/recorded.
func TestApplyManyFinishesAfterRequestCancellation(t *testing.T) {
	driver, fake, ctx := newCancellingRouterOS(t, routerfake.State{}, "POST /execute")
	ops := routeAddOps(t, 150, 41)
	recorded := 0
	err := driver.ApplyMany(ctx, ops, func(_, _ json.RawMessage) { recorded++ })
	if err != nil {
		t.Fatalf("ApplyMany interrupted by client disconnect: %v", err)
	}
	written := len(fake.Snapshot().Tables[ros.PathRoutes])
	if written != 150 || recorded != written {
		t.Fatalf("written %d routes, recorded %d: every written route must be recorded (want 150/150)", written, recorded)
	}
}

// F3: rollback is the safety net and must run to the end like Python's.
func TestRestoreManyIgnoresCancellation(t *testing.T) {
	rows := make([]routerfake.Row, 5)
	undos := make([]json.RawMessage, 5)
	driver, fake, ctx := newCancellingRouterOS(t, routerfake.State{}, "POST /ip/route/print")
	for i := range rows {
		rows[i] = routerfake.Row{".id": fmt.Sprintf("*%X", 0x70+i), "dst-address": fmt.Sprintf("45.42.%d.0/24", i),
			"gateway": "192.0.2.1", "routing-table": "main", "comment": "cf-sync", "disabled": "false"}
		change := ROSChange{Method: "DELETE", Path: ros.PathRoutes, ID: rows[i][".id"].(string),
			Expected: ros.MarshalRow(driver.OwnershipSnapshot(ros.PathRoutes, deviceRow(rows[i])))}
		undos[i], _ = json.Marshal(ROSUndo{Kind: "ros_restore", Changes: []ROSChange{change}})
	}
	fake.Reset(routerfake.State{Tables: map[string][]routerfake.Row{ros.PathRoutes: rows}})
	results := driver.RestoreMany(ctx, undos, false)
	for i, result := range results {
		if result != "" {
			t.Fatalf("restore %d = %q, want success", i, result)
		}
	}
	if left := len(fake.Snapshot().Tables[ros.PathRoutes]); left != 0 {
		t.Fatalf("%d routes left after rollback, want 0", left)
	}
}

// removeOrderDevice answers a batched remove like a router that processed
// the IDs in its own order: the first ID is still present afterwards and
// cannot be deleted, the others are gone.
type removeOrderDevice struct {
	rows    []Row
	stuck   string
	removed map[string]bool
	log     []string
}

func (d *removeOrderDevice) Do(_ context.Context, method, rawURL string, body any, _ time.Duration) (*devhttp.Response, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	path := strings.TrimPrefix(parsed.Path, "/rest")
	status, result := http.StatusOK, any(Row{})
	present := func() []Row {
		out := []Row{}
		for _, row := range d.rows {
			if !d.removed[row.ID()] {
				out = append(out, row)
			}
		}
		return out
	}
	switch {
	case method == http.MethodPost && strings.HasSuffix(path, "/print"):
		result = present()
	case method == http.MethodPost && strings.HasSuffix(path, "/remove"):
		ids := body.(Row)[".id"]
		for _, id := range strings.Split(ids, ",") {
			if id != d.stuck {
				d.removed[id] = true
			}
		}
		status, result = http.StatusInternalServerError, Row{"message": "failure: item busy"}
	case method == http.MethodGet && path == ros.PathRoutes:
		rows := []Row{}
		for _, row := range present() {
			if row.Text("dst-address") == parsed.Query().Get("dst-address") {
				rows = append(rows, row)
			}
		}
		result = rows
	case method == http.MethodGet:
		status, result = http.StatusNotFound, Row{"message": "Not Found"}
		for _, row := range present() {
			if path == ros.PathRoutes+"/"+row.ID() {
				status, result = http.StatusOK, row
			}
		}
	case method == http.MethodDelete:
		status, result = http.StatusInternalServerError, Row{"message": "failure: item busy"}
	}
	data, _ := json.Marshal(result)
	d.log = append(d.log, fmt.Sprintf("%s %s -> %d", method, path, status))
	return &devhttp.Response{Status: status, Body: data}, nil
}

func (*removeOrderDevice) Close() {}

// F6: with stop-on-failure, routes the batched remove did delete are
// reported as restored (Python leaves them None); only routes still present
// after the failure are skipped.
func TestRestoreDeletedReportsRemovedRowsAfterFailure(t *testing.T) {
	rows := make([]Row, 4)
	for i := range rows {
		rows[i] = Row{".id": fmt.Sprintf("*%X", 0x80+i), "dst-address": fmt.Sprintf("45.43.%d.0/24", i),
			"gateway": "192.0.2.1", "routing-table": "main", "comment": "cf-sync", "disabled": "false"}
	}
	device := &removeOrderDevice{rows: rows, stuck: "*80", removed: map[string]bool{}}
	driver := NewRouterOSWithHTTP("http://router.invalid", device)
	undos := make([]json.RawMessage, len(rows))
	for i, row := range rows {
		change := ROSChange{Method: "DELETE", Path: ros.PathRoutes, ID: row.ID(), Expected: ros.MarshalRow(driver.OwnershipSnapshot(ros.PathRoutes, row))}
		undos[i], _ = json.Marshal(ROSUndo{Kind: "ros_restore", Changes: []ROSChange{change}})
	}
	got := driver.RestoreMany(context.Background(), undos, true)
	if len(got) != 4 || got[0] == "" || got[0] == ros.BatchSkipped || got[1] != "" || got[2] != "" || got[3] != "" {
		t.Fatalf("restore results = %#v, want [error, \"\", \"\", \"\"]\n%s", got, strings.Join(device.log, "\n"))
	}
}

// F7: rows holding JSON arrays (not sent by RouterOS today, but accepted by
// the DHCP reader) must not panic the comparison of preview snapshots.
func TestNATUpdateWithArrayValuesDoesNotPanic(t *testing.T) {
	rule := routerfake.Row{".id": "*A", "chain": "srcnat", "action": "accept", "comment": "manual",
		"disabled": "false", "src-address-list": []any{"lan", "guest"}}
	driver, _ := newFakeRouterOS(t, routerfake.State{Tables: map[string][]routerfake.Row{ros.PathNAT: {rule}}})
	op, _ := json.Marshal(map[string]any{"kind": "nat_update", "id": "*A", "comment": "cf-policy",
		"add_dst": []string{"45.44.0.0/24"},
		"expected": map[string]any{"chain": "srcnat", "action": "accept", "comment": "manual", "disabled": "false",
			"src-address-list": []any{"lan", "guest"}}})
	_, err := func() (_ json.RawMessage, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		return driver.Apply(context.Background(), op)
	}()
	if err != nil {
		t.Fatalf("Apply = %v, want the unchanged rule updated", err)
	}
}

// F7: ownership snapshots must tell different composite values apart.
func TestComparableDistinguishesCompositeValues(t *testing.T) {
	value := func(raw string) string { return decodedRow(t, `{"v":`+raw+`}`)["v"] }
	if ros.Comparable("src-address-list", value(`["lan"]`)) == ros.Comparable("src-address-list", value(`["guest"]`)) {
		t.Fatal("different array values compare equal")
	}
	if ros.Comparable("comment", value(`{"a":"1"}`)) == ros.Comparable("comment", "") {
		t.Fatal("an object value compares equal to an empty string")
	}
}

// F8: recreating deleted routes on rollback keeps every console script within
// the 32 KiB limit the add path uses (Python sent up to 100 adds regardless).
func TestRestoreRemovedRespectsScriptByteLimit(t *testing.T) {
	driver, fake := newFakeRouterOS(t, routerfake.State{})
	undos := make([]json.RawMessage, 20)
	for i := range undos {
		values := Row{"dst-address": fmt.Sprintf("45.45.%d.0/24", i), "gateway": "192.0.2.1", "routing-table": "main",
			"comment": strings.Repeat("路", 1000), "disabled": "false"}
		row := ros.MergeRows(values, Row{".id": fmt.Sprintf("*%X", 0x90+i)})
		change := ROSChange{Method: "RESTORE", Path: ros.PathRoutes, ID: row.ID(), Values: values,
			Expected: ros.MarshalRow(driver.OwnershipSnapshot(ros.PathRoutes, row)), KnownIDs: []string{row.ID()}}
		undos[i], _ = json.Marshal(ROSUndo{Kind: "ros_restore", Changes: []ROSChange{change}})
	}
	for i, result := range driver.RestoreMany(context.Background(), undos, false) {
		if result != "" {
			t.Fatalf("restore %d = %q", i, result)
		}
	}
	state := fake.Snapshot()
	if got := len(state.Tables[ros.PathRoutes]); got != 20 {
		t.Fatalf("recreated %d routes, want 20", got)
	}
	scripts := 0
	for _, call := range state.Calls {
		if call.Path != "/execute" {
			continue
		}
		scripts++
		script, _ := call.Body.(routerfake.Row)["script"].(string)
		if len(script) > ros.RouteScriptBytes {
			t.Fatalf("console script of %d bytes exceeds %d", len(script), ros.RouteScriptBytes)
		}
	}
	if scripts == 0 {
		t.Fatal("rollback did not use console batches")
	}
}
