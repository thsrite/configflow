package server

import (
	"bytes"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/routers/routerfake"
	"configflow/internal/setup"
	"configflow/internal/testjson"
)

func TestSetupOwnersAllRegisteredByApp(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	app, err := New(context.Background(), Options{Repo: r, Runtime: rt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Agents.Close)
	owners := app.SetupOwners
	if owners.DNS == nil || owners.Installation == nil || owners.ReadRouteContent == nil || owners.ReadOfflineRouteContent == nil || owners.VerifyRouter == nil {
		t.Fatalf("production wiring left a setup owner unregistered: dns=%t installation=%t content=%t router=%t",
			owners.DNS != nil, owners.Installation != nil, owners.ReadRouteContent != nil, owners.VerifyRouter != nil)
	}
}

// verificationOutcome is the comparable result of one managed-router check.
type verificationOutcome struct {
	Blockers []string `json:"blockers,omitempty"`
	Empty    bool     `json:"empty"`
	Status   int      `json:"status,omitempty"`
	Message  string   `json:"message,omitempty"`
}

func TestVerifySetupRouterUsesRouterPlan(t *testing.T) {
	const fakeIP = "dns:\n  enhanced-mode: fake-ip\n  fake-ip-range: 198.18.0.0/16\n"
	gatewayAgent := map[string]any{"id": "synthetic-gateway", "name": "Synthetic gateway", "host": "192.0.2.4", "port": 8080, "service_type": "mihomo", "profile_id": "default", "deployment_method": "shell"}
	synced := routerfake.State{Tables: map[string][]routerfake.Row{"/ip/route": {{".id": "*2", "dst-address": "198.18.0.0/16", "gateway": "192.0.2.4", "routing-table": "main", "active": "true", "immediate-gw": "192.0.2.4%bridge", "disabled": "false", "comment": "cf"}}}}
	tests := []struct {
		name    string
		device  routerfake.State
		gateway string
		want    verificationOutcome
	}{
		{name: "synced", device: synced, gateway: "synthetic-gateway", want: verificationOutcome{Empty: true}},
		{name: "unsynced", gateway: "synthetic-gateway", want: verificationOutcome{Empty: false}},
		{name: "blockers", device: synced, want: verificationOutcome{Empty: true, Blockers: []string{"blocker"}}},
		{name: "device rejects", gateway: "synthetic-gateway", device: routerfake.State{Faults: []routerfake.Fault{{Method: "GET", Path: "/ip/route", Status: 401, Body: routerfake.Row{"error": 401, "message": "Unauthorized"}}}}, want: verificationOutcome{Status: 502}},
	}
	srv := httpxtest.New(t, httpxtest.Options{})
	err := srv.Repo.UpdateSystem(srv.Context(), func(_ context.Context, c *model.System) error {
		c.Agents = []model.Agent{testjson.Decode[model.Agent](t, gatewayAgent)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := routerfake.New(nil)
			fake.Reset(tt.device)
			device := httptest.NewServer(fake)
			defer device.Close()
			router := map[string]any{"id": "router-one", "name": "Synthetic", "type": "routeros", "endpoint": device.URL,
				"username": "synthetic-user", "password": "synthetic-password", "verify_tls": false,
				"proxy_gateway_agent_id": tt.gateway, "dns_agent_id": "", "manage_return_routing": false, "auto_rule_sync": false,
				"rule_sync_interval": 60, "ip_rule_sets": []string{}, "ros_policy_rule_sets": []string{}}
			config := map[string]any{"mihomo": map[string]any{"custom_config": fakeIP}}
			config["routers"] = map[string]any{"items": []any{router}}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(router)
			var r spec.Router
			if err = json.Unmarshal(b, &r); err != nil {
				t.Fatal(err)
			}
			m, err := agents.New(srv.Context(), srv.Repo)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(m.Close)
			got := outcome(t, func() (setup.RouterVerification, error) {
				return verifySetupRouter(srv.Context(), nil, m, srv.Repo, "default", raw, &r)
			})
			t.Logf("%s: %+v", tt.name, got)
			if got.Empty != tt.want.Empty || got.Status != tt.want.Status || len(got.Blockers) != len(tt.want.Blockers) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for _, call := range fake.Calls() {
				if call.Method != "GET" {
					t.Fatalf("verification wrote to the device: %+v", call)
				}
			}
			if os.Getenv("CONFIGFLOW_DIFF") == "1" {
				fake.Reset(tt.device)
				want := pythonRouterVerification(t, raw, gatewayAgent)
				if got.Empty != want.Empty || !slices.Equal(got.Blockers, want.Blockers) || got.Status != want.Status || got.Message != want.Message {
					t.Fatalf("Go %+v, Python %+v", got, want)
				}
			}
		})
	}
}

func outcome(t *testing.T, verify func() (setup.RouterVerification, error)) verificationOutcome {
	t.Helper()
	v, err := verify()
	var failure *setup.Error
	switch {
	case errors.As(err, &failure):
		return verificationOutcome{Status: failure.Status, Message: failure.Message}
	case err != nil:
		t.Fatal(err)
	}
	return verificationOutcome{Blockers: v.Blockers, Empty: v.Empty}
}

// pythonRouterVerification runs the Python setup completion's router check
// (backend/common/setup_completion.py finish, mode "managed") on the same
// configuration and device.
func pythonRouterVerification(t *testing.T, config []byte, agent map[string]any) verificationOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	const script = `import json,sys
import backend.routes.routers as routes
from backend.routes.routers import _driver,_inspect
from backend.routers.plan import build_plan
from backend.routers.base import RouterError
given=json.load(sys.stdin);config=given['config'];agent=given['agent']
routes._agent=lambda agent_id:dict(agent) if agent_id==agent['id'] else None
router=config['routers']['items'][0];driver=_driver(router)
try:
    _,desired,state,report=_inspect(config,router,driver)
    plan=build_plan(router,desired,state,report,report['return_routing'],driver)
    out={'blockers':plan['blockers'],'empty':plan['empty']}
except RouterError as error:
    out={'status':502,'message':str(error),'empty':False}
finally:
    driver.close()
json.dump(out,sys.stdout,ensure_ascii=False)`
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "-c", script)
	cmd.Dir = "../../.."
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "DATA_DIR="+t.TempDir())
	input, err := json.Marshal(map[string]any{"config": json.RawMessage(config), "agent": agent})
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Python router verification: %v %s", err, stderr.Bytes())
	}
	var out verificationOutcome
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("Python router verification output: %v %s", err, stdout.Bytes())
	}
	return out
}
