package difftest

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx/yamlxtest"
)

type bindingExpectation struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	Body         string `json:"body"`
	ContentType  string `json:"content_type"`
	PythonStatus int    `json:"python_status"`
	PythonBody   string `json:"python_body"`
	GoStatus     int    `json:"go_status"`
	GoBody       string `json:"go_body"`
}

func bindingRequests(t *testing.T) []request {
	t.Helper()
	data, err := os.ReadFile("../testdata/n8_6_bind.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []bindingExpectation
	if err = json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	out := make([]request, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		method, authentication := "POST", ""
		if strings.HasPrefix(row.Path, "/api/tasks") {
			method, authentication = "PUT", "admin"
		}
		out = append(out, request{name: row.Name, method: method, path: row.Path, body: row.Body, contentType: row.ContentType, auth: authentication, binding: row})
	}
	return out
}

// bindingDifference asserts both complete bodies from the independently recorded
// Flask handlers and controls. It only adjusts the named request's comparison
// target after those checks, leaving headers and every other request untouched.
func bindingDifference(r request, py, gor response) (response, []string) {
	want := r.binding
	var diffs []string
	for _, side := range []struct {
		name   string
		got    response
		status int
		body   string
	}{
		{"python", py, want.PythonStatus, want.PythonBody}, {"go", gor, want.GoStatus, want.GoBody},
	} {
		if side.got.status != side.status {
			diffs = append(diffs, fmt.Sprintf("binding %s status %d want %d", side.name, side.got.status, side.status))
		}
		actual, aErr := jsonx.Parse(side.got.body)
		expected, eErr := jsonx.ParseString(side.body)
		if aErr != nil || eErr != nil || !yamlxtest.EqualJSON(actual, expected) {
			diffs = append(diffs, fmt.Sprintf("binding %s body %s want %s", side.name, side.got.body, side.body))
		}
	}
	py.status, py.body = want.GoStatus, []byte(want.GoBody)
	return py, diffs
}

func TestBindingComparatorRejectsUnexpectedChanges(t *testing.T) {
	r := bindingRequests(t)[0]
	want := r.binding
	py := response{status: want.PythonStatus, body: []byte(want.PythonBody)}
	good := response{status: want.GoStatus, body: []byte(want.GoBody)}
	if _, diffs := bindingDifference(r, py, good); len(diffs) != 0 {
		t.Fatal(diffs)
	}
	for _, body := range []string{
		`{}`,
		`{"success":false,"message":"wrong"}`,
		`{"success":false,"message":null}`,
		`{"success":0,"message":"The browser (or proxy) sent a request that this server could not understand."}`,
		`{"success":false,"message":"The browser (or proxy) sent a request that this server could not understand.","extra":1}`,
	} {
		changed := good
		changed.body = []byte(body)
		if _, diffs := bindingDifference(r, py, changed); len(diffs) == 0 {
			t.Fatalf("accepted mutated body %s", body)
		}
	}
	changed := good
	changed.status = 200
	if _, diffs := bindingDifference(r, py, changed); len(diffs) == 0 {
		t.Fatal("accepted wrong status")
	}
	changedPy := py
	changedPy.body = []byte(`{"success":false,"message":"different Python result"}`)
	if _, diffs := bindingDifference(r, changedPy, good); len(diffs) == 0 {
		t.Fatal("ignored changed Python oracle")
	}
}
