package agents

import (
	"bytes"
	"configflow/internal/model"
	"encoding/json"
	"net/http"
	"testing"
)

func TestNativeOperationReservationsMatchFacade(t *testing.T) {
	for _, raw := range []string{`{"id":"target","latest_upgrade":{"status":"downloading"},"dns_follow_state":{"status":"preparing"}}`, `{"id":"target","latest_upgrade":{"status":"succeeded"},"dns_follow_state":{"status":"idle"}}`} {
		old := nativeTestLegacyObject(t, raw)
		var a model.Agent
		if err := json.Unmarshal([]byte(raw), &a); err != nil {
			t.Fatal(err)
		}
		if UpgradeInProgress(&a) != UpgradeBusy(old) {
			t.Fatal("upgrade reservation changed")
		}
		if DNSFollowInProgress([]model.Agent{a}, "target") != DNSFollowBusy([]any{old}, "target") {
			t.Fatal("follow reservation changed")
		}
	}
}

func TestReceiptHookUsesCapturedModelsOnce(t *testing.T) {
	calls := 0
	a := &model.Agent{ID: "target"}
	d := &model.Deployment{DeploymentID: "candidate"}
	ResetHooks()
	t.Cleanup(ResetHooks)
	RegisterHooks(Hooks{ReceiptMatches: func(agent *model.Agent, task *model.Deployment) bool {
		calls++
		if agent != a || task != d {
			t.Fatal("hook did not receive captured model")
		}
		return true
	}})
	ok, err := (&Manager{}).candidateReceiptMatches(a, d)
	if err != nil || !ok || calls != 1 {
		t.Fatal(ok, err, calls)
	}
}

func TestDeploymentModelKeepsRawRequestAndReceiptBytes(t *testing.T) {
	e := newEnv(t)
	reply := []byte(`{"success":true,"status":"ready","deployment_id":"native-receipt","future":{"counter":9223372036854775808}}`)
	e.fake.set(func(request Request) (*Response, error) {
		if request.Method != "GET" || len(request.Body) != 0 || request.Header.Get("Content-Type") != "" || request.Header.Get("Authorization") != "Bearer "+e.agent.Token {
			t.Fatal("request bytes/headers changed")
		}
		return &Response{StatusCode: 200, Body: reply}, nil
	})
	agentObj, err := agentObject(e.agent)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := e.m.DeploymentRequest(e.ctx, agentObj, http.MethodGet, "deployments/native-receipt", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	native, err := e.m.DeploymentRequestModel(e.ctx, e.agent, http.MethodGet, "deployments/native-receipt", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	oldBytes, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	newBytes, err := json.Marshal(native)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(oldBytes, newBytes) || !bytes.Equal(newBytes, reply) {
		t.Fatalf("receipt wire bytes changed: %s / %s", oldBytes, newBytes)
	}
}
