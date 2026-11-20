package repo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestArtifactJSONKeepsLiteralURLSeparators(t *testing.T) {
	const url = "https://example.com/feed?a=1&b=<tag>"
	const want = "{\n  \"url\": \"" + url + "\"\n}\n"
	r := newRepo(t)
	for _, tc := range []struct {
		name  string
		write func(context.Context, string, any) (string, error)
	}{
		{"shared", r.WriteSharedJSON},
		{"profile", func(ctx context.Context, path string, value any) (string, error) {
			return r.WriteProfileJSON(ctx, "default", path, value)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, err := tc.write(t.Context(), "providers/url.json", struct {
				URL string `json:"url"`
			}{url})
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != want {
				t.Fatalf("artifact URL bytes changed: got %q, want %q", data, want)
			}
			var stored struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(data, &stored); err != nil || stored.URL != url {
				t.Fatalf("artifact URL round trip: %q, %v", stored.URL, err)
			}
		})
	}
}

func TestProfileMetadataRecordHonorsCancellation(t *testing.T) {
	r := newRepo(t)
	request, disconnect := context.WithCancel(t.Context())
	disconnect()
	if _, err := r.ProfileMetadataRecord(request, "default"); !errors.Is(err, context.Canceled) {
		t.Fatalf("native metadata ignored cancellation: %v", err)
	}
}
