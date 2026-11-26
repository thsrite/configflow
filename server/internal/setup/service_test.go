package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

const testSessionID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func serviceFixture(t *testing.T, goal string) (*Service, context.Context) {
	t.Helper()
	r, ctx := setupRepository(t)
	service, err := New(r)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"id": testSessionID, "goal": goal, "name": "Synthetic wizard"})
	if _, err := service.Create(ctx, raw); err != nil {
		t.Fatal(err)
	}
	return service, ctx
}

func TestSetupIndexCreationAndSourceResume(t *testing.T) {
	r, ctx := setupRepository(t)
	s, err := New(r)
	if err != nil {
		t.Fatal(err)
	}
	before := r.CommitCount()
	index, err := s.Index(ctx)
	if err != nil || !index.Suggested || len(index.Sessions) != 0 || r.CommitCount() != before {
		t.Fatal("landing state changed storage", err)
	}
	if _, err := os.Stat(filepath.Join(r.DataDir(), "setup-sessions")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only index created a wizard directory")
	}
	create := json.RawMessage(`{"id":"` + testSessionID + `","goal":"gateway","name":"Synthetic wizard"}`)
	first, err := s.Create(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	before = r.CommitCount()
	again, err := s.Create(ctx, create)
	if err != nil || !bytes.Equal(first, again) || r.CommitCount() != before {
		t.Fatal("creation retry wrote another profile/token", err)
	}
	for range 2 {
		if _, err := s.UpdateSources(ctx, testSessionID, json.RawMessage(`{"url":"https://example.com/sub?token=synthetic-private"}`)); err != nil {
			t.Fatal(err)
		}
	}
	shared, err := r.Shared(ctx)
	if err != nil || len(shared.Subscriptions) != 1 {
		t.Fatal("duplicated wizard source", err)
	}
	for _, body := range []string{`{"node_ids":[null]}`, `{"subscription_ids":null}`, `{"url":"file:///synthetic"}`, `{"node_ids":["missing"]}`} {
		before = r.CommitCount()
		if _, err := s.UpdateSources(ctx, testSessionID, json.RawMessage(body)); err == nil || r.CommitCount() != before {
			t.Fatal("invalid source committed", body, err)
		}
	}
	if _, err := s.Patch(ctx, testSessionID, json.RawMessage(`{"step":1}`)); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"step":3}`, `{"step":1.0}`, `{"profile_id":"default"}`} {
		if _, err := s.Patch(ctx, testSessionID, json.RawMessage(body)); err == nil {
			t.Fatal("progress bypass accepted", body)
		}
	}
}

func TestSetupRouterDraftIsPrivateAndLeavesProfileRevisionAlone(t *testing.T) {
	s, ctx := serviceFixture(t, "router")
	store := storeFor(s.repository)
	record, err := store.read(ctx, testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	record.Prepared, record.Step, record.MaxStep = pointer(true), 3, 3
	if err := store.write(ctx, record); err != nil {
		t.Fatal(err)
	}
	before := s.repository.CommitCount()
	response, err := s.SaveRouterDraft(ctx, testSessionID, json.RawMessage(`{"router":{"name":"Synthetic router","type":"ikuai","endpoint":"http://192.0.2.1","username":"operator","password":"synthetic-router-password"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(response, []byte("synthetic-router-password")) || !bytes.Contains(response, []byte(`"has_password":true`)) || s.repository.CommitCount() != before {
		t.Fatal("draft changed profile or exposed credentials", string(response))
	}
	path, _ := store.path(testSessionID)
	private, _ := os.ReadFile(path)
	if !bytes.Contains(private, []byte("synthetic-router-password")) {
		t.Fatal("stored router password was lost")
	}
	if _, err := s.SaveRouterDraft(ctx, testSessionID, json.RawMessage(`{"router":{"name":"Renamed","password":""}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveRouterDraft(ctx, testSessionID, json.RawMessage(`{"router":null}`)); err != nil {
		t.Fatal(err)
	}
	private, _ = os.ReadFile(path)
	if bytes.Contains(private, []byte(`"router_draft"`)) || bytes.Contains(private, []byte("synthetic-router-password")) {
		t.Fatal("explicit draft clearing retained the password")
	}
}

func TestWizardWriteOwnersPreserveRuntimeGeneration(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			s, base := serviceFixture(t, "gateway")
			err := s.repository.UpdateShared(base, func(_ context.Context, shared *model.Shared) error {
				return shared.SetJSON("nodes", json.RawMessage(`[{"id":"synthetic-node","name":"Synthetic","type":"ss","server":"192.0.2.10","port":443,"password":"synthetic-node-password","cipher":"aes-128-gcm"}]`))
			})
			if err != nil {
				t.Fatal(err)
			}
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(t.Context())
			t.Cleanup(func() { runtime.Shutdown(t.Context()) })
			request, cancel := context.WithCancel(base)
			defer cancel()
			ctx := runtime.BindRequest(request)
			if shutdown {
				runtime.Shutdown(t.Context())
			} else {
				cancel()
			}
			before := s.repository.CommitCount()
			store := storeFor(s.repository)
			path, _ := store.path(testSessionID)
			original, _ := os.ReadFile(path)
			_, err = s.UpdateSources(ctx, testSessionID, json.RawMessage(`{"node_ids":["synthetic-node"]}`))
			_, patchErr := s.Patch(ctx, testSessionID, json.RawMessage(`{"step":1}`))
			actual, _ := os.ReadFile(path)
			if shutdown {
				if !errors.Is(err, context.Canceled) || !errors.Is(patchErr, context.Canceled) || !bytes.Equal(original, actual) || s.repository.CommitCount() != before {
					t.Fatal("stopped runtime wrote wizard state", err, patchErr)
				}
			} else if err != nil || patchErr != nil || bytes.Equal(original, actual) || !strings.Contains(string(actual), "synthetic-node") {
				t.Fatal("client disconnect interrupted wizard save", err, patchErr)
			}
		})
	}
}
