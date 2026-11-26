package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// An Agent configuration written by an image before 1.9.3-go says
// "supervisor"; it is read as "s6" and saved as "s6".
func TestLoadConfigNormalizesLegacyContainerManager(t *testing.T) {
	for _, tt := range []struct{ stored, want string }{{"supervisor", "s6"}, {"s6", "s6"}, {"systemd", "systemd"}, {"", ""}} {
		path := filepath.Join(t.TempDir(), "config.json")
		raw, _ := json.Marshal(map[string]any{"service_manager": tt.stored, "service_type": "oxidns", "extra": 1})
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(path)
		if err != nil || cfg.ServiceManager != tt.want {
			t.Fatalf("%q loaded as %q: %v", tt.stored, cfg.ServiceManager, err)
		}
		if err := cfg.Save(); err != nil {
			t.Fatal(err)
		}
		var saved map[string]any
		data, _ := os.ReadFile(path)
		json.Unmarshal(data, &saved)
		if got, _ := saved["service_manager"].(string); got != tt.want || saved["extra"] != 1.0 {
			t.Fatalf("%q saved as %v", tt.stored, saved)
		}
	}
}
