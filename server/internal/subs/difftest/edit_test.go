// Package difftest checks the public N7-2 relink boundary and repository
// transactions through a test HTTP shell. Real proxy-group routes are C33 work.
package difftest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
)

type editCase struct {
	Name         string  `json:"name"`
	Source       string  `json:"source"`
	New          *string `json:"new"`
	FailAfter    bool    `json:"fail_after"`
	Difference   string  `json:"-"`
	PythonStatus int     `json:"-"`
	Expected     string  `json:"-"`
}

type editOutcome struct {
	Status int `json:"status"`
	Body   struct {
		Success bool     `json:"success"`
		Changed []string `json:"changed"`
		Error   string   `json:"error"`
	} `json:"body"`
	Source    string           `json:"source"`
	Groups    []map[string]any `json:"groups"`
	Committed bool             `json:"committed"`
}

func TestPublicRelinkTransactionalHTTPBoundary(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	renamed := "Renamed"
	quoted := "new # : \"quoted\""
	on := "on"
	same := "Pinned"
	aliasChain := "pad: [" + strings.Repeat("0, ", 3000) + "0]\nx: &x [" + strings.Repeat("a, ", 50) + "a]\nu: &u [" +
		strings.Repeat("*x, ", 30) + "*x]\nlisteners:\n- name: in\n  proxy: Pinned\n  users: *u\n"
	base := "# header\nfuture: {keep: false}\nlisteners:\n- name: first # name\n  proxy: Pinned # fixed\n  rule: local\n- {name: second, proxy: DIRECT}\n"
	cases := []editCase{
		{Name: "rename", Source: base, New: &renamed},
		{Name: "unpin", Source: base},
		{Name: "quoted name", Source: base, New: &quoted},
		{Name: "YAML 1.1-looking name", Source: base, New: &on},
		{Name: "same name no-op", Source: base, New: &same},
		{Name: "CRLF comments and blank lines", Source: strings.ReplaceAll(strings.Replace(base, "- {name: second", "\n# gap\n- {name: second", 1), "\n", "\r\n"), New: &renamed},
		{Name: "shared listener", Source: "shared: &base {name: first, proxy: Pinned}\nlisteners: [*base]\n", New: &renamed},
		{Name: "shared listener sequence", Source: "listeners: &all [{name: first, proxy: Pinned}]\nother: *all\n", New: &renamed},
		{Name: "merge", Source: "shared: &base {name: first, proxy: Pinned}\nlisteners:\n- <<: *base\n  port: 17890\n", New: &renamed},
		{Name: "rollback after edit", Source: base, New: &renamed, FailAfter: true},
		{Name: "alias chain in listener (review N7-2 F1)", Source: aliasChain, New: &renamed},
		{Name: "alias chain in unpinned listener (review N7-2 F1)", Source: aliasChain},
		{Name: "merged listener comments (review N7-2 F4)", Source: "# top\nshared: &base {name: first, proxy: Pinned} # base\nlisteners:\n- <<: *base # merge\n  port: 17890\n"},
		{Name: "parse diagnostic", Source: "listeners: [", Difference: "parse-diagnostic"},
		{Name: "listeners wrong type", Source: "listeners: {proxy: Pinned}\n"},
		{Name: "listener wrong type", Source: "listeners: [false]\n"},
		{Name: "target wrong type", Source: "listeners: [{proxy: true}]\n"},
		{Name: "variable blocks whole transaction", Source: "listeners: [{proxy: Pinned}, {proxy: '${POLICY}'}]\n", New: &renamed},
		{Name: "D1 duplicate rollback", Source: "listeners:\n- proxy: Other\n  proxy: Pinned\n", New: &renamed, Difference: "duplicate", PythonStatus: 400},
		{Name: "D1 rejects duplicate even when no relink needed", Source: "listeners:\n- proxy: Pinned\n  proxy: DIRECT\n", New: &renamed, Difference: "duplicate", PythonStatus: 200},
		{Name: "old Python scalar-alias refusal", Source: "pol: &pol Pinned\nlisteners:\n- name: x\n  proxy: *pol\n- name: y\n  proxy: Pinned\n", Difference: "old-editor-refusal", Expected: "pol: Pinned\nlisteners: [{name: x}, {name: y}]\n"},
	}
	expected := pythonRelinkOutcomes(t, cases)
	for i, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			actual := goRelinkOutcome(t, tc)
			want := expected[i]
			if tc.Difference == "duplicate" {
				if want.Status != tc.PythonStatus || want.Committed != (tc.PythonStatus == 200) || actual.Status != 400 || actual.Committed || actual.Source != tc.Source || !strings.Contains(actual.Body.Error, "proxy") || !strings.Contains(actual.Body.Error, "3") {
					t.Fatalf("wrong D1 boundary: python %+v Go %+v", want, actual)
				}
				if actual.Groups[0]["name"] != "Pinned" {
					t.Fatal("duplicate input partly committed")
				}
				return
			}
			if tc.Difference == "old-editor-refusal" {
				if want.Status != 400 || !strings.Contains(want.Body.Error, "无法安全保留") || want.Committed || actual.Status != 200 || !actual.Committed {
					t.Fatalf("wrong recovery boundary: python %+v Go %+v", want, actual)
				}
				sameYAML(t, actual.Source, tc.Expected)
				return
			}
			if actual.Status != want.Status || actual.Body.Success != want.Body.Success || !reflect.DeepEqual(actual.Body.Changed, want.Body.Changed) || actual.Committed != want.Committed || !reflect.DeepEqual(actual.Groups, want.Groups) {
				t.Fatalf("boundary mismatch: python %+v Go %+v", want, actual)
			}
			if tc.Difference == "parse-diagnostic" {
				if !strings.HasPrefix(actual.Body.Error, want.Body.Error+"：") {
					t.Fatalf("native reason not appended: %s", actual.Body.Error)
				}
			} else if actual.Body.Error != want.Body.Error {
				t.Fatalf("error %q want %q", actual.Body.Error, want.Body.Error)
			}
			if actual.Status == 200 {
				sameYAML(t, actual.Source, want.Source)
			} else if actual.Source != tc.Source || want.Source != tc.Source {
				t.Fatal("failed transaction changed source")
			}
		})
	}
}

