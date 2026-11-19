package dnsha

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestPendingJournalReadsDeclaredBoolean(t *testing.T) {
	for _, tt := range []struct {
		raw     string
		pending bool
	}{{`true`, true}, {`false`, false}, {`null`, false}, {`"true"`, false}, {`1`, false}} {
		var state model.Deployment
		if err := json.Unmarshal([]byte(`{"pending":`+tt.raw+`}`), &state); err != nil {
			t.Fatal(err)
		}
		if got := pending(state); got != tt.pending {
			t.Fatalf("raw pending=%s got=%v", tt.raw, got)
		}
	}
}
