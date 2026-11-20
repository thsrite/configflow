package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"configflow/internal/repo"
)

func TestAbandonDryRunDeletionAndRetainedInstallation(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "unused", true: "installation"}[started], func(t *testing.T) {
			s, ctx := serviceFixture(t, "gateway")
			s.owners.Installation = installedOwner()
			s.owners.Installation.HasInstallation = func(context.Context, *repo.Repository, string) (bool, error) { return started, nil }
			if _, err := s.UpdateSources(ctx, testSessionID, json.RawMessage(`{"url":"https://example.com/sub?token=synthetic-private"}`)); err != nil {
				t.Fatal(err)
			}
			store := storeFor(s.repository)
			path, _ := store.path(testSessionID)
			before, _ := os.ReadFile(path)
			commits := s.repository.CommitCount()
			raw, err := s.Action(ctx, testSessionID, "abandon", json.RawMessage(`{"dry_run":true}`), "")
			if err != nil {
				t.Fatal(err)
			}
			var plan abandonPlan
			if err = json.Unmarshal(raw, &plan); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) || commits != s.repository.CommitCount() || plan.DeleteProfile == started {
				t.Fatal("dry run changed storage or returned wrong plan")
			}
			if _, err := s.Action(ctx, testSessionID, "abandon", json.RawMessage(`{}`), ""); err != nil {
				t.Fatal(err)
			}
			system, err := s.repository.System(ctx)
			if err != nil {
				t.Fatal("abandon did not suppress prompt", err)
			}
			dismissedRaw, _, _ := system.SystemConfig.MemberJSON("setup_suggestion_dismissed")
			if string(dismissedRaw) != "true" {
				t.Fatalf("abandon did not suppress prompt: %s", dismissedRaw)
			}
			if !started {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("deleted session remains", err)
				}
				if _, err := s.repository.ProfileMetadataRecord(ctx, "setup_"+testSessionID[:20]); !errors.Is(err, repo.ErrNotFound) {
					t.Fatal("unused profile remains", err)
				}
				shared, err := s.repository.Shared(ctx)
				if err != nil || len(shared.Subscriptions) != 0 {
					t.Fatal("wizard source remains", err)
				}
			} else {
				record, err := store.read(ctx, testSessionID)
				if err != nil || !enabled(record.Abandoned) {
					t.Fatal("dependent profile lost resumable session", err)
				}
				if _, err := s.Patch(ctx, testSessionID, json.RawMessage(`{"step":1}`)); err != nil {
					t.Fatal(err)
				}
				record, _ = store.read(ctx, testSessionID)
				if record.Abandoned != nil {
					t.Fatal("user edit failed to reopen session")
				}
			}
		})
	}
}
