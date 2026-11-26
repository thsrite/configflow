package repo

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

// TestDesensitizedExportEasyTierMember checks that the desensitized export
// only redacts an EasyTier member that is stored: profiles without one (no
// mihomo fields, or an empty mihomo object) gain no "easytier" member, like
// Python's export_all, and a stored one keeps the members of the plain export
// with only its secrets replaced.
func TestDesensitizedExportEasyTierMember(t *testing.T) {
	tests := []struct {
		id      string
		fields  string // profile fields saved after creation; empty saves nothing
		present bool
		want    map[string]string // options members expected in the desensitized export
	}{
		{id: "plain"},
		{id: "emptymihomo", fields: `{"mihomo": {}}`},
		{id: "othermihomo", fields: `{"mihomo": {"custom_config": "x"}}`},
		{id: "stored", fields: `{"mihomo": {"easytier": {"enabled": false}}}`, present: true, want: map[string]string{}},
		{
			id:      "secret",
			fields:  `{"mihomo": {"easytier": {"enabled": false, "options": {"network-name": "n", "network-secret": "s"}}}}`,
			present: true,
			want:    map[string]string{"network-name": "n", "network-secret": "[REDACTED]"},
		},
	}
	ctx := context.Background()
	r, err := Open(t.TempDir(), Options{
		Export: testExportSanitizer{
			sanitize: func(_ context.Context, _ *Repository, doc *model.Document, _ model.SystemConfig) (*model.Document, error) {
				return doc, nil
			},
			oxidns:  func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil },
			routers: func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	for _, tt := range tests {
		if _, err := r.CreateProfileRecord(ctx, ProfileCreation{ID: tt.id, Name: tt.id}, ""); err != nil {
			t.Fatalf("create %s: %v", tt.id, err)
		}
		if tt.fields != "" {
			if _, _, err := r.SaveProfileFieldsJSON(ctx, tt.id, json.RawMessage(tt.fields), nil); err != nil {
				t.Fatalf("save %s: %v", tt.id, err)
			}
		}
	}
	plain, exported := exportEasyTier(t, r, false), exportEasyTier(t, r, true)
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got, present := exported[tt.id]
			if present != tt.present {
				t.Fatalf("mihomo.easytier present = %v (%v), want %v", present, got, tt.present)
			}
			if !tt.present {
				return
			}
			stored := plain[tt.id]
			if len(got) != len(stored) {
				t.Fatalf("mihomo.easytier members %v, plain export %v", got, stored)
			}
			for key := range stored {
				if _, ok := got[key]; !ok {
					t.Fatalf("mihomo.easytier lost %q: %v", key, got)
				}
			}
			var options map[string]string
			if err := json.Unmarshal(got["options"], &options); err != nil {
				t.Fatalf("options %s: %v", got["options"], err)
			}
			if len(options) != len(tt.want) {
				t.Fatalf("options = %v, want %v", options, tt.want)
			}
			for key, want := range tt.want {
				if options[key] != want {
					t.Fatalf("options[%q] = %q, want %q", key, options[key], want)
				}
			}
		})
	}
}

// exportEasyTier returns each profile's stored mihomo.easytier members from a
// full export, omitting profiles without one.
func exportEasyTier(t *testing.T, r *Repository, desensitize bool) map[string]map[string]json.RawMessage {
	t.Helper()
	raw, err := r.ExportAllJSON(context.Background(), desensitize)
	if err != nil {
		t.Fatalf("ExportAllJSON(%v): %v", desensitize, err)
	}
	var exported struct {
		Profiles map[string]struct {
			Mihomo map[string]json.RawMessage `json:"mihomo"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(raw, &exported); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]json.RawMessage{}
	for id, profile := range exported.Profiles {
		value, ok := profile.Mihomo["easytier"]
		if !ok {
			continue
		}
		var members map[string]json.RawMessage
		if err := json.Unmarshal(value, &members); err != nil {
			t.Fatalf("profile %s mihomo.easytier %s: %v", id, value, err)
		}
		out[id] = members
	}
	return out
}
