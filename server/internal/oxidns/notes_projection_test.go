package oxidns

import (
	"bytes"
	"testing"

	"configflow/internal/devicenotes"
	"configflow/internal/model"
)

func TestMalformedStoredNotesOnlyRejectNotesWrites(t *testing.T) {
	for _, tt := range []struct{ value, message string }{
		{`null`, devicenotes.NotesMessage},
		// A stored list reads as empty notes with a load warning (O18 T1a);
		// the next notes write stores a valid object.
		{`[]`, ""},
		{`{"192.0.2.1":false}`, devicenotes.NameMessage},
	} {
		t.Run(tt.value, func(t *testing.T) {
			profile, err := ProfileFromJSON([]byte(`{"oxidns":{},"device_notes":` + tt.value + `}`))
			if err != nil {
				t.Fatal(err)
			}
			before, err := model.Marshal(profile.source.Profile)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = Generate(profile, ""); err != nil {
				t.Fatalf("unrelated generation failed: %v", err)
			}
			rows := []devicenotes.Policy{{IP: "192.0.2.1", Name: "", Mode: "direct"}}
			_, err = SavePolicyRows(profile, profile.Settings(), rows, "")
			if tt.message == "" {
				if err != nil {
					t.Fatalf("notes write refused: %v", err)
				}
				return
			}
			if message, ok := IsSettingsError(err); !ok || message != tt.message {
				t.Fatalf("notes write accepted invalid source: %v", err)
			}
			after, err := model.Marshal(profile.source.Profile)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("invalid source was rewritten")
			}
		})
	}
}
