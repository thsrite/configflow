package agents

import (
	"net/http"
	"testing"

	"configflow/internal/redact"
)

func TestTypedResponseFailureUsesExactStringDiagnostics(t *testing.T) {
	for _, tt := range []struct{ body, message string }{
		{`{"message":"reason","Message":"forged"}`, "reason"},
		{`{"Message":"forged","error":"reason"}`, "reason"},
		{`{"message":7,"error":true}`, "fallback"},
		{`{"message":"first","message":"forged"}`, "fallback"},
		{`{"message":"synthetic-device-secret"}`, redact.Redacted},
	} {
		result, err := ResponseFailureModel(&Response{StatusCode: 502, Header: http.Header{"Content-Type": {"application/json"}}, Body: []byte(tt.body)}, "fallback", redact.NewSecrets([]string{"synthetic-device-secret"}))
		if err != nil || result.Message == nil || *result.Message != tt.message || result.HTTPStatus == nil || *result.HTTPStatus != 502 {
			t.Fatalf("body=%s result=%+v err=%v", tt.body, result, err)
		}
	}
}
