package repo

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"testing"

	"configflow/internal/model"
)

func sharedMemberPositions(t *testing.T, r *Repository) []string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+r.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(t.Context(), "SELECT field, position FROM fields WHERE scope = 'shared' AND owner = '' ORDER BY position")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		var position int
		if err := rows.Scan(&name, &position); err != nil {
			t.Fatal(err)
		}
		if position != len(names) {
			t.Fatalf("member %s position=%d, want %d (noncontiguous or duplicate position)", name, position, len(names))
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

func TestSharedMemberPositionsSurviveImportAndTypedUpdates(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  json.RawMessage
	}{
		{"fresh", nil},
		{"single-profile", json.RawMessage(`{}`)},
		{"schema-2", json.RawMessage(`{"schema_version":2,"system":{"schema_version":2,"profiles":[{"id":"default","name":"Default"}]},"profiles":{"default":{}}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRepo(t)
			if tc.raw != nil {
				if _, err := r.ImportAllJSON(t.Context(), tc.raw); err != nil {
					t.Fatal(err)
				}
			}
			want := []string{"subscriptions", "nodes", "subscription_aggregations", "rule_library", "_revision"}
			check := func() {
				t.Helper()
				if got := sharedMemberPositions(t, r); !slices.Equal(got, want) {
					t.Fatalf("stored member order=%v, want %v", got, want)
				}
			}
			check()
			// Rewriting an existing member changes its value, not its position.
			if err := r.UpdateShared(t.Context(), func(_ context.Context, shared *model.Shared) error {
				shared.Nodes = append(shared.Nodes, model.Node{ID: "n-position", Name: "Position", Type: "http", Server: "192.0.2.9", Port: 80})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			check()
			r.dropCache()
			if _, err := r.Shared(t.Context()); err != nil {
				t.Fatal(err)
			}
			before := must(r.store.Fingerprint())(t)
			if err := r.Update(t.Context(), func(context.Context, *model.Document) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if after := must(r.store.Fingerprint())(t); after != before {
				t.Fatal("reload and no-op update rewrote stored rows")
			}
			check()
		})
	}
}

func profileOxidnsRow(t *testing.T, r *Repository) (string, int64) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+r.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	var revision int64
	if err := db.QueryRowContext(t.Context(), "SELECT value, revision FROM fields WHERE scope = 'profile' AND owner = 'default' AND field = 'oxidns'").Scan(&value, &revision); err != nil {
		t.Fatal(err)
	}
	return value, revision
}

func TestBuiltinOxidnsRowsDoNotIntroduceFormattingRewrites(t *testing.T) {
	r := newRepo(t)
	value, revision := profileOxidnsRow(t, r)
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(value)); err != nil {
		t.Fatal(err)
	}
	if value != compact.String() {
		t.Fatal("built-in defaults stored source formatting")
	}
	if err := r.UpdateSystem(t.Context(), func(_ context.Context, system *model.System) error {
		system.SystemConfig.ServerDomain = "https://changed.example.com"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got, counter := profileOxidnsRow(t, r); got != value || counter != revision {
		t.Fatal("system write rewrote unrelated OxiDNS defaults")
	}
	const userJSON = "{\n  \"config_mode\": \"template\", \"custom_hosts\": \"\"\n}"
	if _, err := r.UpdateProfile(t.Context(), "default", func(_ context.Context, profile *model.Profile) error {
		return profile.OxiDNS.UnmarshalJSON([]byte(userJSON))
	}); err != nil {
		t.Fatal(err)
	}
	value, revision = profileOxidnsRow(t, r)
	// Go stores the value in its own (compact) encoding (O18 T1a).
	compact.Reset()
	noError(t, json.Compact(&compact, []byte(userJSON)))
	if value != compact.String() {
		t.Fatalf("user JSON stored as %s", value)
	}
	if err := r.UpdateSystem(t.Context(), func(_ context.Context, system *model.System) error {
		system.SystemConfig.ConfigToken = "synthetic-changed-token"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got, counter := profileOxidnsRow(t, r); got != value || counter != revision {
		t.Fatal("system write rewrote existing user OxiDNS bytes")
	}
}
