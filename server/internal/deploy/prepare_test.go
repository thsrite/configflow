package deploy

import (
	"configflow/internal/agents"
	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/gateway"
	"configflow/internal/jsonx"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/setupflow"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestPythonPrepare(t *testing.T) {
	mihomoenv.Wire()
	raw, err := os.ReadFile("testdata/prepare.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Name          string
		Source, Agent json.RawMessage
		Pull          AgentConfig
		PullError     string `json:"pull_error"`
		Error         string
		Manifest      Manifest
		Files         map[string]string
	}
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	ctx := testContext(t)
	r, _ := repo.GetRepository(ctx)
	m, err := agents.New(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		t.Run(row.Name, func(t *testing.T) {
			cfg, e := jsonx.Parse(row.Source)
			if e != nil {
				t.Fatal(e)
			}
			record, e := jsonx.Parse(row.Agent)
			if e != nil {
				t.Fatal(e)
			}
			src := NewSource(cfg.(*jsonx.Object), nil)
			target := NewTarget(record.(*jsonx.Object))
			pull, e := GenerateAgentConfig(ctx, src, target, "https://panel.example.com")
			if row.PullError != "" {
				if e == nil || e.Error() != row.PullError {
					t.Fatalf("pull %v, want %s", e, row.PullError)
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				if target.ServiceType == "surge" {
					if pull.Content != row.Pull.Content {
						t.Fatal("Surge content differs")
					}
				} else {
					assertYAML(t, []byte(pull.Content), []byte(row.Pull.Content))
				}
				sum := md5.Sum([]byte(pull.Content))
				if pull.MD5 != hex.EncodeToString(sum[:]) || pull.Version != pull.MD5[:8] {
					t.Fatal("pull digest inconsistent")
				}
			}
			p, e := PrepareContent(ctx, m, target, src, PrepareOptions{BaseURL: "https://panel.example.com", DeploymentID: "prepare-corpus"})
			if row.Error != "" {
				if e == nil {
					t.Fatal("accepted Python rejection")
				}
				if IsPreparationFailure(e) && e.Error() != row.Error {
					t.Fatalf("%s != %s", e, row.Error)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			files, e := p.Bundle.Files()
			if e != nil {
				t.Fatal(e)
			}
			if len(files) != len(row.Files) {
				t.Fatal("file set differs")
			}
			for path, encoded := range row.Files {
				want, _ := base64.StdEncoding.DecodeString(encoded)
				got, ok := files[path]
				if !ok {
					t.Fatal("missing", path)
				}
				if path == row.Manifest.ConfigPath {
					assertYAML(t, got, want)
				} else if string(got) != string(want) {
					t.Fatal("file bytes differ", path)
				}
			}
		})
	}
}
func assertYAML(t *testing.T, a, b []byte) {
	t.Helper()
	x, e := yamlx.Parse(a)
	if e != nil {
		t.Fatal(e)
	}
	y, e := yamlx.Parse(b)
	if e != nil {
		t.Fatal(e)
	}
	if !yamlx.Equal(x, y) {
		t.Fatalf("YAML differs\ngot %s\nwant %s", a, b)
	}
}
func TestPreparationFailureClasses(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{setupflow.ErrInvalidSession, true}, {&PreparationError{}, true}, {&RuleDownloadError{&PreparationError{cause: errors.New("transport")}}, true}, {&Refusal{}, true}, {&rules.StrategyReferenceError{}, true}, {&convert.DialerReferenceError{}, true}, {&convert.BaseSettingsError{}, true}, {&convert.EasyTierSettingsError{}, true}, {&gateway.Error{}, true}, {&oxidns.SettingsError{}, true}, {&oxidns.ValidationError{}, true}, {&oxidns.ReferenceError{}, true}, {&agents.ValueError{}, true}, {&repo.Error{Kind: repo.ErrValidation}, true}, {&repo.Error{Kind: repo.ErrNotFound}, false}, {&convert.CustomConfigYAMLError{}, false}, {&substore.SubscriptionFetchError{}, false}, {context.Canceled, false}, {errors.New("transport"), false},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if got := IsPreparationFailure(fmt.Errorf("wrapped: %w", tt.err)); got != tt.want {
				t.Fatalf("%T: %v", tt.err, got)
			}
		})
	}
}
