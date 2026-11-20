package agents_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

type n810HeartbeatRule struct {
	Original struct {
		Input  string         `json:"input"`
		Status int            `json:"python_status"`
		Body   map[string]any `json:"python_body"`
	} `json:"original"`
	SHA    string         `json:"sha256"`
	Status int            `json:"native_status"`
	Body   map[string]any `json:"native_body"`
}

func heartbeatPolicy(t *testing.T, input string, status int, body map[string]any) (int, any, bool) {
	t.Helper()
	raw, err := os.ReadFile("testdata/n810_heartbeat_policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []n810HeartbeatRule
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		original := map[string]any{"input": row.Original.Input, "python_status": row.Original.Status, "python_body": row.Original.Body}
		encoded, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(encoded)) != row.SHA {
			t.Fatal("original Python heartbeat row changed")
		}
		if row.Original.Input != input {
			continue
		}
		oldRaw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		var oldBody map[string]any
		if err = json.Unmarshal(oldRaw, &oldBody); err != nil {
			t.Fatal(err)
		}
		if status != row.Original.Status || !reflect.DeepEqual(oldBody, row.Original.Body) {
			t.Fatal("old heartbeat response no longer matches complete bound row")
		}
		return row.Status, row.Body, true
	}
	return status, body, false
}
func expectHeartbeatNative(t *testing.T, h heartbeatEnv, input string, status int, body map[string]any) {
	t.Helper()
	nativeStatus, nativeBody, found := heartbeatPolicy(t, input, status, body)
	if !found {
		t.Fatal("unbound heartbeat policy")
	}
	before, err := h.s.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	wantBodyJSON(t, h.s.Post(h.path(), httpxtest.Bearer(h.token), httpxtest.Body([]byte(input), "application/json")).WantStatus(nativeStatus), nativeBody)
	after, err := h.s.Repo.Store().Fingerprint()
	if err != nil || before != after {
		t.Fatal("invalid heartbeat changed repository", err)
	}
}
func TestN810HeartbeatPolicyDoesNotAcceptChangedInput(t *testing.T) {
	if _, _, found := heartbeatPolicy(t, `{"version":1}`, 500, map[string]any{"success": false, "message": "synthetic"}); found {
		t.Fatal("unbound row selected")
	}
}
