package agents

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"configflow/internal/model"
)

func TestNativeOperationReservations(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		upgrade bool
		follow  bool
	}{
		{`{"id":"target","latest_upgrade":{"status":"downloading"},"dns_follow_state":{"status":"preparing"}}`, true, true},
		{`{"id":"target","latest_upgrade":{"status":"succeeded"},"dns_follow_state":{"status":"idle"}}`, false, false},
	} {
		var a model.Agent
		if err := json.Unmarshal([]byte(tc.raw), &a); err != nil {
			t.Fatal(err)
		}
		if UpgradeInProgress(&a) != tc.upgrade {
			t.Fatalf("upgrade reservation %s: got %v want %v", tc.raw, UpgradeInProgress(&a), tc.upgrade)
		}
		if DNSFollowInProgress([]model.Agent{a}, "target") != tc.follow {
			t.Fatalf("follow reservation %s: got %v want %v", tc.raw, DNSFollowInProgress([]model.Agent{a}, "target"), tc.follow)
		}
	}
}

func TestReceiptHookUsesCapturedModelsOnce(t *testing.T) {
	calls := 0
	a := &model.Agent{ID: "target"}
	d := &model.Deployment{DeploymentID: "candidate"}
	m := &Manager{receiptMatches: func(agent *model.Agent, task *model.Deployment) bool {
		calls++
		if agent != a || task != d {
			t.Fatal("hook did not receive captured model")
		}
		return true
	}}
	ok, err := m.candidateReceiptMatches(a, d)
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
	native, err := e.m.DeploymentRequestModel(e.ctx, e.agent, http.MethodGet, "deployments/native-receipt", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	newBytes, err := json.Marshal(native)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(newBytes, reply) {
		t.Fatalf("receipt wire bytes changed: %s", newBytes)
	}
}
