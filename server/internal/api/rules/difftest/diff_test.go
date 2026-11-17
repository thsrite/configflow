// Package difftest exercises C32 through Flask and the complete Go HTTP pipeline.
package difftest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	apirules "configflow/internal/api/rules"
	"configflow/internal/auth"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/notifications"
	"configflow/internal/oxidns"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/taskstatus"
	"gopkg.in/yaml.v3"
)

const secret = "c32-synthetic-secret"

type answer struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body"`
	State   any               `json:"state"`
	Error   string            `json:"oracle_error"`
}
type world struct {
	t                   *testing.T
	srv                 *httpxtest.Server
	stdin               io.WriteCloser
	scanner             *bufio.Scanner
	stderr              bytes.Buffer
	cmd                 *exec.Cmd
	token               string
	ids                 map[string]string
	allowPinnedOmission bool
}

func newWorld(t *testing.T, seed func(context.Context, *repo.Repository)) *world {
	t.Helper()
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on test server")
	}
	w := &world{t: t, ids: map[string]string{}}
	w.srv = httpxtest.New(t, httpxtest.Options{Auth: true, Username: "diff-admin", Password: "diff-password", SecretKey: secret, Register: httpxtest.Routes(apirules.Register)})
	notifications.SetSources(notifications.Sources{Agents: func(context.Context) ([]*jsonx.Object, error) { return []*jsonx.Object{}, nil }, ListTasks: func(ctx context.Context) ([]*jsonx.Object, error) {
		status, err := taskstatus.New(w.srv.DataDir).Load()
		if err != nil {
			return nil, err
		}
		return refresh.RuleRows(ctx, status)
	}})
	t.Cleanup(notifications.ResetSources)
	oldClock := notifications.Clock
	notifications.Clock = fixedClock{}.Now
	t.Cleanup(func() { notifications.Clock = oldClock })
	env := refresh.DefaultEnv()
	env.Clock = fixedClock{}
	t.Cleanup(refresh.SetEnv(env))
	if seed != nil {
		seed(w.srv.Context(), w.srv.Repo)
	}
	pyDir := t.TempDir()
	if out, err := exec.Command("cp", "-a", w.srv.DataDir+"/.", pyDir).CombinedOutput(); err != nil {
		t.Fatalf("copy: %v %s", err, out)
	}
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		home, _ := os.UserHomeDir()
		python = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	root, _ := filepath.Abs("../../../../..")
	script, _ := filepath.Abs("testdata/reference.py")
	w.cmd = exec.Command(python, script)
	w.cmd.Dir = root
	w.cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+pyDir, "ADMIN_USERNAME=diff-admin", "ADMIN_PASSWORD=diff-password", "JWT_SECRET_KEY="+secret, "LOG_FILE="+filepath.Join(pyDir, "app.log"), "PYTHONDONTWRITEBYTECODE=1")
	var err error
	w.stdin, err = w.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := w.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	w.scanner = bufio.NewScanner(stdout)
	w.scanner.Buffer(make([]byte, 65536), 64<<20)
	w.cmd.Stderr = &w.stderr
	if err = w.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		w.stdin.Close()
		w.cmd.Process.Kill()
		w.cmd.Wait()
		if t.Failed() {
			t.Log(w.stderr.String())
		}
	})
	token, err := auth.EncodeHS256(jsonx.Obj("username", "diff-admin", "exp", time.Now().Add(time.Hour).Unix()), secret)
	if err != nil {
		t.Fatal(err)
	}
	w.token = token
	return w
}
func jsonValue(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var result any
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func snapshot(t *testing.T, s *httpxtest.Server) any {
	t.Helper()
	doc, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]any{}
	err = filepath.WalkDir(s.DataDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".json" && ext != ".yaml" && ext != ".list" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var value any
		switch ext {
		case ".json":
			err = json.Unmarshal(data, &value)
		case ".yaml":
			err = yaml.Unmarshal(data, &value)
		default:
			value = string(data)
		}
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(s.DataDir, path)
		files[filepath.ToSlash(relative)] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(s.DataDir, "configflow.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sections := [][]any{}
	rows, err := db.Query("SELECT scope, owner, position FROM sections ORDER BY scope, owner")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var scope, owner string
		var position int
		if err = rows.Scan(&scope, &owner, &position); err != nil {
			t.Fatal(err)
		}
		sections = append(sections, []any{scope, owner, position})
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	fields := [][]any{}
	rows, err = db.Query("SELECT scope, owner, field, position, value FROM fields ORDER BY scope, owner, field")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var scope, owner, field, value string
		var position int
		if err = rows.Scan(&scope, &owner, &field, &position, &value); err != nil {
			t.Fatal(err)
		}
		var data any
		if err = json.Unmarshal([]byte(value), &data); err != nil {
			t.Fatal(err)
		}
		fields = append(fields, []any{scope, owner, field, position, data})
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	return jsonValue(t, map[string]any{"document": doc, "files": files, "sections": sections, "fields": fields})
}

var generatedID = regexp.MustCompile(`^(?:rule|ruleset|lib)_[0-9a-f]{12}$`)

func (w *world) normalize(v any) any {
	switch value := v.(type) {
	case string:
		return w.normalizeString(value)
	case []any:
		if len(value) == 5 && (value[2] == "updated_at" || value[2] == "created_at") {
			if text, ok := value[4].(string); ok {
				if _, err := time.Parse(time.RFC3339Nano, text); err == nil {
					value[4] = "<time>"
				}
			}
		}
		for i, item := range value {
			value[i] = w.normalize(item)
		}
	case map[string]any:
		for key, item := range value {
			if replacement := w.normalizeString(key); replacement != key {
				delete(value, key)
				key = replacement
				value[key] = item
			}
			if key == "native_config" {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					var document any
					if yaml.Unmarshal([]byte(text), &document) == nil {
						value[key] = w.normalize(jsonValue(w.t, document))
						continue
					}
				}
			}
			if key == "elapsed_time" {
				if _, ok := item.(float64); !ok {
					w.t.Errorf("elapsed_time is not numeric: %T", item)
				}
				value[key] = "<elapsed>"
				continue
			}
			if key == "updated_at" || key == "created_at" || key == "last_run" || key == "last_success" || key == "next_run" {
				if text, ok := item.(string); ok {
					if _, err := time.Parse(time.RFC3339Nano, text); err == nil {
						value[key] = "<time>"
						continue
					}
				}
			}
			value[key] = w.normalize(item)
		}
	}
	return v
}
func (w *world) pairIDs(a, b any) {
	// Pair generated IDs by their response positions, preserving one-to-one
	// identity for every later field, reference, key and persisted document.
	aa, aok := a.(map[string]any)
	bb, bok := b.(map[string]any)
	if aok && bok {
		if x, xok := aa["id"].(string); xok && generatedID.MatchString(x) {
			y, yok := bb["id"].(string)
			if !yok || !generatedID.MatchString(y) {
				w.t.Errorf("generated ID mismatch %q %v", x, bb["id"])
			} else {
				replacement := w.ids[x]
				if replacement == "" {
					replacement = fmt.Sprintf("<id-%d>", len(w.ids))
					w.ids[x] = replacement
				}
				if old, ok := w.ids[y]; ok && old != replacement {
					w.t.Error("generated IDs are not bijective")
				}
				w.ids[y] = replacement
				w.ids[oxidns.TagFor("rule", oxidns.S(x))] = replacement + "-tag"
				w.ids[oxidns.TagFor("rule", oxidns.S(y))] = replacement + "-tag"
				w.ids[fmt.Sprintf("shared/rules/by-id/%x.list", sha256.Sum256([]byte(x)))] = replacement + ".list"
				w.ids[fmt.Sprintf("shared/rules/by-id/%x.list", sha256.Sum256([]byte(y)))] = replacement + ".list"
			}
		}
		for k, x := range aa {
			w.pairIDs(x, bb[k])
		}
	}
	ax, aok := a.([]any)
	bx, bok := b.([]any)
	if aok && bok {
		for i := 0; i < len(ax) && i < len(bx); i++ {
			w.pairIDs(ax[i], bx[i])
		}
	}
}
func (w *world) call(method, path, body string, authenticated bool) answer {
	t := w.t
	t.Helper()
	headers := map[string]string{}
	options := []httpxtest.Option{}
	if authenticated {
		headers["Authorization"] = "Bearer " + w.token
		options = append(options, httpxtest.Bearer(w.token))
	}
	var payload *string
	if body != "" {
		payload = &body
		options = append(options, httpxtest.Body([]byte(body), "application/json"))
	}
	request := struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Body    *string           `json:"body"`
		Headers map[string]string `json:"headers"`
	}{method, path, payload, headers}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.stdin.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	if !w.scanner.Scan() {
		t.Fatalf("oracle ended: %v %s", w.scanner.Err(), w.stderr.String())
	}
	var py answer
	if err = json.Unmarshal(w.scanner.Bytes(), &py); err != nil {
		t.Fatalf("oracle JSON %v %s", err, w.scanner.Text())
	}
	if py.Error != "" {
		t.Fatal(py.Error)
	}
	got := w.srv.Do(method, path, options...)
	var actual any
	if method == "HEAD" {
		actual = ""
	} else if strings.Contains(got.Header.Get("Content-Type"), "json") {
		if err = json.Unmarshal(got.Body, &actual); err != nil {
			t.Fatalf("Go JSON %v %s", err, got.Body)
		}
	} else {
		actual = string(got.Body)
	}
	if got.Status != py.Status {
		t.Errorf("%s %s status Python=%d Go=%d Python=%v Go=%s", method, path, py.Status, got.Status, py.Body, got.Body)
	}
	w.pairIDs(py.Body, actual)
	compare := func(label string, a, b any) {
		t.Helper()
		a = w.normalize(jsonValue(t, a))
		b = w.normalize(jsonValue(t, b))
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s %s %s: %s", method, path, label, firstDifference(a, b, ""))
		}
	}
	pyBytes, _ := json.Marshal(py.Body)
	if text, ok := py.Body.(string); ok {
		pyBytes = []byte(text)
	}
	migrated := httpxtest.MigratedAPIError(method, py.Status, got.Status, http.Header{"Content-Type": []string{py.Headers["Content-Type"]}}, got.Header, pyBytes, got.Body)
	if !migrated {
		compare("body", py.Body, actual)
	}
	for _, key := range []string{"Content-Type", "X-ConfigFlow-Cache", "Warning", "X-ConfigFlow-Profile"} {
		if key == "Content-Type" && migrated {
			continue
		}
		if py.Headers[key] != got.Header.Get(key) {
			t.Errorf("header %s Python=%q Go=%q", key, py.Headers[key], got.Header.Get(key))
		}
	}
	compare("state", py.State, snapshot(t, w.srv))
	t.Logf("%s %s Python=%d Go=%d", method, path, py.Status, got.Status)
	return py
}

