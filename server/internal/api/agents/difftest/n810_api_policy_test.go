package difftest

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// n810Variant supplies an independent Flask reference for native ASCII query
// conversion. All altered inputs are fixed artificial rows of the original suite.
func n810Variant(r request) (request, bool) {
	next := r
	switch r.name {
	case "metrics history arabic digit":
		next.path = strings.Replace(r.path, "%D9%A3", "24", 1)
	case "metrics history odd full-width hours":
		next.path = strings.Replace(r.path, "%EF%BC%91", "24", 1)
	case "metrics history too many digits":
		next.path = "/api/agents/agent-1/metrics/history?hours=9223372036854775808"
	case "traffic trend bad values":
		next.path = strings.Replace(r.path, "%D9%A3", "5", 1)
	case "logs unicode lines":
		next.path = strings.Replace(r.path, "%D9%A3%20", "100", 1)
	case "guide digit limit":
		next.body = strings.Replace(r.body, strings.Repeat("9", 4301), "0", 1)
	case "guide digit limit large":
		next.body = strings.Replace(r.body, strings.Repeat("9", 20000), "0", 1)
	case "command digits limit":
		next.body = `{"port":65536}`
	case "review logs zeros over digit limit":
		next.path = "/api/agents/op-1/logs?lines=0"
	default:
		return r, false
	}
	return next, true
}
func n810APIKind(r request) string {
	if _, ok := n810Variant(r); ok {
		return "reference"
	}
	switch r.name {
	case "cache flush dict tag":
		return "cache-tags"
	case "panel NaN reply":
		return "panel-native-number"
	case "register list", "register string", "register numeric host", "manual create list", "manual create null", "update null", "update list", "update number", "update string", "update string with profile_id", "update list with profile_id", "update bool",
		"logs list", "logs string", "logs null", "log sources list", "log clear list", "log validate string body", "logging config list body":
		return "shape"
	case "heartbeat metrics 4300 digits", "heartbeat metrics bad value after huge", "heartbeat metrics digit limit", "heartbeat metrics digit limit version", "heartbeat metrics huge percent", "heartbeat metrics huge total":
		return "heartbeat-number"
	}
	return ""
}

