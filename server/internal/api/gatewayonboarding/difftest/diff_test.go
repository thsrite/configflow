package difftest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/auth"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/routersplit"
	"configflow/internal/setup"
	"configflow/internal/setupflow"
	"configflow/internal/yamlx"
)

type oracleAnswer struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
	State   json.RawMessage   `json:"state"`
	Error   string            `json:"oracle_error"`
}
type oracle struct {
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  bytes.Buffer
	cmd     *exec.Cmd
}

func newOracle(t *testing.T, s *httpxtest.Server, adoptionTrace ...string) *oracle {
	t.Helper()
	directory := t.TempDir()
	if out, err := exec.Command("cp", "-a", s.DataDir+"/.", directory).CombinedOutput(); err != nil {
		t.Fatalf("copy synthetic DB: %v %s", err, out)
	}
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}
	// Reuse A20's public Flask + SQLite/file oracle; no second Flask harness.
	script := filepath.Join(root, "server/internal/api/linuxgateway/difftest/testdata/reference.py")
	if len(adoptionTrace) > 0 {
		script = filepath.Join(root, "server/internal/api/gatewayonboarding/difftest/testdata/adoption.py")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	o := &oracle{}
	o.cmd = exec.CommandContext(ctx, filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), script)
	o.cmd.Dir = root
	o.cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+directory, "ADMIN_USERNAME="+httpxtest.Username, "ADMIN_PASSWORD="+httpxtest.Password, "JWT_SECRET_KEY=a21-test-signing-key", "LOG_FILE="+filepath.Join(directory, "app.log"), "PYTHONDONTWRITEBYTECODE=1")
	if len(adoptionTrace) > 0 {
		o.cmd.Env = append(o.cmd.Env, "A21_ADOPT_TRACE="+adoptionTrace[0])
	}
	o.cmd.Stderr = &o.stderr
	o.stdin, err = o.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := o.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	o.scanner = bufio.NewScanner(stdout)
	o.scanner.Buffer(make([]byte, 65536), 16<<20)
	if err = o.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = o.stdin.Close()
		_ = o.cmd.Process.Kill()
		_ = o.cmd.Wait()
		if t.Failed() {
			t.Log(o.stderr.String())
		}
	})
	return o
}
func (o *oracle) call(t *testing.T, method, path, body, authorization string) oracleAnswer {
	t.Helper()
	request := struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Body    string            `json:"body"`
		Headers map[string]string `json:"headers"`
	}{method, path, body, map[string]string{"Authorization": authorization}}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = o.stdin.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	if !o.scanner.Scan() {
		t.Fatalf("Flask terminated: %v %s", o.scanner.Err(), o.stderr.String())
	}
	var answer oracleAnswer
	if err = json.Unmarshal(o.scanner.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Error != "" {
		t.Fatal(answer.Error)
	}
	return answer
}
func sqlState(t *testing.T, s *httpxtest.Server) json.RawMessage {
	t.Helper()
	var base struct {
		Document json.RawMessage
		Files    map[string]string
	}
	if err := json.Unmarshal(snapshot(t, s), &base); err != nil {
		t.Fatal(err)
	}

	const query = `import json, sqlite3, sys
with sqlite3.connect(sys.argv[1]) as db:
 sections=db.execute('SELECT scope,owner,position FROM sections ORDER BY scope,owner').fetchall()
 fields=[(s,o,k,p,json.loads(v)) for s,o,k,p,v in db.execute('SELECT scope,owner,field,position,value FROM fields ORDER BY scope,owner,field')]
 print(json.dumps(dict(sections=sections,fields=fields)))`
	// An independent, short-lived reader observes the physical SQLite rows and
	// exits before the next write; the test never lends a DB connection to repo.
	data, err := exec.Command(filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "-c", query, filepath.Join(s.DataDir, "configflow.sqlite3")).CombinedOutput()
	if err != nil {
		t.Fatalf("read SQLite boundary: %v %s", err, data)
	}
	var rows struct {
		Sections json.RawMessage `json:"sections"`
		Fields   json.RawMessage `json:"fields"`
	}
	if err = json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(struct {
		Document json.RawMessage   `json:"document"`
		Files    map[string]string `json:"files"`
		Sections json.RawMessage   `json:"sections"`
		Fields   json.RawMessage   `json:"fields"`
	}{base.Document, base.Files, rows.Sections, rows.Fields})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func normalize(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	var walk func(any, []string) any
	timestamp := func(v any) any {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("timestamp shape %T", v)
		}
		if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
			t.Fatalf("timestamp %v", err)
		}
		return "<time>"
	}
	walk = func(v any, path []string) any {
		switch x := v.(type) {
		case map[string]any:
			for key, item := range x {
				profileMeta := len(path) == 3 && path[0] == "document" && path[1] == "profiles"
				if profileMeta && (key == "created_at" || key == "updated_at") {
					x[key] = timestamp(item)
					continue
				}
				if text, ok := item.(string); ok && (key == "custom_config" || key == "native_config") && text != "" {
					node, err := yamlx.Parse([]byte(text))
					if err == nil {
						data, err := yamlx.ToJSON(node)
						if err != nil {
							t.Fatal(err)
						}
						if err = json.Unmarshal(data, &item); err != nil {
							t.Fatal(err)
						}
					}
				}
				x[key] = walk(item, append(append([]string{}, path...), key))
			}
		case []any:
			if len(path) == 1 && path[0] == "fields" {
				for _, row := range x {
					r := row.([]any)
					if r[0] == "profile" && (r[2] == "created_at" || r[2] == "updated_at") {
						r[4] = timestamp(r[4])
					}
				}
			}
			for i, item := range x {
				x[i] = walk(item, append(append([]string{}, path...), "[]"))
			}
		}
		return v
	}
	return walk(value, nil)
}
func compare(t *testing.T, label string, got, want json.RawMessage) {
	t.Helper()
	a, b := normalize(t, got), normalize(t, want)
	if !reflect.DeepEqual(a, b) {
		ga, _ := json.Marshal(a)
		wa, _ := json.Marshal(b)
		t.Fatalf("%s\ngot  %s\nwant %s", label, ga, wa)
	}
}

