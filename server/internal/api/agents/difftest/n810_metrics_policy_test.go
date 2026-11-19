package difftest

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

// These four exceptions bind the complete original request, fixture source,
// Python result and a separately reviewed native result. They cannot match an
// arbitrary error, changed request or changed saved-history fixture.
type metricsHTTPOriginal struct {
	Request      map[string]any `json:"request"`
	SourceFile   string         `json:"source_file"`
	Source       string         `json:"source"`
	PythonStatus int            `json:"python_status"`
	PythonBody   string         `json:"python_body"`
}
type metricsHTTPPolicy struct {
	Name         string          `json:"name"`
	Original     json.RawMessage `json:"original"`
	SHA          string          `json:"sha256"`
	NativeStatus int             `json:"native_status"`
	NativeBody   string          `json:"native_body"`
	input        metricsHTTPOriginal
}

func decodeMetricsHTTPPolicies(raw []byte) ([]metricsHTTPPolicy, error) {
	var rows []metricsHTTPPolicy
	seen := map[string]bool{}
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var row metricsHTTPPolicy
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, err
		}
		if fmt.Sprintf("%x", sha256.Sum256(row.Original)) != row.SHA {
			return nil, fmt.Errorf("original SHA changed: %s", row.Name)
		}
		if err := json.Unmarshal(row.Original, &row.input); err != nil {
			return nil, err
		}
		if seen[row.Name] {
			return nil, fmt.Errorf("duplicate policy %s", row.Name)
		}
		seen[row.Name] = true
		rows = append(rows, row)
	}
	if len(rows) != 4 {
		return nil, fmt.Errorf("expected four policies, got %d", len(rows))
	}
	return rows, nil
}
func metricsHTTPPolicies(t *testing.T) []metricsHTTPPolicy {
	t.Helper()
	raw, err := os.ReadFile("testdata/n810_metrics_http_native.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := decodeMetricsHTTPPolicies(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		source, err := os.ReadFile(row.input.SourceFile)
		if err != nil || row.input.Source == "" || !bytes.Contains(source, []byte(row.input.Source)) {
			t.Fatalf("fixture source changed: %s (%v)", row.Name, err)
		}
	}
	return rows
}
func samePolicyJSON(a, b []byte) bool {
	av, ae := jsonx.Parse(a)
	bv, be := jsonx.Parse(b)
	return ae == nil && be == nil && jsonx.Equal(av, bv)
}
func metricsPolicyRequest(row metricsHTTPPolicy, r request) bool {
	return samePolicyJSON(n810Canonical(row.input.Request), n810Canonical(n810RequestKey(r)))
}

