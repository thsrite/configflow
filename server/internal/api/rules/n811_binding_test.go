package rules

import (
	"encoding/json"
	"fmt"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

func TestN811RejectedFieldsUseJSONText(t *testing.T) {
	for _, raw := range []string{"null", "true", "42", `{"id":1}`} {
		t.Run(raw, func(t *testing.T) {
			err := validateSyntax(record{"rule_type": json.RawMessage(raw)})
			if err == nil || err.Error() != fmt.Sprintf(msgUnknownKind, raw) {
				t.Fatalf("rule type: %v", err)
			}
			_, err = dnsChoice(json.RawMessage(raw))
			if err == nil || err.Error() != fmt.Sprintf(msgDNSUnsupported, raw) {
				t.Fatalf("DNS choice: %v", err)
			}
		})
	}
	if got := missingSelected([]json.RawMessage{json.RawMessage("null"), json.RawMessage(`"missing"`)}); got != `以下规则 id 不存在: [null,"missing"]` {
		t.Fatalf("missing IDs %q", got)
	}
}

func TestN811NonFiniteRequestsFollowBindingBoundary(t *testing.T) {
	app := fixture(t)
	auth := app.Login()
	for _, body := range []string{`{"rule_type":"DOMAIN","value":NaN}`, `{"query":Infinity}`} {
		path := "/api/rules"
		status := 400
		if body == `{"query":Infinity}` {
			path += "/match-test"
			status = 500
		}
		app.Post(path, auth, httpxtest.Body([]byte(body), "application/json")).WantStatus(status)
	}
}
