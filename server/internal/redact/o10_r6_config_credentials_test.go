package redact

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

// TestO10R6ConfigCredentialsMatchEncodedView checks that the remembered
// per-generation credential values equal collecting from the encoded
// ConfigWithOxiDNSAgents view, follow every commit, and come from the working
// copy inside a write transaction without leaking into the cache.
func TestO10R6ConfigCredentialsMatchEncodedView(t *testing.T) {
	ctx := context.Background()
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seeds := NewSecrets([]string{"synthetic-seed"})
	want := func(t *testing.T, ctx context.Context) (Secrets, string) {
		t.Helper()
		view, err := r.ConfigWithOxiDNSAgents(ctx, "default", repo.ResourceRoots{})
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		return CollectCredentials(json.RawMessage(data), seeds), string(data)
	}
	var items []string
	addRouter := func(t *testing.T, ctx context.Context, password string) {
		t.Helper()
		items = append(items, `{"id":"`+password+`","type":"routeros","endpoint":"http://192.0.2.1","username":"admin","password":"`+password+`"}`)
		_, err := r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
			return p.SetJSON("routers", json.RawMessage(`{"items":[`+strings.Join(items, ",")+`]}`))
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name, password string
	}{
		{"first router", "synthetic-first"},
		{"second router", "synthetic-second"},
		{"third router", "synthetic-third"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addRouter(t, ctx, tt.password)
			expected, encoded := want(t, ctx)
			for range 2 { // the second read is served from the cache
				got, err := ConfigCredentials(ctx, r, "default", seeds)
				if err != nil {
					t.Fatal(err)
				}
				if !got.Equal(expected) {
					t.Fatalf("ConfigCredentials = %v, want %v", got, expected)
				}
				view, values, err := ConfigWithCredentials(ctx, r, "default")
				if err != nil {
					t.Fatal(err)
				}
				if got := NewSecrets(seeds, values); !got.Equal(expected) {
					t.Fatalf("ConfigWithCredentials = %v, want %v", got, expected)
				}
				if data, err := json.Marshal(view); err != nil || string(data) != encoded {
					t.Fatalf("ConfigWithCredentials view differs from ConfigWithOxiDNSAgents: %v", err)
				}
				profile, values, err := ProfileWithCredentials(ctx, r, "default")
				if err != nil {
					t.Fatal(err)
				}
				if got := NewSecrets(seeds, values); !got.Equal(expected) {
					t.Fatalf("ProfileWithCredentials = %v, want %v", got, expected)
				}
				if len(profile.Routers.Items) != len(items) {
					t.Fatalf("profile routers = %d, want %d", len(profile.Routers.Items), len(items))
				}
			}
			if !slices.Contains(expected, tt.password) {
				t.Fatalf("expected set %v does not follow the commit", expected)
			}
		})
	}

	t.Run("write transaction", func(t *testing.T) {
		before, err := ConfigCredentials(ctx, r, "default", seeds)
		if err != nil {
			t.Fatal(err)
		}
		rollback := errors.New("rollback")
		err = r.Update(ctx, func(work context.Context, d *model.Document) error {
			for _, p := range d.Profiles {
				if p.Key() == "default" {
					p.Routers.Items = append(p.Routers.Items, model.Router{ID: "uncommitted", Type: "routeros", Password: "synthetic-uncommitted"})
				}
			}
			got, err := ConfigCredentials(work, r, "default", seeds)
			if err != nil {
				return err
			}
			if !slices.Contains(got, "synthetic-uncommitted") {
				t.Errorf("transaction read %v, want the working copy", got)
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal(err)
		}
		after, err := ConfigCredentials(ctx, r, "default", seeds)
		if err != nil {
			t.Fatal(err)
		}
		if !after.Equal(before) {
			t.Fatalf("after rollback %v, want %v", after, before)
		}
	})

	t.Run("unknown profile", func(t *testing.T) {
		if _, err := ConfigCredentials(ctx, r, "missing", seeds); err == nil {
			t.Fatal("unknown profile returned no error")
		}
		_, _, want := ConfigWithCredentials(ctx, r, "missing")
		if _, _, err := ProfileWithCredentials(ctx, r, "missing"); err == nil || want == nil || err.Error() != want.Error() {
			t.Fatalf("ProfileWithCredentials error %v, want %v", err, want)
		}
	})
}