// TestFlaskHTTP compares all three route methods against real Python, including
// SQL member positions, stored documents, files, Chinese errors and no-store.
// The production route registration uses C34's real shared checker.
func TestFlaskHTTP(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("CONFIGFLOW_DIFF=1 runs real Flask")
	}
	s := testServer(t)
	authForOracle(t)
	// testServer's auth signing key is updated consistently for both clients.
	admin := s.Login()
	request := &http.Request{Header: http.Header{}}
	admin(request)
	authorization := request.Header.Get("Authorization")
	o := newOracle(t, s)
	path := "/api/profiles/default/gateway-onboarding"
	cases := []struct{ method, path, body string }{{"GET", path, ""}, {"POST", path + "/preview", validForm}, {"POST", path, validForm}, {"POST", path + "/preview", validForm}, {"POST", path, validForm}, {"POST", path, `{"bad":"body"}`}, {"POST", path + "/preview", "{"}, {"POST", path, "null"}, {"POST", path + "/preview", "[]"}, {"POST", path + "/preview", `{"rule_set_ids":[],"direct_rule_set_ids":[],"proxy_rule_set_ids":[],"local_dns":"udp://192.0.2.53"}`}, {"POST", path, strings.Repeat("x", 262145)},
		// Review A21: one representative per remaining error class (stale guard,
		// pre-schema revision comparison, blocked save report, missing profile).
		{"POST", path, `{"rule_set_ids":[],"direct_rule_set_ids":[],"proxy_rule_set_ids":[],"local_dns":"https://192.0.2.53/dns-query","expected_config_revision":999999}`},
		{"POST", path, `{"rule_set_ids":[],"direct_rule_set_ids":[],"proxy_rule_set_ids":[],"local_dns":"https://192.0.2.53/dns-query","expected_config_revision":"7"}`},
		{"POST", path, `{"rule_set_ids":[],"direct_rule_set_ids":["missing-rule"],"proxy_rule_set_ids":[],"local_dns":"https://192.0.2.53/dns-query"}`},
		{"GET", "/api/profiles/missing-profile/gateway-onboarding", ""},
		{"POST", "/api/profiles/missing-profile/gateway-onboarding", validForm}}
	for i, tc := range cases {
		t.Run(tc.method+tc.path+string(rune('a'+i)), func(t *testing.T) {
			py := o.call(t, tc.method, tc.path, tc.body, authorization)
			goResult := s.Do(tc.method, tc.path, admin, requestBody(tc.body))
			if goResult.Status != py.Status {
				t.Fatalf("status Go=%d Python=%d: %s %s", goResult.Status, py.Status, goResult.Text(), py.Body)
			}
			compare(t, "body", goResult.Body, py.Body)
			for _, key := range []string{"Cache-Control", "Content-Type"} {
				if goResult.Header.Get(key) != py.Headers[key] {
					t.Fatalf("header %s Go=%q Python=%q", key, goResult.Header.Get(key), py.Headers[key])
				}
			}
			compare(t, "SQLite/files", sqlState(t, s), py.State)
		})
	}
}
func authForOracle(t *testing.T) func() {
	t.Helper()
	previous := auth.Configure(auth.Config{AdminUsername: httpxtest.Username, AdminPassword: httpxtest.Password, SecretKey: "a21-test-signing-key"})
	restore := func() { auth.Configure(previous) }
	t.Cleanup(restore)
	return restore
}

