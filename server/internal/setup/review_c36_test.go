package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/yamlx"
)

func TestReviewC36ResponsesRedactPrivateSessionCredentials(t *testing.T) {
	for _, operation := range []string{"get", "patch", "index", "abandon-plan"} {
		t.Run(operation, func(t *testing.T) {
			s, ctx := serviceFixture(t, "router")
			store := storeFor(s.repository)
			record, err := store.read(ctx, testSessionID)
			if err != nil {
				t.Fatal(err)
			}
			const secret = "synthetic-c36-private-password"
			record.RouterDraft = json.RawMessage(`{"id":"synthetic-router","name":"Private router","type":"ikuai","endpoint":"http://192.0.2.1","username":"operator","password":"` + secret + `"}`)
			if err := store.write(ctx, record); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"default", record.ProfileID} {
				if _, err := s.repository.UpdateProfileMetadata(ctx, id, jsonx.Obj("name", "Visible "+secret)); err != nil {
					t.Fatal(err)
				}
			}
			_, err = s.repository.UpdateSharedTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
				shared.Set("subscriptions", []any{jsonx.Obj("id", "synthetic-source", "name", "Visible "+secret, "url", "https://example.com/sub", "enabled", true)})
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			path, _ := store.path(testSessionID)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var public json.RawMessage
			switch operation {
			case "get":
				public, err = s.Get(ctx, testSessionID)
			case "patch":
				public, err = s.Patch(ctx, testSessionID, json.RawMessage(`{"step":0}`))
			case "index":
				var index Index
				index, err = s.Index(ctx)
				if err == nil {
					public, err = json.Marshal(index)
				}
			case "abandon-plan":
				s.owners.Installation = installedOwner()
				public, err = s.Action(ctx, testSessionID, "abandon", json.RawMessage(`{"dry_run":true}`), "")
			}
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(public, []byte(secret)) {
				t.Fatal("response leaked a private session credential")
			}
			var metadata struct {
				DefaultProfile struct {
					Current Choice `json:"current"`
				} `json:"default_profile"`
				Subscriptions []Choice `json:"subscriptions"`
				Profile       Choice   `json:"profile"`
			}
			if err := json.Unmarshal(public, &metadata); err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "get", "patch":
				if metadata.DefaultProfile.Current.ID != "default" || metadata.DefaultProfile.Current.Name != redact.Redacted {
					t.Fatal("default summary disappeared or was not masked")
				}
			case "index":
				if len(metadata.Subscriptions) != 1 || metadata.Subscriptions[0].ID != "synthetic-source" || metadata.Subscriptions[0].Name != redact.Redacted {
					t.Fatal("source choices disappeared or were not masked")
				}
			case "abandon-plan":
				if metadata.Profile.ID != record.ProfileID || metadata.Profile.Name != redact.Redacted {
					t.Fatal("abandon plan disappeared or was not masked")
				}
			}
			if operation != "abandon-plan" && !bytes.Contains(public, []byte(`"username":"operator"`)) {
				t.Fatal("public router username disappeared")
			}
			if operation == "get" || operation == "patch" {
				if !bytes.Contains(public, []byte(`"has_password":true`)) {
					t.Fatal("password-presence metadata changed")
				}
			}
			private, err := os.ReadFile(path)
			if err != nil || !bytes.Contains(private, []byte(secret)) || !bytes.Contains(before, []byte(secret)) {
				t.Fatal("private credential was lost", err)
			}
			if operation != "patch" && !bytes.Equal(before, private) {
				t.Fatal("read-only response rewrote the private session")
			}
		})
	}
}

