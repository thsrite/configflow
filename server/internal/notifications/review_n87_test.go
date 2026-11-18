package notifications

import (
	"configflow/internal/repo"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestN87ReviewOccurrenceCounterDoesNotOverflow(t *testing.T) {
	for _, value := range []struct{ before, after string }{{"9223372036854775807", "9223372036854775808"}, {"9223372036854775808", "9223372036854775809"}, {"60.0", "61"}} {
		t.Run(value.before, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "notifications.json")
			before := `{"future":{"unknown":9007199254740993123456789},"items":{"notice:x":{"state":"pending","source":"notice","occurrences":` + value.before + `,"evidence":"before","sibling":1e400}}}`
			if err := os.WriteFile(path, []byte(before), 0600); err != nil {
				t.Fatal(err)
			}
			if err := UpdateItems(t.Context(), dir, func(items *Items) error {
				return ObserveItem(items, "notice:x", &Item{State: "pending", Source: NoticeSource, Evidence: jsonString("after")})
			}); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Future json.RawMessage `json:"future"`
				Items  map[string]struct {
					Count   json.RawMessage `json:"occurrences"`
					Sibling json.RawMessage `json:"sibling"`
				} `json:"items"`
			}
			if err = json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			if string(doc.Items["notice:x"].Count) != value.after || string(doc.Items["notice:x"].Sibling) != "1e400" || string(doc.Future) != `{"unknown":9007199254740993123456789}` {
				t.Fatalf("counter or sibling changed: %s", raw)
			}
		})
	}
}

func TestN87ReviewNullableLegacyAgentCodecs(t *testing.T) {
	t.Run("absent deployment owner", func(t *testing.T) {
		if err := RecordAgent(t.Context(), nil, nil); err != nil {
			t.Fatalf("nil Agent stopped being a no-op: %v", err)
		}
	})
	t.Run("absent rejected registration", func(t *testing.T) {
		dir := t.TempDir()
		repository, err := repo.OpenDefault(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err = RecordRegistrationRejection(t.Context(), repository, nil, "synthetic denial"); err != nil {
			t.Fatalf("nil registration record was not projected: %v", err)
		}
		var saved storeDocument
		raw, err := os.ReadFile(filepath.Join(dir, "notifications.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Items.Len() != 1 {
			t.Fatalf("registration notice missing: %s", raw)
		}
		for _, item := range saved.Items.All() {
			if item.Source != RegistrationSource {
				t.Fatalf("unexpected incident: %s", raw)
			}
		}
	})
}

func TestN87ReviewOccurrenceR5AndUntouchedNotation(t *testing.T) {
	for _, value := range []string{`60.0`, `true`, `"7"`, `60.5`} {
		t.Run(value, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "notifications.json")
			before := `{"items":{"n":{"state":"pending","source":"notice","occurrences":` + value + `}}}`
			if err := os.WriteFile(path, []byte(before), 0600); err != nil {
				t.Fatal(err)
			}
			if err := UpdateItems(t.Context(), dir, func(items *Items) error {
				item, _ := items.Get("n")
				if value != `60.0` && item.Occurrences != nil {
					t.Fatal("invalid occurrence bypassed R5")
				}
				item.Stale = new(true)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Items map[string]struct {
					Occurrences json.RawMessage `json:"occurrences"`
				} `json:"items"`
			}
			if err = json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			if string(doc.Items["n"].Occurrences) != value {
				t.Fatalf("unowned number/type changed: %s", raw)
			}
		})
	}
}
