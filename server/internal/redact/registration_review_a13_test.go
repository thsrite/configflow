package redact

import (
	"context"
	"net/url"
	"testing"
	"unicode/utf8"

	"configflow/internal/repo"
)

// TestReviewRegistrationFinderSnapshot checks partial masks, encodings and rotation.
func TestReviewRegistrationFinderSnapshot(t *testing.T) {
	const key = "synthetic-registration/key"
	t.Setenv("AGENT_REGISTRATION_KEY", key)
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	find, err := StoredSecretFinder(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "中前-"
	for _, value := range []string{key, url.QueryEscape(key), url.QueryEscape(url.QueryEscape(key))} {
		if len(find(prefix+value+"-tail")) == 0 {
			t.Error("registration credential encoding not recognized")
		}
	}
	ranges := find(prefix + key + "-tail")
	start := utf8.RuneCountInString(prefix)
	if len(ranges) != 1 || ranges[0] != (Range{Start: start, End: start + len(key)}) {
		t.Error("literal credential did not retain its surrounding Unicode context")
	}
	if len(find("ordinary.example.com")) != 0 {
		t.Error("ordinary metadata was treated as a credential")
	}
	global, err := ExternalPayloadSecrets(ctx, nil)
	if err != nil || ContainsEncodedSecret(key, global) {
		t.Error("global JSON hook credential policy changed")
	}
	const rotated = "synthetic-rotated/key"
	t.Setenv("AGENT_REGISTRATION_KEY", rotated)
	next, err := StoredSecretFinder(ctx)
	if err != nil || len(find(key)) == 0 || len(find(rotated)) != 0 || len(next(rotated)) == 0 || len(next(key)) != 0 {
		t.Error("finder did not retain an independent environment snapshot")
	}
}
