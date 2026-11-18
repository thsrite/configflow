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
	"configflow/internal/jsonx"
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

func newOracle(t *testing.T, s *httpxtest.Server) *oracle {
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	o := &oracle{}
	o.cmd = exec.CommandContext(ctx, filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), script)
	o.cmd.Dir = root
	o.cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+directory, "ADMIN_USERNAME="+httpxtest.Username, "ADMIN_PASSWORD="+httpxtest.Password, "JWT_SECRET_KEY=a21-test-signing-key", "LOG_FILE="+filepath.Join(directory, "app.log"), "PYTHONDONTWRITEBYTECODE=1")
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
// Until C34 merges only the independent, non-loop corpus injects the DNS seam.
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
	cases := []struct{ method, path, body string }{{"GET", path, ""}, {"POST", path + "/preview", validForm}, {"POST", path, validForm}, {"POST", path + "/preview", validForm}, {"POST", path, validForm}, {"POST", path, `{"bad":"body"}`}, {"POST", path + "/preview", "{"}, {"POST", path, "null"}, {"POST", path + "/preview", "[]"}, {"POST", path + "/preview", `{"rule_set_ids":[],"direct_rule_set_ids":[],"proxy_rule_set_ids":[],"local_dns":"udp://192.0.2.53"}`}, {"POST", path, strings.Repeat("x", 262145)}}
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
	current, err := s.Repo.GetProfile(s.Context(), "default")
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

func revisionOfProfile(t *testing.T, p *jsonx.Object) int64 {
	t.Helper()
	data, err := jsonx.Compact(p)
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

func TestFlaskSetupExpectedDependencyDifference(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("CONFIGFLOW_DIFF=1 runs real Flask")
	}
	s := testServer(t)
	authForOracle(t)
	admin := s.Login()
	request := &http.Request{Header: http.Header{}}
	admin(request)
	dir := filepath.Join(s.DataDir, "setup-sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, strings.Repeat("a", 32)+".json"), []byte(`{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","profile_id":"default","goal":"gateway","completed":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	o := newOracle(t, s)
	before := snapshot(t, s)
	py := o.call(t, "POST", "/api/profiles/default/gateway-onboarding", validForm, request.Header.Get("Authorization"))
	goResult := s.Post("/api/profiles/default/gateway-onboarding", admin, requestBody(validForm)).WantStatus(503)
	if py.Status != 200 || !bytes.Equal(before, snapshot(t, s)) || !goResult.Contains("等待首次使用向导模块") {
		t.Fatalf("C36 explicit dependency difference Python=%d Go=%d", py.Status, goResult.Status)
	}
}
