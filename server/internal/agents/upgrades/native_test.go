package upgrades

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

func nativeAgent(t *testing.T, e *upgradeEnv) *model.Agent {
	t.Helper()
	a, err := e.m.ReadAgent(e.ctx, e.ident, true)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func TestN810PythonBugBoundary(t *testing.T) {
	data, err := os.ReadFile("testdata/n810_boundary_python.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Original json.RawMessage `json:"original"`
		SHA      string          `json:"row_sha256"`
		Native   string          `json:"native"`
	}
	if jsondecode.Unmarshal(data, &rows) != nil || len(rows) != 5 {
		t.Fatal("incomplete Python boundary archive")
	}
	for _, row := range rows {
		original, err := jsonx.Parse(row.Original)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := jsonx.Compact(original)
		if fmt.Sprintf("%x", sha256.Sum256(raw)) != row.SHA {
			t.Fatal("original Python row binding changed")
		}
		c := original.(*jsonx.Object)
		t.Run(c.GetString("name"), func(t *testing.T) {
			e := newUpgradeEnv(t)
			previousNow := now
			now = func() float64 { return c.GetFloat("now") }
			defer func() { now = previousNow }()
			// Bind only the fixture ID; all proof and body values come from the recorded Python input.
			agent := c.GetObject("agent").DeepCopy()
			agent.Set("id", e.ident)
			if state := agent.GetObject("latest_upgrade"); state != nil {
				state.Set("agent_id", e.ident)
			}
			pairs := []any{}
			for key, value := range agent.All() {
				if key != "id" {
					pairs = append(pairs, key, value)
				}
			}
			e.update(pairs...)
			info := c.GetObject("info").DeepCopy()
			info.Set("agent_id", e.ident)
			worker := c.GetObject("worker")
			t.Cleanup(SetRequestForTest(func(_ context.Context, _ *model.Agent, method, path string, _ []byte) (*agents.Response, error) {
				if strings.HasSuffix(path, "info") {
					return AgentResponse(info, 200), nil
				}
				if method == "POST" {
					t.Fatal("invalid architecture submitted")
				}
				return AgentResponse(worker, 200), nil
			}))
			before, err := e.repo.Store().Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			if row.Native == "pending" {
				if c.GetObject("python").GetObject("result").GetString("status") != "succeeded" {
					t.Fatal("Python bug positive control missing")
				}
				state, err := PollModel(e.ctx, e.m, e.ident)
				if err != nil || state.Status != "checking" {
					t.Fatal("wrong worker released task", state, err)
				}
			} else {
				if c.GetString("name") == "architecture number" {
					if c.GetObject("stored").Has("latest_upgrade") || c.GetObject("python").GetString("error_class") != "ValueError" {
						t.Fatal("Python numeric architecture refusal control missing")
					}
				} else if c.GetObject("stored").GetObject("latest_upgrade").GetString("status") != "queued" || c.GetObject("python").GetString("error_class") == "" {
					t.Fatal("Python reservation/error control missing")
				}
				_, err := StartModel(e.ctx, e.m, e.ident)
				if !IsValueError(err) || err.Error() != msgArtifactsNotReady {
					t.Fatal("missing architecture not refused", err)
				}
			}
			after, err := e.repo.Store().Fingerprint()
			if err != nil || after != before {
				t.Fatal("refused/unconfirmed proof wrote database", err)
			}
		})
	}
}
func TestN810CorruptGuardsRefuseWithoutWriteAndRecover(t *testing.T) {
	tests := []struct {
		field string
		raw   string
	}{
		{"latest_upgrade", `false`}, {"latest_upgrade", `[]`}, {"latest_upgrade", `"broken"`},
		{"latest_deployment", `0`}, {agents.HAField, `[]`}, {agents.HAField, `{"status":false}`},
	}
	for _, test := range tests {
		t.Run(test.field+test.raw, func(t *testing.T) {
			e := newUpgradeEnv(t)
			value, err := jsonx.Parse([]byte(test.raw))
			if err != nil {
				t.Fatal(err)
			}
			e.update(test.field, value)
			t.Cleanup(SetRequestForTest(func(context.Context, *model.Agent, string, string, []byte) (*agents.Response, error) {
				return AgentResponse(nil, 404), nil
			}))
			before, err := e.repo.Store().Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			_, err = StartModel(e.ctx, e.m, e.ident)
			var damaged *CorruptStateError
			if !errors.As(err, &damaged) {
				t.Fatal("damaged guard accepted", err)
			}
			after, err := e.repo.Store().Fingerprint()
			if err != nil || after != before {
				t.Fatal("damaged guard was overwritten", err)
			}
			// Simulate the documented offline repair: delete precisely the corrupt member.
			if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, rows *[]model.Agent) error { return (*rows)[0].Clear(test.field) }); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(SetRequestForTest(func(_ context.Context, _ *model.Agent, method, path string, _ []byte) (*agents.Response, error) {
				if method == "GET" {
					return AgentResponse(nil, 404), nil
				}
				return AgentResponse(jsonx.Obj(), 202), nil
			}))
			state, err := StartModel(e.ctx, e.m, e.ident)
			if err != nil || state.Status != "queued" {
				t.Fatal("explicit repair did not restore upgrade", state, err)
			}
		})
	}
}
func TestN810NativeShutdownAndDisconnect(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprint(shutdown), func(t *testing.T) {
			e := newUpgradeEnv(t)
			runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
			runner.Start(e.ctx)
			defer runner.Shutdown(context.Background())
			bound := runner.BindRequest(e.ctx)
			request, disconnect := context.WithCancel(bound)
			disconnect()
			calls := 0
			t.Cleanup(SetRequestForTest(func(_ context.Context, _ *model.Agent, method, path string, _ []byte) (*agents.Response, error) {
				calls++
				if shutdown {
					runner.Shutdown(context.Background())
				}
				if method == "GET" {
					return AgentResponse(nil, 404), nil
				}
				return AgentResponse(jsonx.Obj(), 202), nil
			}))
			before, _ := e.repo.Store().Fingerprint()
			state, err := StartModel(request, e.m, e.ident)
			if shutdown {
				if !errors.Is(err, context.Canceled) || state != nil {
					t.Fatal("shutdown accepted reservation", state, err)
				}
				after, _ := e.repo.Store().Fingerprint()
				if before != after {
					t.Fatal("shutdown wrote reservation")
				}
				callsBefore := calls
				if _, err := StartModel(bound, e.m, e.ident); !errors.Is(err, context.Canceled) || calls != callsBefore {
					t.Fatal("stopped generation sent request", err)
				}
			} else if err != nil || state.Status != "queued" {
				t.Fatal("disconnect interrupted durable update", state, err)
			}
		})
	}
}
func TestN810NativeWireBytesAndOriginalIdentity(t *testing.T) {
	e := newUpgradeEnv(t)
	var got []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, recorded{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(body)})
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()
	host, port, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")
	a := legacyAgent(jsonx.Obj("host", host, "port", jsonx.Number(port), "token", "synthetic-token"))
	if _, err := requestModel(e.ctx, e.m, a, "GET", "/api/upgrade-info", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := requestModel(e.ctx, e.m, a, "POST", "/api/update", struct {
		Version string `json:"version"`
		ID      string `json:"update_id"`
	}{"1.0", "job"}); err != nil {
		t.Fatal(err)
	}
	want := []recorded{{"GET", "/api/upgrade-info", "Bearer synthetic-token", "", ""}, {"POST", "/api/update", "Bearer synthetic-token", "application/json", `{"version": "1.0", "update_id": "job"}`}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("wire %v; want %v", got, want)
	}
	for _, raw := range []string{`7`, `null`, `false`} {
		if err := a.SetJSON("token", []byte(raw)); err != nil {
			t.Fatal(err)
		}
		if _, err := requestModel(e.ctx, e.m, a, "GET", "/api/update", nil); !errors.Is(err, agents.ErrAgentEndpoint) {
			t.Fatal("coerced stored credential", err)
		}
	}
	if len(got) != 2 {
		t.Fatal("invalid credential sent request")
	}
}
func TestN810ProtocolEvidence(t *testing.T) {
	tests := []string{`null`, `[]`, `{"agent_id":"a","agent_id":"b"}`, `{"agent_id":"a","future":{"x":1,"x":2}}`, `{"Agent_ID":"a","version":"1.0","migration_ready":true}`}
	for _, raw := range tests {
		if _, err := decodeInfo([]byte(raw)); err == nil {
			t.Fatal("invalid proof accepted", raw)
		}
	}
	for _, text := range []string{"版本1", "１.0", strings.Repeat("1", 65), "1.0\n"} {
		if validVersion(text) {
			t.Fatal("invalid machine version", text)
		}
	}
	for _, text := range []string{"1.0-go", "v2.0+alpha_1", strings.Repeat("1", 64)} {
		if !validVersion(text) {
			t.Fatal("valid machine version refused", text)
		}
	}
	var info Info
	if decodeProtocol([]byte(`{"agent_id":"a","migration_ready":1}`), &info) == nil {
		t.Fatal("non-boolean readiness proof accepted")
	}
}
func FuzzUpgradeNativeProtocol(f *testing.F) {
	for _, raw := range []string{`{}`, `null`, `[]`, `{"update_id":"job","agent_id":"a","status":"succeeded"}`, `{"agent_id":"a","version":"1.0","migration_ready":true}`, `{"update_id":"a","update_id":"b"}`} {
		f.Add([]byte(raw))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = decodeWorker(data)
		_, _ = decodeInfo(data)
		var report LegacyReport
		if err := decodeProtocol(data, &report); err == nil {
			encoded, err := json.Marshal(report)
			if err != nil || !json.Valid(encoded) {
				t.Fatal("valid report cannot roundtrip", err)
			}
		}
		var a model.Agent
		if jsondecode.Unmarshal(data, &a) == nil {
			_, _ = readState(&a)
			_, _ = activeHA(&a)
		}
		if ip := Usable(string(bytes.TrimSpace(data))); ip != "" && Usable(ip) != ip {
			t.Fatal("address not canonical")
		}
	})
}

