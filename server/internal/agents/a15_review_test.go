package agents

// A15 review: Python references from
// upgrades/testdata/gen_a15_review.py (upgrades/testdata/a15_review.json).

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"

	"configflow/internal/model"
)

type a15ISORow struct {
	Value     string  `json:"value"`
	Error     *string `json:"error"`
	Timestamp float64 `json:"timestamp"`
}

func a15Reference(t *testing.T) map[string][]a15ISORow {
	t.Helper()
	data, err := os.ReadFile("upgrades/testdata/a15_review.json")
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		ISO map[string][]a15ISORow `json:"iso"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	return root.ISO
}

// TestA15ReviewISOTimestampYearRange: datetime.fromisoformat(v).timestamp()
// raises ValueError when resolving a naive time needs a local time outside
// the years 1–9999 (first day of year 1 everywhere, the last hours of 9999
// east of UTC); poll's recovery and deadline checks then skip the heartbeat.
func TestA15ReviewISOTimestampYearRange(t *testing.T) {
	previous := localZone
	t.Cleanup(func() { localZone = previous })
	for zone, rows := range a15Reference(t) {
		location, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		localZone = location
		for _, row := range rows {
			got, ok := ISOTimestamp(row.Value)
			if row.Error != nil {
				if ok {
					t.Errorf("%s %q: %v, Python %s", zone, row.Value, got, *row.Error)
				}
				continue
			}
			if !ok || got != row.Timestamp {
				t.Errorf("%s %q: %v %v, Python %v", zone, row.Value, got, ok, row.Timestamp)
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
	id := e.id()
	set := func(change func(agent *model.Agent)) {
		t.Helper()
		if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, list *[]model.Agent) error {
			change(&(*list)[0])
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	wantMessage := func(name, want string, call func() (json.RawMessage, error)) {
		t.Helper()
		before := remoteReferenceHead(t, e)
		value, err := call()
		verifyNativeCallPreparation(t, e, name, want, err, before)
		if len(value) != 0 {
			t.Fatalf("native preparation returned a result despite refusal: %s", name)
		}
	}
	operations := map[string]func() (json.RawMessage, error){
		"restart": func() (json.RawMessage, error) {
			return e.m.RunDirectOperation(e.ctx, id, RestartService, nil, nil)
		},
		"uninstall": func() (json.RawMessage, error) {
			return e.m.RunDirectOperation(e.ctx, id, UninstallService, nil, nil)
		},
		"logs": func() (json.RawMessage, error) {
			raw, _, err := e.m.RemoteCall(e.ctx, id, RemoteRequest{Method: "GET", Path: "/api/logs?lines=5", Timeout: Seconds(5)})
			return raw, err
		},
		"log sources": func() (json.RawMessage, error) {
			raw, _, err := e.m.RemoteCall(e.ctx, id, RemoteRequest{Method: "GET", Path: "/api/logs/sources", Timeout: Seconds(5), LegacyLogSources: true})
			return raw, err
		},
		"clear": func() (json.RawMessage, error) {
			header, body, err := jsonBody(map[string]any{"log_path": "/x"})
			if err != nil {
				return nil, err
			}
			raw, _, err := e.m.RemoteCall(e.ctx, id, RemoteRequest{Method: "POST", Path: "/api/logs/clear", Header: header, Body: body, Timeout: Seconds(5)})
			return raw, err
		},
		"validate": func() (json.RawMessage, error) {
			header, body, err := jsonBody(map[string]any{"path": "/x"})
			if err != nil {
				return nil, err
			}
			raw, _, err := e.m.RemoteCall(e.ctx, id, RemoteRequest{Method: "POST", Path: "/api/logs/validate", Header: header, Body: body, Timeout: Seconds(5)})
			return raw, err
		},
		"logging": func() (json.RawMessage, error) {
			raw, _, err := e.m.RemoteCall(e.ctx, id, RemoteRequest{Method: "GET", Path: "/api/config/logging", Timeout: Seconds(5)})
			return raw, err
		},
		"set logging": func() (json.RawMessage, error) {
			header, body, err := jsonBody(map[string]any{"enabled": true})
			if err != nil {
				return nil, err
			}
			return e.m.SetLoggingModel(e.ctx, id, json.RawMessage(`true`), header, body)
		},
		"status": func() (json.RawMessage, error) {
			raw, _, err := e.m.RemoteCall(e.ctx, id, RemoteRequest{Method: "GET", Path: "/api/status", Timeout: Seconds(5)})
			return raw, err
		},
	}
	set(func(agent *model.Agent) { agent.Token = "密钥" })
	for name, call := range operations {
		wantMessage(name+" cjk token", "'latin-1' codec can't encode characters in position 7-8: ordinal not in range(256)", call)
	}
	for _, key := range []string{"token", "port", "host"} {
		set(func(agent *model.Agent) { _ = agent.Clear(key) })
		for name, call := range operations {
			wantMessage(name+" without "+key, "'"+key+"'", call)
		}
	}
	// A NaN body is requests' InvalidJSONError (a RequestException) only
	// after the URL and header arguments were evaluated.
	wantMessage("set logging NaN without host", "'host'", func() (json.RawMessage, error) {
		_, _, err := jsonBody(map[string]any{"enabled": math.NaN()})
		return nil, e.m.remotePayloadError(e.ctx, id, err)
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
