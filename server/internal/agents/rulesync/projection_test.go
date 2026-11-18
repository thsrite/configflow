package rulesync

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"configflow/internal/deploy"
	"configflow/internal/yamlx"
)

func TestProjectionAgainstPython(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 for the real Python oracle")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python")
	cmd := exec.Command(python, "server/internal/agents/rulesync/testdata/reference.py")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH=.")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("Python %v: %s", err, stderr.String())
	}
	var cases []struct {
		Name, Service, Error     string
		Base, Current, Projected json.RawMessage
		Settings                 Settings
		Kind                     Kind
		Withheld                 []string
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty oracle")
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			base, err := deploy.SourceFromJSON(tt.Base, nil)
			if err != nil {
				t.Fatal(err)
			}
			current, err := deploy.SourceFromJSON(tt.Current, nil)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := base.Raw()
			desired, _ := current.Raw()
			kind, projected, withheld, err := RuleChanges(base, current, tt.Service, tt.Settings)
			if tt.Error != "" {
				if err == nil || err.Error() != tt.Error {
					t.Fatalf("refusal got %v want %q", err, tt.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if kind != tt.Kind {
				t.Errorf("kind %s want %s", kind, tt.Kind)
			}
			if len(withheld) != len(tt.Withheld) {
				t.Errorf("withheld %v want %v", withheld, tt.Withheld)
			}
			got, _ := projected.Raw()
			a, e := sourceDocument(projected)
			if e != nil {
				t.Fatal(e)
			}
			b, e := yamlx.Parse(tt.Projected)
			if e != nil {
				t.Fatal(e)
			}
			// Native YAML is a string member: compare that member structurally, then the
			// entire source, retaining every other field rather than masking settings.
			left, right := get(get(a, "oxidns"), "native_config"), get(get(b, "oxidns"), "native_config")
			if left != nil && right != nil {
				ln, e := yamlx.Parse([]byte(text(left)))
				if e != nil {
					t.Fatal(e)
				}
				rn, e := yamlx.Parse([]byte(text(right)))
				if e != nil {
					t.Fatal(e)
				}
				if !yamlx.Equal(ln, rn) {
					t.Errorf("native document differs")
				}
				yamlx.Set(get(a, "oxidns"), "native_config", yamlx.Str("structurally compared"))
				yamlx.Set(get(b, "oxidns"), "native_config", yamlx.Str("structurally compared"))
			}
			if !yamlx.Equal(a, b) {
				t.Errorf("projected source differs\ngot %s\nwant %s", got, tt.Projected)
			}
			after, _ := base.Raw()
			now, _ := current.Raw()
			if !bytes.Equal(before, after) || !bytes.Equal(desired, now) {
				t.Fatal("projection mutated inputs")
			}
		})
	}
}
