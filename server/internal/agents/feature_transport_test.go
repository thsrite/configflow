package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

func TestNativeFeatureRequestsKeepRawWireContract(t *testing.T) {
	const payload = `{"value":"中文","n":9007199254740993}`
	const wire = payload // compact UTF-8; the exact large integer survives
	tests := []struct {
		name, method, path, body string
		timeout                  Timeout
		call                     func(context.Context, *Manager, *model.Agent) error
	}{
		{"probe", http.MethodPost, "/api/domain-probe", wire, Seconds(200), func(ctx context.Context, m *Manager, a *model.Agent) error {
			_, err := m.ProbeDomainsModel(ctx, a, json.RawMessage(payload), Timeout{})
			return err
		}},
		{"region", http.MethodPost, "/api/region-check", wire, Seconds(120), func(ctx context.Context, m *Manager, a *model.Agent) error {
			_, err := m.RegionCheckModel(ctx, a, json.RawMessage(payload))
			return err
		}},
		{"targets", http.MethodGet, "/api/region-check/targets", "", Seconds(15), func(ctx context.Context, m *Manager, a *model.Agent) error {
			_, err := m.RegionTargetsModel(ctx, a)
			return err
		}},
		{"status", http.MethodGet, "/api/return-routing", "", Seconds(15), func(ctx context.Context, m *Manager, a *model.Agent) error {
			_, err := m.ReturnRoutingStatusModel(ctx, a)
			return err
		}},
		{"apply", http.MethodPut, "/api/return-routing", wire, Seconds(60), func(ctx context.Context, m *Manager, a *model.Agent) error {
			_, err := m.ReturnRoutingApplyModel(ctx, a, json.RawMessage(payload))
			return err
		}},
		{"remove", http.MethodDelete, "/api/return-routing?restore_legacy=1", "", Seconds(60), func(ctx context.Context, m *Manager, a *model.Agent) error {
			_, err := m.ReturnRoutingRemoveModel(ctx, a, true)
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			a, err := e.m.ReadAgent(e.ctx, e.id(), false)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			e.fake.set(func(r Request) (*Response, error) {
				calls++
				if r.Method != tc.method || !strings.HasSuffix(r.URL, tc.path) || string(r.Body) != tc.body || r.Timeout != tc.timeout {
					t.Fatalf("raw request: %+v body=%q", r, r.Body)
				}
				if r.Header.Get("Authorization") != "Bearer "+a.Token || r.Header.Get("Content-Type") != map[bool]string{true: "application/json", false: ""}[tc.body != ""] {
					t.Fatalf("headers: %v", r.Header)
				}
				if tc.body == "" && r.Body != nil {
					t.Fatal("bodyless request sent a JSON body")
				}
				return &Response{StatusCode: 200, Body: []byte(`{"success":true,"targets":[],"results":[]}`)}, nil
			})
			if err := tc.call(e.ctx, e.m, a); err != nil || calls != 1 {
				t.Fatal(err, calls)
			}
		})
	}
}

func TestNativeFeatureWritesFollowRuntimeLifetime(t *testing.T) {
	e := newEnv(t)
	a, err := e.m.ReadAgent(e.ctx, e.id(), false)
	if err != nil {
		t.Fatal(err)
	}
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(e.ctx)
	defer runtime.Shutdown(context.Background())
	bound := runtime.BindRequest(e.ctx)
	request, disconnect := context.WithCancel(bound)
	disconnect()
	calls := 0
	e.fake.set(func(Request) (*Response, error) {
		calls++
		return &Response{StatusCode: 200, Body: []byte(`{"success":true}`)}, nil
	})
	if _, err := e.m.ReturnRoutingApplyModel(request, a, json.RawMessage(`{}`)); err != nil || calls != 1 {
		t.Fatal("disconnect dropped write", err, calls)
	}
	runtime.Shutdown(context.Background())
	if _, err := e.m.ReturnRoutingRemoveModel(bound, a, false); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("shutdown sent device write", err, calls)
	}
}
