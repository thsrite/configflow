package installation

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents/agentfake"
	"configflow/internal/yamlx"
)

func TestPythonPublicationStateAndGeneratedArtifacts(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("CONFIGFLOW_DIFF=1")
	}
	for _, scenario := range []string{"normal", "first-failed", "uncertain-upload"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			f.enroll(t)
			f.prepareRun(t)
			opts := map[string]agentfake.Options{}
			switch scenario {
			case "first-failed":
				opts["mihomo"] = agentfake.Options{ActivationOutcome: "rolled_back"}
			case "uncertain-upload":
				opts["mihomo"] = agentfake.Options{Script: []agentfake.Fault{{Method: "POST", Path: "/api/deployments", LoseResponse: true}}}
			}
			goFake := f.fakePair(t, opts)
			pyDir := t.TempDir()
			out, e := exec.Command("cp", "-a", f.s.Repository.DataDir()+"/.", pyDir).CombinedOutput()
			if e != nil {
				t.Fatal(e, string(out))
			}
			pyFake := map[string]*agentfake.Agent{}
			dest := map[string]string{}
			for _, kind := range services {
				a, e := f.s.agent(f.ctx, f.r.AgentIDs[kind])
				must(t, e)
				o := opts[kind]
				o.AgentID = a.ID
				o.Token = a.Token
				o.ServiceType = kind
				fake := agentfake.New(o)
				srv := httptest.NewServer(fake)
				defer srv.Close()
				dest[a.Host+":"+stringInt(a.Port)] = srv.URL
				pyFake[kind] = fake
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			root, e := filepath.Abs("../../../..")
			must(t, e)
			cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "testdata/publication.py")
			cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "PYTHONDONTWRITEBYTECODE=1")
			cmd.Stdin = bytes.NewReader(raw(map[string]any{"directory": pyDir, "session_id": f.r.ID, "destinations": dest}))
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			expected, e := cmd.Output()
			if e != nil {
				t.Fatal(e, stderr.String())
			}
			err := f.s.RunPublication(f.ctx, f.r.ID)
			if scenario == "first-failed" {
				if err == nil {
					t.Fatal("failed publication succeeded")
				}
			} else {
				must(t, err)
			}
			var py struct {
				Session json.RawMessage `json:"session"`
				Public  json.RawMessage `json:"public"`
			}
			must(t, json.Unmarshal(expected, &py))
			compareNativeJSON(t, recordBytes(t, f), py.Session)
			got, e := f.s.Get(f.ctx, f.r.ID)
			must(t, e)
			compareNativeJSON(t, got, py.Public)
			for _, kind := range services {
				a, b := uploads(goFake[kind]), uploads(pyFake[kind])
				if len(a) != len(b) {
					t.Fatal("Python upload count differs", kind, len(a), len(b))
				}
				for i := range a {
					if a[i].Headers.Get("X-Activate") != b[i].Headers.Get("X-Activate") || a[i].Headers.Get("Authorization") != b[i].Headers.Get("Authorization") {
						t.Fatal("raw upload activation/authorization differs")
					}
					compareArchive(t, a[i], b[i])
				}
			}
		})
	}
}
func stringInt(n int) string { return fmt.Sprint(n) }
func compareNativeJSON(t *testing.T, a, b []byte) {
	t.Helper()
	var x, y any
	must(t, json.Unmarshal(a, &x))
	must(t, json.Unmarshal(b, &y))
	if !reflect.DeepEqual(x, y) {
		t.Fatalf("Python state mismatch\nGo: %s\nPython: %s", raw(x), raw(y))
	}
}
func archiveFiles(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	gzipReader, e := gzip.NewReader(bytes.NewReader(b))
	must(t, e)
	defer gzipReader.Close()
	r := tar.NewReader(gzipReader)
	out := map[string][]byte{}
	for {
		h, e := r.Next()
		if e == io.EOF {
			break
		}
		must(t, e)
		out[h.Name], e = io.ReadAll(r)
		must(t, e)
	}
	return out
}
func compareArchive(t *testing.T, a, b agentfake.Call) {
	t.Helper()
	af, bf := archiveFiles(t, a.Body), archiveFiles(t, b.Body)
	if len(af) != len(bf) {
		t.Fatal("archive file set differs")
	}
	for path, body := range af {
		other, ok := bf[path]
		if !ok {
			t.Fatal("Python file missing", path)
		}
		if path == "manifest.json" {
			var x, y agentfake.Manifest
			must(t, json.Unmarshal(body, &x))
			must(t, json.Unmarshal(other, &y))
			if x.AgentID != y.AgentID || x.DeploymentID != y.DeploymentID || x.ServiceType != y.ServiceType || x.ConfigPath != y.ConfigPath || x.ProtocolVersion != y.ProtocolVersion || x.FinalConfig != y.FinalConfig || len(x.Files) != len(y.Files) {
				t.Fatal("manifest identity differs")
			}
			for i, f := range x.Files {
				g := y.Files[i]
				if f.Path != g.Path || f.Role != g.Role || f.Name != g.Name {
					t.Fatal("manifest order or role differs")
				}
			}
			continue
		}
		if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
			x, e := yamlx.Parse(body)
			must(t, e)
			y, e := yamlx.Parse(other)
			must(t, e)
			if !yamlx.Equal(x, y) {
				t.Fatalf("generated YAML differs %s", path)
			}
		} else if !bytes.Equal(body, other) {
			t.Fatal("generated file differs", path)
		}
	}
}