func sameYAML(t *testing.T, a, b string) {
	t.Helper()
	na, ea := yamlx.Parse([]byte(a))
	nb, eb := yamlx.Parse([]byte(b))
	if ea != nil || eb != nil || !yamlx.Equal(na, nb) {
		t.Fatalf("different YAML meaning: %v/%v\n%s\n%s", ea, eb, a, b)
	}
}

func goRelinkOutcome(t *testing.T, tc editCase) editOutcome {
	t.Helper()
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
		r.POST("/static/n7-2/relink", "test.n7_relink", func(c *httpx.Ctx) (*httpx.Response, error) {
			payload := c.JSONObject()
			if payload == nil {
				return httpx.JSONStatus(400, jsonx.Obj("success", false, "error", "synthetic object body required")), nil
			}
			var newName *string
			if text, ok := payload.Value("new").(string); ok {
				newName = &text
			}
			failAfter := payload.Value("fail_after") == true
			var changed []string
			repository, err := repo.GetRepository(c.Context())
			if err != nil {
				return nil, err
			}
			_, _, err = repository.UpdateProfileTransaction(context.WithoutCancel(c.Context()), "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
				group := p.GetList("proxy_groups")[0].(*jsonx.Object)
				name := "Disabled"
				if newName != nil {
					name = *newName
				}
				group.Set("name", name)
				var err error
				changed, err = subs.RelinkGroupListeners(p, "Pinned", newName)
				if err != nil {
					return nil, err
				}
				if failAfter {
					return nil, errors.New("synthetic rollback")
				}
				return nil, nil
			})
			if err != nil {
				return httpx.JSONStatus(400, jsonx.Obj("success", false, "error", err.Error())), nil
			}
			return httpx.JSON(jsonx.Obj("success", true, "changed", jsonx.ListOf(changed))), nil
		})
	})})
	_, _, err := srv.Repo.UpdateProfileTransaction(srv.Context(), "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
		p.Set("proxy_groups", []any{jsonx.Obj("id", "g", "name", "Pinned", "type", "select", "manual_nodes", []any{"DIRECT"}, "future", "keep")})
		p.Set("mihomo", jsonx.Obj("custom_config", tc.Source))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := srv.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	var newValue any
	if tc.New != nil {
		newValue = *tc.New
	}
	response := srv.Post("/static/n7-2/relink", httpxtest.JSON(jsonx.Obj("new", newValue, "fail_after", tc.FailAfter)))
	after, err := srv.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := srv.Repo.GetProfile(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := jsonx.Compact(jsonx.Obj("status", response.Status, "body", response.JSON(), "source", saved.GetObject("mihomo").Value("custom_config"), "groups", saved.Value("proxy_groups"), "committed", before != after))
	var out editOutcome
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func pythonRelinkOutcomes(t *testing.T, cases []editCase) []editOutcome {
	t.Helper()
	dir := t.TempDir()
	input := filepath.Join(dir, "input.json")
	output := filepath.Join(dir, "output.json")
	data, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	interpreter := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if interpreter == "" {
		home, _ := os.UserHomeDir()
		interpreter = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, interpreter, filepath.Join(root, "server/internal/subs/testdata/edit_boundary.py"), input, output, filepath.Join(dir, "python"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "PYTHONDONTWRITEBYTECODE=1")
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Run(); err != nil {
		t.Fatalf("Python boundary: %v\n%s", err, logs.Bytes())
	}
	data, err = os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var outcomes []editOutcome
	if err := json.Unmarshal(data, &outcomes); err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != len(cases) {
		t.Fatal("truncated Python results")
	}
	return outcomes
}
