package agents

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/model"
)

// storedAgentForPython returns text as Go stores the Agent record, with empty
// HA summaries aligned (alignEmptyHASummaries).
func storedAgentForPython(t *testing.T, text string) []byte {
	t.Helper()
	var stored model.Agent
	if err := json.Unmarshal([]byte(text), &stored); err != nil {
		t.Fatal(err)
	}
	data, err := model.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	var record any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	alignEmptyHASummaries(record)
	data, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// alignEmptyHASummaries replaces an HA journal or deployment HA summary that
// holds only active_agent_id:null (Go always writes it) by {}: Python reads a
// non-empty summary as a publication and requires its status.
func alignEmptyHASummaries(record any) {
	object, _ := record.(map[string]any)
	align := func(holder map[string]any, key string) {
		if summary, ok := holder[key].(map[string]any); ok && len(summary) == 1 {
			if value, present := summary["active_agent_id"]; present && value == nil {
				holder[key] = map[string]any{}
			}
		}
	}
	if object == nil {
		return
	}
	align(object, "oxidns_ha_publication")
	if latest, ok := object["latest_deployment"].(map[string]any); ok {
		align(latest, "ha_publication")
	}
	if deployments, ok := object["deployments"].(map[string]any); ok {
		for _, deployment := range deployments {
			if deployment, ok := deployment.(map[string]any); ok {
				align(deployment, "ha_publication")
			}
		}
	}
}

// TestPublicAgentPython compares public records with the real Python implementation.
func TestPublicAgentPython(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1")
	}
	root, _ := filepath.Abs("../../../..")
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		home, _ := os.UserHomeDir()
		python = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	cmd := exec.Command(python, filepath.Join(root, "server/internal/api/agents/testdata/public_agent.py"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "PYTHONDONTWRITEBYTECODE=1")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		input.Close()
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			t.Log(stderr.String())
		}
	})
	scanner := bufio.NewScanner(output)
	cases := []string{
		`{"id":"device","token":"secret-token","applied_content":"secret-content","dns_follow_job":{"token":"secret-job"},"future":{"keep":[1,true,null]},"latest_deployment":null}`,
		`{"id":"device","deployments":{"one":{"status":"failed","message":"Connection refused","prepared_content":"secret-prepared","oxidns_validation_receipt":{"agent_id":"secret-receipt"},"events":[{"status":"failed","message":"timeout"}]}},"latest_deployment":{"status":"succeeded","message":"ok","prepared_content":"secret-latest"},"latest_upgrade":{"status":"failed","error":"Connection refused","message":"timeout","events":[{"status":"failed","message":"Connection refused"}]}}`,
		`{"oxidns_ha_publication":{"id":"pair","status":"failed","message":"失败","members":["a","b"],"children":{"a":"secret-child"},"private":"secret-private"},"latest_deployment":{"ha_publication":{"status":"failed","message":"同步失败"},"pending":true,"http_status":202,"prepared_content":"secret-content"}}`,
		`{"deployments":{},"latest_upgrade":{},"oxidns_ha_publication":{}}`,
		`{"latest_deployment":{"status":"queued","ha_publication":{},"prepared_content":"secret-empty-ha"},"deployments":{"empty":{"status":"ready","ha_publication":{}}}}`,
		`{}`,
		`{"id":"legacy","host":"192.0.2.8","port":8080,"version":"1.0.0","name":7,"dns_follow":null,"latest_upgrade":null}`,
		`{"id":"ha","oxidns_ha_publication":{"id":"pair","status":"succeeded","phase":"done","message":"已同步","owner_agent_id":"a","owner_deployment_id":"d","active_agent_id":"b","members":["a","b"],"warnings":["提示"],"routers":["router"],"children":{"a":"secret-child"},"private_key":"secret-key"}}`,
		`{"id":"deployment","deployments":{"old":{"status":"failed","message":"校验失败","prepared_content":{"token":"secret-prepared"},"oxidns_validation_receipt":{"token":"secret-receipt"}},"ready":{"status":"ready","message":"已准备","ha_publication":{}}},"latest_deployment":{"status":"ready","success":true,"pending":true,"http_status":202,"ha_publication":{}}}`,
		`{"id":"upgrade","latest_upgrade":{"status":"rolled_back","message":"Authorization: Bearer hidden","error":"Agent download write failed","events":[{"at":"2026-01-01T00:00:00","status":"failed","message":"password=hidden","unexpected":"secret-event"}]}}`,
		`{"id":"ha-deployment","latest_deployment":{"status":"ready","http_status":202,"pending":true,"prepared_content":{"password":"secret-password"},"ha_publication":{"status":"rollback_failed","message":"恢复失败","members":["a","b"]}}}`,
		`{"id":"old","latest_deployment":"legacy scalar","latest_upgrade":false,"future":{"name":"保留","list":[null,{},[]]}}`,
	}
	for i, text := range cases {
		for _, unread := range []bool{false, true} {
			record := parseJSONMap(t, []byte(text))
			// Python projects the record as Go stores it (O18 T1b, accepted):
			// undeclared members, nulls and values of another type are
			// dropped on load, and an empty HA summary is written with its
			// always-present active_agent_id (aligned to {} on both sides).
			request := map[string]any{"record": json.RawMessage(storedAgentForPython(t, text))}
			if unread {
				request["unread"] = []string{"deployments", "latest_deployment", "verification_runs"}
				for _, key := range []string{"deployments", "latest_deployment", "verification_runs"} {
					record[key] = testAgentHistoryNotRead
				}
			}
			wire, _ := json.Marshal(request)
			if _, err := input.Write(append(wire, '\n')); err != nil {
				t.Fatal(err)
			}
			if !scanner.Scan() {
				t.Fatalf("oracle: %v %s", scanner.Err(), stderr.String())
			}
			public := publicAgent(record)
			got, err := json.Marshal(public)
			if err != nil {
				t.Fatal(err)
			}
			var g, w any
			if err := json.Unmarshal(got, &g); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(scanner.Bytes(), &w); err != nil {
				t.Fatal(err)
			}
			alignEmptyHASummaries(g)
			if !reflect.DeepEqual(g, w) {
				t.Fatalf("case %d unread=%v: got %s want %s", i, unread, got, scanner.Bytes())
			}
			if strings.Contains(string(got), "secret-") {
				t.Fatalf("private data escaped: %s", got)
			}
			if !unread {
				original, _ := json.Marshal(record)
				var before any
				json.Unmarshal([]byte(text), &before)
				var after any
				json.Unmarshal(original, &after)
				if !reflect.DeepEqual(before, after) {
					t.Fatal("source record changed")
				}
			}
		}
	}
}

func TestPublicAgentOmitsUnreadAndSecrets(t *testing.T) {
	record := map[string]any{
		"id":              "device",
		"token":           "secret",
		"applied_content": "secret",
		"dns_follow_job":  map[string]any{"private": true},
		"deployments":     testAgentHistoryNotRead,
	}
	public := publicAgent(record)
	if len(public) != 1 || mapStr(public, "id") != "device" {
		t.Fatalf("public: %#v", public)
	}
	if len(record) != 5 {
		t.Fatal("source changed")
	}
}
