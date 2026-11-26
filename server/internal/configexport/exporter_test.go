package configexport

import (
	"encoding/json"
	"testing"
)

func TestExporterFailsClosedWithoutRouterRedactor(t *testing.T) {
	exp := Exporter{}
	if _, err := exp.RedactRoutersJSON(json.RawMessage(`{"items":[]}`)); err == nil {
		t.Fatal("expected error when RedactRouters is nil")
	}
}