func TestReviewC36MissingOwnersReturn503WithoutCommit(t *testing.T) {
	for _, owner := range []string{"dns", "installation", "router"} {
		t.Run(owner, func(t *testing.T) {
			var s *Service
			var ctx context.Context
			body := json.RawMessage(`{}`)
			operation := "check"
			if owner == "router" {
				s, ctx = preparedFixture(t, "router")
				s.owners.VerifyRouter = nil
				if _, err := s.Prepare(ctx, testSessionID, json.RawMessage(`{"domain_rules":{"direct":[],"proxy":[]}}`), "http://localhost"); err != nil {
					t.Fatal(err)
				}
				s.owners.Installation = installedOwner()
				s.owners.Installation.GatewayStatus = func(context.Context, *repo.Repository, string, string) (GatewayStatus, error) {
					return GatewayStatus{IPv4Forwarding: &ForwardingStatus{Current: "1", Persisted: pointer(true)}, TUN: &TUNStatus{Configured: pointer(true), Present: pointer(true)}}, nil
				}
				s.owners.ReadRouteContent = func(context.Context, json.RawMessage, routers.RuleSet, *routers.LibraryRule, RouteReadOptions) (string, error) {
					return "", nil
				}
				_, _, err := s.repository.UpdateProfileTransaction(ctx, "setup_"+testSessionID[:20], func(_ context.Context, profile *jsonx.Object) (*jsonx.Object, error) {
					profile.Set("routers", jsonx.Obj("items", []any{jsonx.Obj("id", "synthetic-router", "name", "Router", "type", "ikuai", "proxy_gateway_agent_id", "synthetic-mihomo", "dns_agent_id", "synthetic-oxidns")}))
					return nil, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				operation, body = "finish", json.RawMessage(`{"router_mode":"managed","router_id":"synthetic-router"}`)
			} else {
				s, ctx = serviceFixture(t, "router")
				s.owners = ExternalOwners{}
			}
			store := storeFor(s.repository)
			record, err := store.read(ctx, testSessionID)
			if err != nil {
				t.Fatal(err)
			}
			record.Prepared = pointer(true)
			if err := store.write(ctx, record); err != nil {
				t.Fatal(err)
			}
			path, _ := store.path(testSessionID)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			fingerprint, err := s.repository.Store().Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			if owner == "dns" {
				_, err = s.Prepare(ctx, testSessionID, body, "http://localhost")
			} else {
				_, err = s.Action(ctx, testSessionID, operation, body, "")
			}
			var failure *Error
			if !errors.As(err, &failure) || failure.Status != 503 || failure.Message != msgAwaitOwner {
				t.Fatalf("missing %s owner: %v", owner, err)
			}
			after, readErr := os.ReadFile(path)
			afterFingerprint, fingerprintErr := s.repository.Store().Fingerprint()
			if readErr != nil || fingerprintErr != nil || !bytes.Equal(before, after) || fingerprint != afterFingerprint {
				t.Fatal("unavailable owner changed session or DB", readErr, fingerprintErr)
			}
		})
	}
}

func TestReviewC36PrepareRevisionAndStoredOwnership(t *testing.T) {
	s, ctx := preparedFixture(t, "router")
	store := storeFor(s.repository)
	record, err := store.read(ctx, testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := s.repository.GetProfile(ctx, record.ProfileID)
	if err != nil {
		t.Fatal(err)
	}
	previous := profile.GetInt("_revision")
	body := json.RawMessage(`{"domain_rules":{"direct":[],"proxy":[]}}`)
	for attempt := range 2 {
		if _, err := s.Prepare(ctx, testSessionID, body, "http://localhost"); err != nil {
			t.Fatal(err)
		}
		profile, err = s.repository.GetProfile(ctx, record.ProfileID)
		if err != nil {
			t.Fatal(err)
		}
		record, err = store.read(ctx, testSessionID)
		if err != nil {
			t.Fatal(err)
		}
		revision, err := s.repository.ConfigRevision(ctx, record.ProfileID)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(profile)
		if err != nil {
			t.Fatal(err)
		}
		full, err := OwnedDigest(raw, false)
		if err != nil {
			t.Fatal(err)
		}
		without, err := OwnedDigest(raw, true)
		if err != nil {
			t.Fatal(err)
		}
		if profile.GetInt("_revision") != previous+1 || record.ProfileRevision == nil || *record.ProfileRevision != model.Int(previous+1) || record.ConfigRevision == nil || *record.ConfigRevision != model.Int(revision) || text(record.OwnedDigest) != full || text(record.OwnedDigestWithoutBase) != without {
			t.Fatalf("prepare attempt %d did not preserve revision/ownership contract", attempt)
		}
	}
}

func TestReviewC36ShutdownAfterExternalReadPreventsCommit(t *testing.T) {
	for _, operation := range []string{"prepare", "abandon"} {
		t.Run(operation, func(t *testing.T) {
			s, base := preparedFixture(t, "gateway")
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: time.Second})
			runtime.Start(t.Context())
			defer runtime.Shutdown(t.Context())
			ctx := runtime.BindRequest(base)
			path, _ := storeFor(s.repository).path(testSessionID)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var commits int64
			called := false
			stop := func() {
				called = true
				commits = s.repository.CommitCount()
				runtime.Shutdown(context.Background())
			}
			if operation == "prepare" {
				original := s.owners.DNS.PrepareMihomoDNS
				s.owners.DNS.PrepareMihomoDNS = func(document *yamlx.Node) (*yamlx.Node, error) {
					result, err := original(document)
					stop()
					return result, err
				}
				_, err = s.Prepare(ctx, testSessionID, json.RawMessage(`{"domain_rules":{"direct":[],"proxy":[]}}`), "http://localhost")
			} else {
				s.owners.Installation = &InstallationOwner{HasInstallation: func(context.Context, *repo.Repository, string) (bool, error) { stop(); return false, nil }}
				_, err = s.Action(ctx, testSessionID, "abandon", json.RawMessage(`{}`), "")
			}
			after, readErr := os.ReadFile(path)
			if !called || !errors.Is(err, context.Canceled) || readErr != nil || !bytes.Equal(before, after) || commits != s.repository.CommitCount() {
				t.Fatalf("post-read shutdown crossed a commit point: called=%v err=%v read=%v", called, err, readErr)
			}
		})
	}
}
