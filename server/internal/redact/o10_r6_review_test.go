package redact

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

// TestO10R6ReviewPublicSecretsMatchEncodedView checks the whole set a router
// response hides (external seeds + stored Agent credentials + the profile's
// view credentials) against collecting from the encoded view as before R6,
// for credentials that occur only in a shared resource a profile references,
// only in an Agent record's OxiDNS projection, and after an Agent token is
// replaced. Every step commits outside the profile itself, so a cache keyed
// by anything narrower than the document generation would go stale.
func TestO10R6ReviewPublicSecretsMatchEncodedView(t *testing.T) {
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	if _, err := r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "second", Name: "Second"}, ""); err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, contains, absent map[string][]string) {
		t.Helper()
		for _, profile := range []string{"default", "second"} {
			base, err := ExternalPayloadSecrets(ctx, nil)
			if err == nil {
				base, err = AgentRecordCredentials(ctx, base)
			}
			if err != nil {
				t.Fatal(err)
			}
			view, err := r.ConfigWithOxiDNSAgents(ctx, profile, repo.ResourceRoots{})
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(view)
			if err != nil {
				t.Fatal(err)
			}
			want := CollectCredentials(json.RawMessage(data), base)
			for range 2 { // second round from the cache
				got, err := ConfigCredentials(ctx, r, profile, base)
				if err != nil {
					t.Fatal(err)
				}
				if !got.Equal(want) {
					t.Fatalf("%s: ConfigCredentials = %v, want %v", profile, got, want)
				}
				_, values, err := ProfileWithCredentials(ctx, r, profile)
				if err != nil {
					t.Fatal(err)
				}
				if got := NewSecrets(base, values); !got.Equal(want) {
					t.Fatalf("%s: ProfileWithCredentials = %v, want %v", profile, got, want)
				}
			}
			for _, value := range contains[profile] {
				if !slices.Contains(want, value) {
					t.Fatalf("%s: %q missing from %v", profile, value, want)
				}
			}
			for _, value := range absent[profile] {
				if slices.Contains(want, value) {
					t.Fatalf("%s: stale %q still in %v", profile, value, want)
				}
			}
		}
	}
	setNode := func(t *testing.T, password string) {
		t.Helper()
		err := r.UpdateShared(ctx, func(_ context.Context, s *model.Shared) error {
			return s.SetJSON("nodes", json.RawMessage(`[{"id":"n1","name":"N1","type":"ss","server":"198.51.100.7","port":8388,"params":{"password":"`+password+`"}}]`))
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	setAgent := func(t *testing.T, token, apiKey string) {
		t.Helper()
		err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
			return s.SetJSON("agents", json.RawMessage(`[{"id":"a1","name":"DNS","host":"192.0.2.53","port":8080,"service_type":"oxidns","profile_id":"default","token":"`+token+`","rule_sync":{"api_key":"`+apiKey+`"}}]`))
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	check(t, nil, nil)

	// A shared node only the default profile references.
	setNode(t, "synthetic-node-secret-1")
	if _, _, err := r.SaveProfileFieldsJSON(ctx, "second", json.RawMessage(`{"proxy_groups":[{"id":"g","name":"G","type":"select","manual_nodes":["n1"]}]}`), nil); err != nil {
		t.Fatal(err)
	}
	check(t, map[string][]string{"second": {"synthetic-node-secret-1"}}, map[string][]string{"default": {"synthetic-node-secret-1"}})
	// Changing only the shared node must reach the profile's set.
	setNode(t, "synthetic-node-secret-2")
	check(t, map[string][]string{"second": {"synthetic-node-secret-2"}}, map[string][]string{"second": {"synthetic-node-secret-1"}, "default": {"synthetic-node-secret-2"}})

	// An Agent record: its token reaches the set through the stored Agent
	// credentials, the nested key through the OxiDNS projection of every view.
	setAgent(t, "synthetic-agent-token-1", "synthetic-agent-key-1")
	// The undeclared rule_sync member is dropped on load (O18 T1b); only the
	// token remains a credential.
	both := []string{"synthetic-agent-token-1"}
	check(t, map[string][]string{"default": both, "second": both}, nil)
	setAgent(t, "synthetic-agent-token-2", "synthetic-agent-key-2")
	both = []string{"synthetic-agent-token-2"}
	old := []string{"synthetic-agent-token-1"}
	check(t, map[string][]string{"default": both, "second": both}, map[string][]string{"default": old, "second": old})
	// The projection's own credentials, without the stored Agent seeds.
	got, err := ConfigCredentials(ctx, r, "second", nil)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(got, "synthetic-agent-key-2") || slices.Contains(got, "synthetic-agent-token-2") {
		t.Fatalf("projection credentials %v: want neither the dropped key nor the token", got)
	}
}

// TestO10R6ReviewCredentialsBelongToTheirRecords races readers of
// ConfigWithCredentials/ProfileWithCredentials with commits that rotate a
// router credential: every reader must get the values of exactly the view (or
// stored profile) it was handed, never another generation's. The rotated
// credential is the router password (plain records keep no undeclared
// members, O18 T1c).
func TestO10R6ReviewCredentialsBelongToTheirRecords(t *testing.T) {
	r, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// A password change replaces the connection: the publication lock is free.
	defer repo.SetHooks(repo.SetHooks(repo.Hooks{RecordHooks: repo.RecordHooks{JobLockRecord: func(context.Context, string, string, model.Router) (func(), bool) {
		return func() {}, true
	}}}))
	rotate := func(i int) error {
		_, err := r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
			return p.SetJSON("routers", json.RawMessage(fmt.Sprintf(`{"items":[{"id":"r1","type":"routeros","endpoint":"http://192.0.2.1","username":"admin","password":"synthetic-rot-%d"}]}`, i)))
		})
		return err
	}
	if err := rotate(0); err != nil {
		t.Fatal(err)
	}
	rotated := func(values []string) []string {
		var out []string
		for _, v := range values {
			if strings.HasPrefix(v, "synthetic-rot-") {
				out = append(out, v)
			}
		}
		return out
	}
	const rounds = 40
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	done := make(chan struct{})
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				view, values, err := ConfigWithCredentials(ctx, r, "default")
				if err != nil {
					errs <- err
					return
				}
				data, err := json.Marshal(view)
				if err != nil {
					errs <- err
					return
				}
				if want := CollectCredentials(json.RawMessage(data), nil); !NewSecrets(values).Equal(want) {
					errs <- fmt.Errorf("ConfigWithCredentials values %v, view has %v", values, want)
					return
				}
				profile, values, err := ProfileWithCredentials(ctx, r, "default")
				if err != nil {
					errs <- err
					return
				}
				raw, _, err := model.EncodeMember(&profile.Routers.Items[0], "password")
				if err != nil {
					errs <- err
					return
				}
				var key string
				if err := json.Unmarshal(raw, &key); err != nil {
					errs <- err
					return
				}
				want := []string{key}
				if got := rotated(values); !slices.Equal(got, want) {
					errs <- fmt.Errorf("ProfileWithCredentials values %v, profile has %v", got, want)
					return
				}
			}
		})
	}
	for i := 1; i <= rounds; i++ {
		if err := rotate(i); err != nil {
			t.Fatal(err)
		}
	}
	close(done)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
