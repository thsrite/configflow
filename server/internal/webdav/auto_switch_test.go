package webdav

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestAutoBackupOffSkipsOnlyAnExplicitFalse(t *testing.T) {
	tests := []struct {
		name   string
		backup string
		off    bool
	}{
		{"false", `{"auto_backup":false}`, true},
		{"true", `{"auto_backup":true}`, false},
		{"missing", `{}`, false},
		{"null", `{"auto_backup":null}`, false},
		{"mistyped keeps the comparison", `{"auto_backup":"yes"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var document model.Document
			if err := json.Unmarshal([]byte(`{"profiles":{},"shared":{},"system":{"backup":`+tt.backup+`}}`), &document); err != nil {
				t.Fatal(err)
			}
			if got := autoBackupOff(&document); got != tt.off {
				t.Fatalf("autoBackupOff = %v, want %v", got, tt.off)
			}
		})
	}
	if autoBackupOff(nil) {
		t.Fatal("nil generation must keep the comparison")
	}
}
