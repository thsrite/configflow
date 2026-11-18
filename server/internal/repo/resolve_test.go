package repo

import (
	"testing"
)

func TestPublishConfigMatchesPublishSnapshot(t *testing.T) {
	r := must(OpenDefault(t.TempDir()))(t)
	for _, agents := range []bool{true, false} {
		config, revision, err := r.PublishConfig(bg, DefaultProfileID, agents)
		noError(t, err)
		legacy, legacyRevision, err := r.GetPublishSnapshot(bg, DefaultProfileID, agents)
		noError(t, err)
		if revision != legacyRevision || refText(t, must(legacyObject(config))(t)) != refText(t, legacy) {
			t.Fatalf("agents=%v differs", agents)
		}
		if config.HasAgents != agents {
			t.Fatal("HasAgents")
		}
	}
	_, _, err := r.PublishConfig(bg, "missing", true)
	wantError(t, err, ErrNotFound, "missing")
}
