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

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

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
	}
	for i, text := range cases {
		for _, unread := range []bool{false, true} {
			recordValue, err := jsonx.Parse([]byte(text))
			if err != nil {
				t.Fatal(err)
			}
			record := recordValue.(*jsonx.Object)
			request := map[string]any{"record": json.RawMessage(text)}
			if unread {
				request["unread"] = []string{"deployments", "latest_deployment", "verification_runs"}
				for _, key := range []string{"deployments", "latest_deployment", "verification_runs"} {
					record.Set(key, repo.AgentHistoryNotRead)
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
	record := jsonx.Obj("id", "device", "token", "secret", "applied_content", "secret", "dns_follow_job", jsonx.Obj("private", true), "deployments", repo.AgentHistoryNotRead)
	public := publicAgent(record)
	if public.Len() != 1 || public.GetString("id") != "device" {
		t.Fatalf("public: %#v", public)
	}
	if record.Len() != 5 {
		t.Fatal("source changed")
	}
}