func firstDifference(a, b any, path string) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	if aa, ok := a.(map[string]any); ok {
		if bb, ok := b.(map[string]any); ok {
			keys := make([]string, 0, len(aa)+len(bb))
			seen := map[string]bool{}
			for key := range aa {
				seen[key] = true
				keys = append(keys, key)
			}
			for key := range bb {
				if !seen[key] {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			for _, key := range keys {
				av, aok := aa[key]
				bv, bok := bb[key]
				if aok != bok {
					return fmt.Sprintf("%s.%s present Python=%v Go=%v", path, key, aok, bok)
				}
				if d := firstDifference(av, bv, path+"."+key); d != "" {
					return d
				}
			}
		}
	}
	if aa, ok := a.([]any); ok {
		if bb, ok := b.([]any); ok {
			if len(aa) != len(bb) {
				return fmt.Sprintf("%s length Python=%d Go=%d", path, len(aa), len(bb))
			}
			for i := range aa {
				if d := firstDifference(aa[i], bb[i], fmt.Sprintf("%s[%d]", path, i)); d != "" {
					return d
				}
			}
		}
	}
	return fmt.Sprintf("%s Python=%v Go=%v", path, a, b)
}

type fixedClock struct{}

func (fixedClock) Now() time.Time      { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
func (fixedClock) Monotonic() float64  { return 0 }
func (fixedClock) Sleep(time.Duration) {}

func (w *world) normalizeString(value string) string {
	keys := make([]string, 0, len(w.ids))
	for key := range w.ids {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	pairs := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		pairs = append(pairs, key, w.ids[key])
	}
	if len(pairs) == 0 {
		return value
	}
	return strings.NewReplacer(pairs...).Replace(value)
}