type n810APIResult struct {
	Status  int               `json:"status"`
	Body    string            `json:"body"`
	Calls   []string          `json:"calls,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}
type n810APIRow struct {
	Original map[string]any `json:"original"`
	SHA      string         `json:"sha256"`
	Kind     string         `json:"kind"`
	Native   *n810APIResult `json:"native,omitempty"`
}

func n810RequestKey(r request) map[string]any {
	return map[string]any{"name": r.name, "method": r.method, "path": r.path, "body": r.body, "content_type": r.contentType, "admin": r.admin, "headers": r.headers, "mask": r.mask, "capture": r.capture, "no_oracle": r.noOracle, "nonfinite_metrics": r.nonFiniteMetrics}
}
func n810Body(t *testing.T, r request, response response) string {
	t.Helper()
	value, err := jsonx.Parse(response.body)
	if err != nil {
		return string(response.body)
	}
	value = maskJSON(value, r.mask)
	raw, err := jsonx.Sorted(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func n810Canonical(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		panic(err)
	}
	result, err := json.Marshal(normalized)
	if err != nil {
		panic(err)
	}
	return result
}
func n810RecordAPI(t *testing.T, r request, python response, calls []string, native *n810APIResult) {
	t.Helper()
	path := os.Getenv("N810_RECORD_API_PYTHON")
	if path == "" || n810APIKind(r) == "" {
		return
	}
	original := map[string]any{"request": n810RequestKey(r), "python": n810Result(t, r, python, calls)}
	row := n810APIRow{Original: original, SHA: fmt.Sprintf("%x", sha256.Sum256(n810Canonical(original))), Kind: n810APIKind(r), Native: native}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(row); err != nil {
		t.Fatal(err)
	}
}
func n810BoundAPI(t *testing.T, r request, python response, calls []string) *n810APIRow {
	t.Helper()
	if n810APIKind(r) == "" || os.Getenv("N810_RECORD_API_PYTHON") != "" {
		return nil
	}
	raw, err := os.ReadFile("testdata/n810_api_python.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var row n810APIRow
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(n810Canonical(row.Original))) != row.SHA {
			t.Fatal("complete original API row changed")
		}
		requestRaw := row.Original["request"].(map[string]any)
		if requestRaw["name"] != r.name {
			continue
		}
		if row.Kind != n810APIKind(r) {
			t.Fatal("native API policy kind changed")
		}
		current := map[string]any{"request": n810RequestKey(r), "python": n810Result(t, r, python, calls)}
		if !n810MatchesOriginal(r, current, row.Original) {
			t.Fatalf("bound original API input/result changed: %s", r.name)
		}
		return &row
	}
	t.Fatalf("native API policy row missing: %s", r.name)
	return nil
}
func n810CompareAPI(t *testing.T, r request, python, native response, calls, goCalls []string) ([]string, bool) {
	row := n810BoundAPI(t, r, python, calls)
	if row == nil {
		return nil, false
	}
	if row.Kind == "reference" {
		headers := http.Header{}
		for key, value := range row.Native.Headers {
			headers.Set(key, value)
		}
		expected := response{status: row.Native.Status, body: []byte(row.Native.Body), header: headers}
		if r.nonFiniteMetrics {
			r.mask = append(append([]string(nil), r.mask...), "data.history.*.timestamp")
		}
		diffs := compare(r, expected, native)
		if strings.Join(row.Native.Calls, "\n") != strings.Join(goCalls, "\n") {
			diffs = append(diffs, "native device request trace differs from independent Flask reference")
		}
		return diffs, true
	}
	expectedStatus := python.status
	var expected any
	var err error
	if row.Kind != "cache-tags" {
		expected, err = jsonx.Parse([]byte(n810Body(t, r, python)))
		if err != nil {
			t.Fatal(err)
		}
	}
	if row.Kind == "panel-native-number" {
		expectedStatus = 200
		expected, _ = jsonx.Parse([]byte(`{"success":true,"data":{"a":null,"b":1e400,"c":-0.0,"d":1.5}}`))
		if !json.Valid(native.body) {
			return []string{"native panel response is invalid JSON"}, true
		}
	}
	if row.Kind == "cache-tags" {
		expectedStatus = 400
		expected = jsonx.Obj("success", false, "code", "INVALID_REQUEST", "message", "清理缓存请求无效。")
	}
	if row.Kind == "heartbeat-number" {
		expectedStatus = 400
		expected = jsonx.Obj("success", false, "message", "Invalid heartbeat data")
	}
	got, err := jsonx.Parse(native.body)
	var diffs []string
	if native.status != expectedStatus {
		diffs = append(diffs, fmt.Sprintf("native status %d want %d", native.status, expectedStatus))
	}
	for _, header := range []string{"Content-Type", "Cache-Control", "X-ConfigFlow-Profile"} {
		want := python.header.Get(header)
		if row.Kind == "cache-tags" {
			if header == "Content-Type" {
				want = "application/json"
			}
			if header == "Cache-Control" {
				want = "no-store"
			}
		}
		if want != native.header.Get(header) {
			diffs = append(diffs, "native error header changed: "+header)
		}
	}
	if row.Kind == "shape" && err == nil {
		a, aok := expected.(*jsonx.Object)
		b, bok := got.(*jsonx.Object)
		if !aok || !bok || a.Len() != 2 || b.Len() != 2 || a.Value("success") != false || b.Value("success") != false {
			diffs = append(diffs, "native shape error envelope changed")
		} else {
			text, ok := b.Value("message").(string)
			if !ok || text == "" || strings.ContainsFunc(text, func(r rune) bool { return r > 127 }) {
				diffs = append(diffs, "native shape diagnostic missing")
			}
		}
	} else if err != nil || !jsonx.Equal(expected, got) {
		diffs = append(diffs, "native error body differs")
	}
	if strings.Join(calls, "\n") != strings.Join(goCalls, "\n") {
		diffs = append(diffs, "refused operation changed device request trace")
	}
	return diffs, true
}

func n810StoredPath() string {
	return strings.TrimSuffix(os.Getenv("N810_RECORD_API_PYTHON"), ".jsonl") + "_stored.jsonl"
}

var n810DockerHosts = map[string]string{"docker-0": "172.١٧.0.1", "docker-1": "172.１７.0.1", "docker-2": "172. 17.0.1", "docker-3": "172.1_7.0.1", "docker-4": "172.+17.0.1", "docker-5": "172.017.0.1", "docker-12": "172.17 .0.1"}

func n810AdjustStored(t *testing.T, python, native any) {
	t.Helper()
	if os.Getenv("N810_RECORD_API_PYTHON") != "" {
		file, err := os.OpenFile(n810StoredPath(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		for _, item := range python.([]any) {
			record := item.(*jsonx.Object)
			host, present := n810DockerHosts[record.GetString("name")]
			if !present {
				continue
			}
			before, err := jsonx.Sorted(record)
			if err != nil {
				t.Fatal(err)
			}
			original := map[string]any{"name": record.GetString("name"), "python_agent": string(before), "native_host": host}
			row := n810APIRow{Original: original, SHA: fmt.Sprintf("%x", sha256.Sum256(n810Canonical(original))), Kind: "stored-host"}
			if err := json.NewEncoder(file).Encode(row); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	raw, err := os.ReadFile("testdata/n810_api_python_stored.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var row n810APIRow
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(n810Canonical(row.Original))) != row.SHA {
			t.Fatal("complete original stored Agent row changed")
		}
		name := row.Original["name"].(string)
		host, present := n810DockerHosts[name]
		if !present || row.Original["native_host"] != host {
			t.Fatal("native host policy widened")
		}
		var old, new *jsonx.Object
		for _, item := range python.([]any) {
			if a := item.(*jsonx.Object); a.GetString("name") == name {
				old = a
			}
		}
		for _, item := range native.([]any) {
			if a := item.(*jsonx.Object); a.GetString("name") == name {
				new = a
			}
		}
		if old == nil || new == nil {
			t.Fatal("bound stored Agent missing", name)
		}
		encoded, err := jsonx.Sorted(old)
		if err != nil || string(encoded) != row.Original["python_agent"] {
			t.Fatal("original stored Agent changed", name, err)
		}
		expected := old.Clone()
		expected.Set("host", host)
		if !jsonx.Equal(expected, new) {
			t.Fatal("native stored Agent changed outside bound host", name)
		}
		old.Set("host", host)
		seen++
	}
	if seen != len(n810DockerHosts) {
		t.Fatal("not all stored host rows executed", seen)
	}
}

func n810Result(t *testing.T, r request, response response, calls []string) n810APIResult {
	headers := map[string]string{}
	for _, name := range []string{"Content-Type", "Cache-Control", "X-ConfigFlow-Profile"} {
		headers[name] = response.header.Get(name)
	}
	return n810APIResult{response.status, n810Body(t, r, response), calls, headers}
}
func n810InstallerInput(r installerRequest) request {
	headers := map[string]string{"_fixture_host": r.host, "_fixture_download": fmt.Sprint(r.download)}
	for key, value := range r.headers {
		headers[key] = value
	}
	return request{name: r.name, method: r.method, path: r.path, admin: r.admin, headers: headers, body: r.body, contentType: r.contentType}
}

// n810MatchesOriginal retains the complete frozen row and only normalizes the
// seeded history clock, which the live fixture rebuilds on each invocation.
func n810MatchesOriginal(r request, current, original map[string]any) bool {
	if !r.nonFiniteMetrics {
		return bytes.Equal(n810Canonical(current), n810Canonical(original))
	}
	normalize := func(value map[string]any) []byte {
		raw := n810Canonical(value)
		var copy map[string]any
		if json.Unmarshal(raw, &copy) != nil {
			return nil
		}
		python := copy["python"].(map[string]any)
		python["body"] = timestampField.ReplaceAllString(python["body"].(string), `"timestamp":"<t>"`)
		return n810Canonical(copy)
	}
	return bytes.Equal(normalize(current), normalize(original))
}
func TestN810APIOriginalBinding(t *testing.T) {
	raw, err := os.ReadFile("testdata/n810_api_python.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var row n810APIRow
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(n810Canonical(row.Original))) != row.SHA {
			t.Fatal("original row checksum changed")
		}
		req := row.Original["request"].(map[string]any)
		name := req["name"].(string)
		if seen[name] {
			t.Fatal("duplicate original row", name)
		}
		seen[name] = true
		var changed map[string]any
		if err := json.Unmarshal(n810Canonical(row.Original), &changed); err != nil {
			t.Fatal(err)
		}
		changed["request"].(map[string]any)["body"] = req["body"].(string) + " "
		r := request{nonFiniteMetrics: req["nonfinite_metrics"].(bool)}
		if !n810MatchesOriginal(r, row.Original, row.Original) || n810MatchesOriginal(r, changed, row.Original) {
			t.Fatal("original input binding ignored", name)
		}
	}
	if len(seen) != 36 {
		t.Fatal("original rows missing", len(seen))
	}
}
