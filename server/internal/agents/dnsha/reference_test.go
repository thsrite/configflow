package dnsha

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"configflow/internal/deploy"
	"configflow/internal/model"
)

func TestValidationAgainstPython(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 for real Python")
	}
	root, e := filepath.Abs("../../../..")
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "server/internal/agents/dnsha/testdata/reference.py")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH=.")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, e := cmd.Output()
	if e != nil {
		t.Fatalf("Python: %v %s", e, stderr.String())
	}
	var rows []struct {
		Name, Kind, Error     string
		Body, A, B            json.RawMessage
		Agents, Members, Caps []json.RawMessage
		State                 json.RawMessage
		Config                string
		Want                  json.RawMessage
	}
	if e = json.Unmarshal(data, &rows); e != nil {
		t.Fatal(e)
	}
	if len(rows) < 40 {
		t.Fatal("oracle unexpectedly empty", len(rows))
	}
	f := fixtureNew(t, Options{}, false, nil)
	for _, row := range rows {
		t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
			decode := func(raw json.RawMessage) *agent {
				if string(raw) == "null" {
					return nil
				}
				var rec model.Agent
				if e := json.Unmarshal(raw, &rec); e != nil {
					t.Fatal(e)
				}
				a, e := modelAgent(&rec)
				if e != nil {
					t.Fatal(e)
				}
				return a
			}
			var got any
			var err error
			switch row.Kind {
			case "normalize":
				list := []*agent{}
				for _, r := range row.Agents {
					list = append(list, decode(r))
				}
				n, e := normalize(row.Body, decode(row.A), decode(row.B), list)
				err = e
				got = struct {
					PeerID   string                       `json:"peer_id"`
					VIP      string                       `json:"vip"`
					VRID     model.Int                    `json:"vrid"`
					Previous map[string]*model.HASettings `json:"previous"`
				}{n.intent.PeerID, n.intent.VIP, n.intent.VRID, n.previous}
			case "environment":
				list := []*agent{}
				caps := []object{}
				for _, r := range row.Members {
					list = append(list, decode(r))
				}
				for _, r := range row.Caps {
					caps = append(caps, parseObject(r))
				}
				err = f.s.validateEnvironments(t.Context(), list, caps)
				got = true
			case "health":
				members := []MemberView{}
				for _, r := range row.Members {
					o := parseObject(r)
					members = append(members, MemberView{Runtime: runtimeOf(o), Error: o.text("error")})
				}
				var st model.HAState
				_ = json.Unmarshal(row.State, &st)
				got = health(members, &st)
			case "listener":
				m := deploy.Manifest{ProtocolVersion: 1, DeploymentID: "candidate", AgentID: "primary", ProfileID: "default", ServiceType: "oxidns", ConfigPath: "config.yaml"}
				m.Files = []deploy.File{{Path: "config.yaml", Size: int64(len(row.Config)), SHA256: digest([]byte(row.Config)), Role: deploy.RoleConfig}}
				b, e := deploy.Pack(m, map[string]deploy.Content{"config.yaml": {Data: []byte(row.Config), Role: deploy.RoleConfig}}, "version", nil)
				if e != nil {
					t.Fatal(e)
				}
				err = requireListener(b)
				got = true
			}
			if row.Error != "" {
				if err == nil || err.Error() != row.Error {
					t.Fatalf("got %v want %s", err, row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !equal(json.RawMessage(row.Want), got) {
				t.Fatalf("got %s want %s", rawObject(got), row.Want)
			}
		})
	}
}
