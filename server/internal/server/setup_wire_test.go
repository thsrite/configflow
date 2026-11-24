package server

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	apionboarding "configflow/internal/api/gatewayonboarding"
	apisetup "configflow/internal/api/setup"
	"configflow/internal/gateway/onboarding"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/setup"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

func TestSetupServerOwnerRegistrationRemovesUnavailable(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{RepoOptions: RepoOptions(), Auth: true})
	setupHandler := apisetup.NewHandler(srv.Repo, nil)
	setupHandler.Register(srv.App.Router)
	onboardingHandler := &apionboarding.Handler{Repo: srv.Repo, DNSLoop: onboarding.CheckDNSLoop}
	onboardingHandler.Register(srv.App.Router)
	admin := srv.Login()
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const profileID = "setup_aaaaaaaaaaaaaaaaaaaa"
	srv.Post("/api/setup", httpxtest.JSON(map[string]string{"id": id, "goal": "gateway", "name": "Synthetic"}), admin).WantStatus(200)
	path := "/api/setup/" + id + "/prepare"
	body := map[string]any{"domain_rules": map[string]any{"direct": []string{}, "proxy": []string{}}, "local_dns": "https://192.0.2.53/dns-query", "controller_secret": "synthetic-controller"}
	before := srv.Repo.CommitCount()
	srv.Post(path, httpxtest.JSON(body), admin).WantStatus(503)
	if srv.Repo.CommitCount() != before {
		t.Fatal("missing owner committed")
	}
	// Exercise the actual application wiring, including both C32 and A21. No
	// no-op DNS/adoption callbacks are installed by this success control.
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	app, err := New(context.Background(), Options{Repo: srv.Repo, Runtime: rt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Agents.Close)
	setupHandler.Repo = srv.Repo
	setupHandler.Owners = &app.SetupOwners
	onboardingHandler.Repo = srv.Repo
	onboardingHandler.AdoptOwnedWrite = setup.AdoptOwnedWrite
	ctx := srv.Context()
	err = srv.Repo.UpdateShared(ctx, func(_ context.Context, c *model.Shared) error {
		c.Nodes = []model.Node{testjson.Decode[model.Node](t, map[string]any{
			"id": "n", "name": "Synthetic", "type": "ss", "server": "192.0.2.2", "port": 443,
			"cipher": "aes-128-gcm", "password": "synthetic-node-password",
		})}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Post("/api/setup/"+id+"/source", httpxtest.JSON(map[string]any{"node_ids": []string{"n"}}), admin).WantStatus(200)
	srv.Post(path, httpxtest.JSON(body), admin).WantStatus(200)
	profile, err := srv.Repo.Profile(ctx, profileID)
	if err != nil {
		t.Fatal(err)
	}
	rawProfile, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	parsedProfile, err := testjson.Parse(rawProfile)
	if err != nil {
		t.Fatal(err)
	}
	customConfigVal, _ := testjson.Path(parsedProfile, "mihomo", "custom_config")
	customConfig, _ := customConfigVal.(string)
	node, err := yamlx.Parse([]byte(customConfig))
	if err != nil || yamlx.Get(yamlx.Get(node, "dns"), "nameserver") == nil {
		t.Fatal("real DNS leaf did not fill nameservers", err)
	}
	sessionPath := filepath.Join(srv.DataDir, "setup-sessions", id+".json")
	readSession := func() []byte {
		t.Helper()
		raw, err := os.ReadFile(sessionPath)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	oldSession := readSession()
	// A deliberate change goes through the production adoption hook and C36's
	// LevelSetupSession owner after A21 releases repo L3.
	form := map[string]any{"rule_set_ids": []string{}, "direct_rule_set_ids": []string{}, "proxy_rule_set_ids": []string{}, "local_dns": "https://192.0.2.54/dns-query"}
	route := "/api/profiles/" + profileID + "/gateway-onboarding"
	previous := int64(profile.Revision)
	srv.Post(route, httpxtest.JSON(form), admin).WantStatus(200)
	profile, err = srv.Repo.Profile(ctx, profileID)
	if err != nil || int64(profile.Revision) != previous+1 {
		t.Fatal("embedded write did not advance once", err)
	}
	adopted := readSession()
	if bytes.Equal(adopted, oldSession) {
		t.Fatal("server did not register the real adoption owner")
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	var record setup.Record
	if err := json.Unmarshal(adopted, &record); err != nil {
		t.Fatal(err)
	}
	full, err := setup.OwnedDigest(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	without, err := setup.OwnedDigest(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if record.OwnedDigest == nil || *record.OwnedDigest != full || record.OwnedDigestWithoutBase == nil || *record.OwnedDigestWithoutBase != without {
		t.Fatal("real owner did not adopt both stored-profile proofs")
	}
	// Python advances revision even for an identical save; it remains a +1
	// write and the real owner must run again.
	srv.Post(route, httpxtest.JSON(form), admin).WantStatus(200)
	again, err := srv.Repo.Profile(ctx, profileID)
	if err != nil || int64(again.Revision) != previous+2 || bytes.Equal(adopted, readSession()) {
		t.Fatal("repeated save did not advance/adopt exactly once", err)
	}
	if record.DigestVersion == nil || *record.DigestVersion != 1 {
		t.Fatalf("expected native digest_version=1, got %v", record.DigestVersion)
	}
	if os.Getenv("CONFIGFLOW_DIFF") == "1" {
		assertNativeStoredOwnership(t, raw, full, without)
	}
}

func assertNativeStoredOwnership(t *testing.T, profile json.RawMessage, full, without string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "-c", `import json,sys;from backend.common.setup_flow import owned_digest
p=json.load(sys.stdin);json.dump([owned_digest(p),owned_digest(p,base=False)],sys.stdout)`)
	cmd.Dir = "../../.."
	cmd.Env = append(os.Environ(), "PYTHONPATH=.")
	cmd.Stdin = bytes.NewReader(profile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python stored ownership: %v %s", err, out)
	}
	var legacy []string
	if err := json.Unmarshal(out, &legacy); err != nil || len(legacy) != 2 || legacy[0] == full || legacy[1] == without {
		t.Fatalf("expected native digest to differ from Python encoding: %s %v", out, err)
	}
}
