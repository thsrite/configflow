package agents_test

import (
	"encoding/json"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/store"
)

func TestN810TrafficReportRejectsCorruptStoredBindingBeforeWrite(t *testing.T) {
	e := newTrafficEnv(t, false)
	e.setDiscovery(t, true)
	entry, err := e.s.Repo.Store().Load()
	if err != nil {
		t.Fatal(err)
	}
	document := *entry.Document
	document.System = &store.Section{Fields: append([]store.Field(nil), entry.Document.System.Fields...)}
	for i, field := range document.System.Fields {
		if field.Name != "agents" {
			continue
		}
		var records []map[string]json.RawMessage
		if err := json.Unmarshal(field.Value, &records); err != nil {
			t.Fatal(err)
		}
		records[0]["profile_id"] = json.RawMessage(`7`)
		raw, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		document.System.Fields[i].Value = raw
	}
	if _, committed, err := e.s.Repo.Store().Commit(entry, &document, nil); err != nil || !committed {
		t.Fatal("inject imported binding", err)
	}
	before, err := e.s.Repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	e.s.Post("/api/agents/gw/traffic-report", httpxtest.JSON(discoveryReport()), agentAuth(trafficToken)).WantStatus(400)
	if after, err := e.s.Repo.Store().Fingerprint(); err != nil || before != after {
		t.Fatal("corrupt binding changed repository", err)
	}
	if e.trafficFile(t) != "" {
		t.Fatal("corrupt binding wrote domain discovery traffic")
	}
	e.s.Get("/api/agents?summary=1").WantStatus(200)
}
