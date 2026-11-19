package difftest

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/store"
)

type n810OperationRow struct {
	Original json.RawMessage `json:"original"`
	SHA      string          `json:"sha256"`
	Kind     string          `json:"native_kind"`
}

// These rows were recorded from the real Python HTTP side of the unchanged
// A15 differential on .32. The full original response bytes remain bound.
func n810OperationRows(t *testing.T) []n810OperationRow {
	t.Helper()
	raw, err := os.ReadFile("testdata/n810_operation_error_rows.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != "37564c69e6be56757e22ff114af1d2e470871efbb657233acea9dfb14f0f31cd" {
		t.Fatal("original Python HTTP error archive changed", err)
	}
	var rows []n810OperationRow
	if json.Unmarshal(raw, &rows) != nil || len(rows) != 10 {
		t.Fatal("HTTP error policy scope changed")
	}
	for _, row := range rows {
		value, err := jsonx.Parse(row.Original)
		if err != nil {
			t.Fatal(err)
		}
		compact, err := jsonx.Compact(value)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(compact)) != row.SHA {
			t.Fatal("original HTTP row changed")
		}
	}
	return rows
}
func n810InputMatches(row *jsonx.Object, r upgradesRequest) bool {
	return row.GetString("name") == r.name && row.GetString("method") == r.method && row.GetString("path") == r.path && row.GetString("phase") == r.phase && row.GetString("body") == r.body && row.GetString("content_type") == r.contentType && row.GetBool("admin") == r.admin && len(r.headers) == 0 && len(r.mask) == 0 && len(r.capture) == 0 && r.wait == 0 && !r.nonFiniteMetrics
}
func n810OperationPolicy(t *testing.T, r upgradesRequest) *n810OperationRow {
	t.Helper()
	for _, row := range n810OperationRows(t) {
		value, _ := jsonx.Parse(row.Original)
		original := value.(*jsonx.Object)
		if original.GetString("name") != r.name {
			continue
		}
		if !n810InputMatches(original, r) {
			t.Fatal("bound HTTP input changed", r.name)
		}
		return &row
	}
	return nil
}
func n810OperationFingerprint(t *testing.T, dir string) store.Fingerprint {
	t.Helper()
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	fp, err := r.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return fp
}
func compareN810Operation(t *testing.T, row *n810OperationRow, python, native response) []string {
	t.Helper()
	value, _ := jsonx.Parse(row.Original)
	original := value.(*jsonx.Object)
	var diffs []string
	if int64(python.status) != original.GetInt("python_status") || string(python.body) != original.GetString("python_body") {
		diffs = append(diffs, "original Python status/body bytes changed")
	}
	for _, header := range []string{"Content-Type", "Cache-Control", "X-ConfigFlow-Profile"} {
		if strings.Join(python.header.Values(header), ",") != strings.Join(native.header.Values(header), ",") {
			diffs = append(diffs, "response header changed: "+header)
		}
	}
	parsed, err := jsonx.Parse(native.body)
	obj, ok := parsed.(*jsonx.Object)
	if err != nil || !ok || obj == nil || obj.Value("success") != false {
		return append(diffs, "native error did not refuse")
	}
	status := 500
	switch row.Kind {
	case "reply":
		status = 502
		if !jsonx.Equal(obj, jsonx.Obj("success", false, "code", "INVALID_RESPONSE", "message", agents.ErrInvalidAgentReply.Message)) {
			diffs = append(diffs, "malformed Agent reply lost structured Chinese 502")
		}
	case "header":
		if obj.Len() != 2 || !strings.HasPrefix(obj.GetString("message"), agents.ErrHeaderEncoding.Error()) {
			diffs = append(diffs, "native header error category changed")
		}
	case "endpoint":
		if !jsonx.Equal(obj, jsonx.Obj("success", false, "message", agents.ErrAgentEndpoint.Error())) {
			diffs = append(diffs, "native endpoint refusal changed")
		}
	case "payload":
		if obj.Len() != 2 || obj.GetString("message") == "" {
			diffs = append(diffs, "invalid JSON payload was not refused")
		}
	default:
		diffs = append(diffs, "unreviewed HTTP error policy")
	}
	if native.status != status {
		diffs = append(diffs, fmt.Sprintf("native status %d want %d", native.status, status))
	}
	for _, secret := range []string{"tok-op-1", "tok-op-2", "令牌"} {
		if strings.Contains(string(native.body), secret) {
			diffs = append(diffs, "credential leaked")
		}
	}
	return diffs
}
func TestN810HTTPPolicyRowsCannotBeSkipped(t *testing.T) {
	rows := n810OperationRows(t)
	seen := map[string]bool{}
	for _, r := range upgradesRequests("192.0.2.55") {
		row := n810OperationPolicy(t, r)
		if row == nil {
			continue
		}
		value, _ := jsonx.Parse(row.Original)
		original := value.(*jsonx.Object)
		if !n810InputMatches(original, r) {
			t.Fatal("original row not checked")
		}
		forged := r
		forged.body += "forged"
		if n810InputMatches(original, forged) {
			t.Fatal("forged input bypassed binding")
		}
		if seen[r.name] {
			t.Fatal("duplicate bound input")
		}
		seen[r.name] = true
	}
	if len(seen) != len(rows) {
		t.Fatal("unexecuted policy rows", len(seen), len(rows))
	}
}
