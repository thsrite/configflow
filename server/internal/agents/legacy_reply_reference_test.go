package agents

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonresp"
	"configflow/internal/store"
)

func rawLegacyReply(payload any) []byte { raw, _ := jsonresp.EncodeUTF8(payload); return raw }

// verifyNativeLegacyReply binds the original false-result expectations to real
// Python replies. Only these exact malformed success-HTTP rows use native 502.
func verifyNativeLegacyReply(t *testing.T, e *env, method, body string, want map[string]any, got error, before store.Head) bool {
	t.Helper()
	raw, err := os.ReadFile("testdata/n810_legacy_reply.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != "ae7cec5313879b171fc86d5cb966cef1a654a29ccd99250f9e86047ee05bfc81" {
		t.Fatal("independent Python legacy archive changed", err)
	}
	var rows []struct {
		Original json.RawMessage `json:"original"`
		SHA      string          `json:"sha256"`
	}
	if json.Unmarshal(raw, &rows) != nil || len(rows) != 10 {
		t.Fatal("legacy reply scope changed")
	}
	for _, row := range rows {
		compact := compactRawJSON(t, row.Original)
		if fmt.Sprintf("%x", sha256.Sum256(compact)) != row.SHA {
			t.Fatal("original legacy row changed")
		}
		var obj struct {
			Method       string         `json:"method"`
			Body         string         `json:"body"`
			Status       int            `json:"status"`
			PythonResult map[string]any `json:"python_result"`
		}
		if err := json.Unmarshal(row.Original, &obj); err != nil {
			t.Fatal(err)
		}
		if obj.Method != method || obj.Body != body {
			continue
		}
		if obj.Status != 200 {
			t.Fatal("original status changed")
		}
		result := obj.PythonResult
		if want != nil {
			if diff := diffJSONValues(t, want, result); diff != "" {
				t.Fatalf("original false-result expectation changed: %s", diff)
			}
		} else {
			details, _ := result["error_details"].(string)
			if result["success"] != false || !strings.Contains(details, "upstream service unavailable") || strings.Contains(details, "do-not-display") {
				t.Fatal("original safe plain-text expectation changed")
			}
		}
		if !errors.Is(got, ErrInvalidAgentReply) {
			t.Fatalf("native command did not refuse malformed reply: %v", got)
		}
		after, err := e.repo.Head(e.ctx)
		if err != nil || after != before {
			t.Fatal("malformed command wrote transaction", err)
		}
		return true
	}
	t.Fatalf("unbound legacy reply %s %q", method, body)
	return false
}
