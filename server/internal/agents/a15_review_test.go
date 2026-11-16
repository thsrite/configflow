package agents

// A15 review: Python references from
// upgrades/testdata/gen_a15_review.py (upgrades/testdata/a15_review.json).

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

func a15Reference(t *testing.T) *jsonx.Object {
	t.Helper()
	data, err := os.ReadFile("upgrades/testdata/a15_review.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
}

// TestA15ReviewISOTimestampYearRange: datetime.fromisoformat(v).timestamp()
// raises ValueError when resolving a naive time needs a local time outside
// the years 1–9999 (first day of year 1 everywhere, the last hours of 9999
// east of UTC); poll's recovery and deadline checks then skip the heartbeat.
func TestA15ReviewISOTimestampYearRange(t *testing.T) {
	previous := localZone
	t.Cleanup(func() { localZone = previous })
	for zone, rows := range a15Reference(t).GetObject("iso").All() {
		location, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		localZone = location
		for _, item := range rows.([]any) {
			row := item.(*jsonx.Object)
			value := row.GetString("value")
			got, ok := ISOTimestamp(value)
			if row.Has("error") {
				if ok {
					t.Errorf("%s %q: %v, Python %s", zone, value, got, row.GetString("error"))
				}
				continue
			}
			if want := row.GetFloat("timestamp"); !ok || got != want {
				t.Errorf("%s %q: %v %v, Python %v", zone, value, got, ok, want)
			}
		}
	}
}

// TestA15ReviewCallRaisesLikePython: the Agent-operation methods build the
// URL and the Bearer header outside their `except RequestException`; a
// record without host/port/token is a KeyError and a token latin-1 cannot
// encode a UnicodeEncodeError, both leaving the method (the route answers
// 500 with str(e)), never "Connection error".
func TestA15ReviewCallRaisesLikePython(t *testing.T) {
	e := newEnv(t)
	e.m.SetClient(HTTPClient{})
	id := e.agent.GetString("id")
	set := func(change func(agent *jsonx.Object)) {
		t.Helper()
		_, err := e.m.UpdateAgents(e.ctx, "", func(_ context.Context, list *[]any) (any, error) {
			change((*list)[0].(*jsonx.Object))
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	wantMessage := func(name, want string, call func() (any, error)) {
		t.Helper()
		value, err := call()
		if err == nil || err.Error() != want {
			t.Errorf("%s: %s, %v; Python raises %q", name, jsonx.Repr(value), err, want)
		}
	}
	operations := map[string]func() (any, error){
		"restart":     func() (any, error) { return e.m.RestartAgentService(e.ctx, id) },
		"uninstall":   func() (any, error) { return e.m.UninstallAgent(e.ctx, id) },
		"logs":        func() (any, error) { return e.m.GetAgentLogs(e.ctx, id, jsonx.Int(5), "", "") },
		"log sources": func() (any, error) { return e.m.GetAgentLogSources(e.ctx, id) },
		"clear":       func() (any, error) { return e.m.ClearAgentLog(e.ctx, id, "/x", "") },
		"validate":    func() (any, error) { return e.m.ValidateAgentLogPath(e.ctx, id, "/x") },
		"logging":     func() (any, error) { return e.m.GetLoggingConfig(e.ctx, id) },
		"set logging": func() (any, error) { return e.m.SetLoggingConfig(e.ctx, id, true) },
		"status":      func() (any, error) { return e.m.GetAgentStatus(e.ctx, id) },
	}
	set(func(agent *jsonx.Object) { agent.Set("token", "密钥") })
	for name, call := range operations {
		wantMessage(name+" cjk token", "'latin-1' codec can't encode characters in position 7-8: ordinal not in range(256)", call)
	}
	for _, key := range []string{"token", "port", "host"} {
		set(func(agent *jsonx.Object) { agent.Delete(key) })
		for name, call := range operations {
			wantMessage(name+" without "+key, "'"+key+"'", call)
		}
	}
	// A NaN body is requests' InvalidJSONError (a RequestException) only
	// after the URL and header arguments were evaluated.
	wantMessage("set logging NaN without host", "'host'", func() (any, error) {
		return e.m.SetLoggingConfig(e.ctx, id, jsonx.Float(math.NaN()))
	})
}

// TestA15ReviewRuntimeTimestampYearRange: runtime_state._timestamp catches
// the same ValueError (-inf), e.g. a naive registration time on 0001-01-01.
func TestA15ReviewRuntimeTimestampYearRange(t *testing.T) {
	previous := localZone
	t.Cleanup(func() { localZone = previous })
	localZone = time.UTC
	if got := timestampOf("0001-01-01T12:00:00"); !math.IsInf(got, -1) {
		t.Fatalf("_timestamp = %v, Python -inf", got)
	}
	if got := timestampOf("0001-01-02T00:00:00"); math.IsInf(got, -1) {
		t.Fatalf("_timestamp = %v, Python a time", got)
	}
}