func TestFlaskHEADExpectedReadOnlyDifference(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("CONFIGFLOW_DIFF=1 runs real Flask")
	}
	s := testServer(t)
	authForOracle(t)
	admin := s.Login()
	request := &http.Request{Header: http.Header{}}
	admin(request)
	o := newOracle(t, s)
	before := snapshot(t, s)
	revision, _ := s.Repo.ConfigRevision(s.Context(), "default")
	py := o.call(t, "HEAD", "/api/profiles/default/gateway-onboarding", validForm, request.Header.Get("Authorization"))
	goResult := s.Do("HEAD", "/api/profiles/default/gateway-onboarding", admin, requestBody(validForm)).WantStatus(200)
	if py.Status != 200 || len(goResult.Body) != 0 || !bytes.Equal(before, snapshot(t, s)) {
		t.Fatal("HEAD expected difference failed")
	}
	var state struct {
		Document struct {
			Profiles map[string]struct {
				Revision int64 `json:"_revision"`
			} `json:"profiles"`
		} `json:"document"`
	}
	if err := json.Unmarshal(py.State, &state); err != nil {
		t.Fatal(err)
	}
	current, err := s.Repo.Profile(s.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if state.Document.Profiles["default"].Revision != revisionOfProfile(t, current)+1 {
		t.Fatal("Python HEAD did not enter save transaction")
	}
	after, _ := s.Repo.ConfigRevision(s.Context(), "default")
	if revision != after {
		t.Fatal("Go HEAD changed revision")
	}
}

func revisionOfProfile(t *testing.T, p *model.Profile) int64 {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Revision int64 `json:"_revision"`
	}
	if err = json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v.Revision
}

