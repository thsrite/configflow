package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/logx"
	"configflow/internal/testjson"
)

// TestReviewA13ProjectionCredentialReferences checks references across public subrecords.
func TestReviewA13ProjectionCredentialReferences(t *testing.T) {
	for _, field := range []string{"journal", "oxidns_ha_publication"} {
		t.Run(field, func(t *testing.T) {
			record := map[string]any{
				"id":          "device",
				"deployments": map[string]any{"old": map[string]any{"status": "failed", "api_key": "synthetic-cross-key"}},
			}
			if field == "journal" {
				record[field] = "old synthetic-cross-key"
			} else {
				record[field] = map[string]any{"status": "failed", "message": "old synthetic-cross-key", "members": []any{}}
			}
			before := cloneMap(t, record)
			raw, err := json.Marshal(publicAgent(record))
			if err != nil || strings.Contains(string(raw), "synthetic-cross-key") {
				t.Errorf("projection lost credential knowledge: %s (%v)", raw, err)
			}
			if !testjson.Equal(record, before) {
				t.Error("public projection mutated the original record")
			}
		})
	}
}

// TestReviewA13ProjectionDoesNotLogSecrets checks the raw collector's diagnostics.
func TestReviewA13ProjectionDoesNotLogSecrets(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "app.log")
	root, err := logx.Setup(logx.Config{Level: logx.Debug, File: logFile})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		root.Close()
		if _, err := logx.Setup(logx.Config{Level: logx.LevelFromEnv(), Console: os.Stderr}); err != nil {
			t.Error(err)
		}
	})
	const secret = "synthetic-cross-key"
	record := map[string]any{
		"id":          "device",
		"deployments": map[string]any{"old": map[string]any{"status": "failed", "api_key": secret}},
		"journal":     "old " + secret,
	}
	record["cycle"] = record
	_, err = json.Marshal(publicAgent(record))
	if err != nil {
		if strings.Contains(err.Error(), secret) {
			t.Error("collected credential escaped an encoding error")
		}
		t.Fatal("public metadata did not encode safely")
	}
	data, err := os.ReadFile(logFile)
	if err != nil || strings.Contains(string(data), secret) {
		t.Error("collected credential escaped application logs")
	}
}
