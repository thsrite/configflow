package routers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
)

func TestRouterFakePythonScriptHelpers(t *testing.T) {
	home, _ := os.UserHomeDir()
	python := filepath.Join(home, "cf-test/.venv/bin/python")
	if _, err := os.Stat(python); err != nil {
		t.Skip("Python helper unavailable")
	}
	wd, _ := os.Getwd()
	root := filepath.Clean(filepath.Join(wd, "../../.."))
	initial := r07State("legacy", 5)
	op := r07PlannedOperation(t, initial, 6)
	f := routerfake.New(initial.Tables)
	srv := httptest.NewServer(f)
	defer srv.Close()
	d := routers.NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	undo, err := d.Apply(context.Background(), rawValue(op))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Apply(context.Background(), undo); err != nil {
		t.Fatal(err)
	}
	if _, err = d.FetchAddresses(context.Background(), routers.NATSrcList); err != nil {
		t.Fatal(err)
	}
	if _, err = d.OccupiedAddressNames(context.Background(), []string{"cf-.*"}, nil); err != nil {
		t.Fatal(err)
	}
	scripts := []string{}
	for _, call := range f.Calls() {
		if call.Path != "/execute" {
			continue
		}
		data, _ := json.Marshal(call.Body)
		var body struct{ Script string }
		_ = json.Unmarshal(data, &body)
		if !slices.Contains(scripts, body.Script) {
			scripts = append(scripts, body.Script)
		}
	}
	seed := f.Snapshot().Tables
	for i := 0; i < 501; i++ {
		seed["/ip/firewall/address-list"] = append(seed["/ip/firewall/address-list"], routerfake.Row{".id": fmt.Sprintf("*%X", 0xF000+i), "list": routers.NATSrcList, "address": fmt.Sprintf("198.18.%d.%d", i/256, i%256), "comment": "中文;x=1", "disabled": "false", "creation-time": "synthetic"})
	}
	for _, script := range scripts {
		t.Run(scriptClass(script), func(t *testing.T) {
			f.Reset(routerfake.State{Tables: seed})
			payload := rawValue(map[string]any{"script": script, "as-string": ""})
			response, err := http.Post(srv.URL+"/rest/execute", "application/json", bytes.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if ret, ok := result["ret"].(string); ok {
				result["ret"] = strings.ReplaceAll(ret, "\r", "")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, python, filepath.Join(wd, "testdata/r07_fake_reference.py"))
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "PYTHONPATH=.")
			cmd.Stdin = bytes.NewReader(rawValue(map[string]any{"script": script, "tables": seed}))
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Python interpreter: %v %s", err, output)
			}
			var expected struct {
				Result any
				Tables map[string][]routerfake.Row
			}
			if err = json.Unmarshal(output, &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(r07Comparable(t, expected.Result), r07Comparable(t, result)) {
				t.Fatalf("script output %s", jsonDiff(r07Comparable(t, expected.Result), r07Comparable(t, result)))
			}
			if !reflect.DeepEqual(r07Comparable(t, expected.Tables), r07Comparable(t, f.Snapshot().Tables)) {
				t.Fatal("Python and Go script interpreters changed different rows")
			}
		})
	}
}
func scriptClass(script string) string {
	switch {
	case strings.Contains(script, "cf-digest:"):
		return "digest"
	case strings.Contains(script, "cf-rows:"):
		return "fetch"
	case strings.Contains(script, "cf-names:"):
		return "names"
	case strings.Contains(script, "address-list/add"):
		return "address_batch"
	default:
		return "nat_restore"
	}
}
