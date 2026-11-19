package agents

import (
	"configflow/internal/jsonx"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/repo"
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
	if _, committed, err := s.Repo.Store().Commit(entry, &document, nil); err != nil || !committed {
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
func TestN810ClientCorruptBindingNeverFallsBackToDefault(t *testing.T) {
	s := a24Fixture(t)
	admin := s.Login()
	var calls atomic.Int64
	device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer device.Close()
	if _, err := s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		for _, raw := range system.GetList("agents") {
			a := raw.(*jsonx.Object)
			a.Set("port", device.Listener.Addr().(*net.TCPAddr).Port)
		}
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"m", "d"} {
		corruptClientBinding(t, s, id)
	}
	before, err := s.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	files := clientCollectedFiles(t, s)
	for _, test := range []struct{ method, path, body string }{
		{"GET", "m/client-notes", ""},
		{"PUT", "m/client-notes", `{"profile_id":"default","ip":"192.0.2.5","name":"must not land","previous_name":""}`},
		{"POST", "m/mihomo/rule-targets/apply", `{"profile_id":"default","ruleset_id":"domains","target":"direct","value":"new.example","rule_type":"DOMAIN"}`},
		{"GET", "d/oxidns/device-policies", ""},
		{"PUT", "d/oxidns/device-policies", `{"profile_id":"default","revision":"ignored","notes_revision":"ignored","policies":[]}`},
	} {
		var response *httpxtest.Result
		path := "/api/agents/" + test.path
		switch test.method {
		case "GET":
			response = s.Get(path, admin)
		case "PUT":
			response = s.Put(path, admin, httpxtest.Body([]byte(test.body), "application/json"))
		case "POST":
			response = s.Post(path, admin, httpxtest.Body([]byte(test.body), "application/json"))
		}
		response.WantStatus(400)
		if response.Header.Get("Cache-Control") != "no-store" || response.Object().GetString("message") != "Invalid profile id" {
			t.Fatal("stored binding guard lost error", test.path, response)
		}
	}
	// These calls enter the write owners directly, proving the transaction's
	// re-read cannot be bypassed by a previously captured valid profile.
	for name, run := range map[string]func(context.Context) error{
		"note": func(ctx context.Context) error {
			return (clientTools{}).SaveNote(ctx, "m", noteEdit{ProfileID: "office", IP: "192.0.2.5", Name: "new", Previous: "旧电脑"})
		},
		"policy": func(ctx context.Context) error {
			return (clientTools{}).SavePolicies(ctx, "d", policyEdit{ProfileID: "office"})
		},
		"rule": func(ctx context.Context) error {
			_, err := (clientTools{}).ApplyConnection(ctx, "m", connectionEdit{ProfileID: "office", RulesetID: "domains", Target: "direct", Value: json.RawMessage(`"new.example"`), RuleType: "DOMAIN"})
			return err
		},
	} {
		if err := run(s.Context()); !errors.Is(err, repo.ErrValidation) {
			t.Fatalf("%s transaction did not reject stored binding: %v", name, err)
		}
	}
	if after, err := s.Repo.Store().Fingerprint(); err != nil || after != before {
		t.Fatal("corrupt binding wrote a profile (including default)", err)
	}
	if !reflect.DeepEqual(files, clientCollectedFiles(t, s)) {
		t.Fatal("corrupt binding wrote collected/cache files")
	}
	if calls.Load() != 0 {
		t.Fatal("corrupt binding contacted Agent", calls.Load())
	}
	s.Get("/api/agents?summary=1", admin).WantStatus(200)
}
func TestN810PolicyPeersSkipCorruptBinding(t *testing.T) {
	s := a24Fixture(t)
	admin := s.Login()
	corruptClientBinding(t, s, "peer")
	response := s.Get("/api/agents/d/oxidns/device-policies", admin).WantStatus(200).Object()
	peers := response.GetList("agents")
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
