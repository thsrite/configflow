package routers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/routers/routerfake"
)

func TestRouteBatchesRespectDuplicatesAndLimits(t *testing.T) {
	driver := &RouterOS{routeBatch: 3}
	ops := marshalOperations(t,
		map[string]any{"kind": "route_add", "cidr": "198.51.100.0/24", "gateway": "192.0.2.1"},
		map[string]any{"kind": "route_add", "cidr": "203.0.113.0/24", "gateway": "192.0.2.1"},
		map[string]any{"kind": "route_add", "cidr": "198.51.100.0/24", "gateway": "192.0.2.2"},
	)
	if got := driver.routeBatches(ops, 0, "route_add"); !equalInts(got, []int{0, 1}) {
		t.Fatalf("duplicate destination should end batch, got %v", got)
	}

	large := marshalOperations(t,
		map[string]any{"kind": "route_add", "cidr": "198.51.100.0/24", "gateway": "192.0.2.1", "comment": strings.Repeat("a", 15000)},
		map[string]any{"kind": "route_add", "cidr": "203.0.113.0/24", "gateway": "192.0.2.1", "comment": strings.Repeat("b", 18000)},
	)
	if got := driver.routeBatches(large, 0, "route_add"); !equalInts(got, []int{0}) {
		t.Fatalf("script byte limit should split batch, got %v", got)
	}
}

func TestConsoleScriptRoundTripsUntrustedValues(t *testing.T) {
	comment := "cf 引用 \"$;\\\n换行🙂"
	line := consoleAddLine(7, Row{"dst-address": "198.51.100.0/24", "gateway": "192.0.2.1", "comment": comment}, rosRoutes, false)
	adds, err := routerfake.ParseConsoleAdds(line)
	if err != nil {
		t.Fatal(err)
	}
	if len(adds) != 1 || adds[0].Index != 7 || adds[0].Collection != rosRoutes || adds[0].Values["comment"] != comment {
		t.Fatalf("script did not preserve command fields: %#v", adds)
	}
}

func TestRestoreManyStopsAfterFirstSingleRestoreFailure(t *testing.T) {
	rows := []Row{
		{".id": "*1", "dst-address": "198.51.100.0/24", "gateway": "192.0.2.1", "routing-table": "main", "comment": "first", "disabled": "false"},
		{".id": "*2", "dst-address": "203.0.113.0/24", "gateway": "192.0.2.1", "routing-table": "main", "comment": "second", "disabled": "false"},
		{".id": "*3", "dst-address": "192.0.2.0/24", "gateway": "192.0.2.1", "routing-table": "main", "comment": "third", "disabled": "false"},
	}
	client := &scriptedDeviceHTTP{rows: rows, failDelete: "*1"}
	driver := NewRouterOSWithHTTP("http://router.invalid", client)
	driver.batchMin = 3
	undos := make([]json.RawMessage, len(rows))
	for i, row := range rows {
		change := ROSChange{Method: "DELETE", Path: rosRoutes, ID: row.id(), Expected: marshalRow(driver.ownershipSnapshot(rosRoutes, row))}
		undos[i], _ = json.Marshal(ROSUndo{Kind: "ros_restore", Changes: []ROSChange{change}})
	}
	got := driver.RestoreMany(context.Background(), undos, true)
	if len(got) != 3 || got[0] == "" || got[1] != BatchSkipped || got[2] != BatchSkipped {
		t.Fatalf("restore results = %#v, want one error followed by skipped rows", got)
	}
	for _, call := range client.calls {
		if strings.HasSuffix(call, "/*2") || strings.HasSuffix(call, "/*3") {
			t.Fatalf("stop-on-failure touched a later row: %v", client.calls)
		}
	}
}

func marshalOperations(t *testing.T, operations ...map[string]any) []json.RawMessage {
	t.Helper()
	out := make([]json.RawMessage, len(operations))
	for i, operation := range operations {
		out[i], _ = json.Marshal(operation)
	}
	return out
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

type scriptedDeviceHTTP struct {
	rows       []Row
	failDelete string
	calls      []string
}

func (f *scriptedDeviceHTTP) Do(_ context.Context, method, rawURL string, _ any, _ time.Duration) (*devhttp.Response, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	path := strings.TrimPrefix(parsed.Path, "/rest")
	f.calls = append(f.calls, method+" "+path)
	var status int
	var result any
	switch {
	case method == http.MethodPost && strings.HasSuffix(path, "/print"):
		status, result = http.StatusOK, f.rows
	case method == http.MethodGet:
		id := strings.TrimPrefix(path, rosRoutes+"/")
		for _, row := range f.rows {
			if row.id() == id {
				status, result = http.StatusOK, row
				break
			}
		}
		if status == 0 {
			status, result = http.StatusNotFound, Row{"message": "Not Found"}
		}
	case method == http.MethodPost && strings.HasSuffix(path, "/remove"):
		status, result = http.StatusInternalServerError, Row{"message": "injected remove failure"}
	case method == http.MethodDelete && strings.HasSuffix(path, "/"+f.failDelete):
		status, result = http.StatusInternalServerError, Row{"message": "injected delete failure"}
	default:
		status, result = http.StatusNoContent, Row{}
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode scripted result: %w", err)
	}
	return &devhttp.Response{Status: status, Body: body}, nil
}

func (*scriptedDeviceHTTP) Close() {}