// TestFlaskAdoptionRevisionCondition observes Python's real route guard and
// real setup owner, including a metadata write between the before snapshot and
// save transaction. That intervening revision must prevent session adoption.
func TestFlaskAdoptionRevisionCondition(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("CONFIGFLOW_DIFF=1 runs real Flask")
	}
	for _, concurrent := range []bool{false, true} {
		name := "normal-and-repeat"
		if concurrent {
			name = "intervening-revision"
		}
		t.Run(name, func(t *testing.T) {
			s := testServer(t)
			authForOracle(t)
			admin := s.Login()
			request := &http.Request{Header: http.Header{}}
			admin(request)
			profile, err := s.Repo.Profile(s.Context(), "default")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(profile)
			if err != nil {
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
			pyDigests := pythonOwnedDigests(t, raw)
			dir := filepath.Join(s.DataDir, "setup-sessions")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			pyRecord, err := json.Marshal(map[string]any{"id": id, "profile_id": "default", "goal": "gateway", "completed": false, "owned_digest": pyDigests[0], "owned_digest_without_base": pyDigests[1]})
			if err != nil {
				t.Fatal(err)
			}
			record, err := json.Marshal(map[string]any{"id": id, "profile_id": "default", "goal": "gateway", "completed": false, "digest_version": 1, "owned_digest": full, "owned_digest_without_base": without})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, id+".json")
			if err := os.WriteFile(path, pyRecord, 0600); err != nil {
				t.Fatal(err)
			}
			trace := filepath.Join(t.TempDir(), "adopt.jsonl")
			t.Setenv("A21_CONCURRENT_WRITE", "0")
			attempts := 2
			if concurrent {
				attempts = 1
				t.Setenv("A21_CONCURRENT_WRITE", "1")
				calls := 0
				t.Cleanup(routersplit.SetSessionReader(func(ctx context.Context, r *repo.Repository, profileID string) ([]routersplit.Session, error) {
					calls++
					if calls == 1 {
						name := "Concurrent metadata"
						if _, err := r.UpdateProfileMetadataRecord(ctx, profileID, repo.ProfileMetadataUpdate{Name: &name}); err != nil {
							return nil, err
						}
					}
					sessions, err := setupflow.ProfileSessions(ctx, r.DataDir(), profileID)
					if err != nil {
						return nil, err
					}
					out := make([]routersplit.Session, 0, len(sessions))
					for _, session := range sessions {
						var v routersplit.Session
						if err := json.Unmarshal(session.Raw, &v); err != nil {
							return nil, err
						}
						out = append(out, v)
					}
					return out, nil
				}))
			}
			o := newOracle(t, s, trace)
			if err := os.WriteFile(path, record, 0600); err != nil {
				t.Fatal(err)
			}
			previous := revisionOfProfile(t, profile)
			for attempt := range attempts {
				py := o.call(t, "POST", "/api/profiles/default/gateway-onboarding", validForm, request.Header.Get("Authorization"))
				got := s.Post("/api/profiles/default/gateway-onboarding", admin, requestBody(validForm)).WantStatus(200)
				if py.Status != 200 {
					t.Fatalf("Flask %d %s", py.Status, py.Body)
				}
				compare(t, "embedded body", got.Body, py.Body)
				current, err := s.Repo.Profile(s.Context(), "default")
				if err != nil {
					t.Fatal(err)
				}
				expectedRevision := previous + int64(attempt) + 1
				if concurrent {
					expectedRevision++
				}
				var state struct {
					Document struct {
						Profiles map[string]struct {
							Revision int64 `json:"_revision"`
						} `json:"profiles"`
					} `json:"document"`
					Files map[string]json.RawMessage `json:"files"`
				}
				if err := json.Unmarshal(py.State, &state); err != nil {
					t.Fatal(err)
				}
				if revisionOfProfile(t, current) != expectedRevision || state.Document.Profiles["default"].Revision != expectedRevision {
					t.Fatal("stored revision diverged")
				}
				observed, err := os.ReadFile(trace)
				saved, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if concurrent {
					if !os.IsNotExist(err) || !bytes.Equal(record, saved) {
						t.Fatalf("+2 write adopted: trace=%s err=%v", observed, err)
					}
					compare(t, "Python +2 session unchanged", pyRecord, state.Files["setup-sessions/"+id+".json"])
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				lines := bytes.Split(bytes.TrimSpace(observed), []byte("\n"))
				if len(lines) != attempt+1 {
					t.Fatalf("Python adoption calls %s", observed)
				}
				for n, line := range lines {
					var revisions []int64
					if err := json.Unmarshal(line, &revisions); err != nil || len(revisions) != 2 || revisions[0] != previous+int64(n) || revisions[1] != previous+int64(n)+1 {
						t.Fatalf("Python +1 guard %s %v", line, err)
					}
				}
				if bytes.Equal(record, saved) {
					t.Fatal("Go +1 did not invoke real C36 owner")
				}
				record = saved
				var goSession, pySession setup.Record
				if err := json.Unmarshal(saved, &goSession); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(state.Files["setup-sessions/"+id+".json"], &pySession); err != nil {
					t.Fatal(err)
				}
				if goSession.OwnedDigest == nil || pySession.OwnedDigest == nil || goSession.OwnedDigestWithoutBase == nil || pySession.OwnedDigestWithoutBase == nil {
					t.Fatal("missing adoption proof")
				}
			}
		})
	}
}

func pythonOwnedDigests(t *testing.T, profile json.RawMessage) [2]string {
	t.Helper()
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "-c", `import json,sys;from backend.common.setup_flow import owned_digest
p=json.load(sys.stdin);json.dump([owned_digest(p),owned_digest(p,base=False)],sys.stdout)`)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root)
	cmd.Stdin = bytes.NewReader(profile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python owned_digest: %v %s", err, out)
	}
	var digests [2]string
	if err := json.Unmarshal(out, &digests); err != nil {
		t.Fatal(err)
	}
	return digests
}