// checkMetricsSeed proves that the live input still is the bound oddMetrics
// fixture. Only the clock is variable; the future timestamp and corrupt bytes
// remain exact. The first point is deliberately a BOM-prefixed JSON line.
func checkMetricsSeed(t *testing.T, seedDir string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(seedDir, "metrics", "agent-odd_metrics.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := bytes.Cut(raw, []byte("\n"))
	var point struct {
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(bytes.TrimPrefix(first, []byte("\xef\xbb\xbf")), &point); err != nil {
		t.Fatal(err)
	}
	at, err := time.Parse("2006-01-02T15:04:05.999999", point.Timestamp)
	if err != nil || !bytes.Equal(raw, []byte(oddMetrics(at.Add(50*time.Minute)))) {
		t.Fatal("live oddMetrics seed changed", err)
	}
}
func compareMetricsPolicy(row metricsHTTPPolicy, py, gor response) []string {
	var diffs []string
	if py.status != row.input.PythonStatus || gor.status != row.NativeStatus {
		diffs = append(diffs, "bound metrics policy status changed")
	}
	if !samePolicyJSON(py.body, []byte(row.input.PythonBody)) {
		diffs = append(diffs, fmt.Sprintf("bound Python body changed: %s", py.body))
	}
	if !samePolicyJSON(gor.body, []byte(row.NativeBody)) {
		diffs = append(diffs, fmt.Sprintf("native metrics body changed: %s", gor.body))
	}
	for _, name := range []string{"Content-Type", "Cache-Control", "X-ConfigFlow-Profile"} {
		if strings.Join(py.header.Values(name), ",") != strings.Join(gor.header.Values(name), ",") {
			diffs = append(diffs, "bound policy header changed: "+name)
		}
	}
	if !strings.HasPrefix(gor.header.Get("Content-Type"), "application/json") {
		diffs = append(diffs, "native policy requires JSON response")
	}
	return diffs
}
func compareMetricsHTTP(t *testing.T, seedDir string, r request, py, gor response) ([]string, bool) {
	t.Helper()
	for _, row := range metricsHTTPPolicies(t) {
		if row.Name != r.name {
			continue
		}
		if !metricsPolicyRequest(row, r) {
			return []string{"bound metrics request changed"}, true
		}
		if row.Name == "metrics summary odd 6h" {
			checkMetricsSeed(t, seedDir)
		} else {
			editedSeed(t, seedDir, row)
		}
		return compareMetricsPolicy(row, py, gor), true
	}
	return nil, false
}

// editedSeed normalizes only the two dated keys, preserving every other source
// byte. The untouched native file must equal this complete original seed.
func editedSeed(t *testing.T, seedDir string, row metricsHTTPPolicy) *strings.Replacer {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(seedDir, "traffic", "agent-edit.json"))
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	object, ok := value.(*jsonx.Object)
	if !ok {
		t.Fatal("edited seed is not an object")
	}
	var days []string
	for _, key := range object.GetObject("days").Keys() {
		if key != "zzz" {
			days = append(days, key)
		}
	}
	if len(days) != 2 {
		t.Fatal("edited seed dates changed")
	}
	today, e1 := time.Parse("2006-01-02", days[0])
	minus6, e2 := time.Parse("2006-01-02", days[1])
	if e1 != nil || e2 != nil || !today.AddDate(0, 0, -6).Equal(minus6) {
		t.Fatal("edited seed date relationship changed")
	}
	replace := strings.NewReplacer(days[0], "<today>", days[1], "<minus6>")
	const original = `{"status": "old", "days": {"<today>": {"ip_only_conns": 1.5, "dropped": true, "hosts": []}, "zzz": {}, "<minus6>": {}}}`
	if replace.Replace(string(raw)) != original {
		t.Fatalf("edited live seed changed: %s", raw)
	}
	return replace
}
func compareEditedTrafficFile(t *testing.T, seedDir, py, gor string) []string {
	t.Helper()
	for _, row := range metricsHTTPPolicies(t) {
		if row.Name != "traffic/agent-edit.json" {
			continue
		}
		replace := editedSeed(t, seedDir, row)
		if row.input.Request["file"] != "traffic/agent-edit.json" || row.input.PythonStatus != 0 || row.NativeStatus != 0 {
			return []string{"bound file fixture changed"}
		}
		var diffs []string
		if !samePolicyJSON([]byte(replace.Replace(py)), []byte(row.input.PythonBody)) {
			diffs = append(diffs, "bound Python edited file changed: "+py)
		}
		if replace.Replace(gor) != row.NativeBody {
			diffs = append(diffs, "native refusal changed original file bytes: "+gor)
		}
		return diffs
	}
	return []string{"edited traffic file policy missing"}
}

func TestN810MetricsHTTPPolicyBinding(t *testing.T) {
	rows := metricsHTTPPolicies(t)
	requestsByName := map[string]request{}
	for _, r := range requests() {
		requestsByName[r.name] = r
	}
	for _, row := range rows {
		if row.Name == "traffic/agent-edit.json" {
			continue
		}
		r, ok := requestsByName[row.Name]
		if !ok || !metricsPolicyRequest(row, r) {
			t.Fatalf("request binding changed: %s", row.Name)
		}
		r.body += " "
		if metricsPolicyRequest(row, r) {
			t.Fatal("changed request passed binding")
		}
		py := response{status: row.input.PythonStatus, body: []byte(row.input.PythonBody), header: map[string][]string{"Content-Type": {"application/json"}}}
		gor := response{status: row.NativeStatus, body: []byte(row.NativeBody), header: py.header}
		if diffs := compareMetricsPolicy(row, py, gor); len(diffs) > 0 {
			t.Fatal(diffs)
		}
		gor.body = []byte(`{"success":false,"message":"different error"}`)
		if len(compareMetricsPolicy(row, py, gor)) == 0 {
			t.Fatal("unrelated native error accepted")
		}
	}
	raw, err := os.ReadFile("testdata/n810_metrics_http_native.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace(raw, []byte(`"schema\": 1`), []byte(`"schema\": 2`), 1)
	if bytes.Equal(raw, changed) {
		t.Fatal("negative fixture control did not mutate input")
	}
	if _, err := decodeMetricsHTTPPolicies(changed); err == nil {
		t.Fatal("changed original accepted without updated SHA")
	}
	if samePolicyJSON([]byte(`{"n":1,"a":[1,2]}`), []byte(`{"a":[2,1],"n":1}`)) || samePolicyJSON([]byte(`{"n":1}`), []byte(`{"n":2}`)) {
		t.Fatal("structural comparison discarded array order or counters")
	}
}