func TestN810ShutdownAfterAcceptanceDoesNotWriteObservation(t *testing.T) {
	e := newUpgradeEnv(t)
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runner.Start(e.ctx)
	defer runner.Shutdown(context.Background())
	bound := runner.BindRequest(e.ctx)
	var reservedHead string
	t.Cleanup(SetRequestForTest(func(_ context.Context, _ *model.Agent, method, path string, _ []byte) (*agents.Response, error) {
		if method == "GET" {
			return AgentResponse(nil, 404), nil
		}
		fp, err := e.repo.Store().Fingerprint()
		if err != nil {
			t.Fatal(err)
		}
		reservedHead = fmt.Sprint(fp)
		runner.Shutdown(context.Background())
		return AgentResponse(jsonx.Obj(), 202), nil
	}))
	state, err := StartModel(bound, e.m, e.ident)
	if state != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown saved acceptance", state, err)
	}
	fp, err := e.repo.Store().Fingerprint()
	if err != nil || reservedHead == "" || fmt.Sprint(fp) != reservedHead {
		t.Fatal("late observation changed durable reservation", err)
	}
	a := nativeAgent(t, e)
	if a.LatestUpgrade == nil || a.LatestUpgrade.Status != "queued" || a.LatestUpgrade.Message == nil || !strings.HasPrefix(*a.LatestUpgrade.Message, "正在提交") {
		t.Fatal("initial durable task lost", a.LatestUpgrade)
	}
}

func TestN810ArtifactShutdownDoesNotPublishDigest(t *testing.T) {
	directory := artifactDir(t)
	put(t, directory, []byte("\x7fELFsynthetic"), Arches[0])
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runner.Start(context.Background())
	defer runner.Shutdown(context.Background())
	bound := runner.BindRequest(context.Background())
	readHook = func(string, int, int) error { runner.Shutdown(context.Background()); return nil }
	result, err := ArtifactsModel(bound)
	if !errors.Is(err, context.Canceled) || result.Len() != 0 || cacheLen() != 0 {
		t.Fatal("shutdown published artifact digest", result.Len(), cacheLen(), err)
	}
	readHook = nil
	if result, err := ArtifactsModel(context.Background()); err != nil || result.Len() != 1 || cacheLen() != 1 {
		t.Fatal("artifact retry did not recover", result.Len(), err)
	}
}
