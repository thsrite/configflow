package draft

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/gateway"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/setupflow"
	"configflow/internal/yamlx"
)

func draftService(t *testing.T) *Service {
	t.Helper()
	gateway.Wire()
	mihomoenv.Wire()
	setupflow.Wire()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = r.UpdateProfileTransaction(context.Background(), "default", func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		c.Set("mihomo", jsonx.Obj("custom_config", "mixed-port: 7890\n", "linux_gateway", jsonx.Obj("enabled", false, "rule_set_ids", []any{}), "future", []any{jsonx.Obj("nested", nil)}))
		c.Set("oxidns", jsonx.Obj("custom_config", "plugins: []\n", "extra", 7))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Repository: r}
}
func opBody(t *testing.T, raw string) Request {
	t.Helper()
	var body Request
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	return body
}
func state(t *testing.T, s *Service) string {
	t.Helper()
	v, err := s.Repository.ExportAll(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := jsonx.Compact(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDraftRuntimeAndDisconnect(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		for _, restore := range []bool{false, true} {
			t.Run(string(encode([]bool{stopped, restore})), func(t *testing.T) {
				s := draftService(t)
				op := Operation{ProfileID: "default", BaseURL: "http://localhost", Write: true, Body: opBody(t, `{"enabled":true,"rule_set_ids":[]}`)}
				if restore {
					if _, err := s.Execute(context.Background(), op); err != nil {
						t.Fatal(err)
					}
					rev, _ := s.Repository.ConfigRevision(context.Background(), "default")
					op.Restore = true
					op.Preview = true
					op.Body = opBody(t, string(encode(map[string]int64{"expected_config_revision": rev})))
					raw, err := s.Execute(context.Background(), op)
					if err != nil {
						t.Fatal(err)
					}
					var p Preview
					if err = json.Unmarshal(raw, &p); err != nil {
						t.Fatal(err)
					}
					op.Body["preview_token"] = encode(p.PreviewToken)
					op.Preview = false
				}
				before := state(t, s)
				runtime := lifecycle.NewRuntime(lifecycle.Options{Mode: lifecycle.ModeManual, Registry: lifecycle.NewRegistry()})
				runtime.Start(context.Background())
				request, cancel := context.WithCancel(context.Background())
				ctx := runtime.BindRequest(request)
				cancel()
				if stopped {
					runtime.Shutdown(context.Background())
				}
				_, err := s.Execute(ctx, op)
				after := state(t, s)
				if stopped {
					if !errors.Is(err, context.Canceled) || before != after {
						t.Fatalf("shutdown write err=%v changed=%v", err, before != after)
					}
				} else {
					if err != nil || before == after {
						t.Fatalf("disconnect lost write err=%v changed=%v", err, before != after)
					}
					runtime.Shutdown(context.Background())
				}
			})
		}
	}
}

func TestDraftGuardAtomicityAndUnknownMembers(t *testing.T) {
	s := draftService(t)
	ctx := context.Background()
	config, rev, err := s.Repository.GetPublishSnapshot(ctx, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	original := config.DeepCopy()
	op := Operation{ProfileID: "default", BaseURL: "http://localhost", Write: true, Preview: true, Body: opBody(t, string(encode(map[string]any{"settings": map[string]any{"enabled": true, "rule_set_ids": []string{}}, "expected_config_revision": rev})))}
	before := state(t, s)
	raw, err := s.Execute(ctx, op)
	if err != nil || before != state(t, s) {
		t.Fatalf("preview wrote: %v", err)
	}
	var p Preview
	_ = json.Unmarshal(raw, &p)
	op.Preview = false
	op.Body["preview_token"] = encode(p.PreviewToken)
	// System configuration changes do not increment the profile revision; the
	// complete input identity must still reject an otherwise current preview.
	_, err = s.Repository.UpdateSystemTransaction(ctx, func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		c.GetObject("system_config").Set("server_domain", "https://changed.example.com")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before = state(t, s)
	if _, err = s.Execute(ctx, op); !errors.Is(err, repo.ErrInUse) || before != state(t, s) {
		t.Fatal("stale inputs committed", err)
	}
	op.Preview = true
	raw, err = s.Execute(ctx, op)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &p)
	op.Preview = false
	op.Body["preview_token"] = encode(p.PreviewToken)
	if _, err = s.Execute(ctx, op); err != nil {
		t.Fatal(err)
	}
	current, _, _ := s.Repository.GetPublishSnapshot(ctx, "default", true)
	point := current.GetObject("mihomo").GetObject(RestoreKey)
	if !jsonx.Equal(point.Value("mihomo"), original.Value("mihomo")) || !jsonx.Equal(point.Value("oxidns"), original.Value("oxidns")) {
		t.Fatal("recovery documents lost unknown members")
	}
	savedPoint := point.DeepCopy()
	op.Body = opBody(t, `{"enabled":false,"rule_set_ids":[]}`)
	if _, err = s.Execute(ctx, op); err != nil {
		t.Fatal(err)
	}
	current, _, _ = s.Repository.GetPublishSnapshot(ctx, "default", true)
	if !jsonx.Equal(savedPoint, current.GetObject("mihomo").Value(RestoreKey)) {
		t.Fatal("first recovery replaced")
	}
}

func TestDraftSessionEvidenceAndPrivacy(t *testing.T) {
	s := draftService(t)
	root := filepath.Join(s.Repository.DataDir(), "setup-sessions")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, strings.Repeat("a", 32)+".json")
	if err := os.WriteFile(file, []byte(`{"profile_id":"default","goal":[],"updated_at":"2026-01-01"}`), 0600); err != nil {
		t.Fatal(err)
	}
	// Turning on the gateway initially needs no legacy context; once it is on,
	// malformed wizard evidence cannot be mistaken for an ordinary profile.
	_, _, err := s.Repository.UpdateProfileTransaction(context.Background(), "default", func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		c.GetObject("mihomo").Set("linux_gateway", jsonx.Obj("enabled", true, "rule_set_ids", []any{}))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before := state(t, s)
	_, err = s.Execute(context.Background(), Operation{ProfileID: "default", Write: true, Body: opBody(t, `{"enabled":false}`)})
	if !errors.Is(err, setupflow.ErrReadSession) || before != state(t, s) {
		t.Fatal("invalid wizard evidence committed", err)
	}
	node, err := yamlx.FromJSON([]byte(`{"native_config":"password: opaque-secret","password":"synthetic-password","remote_dns":"https://operator:url-password@example.com/dns?token=query-secret","registration_key":"legacy-registration","message":"authorization: Bearer\ncontinued-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	shown := display(node, "settings")
	for _, secret := range []string{"opaque-secret", "synthetic-password", "url-password", "query-secret", "legacy-registration", "continued-secret"} {
		if strings.Contains(shown, secret) {
			t.Fatalf("preview leaked %s: %s", secret, shown)
		}
	}
}

func TestDraftNodeGraphs(t *testing.T) {
	for _, source := range []string{"a: &a [*a]\n", "a: 1\na: 2\n", "a: &a [one, two]\nb: [*a, *a]\n"} {
		node, err := yamlx.Parse([]byte(source))
		if err != nil {
			continue
		}
		changes := changes(yamlx.Map(), node, "Mihomo")
		if len(changes) == 0 {
			t.Fatal("accepted graph disappeared")
		}
	}
}

func FuzzGatewayDraft(f *testing.F) {
	for _, s := range []string{`{}`, `{"mihomo":{"linux_gateway_restore_point":{"version":1}}}`, `{"mihomo":{"linux_gateway":{"enabled":false,"rule_set_ids":[]}},"oxidns":{}}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !json.Valid([]byte(s)) {
			return
		}
		config := fields([]byte(s))
		if config == nil {
			return
		}
		before := encode(config)
		_ = point(config)
		_ = summary(config)
		candidate, err := prepare(config, Request{"enabled": json.RawMessage(`false`)}, false)
		if err == nil {
			if !json.Valid(encode(candidate)) {
				t.Fatal("invalid candidate")
			}
		}
		if !reflect.DeepEqual(before, encode(config)) {
			t.Fatal("mutated original")
		}
	})
}

func TestStoredSnapshotShapeAndRevisionForms(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status string
	}{{`{"expected_config_revision":true}`, "missing"}, {`{"expected_config_revision":1.0}`, "missing"}, {`{"expected_config_revision":-99999999999999999999999999}`, "missing"}, {`{"expected_config_revision":99999999999999999999999999}`, "changed"}, {`{"expected_config_revision":1}`, "ok"}} {
		err := checkRevision(opBody(t, tc.body), 1)
		switch tc.status {
		case "missing":
			if err == nil || err.Error() != msgRevisionMissing {
				t.Fatal(tc.body, err)
			}
		case "changed":
			if !errors.Is(err, repo.ErrInUse) {
				t.Fatal(tc.body, err)
			}
		default:
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, raw := range []string{`{"mihomo":{"linux_gateway_restore_point":{"version":1,"id":"a","created_at":null,"mihomo":{},"oxidns":{}}}}`, `{"mihomo":{"linux_gateway_restore_point":{"version":1,"id":"a","created_at":"t","mihomo":{"linux_gateway_restore_point":null},"oxidns":{}}}}`} {
		if point(fields([]byte(raw))) != nil {
			t.Fatal("malformed snapshot accepted", raw)
		}
	}
}

func TestShutdownBeforeRepositoryCommitRefusesDraft(t *testing.T) {
	s := draftService(t)
	rev, _ := s.Repository.ConfigRevision(context.Background(), "default")
	op := Operation{ProfileID: "default", Write: true, Preview: true, BaseURL: "http://localhost", Body: opBody(t, string(encode(map[string]any{"settings": map[string]any{"enabled": true, "rule_set_ids": []string{}}, "expected_config_revision": rev})))}
	raw, err := s.Execute(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	var p Preview
	_ = json.Unmarshal(raw, &p)
	op.Body["preview_token"] = encode(p.PreviewToken)
	op.Preview = false
	runtime := lifecycle.NewRuntime(lifecycle.Options{Mode: lifecycle.ModeManual, Registry: lifecycle.NewRegistry()})
	runtime.Start(context.Background())
	ctx := runtime.BindRequest(context.Background())
	before := state(t, s)
	hooks := gateway.Hooks()
	apply := hooks.ApplyLinuxGateway
	hooks.ApplyLinuxGateway = func(m, c *jsonx.Object, base string) error {
		err := apply(m, c, base)
		runtime.Shutdown(context.Background())
		return err
	}
	t.Cleanup(convert.RegisterMihomoHooks(hooks))
	if _, err = s.Execute(ctx, op); !errors.Is(err, context.Canceled) || before != state(t, s) {
		t.Fatal("shutdown during transaction committed", err)
	}
}

func TestBackupRuntimeDoesNotInvalidateAndConcurrentSaveConflicts(t *testing.T) {
	s := draftService(t)
	ctx := context.Background()
	rev, _ := s.Repository.ConfigRevision(ctx, "default")
	op := Operation{ProfileID: "default", Write: true, Preview: true, BaseURL: "http://localhost", Body: opBody(t, string(encode(map[string]any{"settings": map[string]any{"enabled": true, "rule_set_ids": []string{}}, "expected_config_revision": rev})))}
	raw, err := s.Execute(ctx, op)
	if err != nil {
		t.Fatal(err)
	}
	var p Preview
	_ = json.Unmarshal(raw, &p)
	op.Body["preview_token"] = encode(p.PreviewToken)
	op.Preview = false
	_, err = s.Repository.UpdateSystemTransaction(ctx, func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		c.GetObject("backup").Set("last_backup", "runtime-only")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, err := s.Execute(repo.WithScope(ctx), op); results <- err }()
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, repo.ErrInUse) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

func TestWrongTypedDraftSectionsCannotBecomeEmptySnapshots(t *testing.T) {
	for _, config := range []string{`{"mihomo":7,"oxidns":{}}`, `{"mihomo":null,"oxidns":{}}`, `{"mihomo":{},"oxidns":[]}`} {
		native := fields([]byte(config))
		before := encode(native)
		candidate, err := prepare(native, opBody(t, `{"enabled":true,"rule_set_ids":[]}`), false)
		if err == nil {
			err = remember(native, candidate)
		}
		if err == nil || string(before) != string(encode(native)) {
			t.Fatal("wrong-typed section became empty evidence", config, err)
		}
	}
}
