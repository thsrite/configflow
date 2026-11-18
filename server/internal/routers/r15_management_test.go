package routers

import (
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/redact"
)

func TestR15ManagementDiagnosticsHidePeerCredentials(t *testing.T) {
	config := json.RawMessage(`{"routers":{"items":[{"id":"peer","password":"synthetic-peer-password"}]}}`)
	r := &Router{Password: "synthetic-old-password"}
	for _, text := range []string{"reference synthetic-peer-password", "reference synthetic-old-password", "reference synthetic-peer%2Dpassword"} {
		if actual := managementLogText(config, nil, r, text); actual != redact.Redacted {
			t.Fatalf("credential reference exposed: %s", actual)
		}
	}
	if actual := managementLogText(config, nil, r, "visible-user"); !strings.Contains(actual, "visible-user") {
		t.Fatal("ordinary username masked")
	}
}
