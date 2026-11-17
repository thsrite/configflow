package difftest

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
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
		if aErr != nil || eErr != nil || !jsonx.Equal(actual, expected) {
			diffs = append(diffs, fmt.Sprintf("binding %s body %s want %s", side.name, side.got.body, side.body))
		}
	}
	py.status, py.body = want.GoStatus, []byte(want.GoBody)
	return py, diffs
}
