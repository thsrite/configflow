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
		// Automatic backups push on their own: a stored null or a wrong type
		// reads as off (O18 T1c ruling), where Python skipped only false.
		{"null", `{"auto_backup":null}`, true},
		{"mistyped is off", `{"auto_backup":"yes"}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var document model.Document
			if err := json.Unmarshal([]byte(tt.backup), &document.System.Backup); err != nil {
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
