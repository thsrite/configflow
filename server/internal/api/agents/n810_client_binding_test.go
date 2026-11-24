package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/store"
)

func corruptClientBinding(t *testing.T, s *httpxtest.Server, id string) {
	t.Helper()
	entry, err := s.Repo.Store().Load()
	if err != nil {
		t.Fatal(err)
	}
	document := *entry.Document
	document.System = &store.Section{Fields: append([]store.Field(nil), entry.Document.System.Fields...)}
	found := false
	for i, field := range document.System.Fields {
		if field.Name != "agents" {
			continue
		}
		var rows []map[string]json.RawMessage
		if err := json.Unmarshal(field.Value, &rows); err != nil {
			t.Fatal(err)
		}
		for _, a := range rows {
			var key string
			if json.Unmarshal(a["id"], &key) == nil && key == id {
				a["profile_id"] = json.RawMessage(`7`)
				found = true
			}
		}
		raw, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		document.System.Fields[i].Value = raw
	}
	if !found {
		t.Fatal("synthetic Agent missing", id)
	}
	if _, committed, err := s.Repo.Store().Commit(t.Context(), entry, &document, nil); err != nil || !committed {
		t.Fatal("inject imported binding", err)
	}
}
func clientCollectedFiles(t *testing.T, s *httpxtest.Server) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, folder := range []string{"traffic", "probes", "regions", "traffic_history", "rule_cache"} {
		root := filepath.Join(s.DataDir, folder)
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			raw, err := os.ReadFile(path)
			out[path] = string(raw)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}
func TestN810PolicyPeersSkipCorruptBinding(t *testing.T) {
	s := a24Fixture(t)
	admin := s.Login()
	corruptClientBinding(t, s, "peer")
	response := parseBodyMap(t, s.Get("/api/agents/d/oxidns/device-policies", admin).WantStatus(200))
	peers := mapList(response, "agents")
	found := false
	for _, raw := range peers {
		data, _ := json.Marshal(raw)
		var row struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(data, &row); err != nil {
			t.Fatal(err)
		}
		if row.ID == "peer" {
			t.Fatal("corrupt peer fell back to default")
		}
		found = found || row.ID == "d"
	}
	if !found {
		t.Fatal("healthy peer disappeared")
	}
	var warnings peerWarningTimes
	at := time.Unix(123, 0)
	if !warnings.due("peer", at) || warnings.due("peer", at.Add(time.Minute)) || !warnings.due("peer", at.Add(10*time.Minute)) {
		t.Fatal("peer warning throttle changed")
	}
}
