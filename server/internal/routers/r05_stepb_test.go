package routers_test

import (
	"bytes"
	"configflow/internal/routers/device"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
	"configflow/internal/testjson"
)

type stepBWire struct{ method, path, contentType, body string }

// stepBIkuai preserves Step B's default static_rt presentation: the table
// stores a plain mask while show appends a display suffix. Plain is an explicit
// positive control; the shared fake and the Step B black-box fake stay unchanged.
func stepBIkuai(t *testing.T, state routerfake.IkuaiState, plain bool, capture ...chan<- stepBWire) (*routers.Ikuai, *routerfake.Ikuai, string) {
	t.Helper()
	fake := routerfake.NewIkuai(state)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(capture) > 0 {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			capture[0] <- stepBWire{r.Method, r.URL.Path, r.Header.Get("Content-Type"), string(body)}
		}
		reply := httptest.NewRecorder()
		fake.ServeHTTP(reply, r)
		for key, values := range reply.Header() {
			w.Header()[key] = values
		}
		body := reply.Body.Bytes()
		var data map[string]any
		if !plain && json.Unmarshal(body, &data) == nil {
			if results, ok := data["results"].(map[string]any); ok {
				if rows, ok := results["data"].([]any); ok {
					// Only static_rt rows have dst_addr and ip_type; leave other lists alone.
					for _, value := range rows {
						if row, ok := value.(map[string]any); ok && row["ip_type"] == "4" && row["dst_addr"] != nil {
							if mask, ok := row["netmask"].(string); ok {
								row["netmask"] = strings.SplitN(mask, " ", 2)[0] + " (synthetic display)"
							}
						}
					}
					body, _ = json.Marshal(data)
				}
			}
		}
		w.WriteHeader(reply.Code)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	driver := routers.NewIkuai(server.URL, routerfake.IkuaiUser, routerfake.IkuaiPassword, false)
	t.Cleanup(driver.Close)
	return driver, fake, server.URL
}

func TestR05StepBStaticMaskPublishAndUndo(t *testing.T) {
	for _, plain := range []bool{false, true} {
		for _, batch := range []bool{false, true} {
			name := "suffix"
			if plain {
				name = "plain"
			}
			if batch {
				name += "/batch"
			}
			t.Run(name, func(t *testing.T) {
				d, fake, _ := stepBIkuai(t, routerfake.IkuaiState{}, plain)
				var undo json.RawMessage
				apply := func() error {
					var err error
					undo, err = d.Apply(t.Context(), mustJSON(t, routeAdd("198.51.100.0/24", "cf-stepb")))
					return err
				}
				var err error
				if batch {
					err = d.Batch(t.Context(), false, func(_ctx context.Context) error { return apply() })
				} else {
					err = apply()
				}
				if err != nil {
					t.Fatalf("publish failed: %v", err)
				}
				state, err := d.ReadState(t.Context())
				if err != nil || len(state.Routes) != 1 || state.Routes[0].Dst != "198.51.100.0/24" {
					t.Fatalf("route omitted: %+v %v", state, err)
				}
				if _, err = d.Apply(t.Context(), undo); err != nil {
					t.Fatalf("undo: %v", err)
				}
				if len(fake.Snapshot().Tables["static_rt"]) != 0 {
					t.Fatal("undo left route")
				}
			})
		}
	}
}

func TestR05StepBStaticMaskCompensation(t *testing.T) {
	for _, failure := range []string{"lost add reply", "later refusal", "external gateway"} {
		t.Run(failure, func(t *testing.T) {
			state := routerfake.IkuaiState{}
			switch failure {
			case "lost add reply":
				state.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: "add", Count: 1, LoseReply: true}}
			case "later refusal":
				code := 3001
				state.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: "add", Count: 2, Code: &code}}
			case "external gateway":
				state.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: "add", Count: 1, Corrupt: routerfake.Row{"gateway": "192.0.2.99"}}}
			}
			d, fake, _ := stepBIkuai(t, state, false)
			var undo json.RawMessage
			err := d.Batch(t.Context(), false, func(ctx context.Context) error {
				var err error
				undo, err = d.Apply(ctx, mustJSON(t, routeAdd("198.51.100.0/24", "cf-stepb")))
				if err != nil {
					undo = device.UndoOf(err)
					return err
				}
				if failure == "later refusal" {
					_, err = d.Apply(ctx, mustJSON(t, routeAdd("203.0.113.0/24", "cf-stepb")))
				}
				return err
			})
			if err == nil || len(undo) == 0 {
				t.Fatalf("failure/undo missing: %v %s", err, undo)
			}
			_, err = d.Apply(t.Context(), undo)
			rows := fake.Snapshot().Tables["static_rt"]
			if failure == "external gateway" {
				if err == nil || len(rows) != 1 || rows[0]["gateway"] != "192.0.2.99" {
					t.Fatalf("foreign row deleted: %v %+v", err, rows)
				}
			} else if err != nil || len(rows) != 0 {
				t.Fatalf("compensation left route: %v %+v", err, rows)
			}
		})
	}
}

func TestR05StepBProbeInvalidResponses(t *testing.T) {
	tests := []struct {
		name, message string
		fault         routerfake.IkuaiFault
	}{
		{"verinfo scalar", "iKuai 版本信息格式无法识别，请检查固件版本和管理接口。", routerfake.IkuaiFault{Func: "sysstat", Reply: map[string]any{"code": 0, "results": map[string]any{"verinfo": true}}}},
		{"verinfo array", "iKuai 版本信息格式无法识别，请检查固件版本和管理接口。", routerfake.IkuaiFault{Func: "sysstat", Reply: map[string]any{"code": 0, "results": map[string]any{"verinfo": []any{map[string]any{"version": "4.0.314"}}}}}},
		{"invalid Location", "iKuai 重定向地址格式无法识别，请在浏览器核对管理地址、协议和端口。", routerfake.IkuaiFault{Func: "login", Status: 301, Location: "http://[invalid/private?token=synthetic-secret"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, fake, _ := stepBIkuai(t, routerfake.IkuaiState{Faults: []routerfake.IkuaiFault{tc.fault}}, false)
			info, err := d.Probe(t.Context())
			var re *device.RouterError
			if info != nil || !errors.As(err, &re) || re.Kind != device.KindInvalidResponse || re.Message != tc.message {
				t.Fatalf("unusable probe failure: info=%+v err=%#v", info, err)
			}
			stepBSaveFailedProbe(t, re)
			if strings.Contains(err.Error(), "synthetic-secret") || len(writes(fake.Snapshot())) != 0 {
				t.Fatal("probe leaked credentials or wrote device")
			}
		})
	}
}

// This is R15's repository-consumer contract, not an implemented HTTP handler:
// catching RouterError must retain last_probe on failure, with one revision.
func stepBSaveFailedProbe(t *testing.T, failure *device.RouterError) {
	t.Helper()
	recordRaw := json.RawMessage(`{"id":"r-ik","type":"ikuai","endpoint":"http://example.com","last_probe":{"ok":true},"custom":"kept"}`)
	otherRaw := json.RawMessage(`{"id":"other","type":"ikuai","endpoint":"http://other.example.com","last_probe":{"ok":true}}`)
	initRaw := mustJSON(t, map[string]any{"routers": map[string]any{"items": []json.RawMessage{recordRaw, otherRaw}}})
	db, err := repo.Open(t.TempDir(), repo.Options{InitialSettings: func() *model.Config {
		cfg, err := model.DecodeConfig(initRaw)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := db.ConfigRevision(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := db.Profile(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	expected := profile.Routers.Items[0]
	expectedBytes := mustJSON(t, expected)
	replacement := expected
	replacement.LastProbe = &model.RouterProbe{At: time.Now().Format("2006-01-02T15:04:05"), OK: false, Message: new(failure.Message)}
	replacementBytes := mustJSON(t, replacement)
	if err = db.SaveRouterSettingsJSON(t.Context(), "default", expectedBytes, replacementBytes); err != nil {
		t.Fatal(err)
	}
	actual, err := db.Profile(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	rows := actual.Routers.Items
	revision, err := db.ConfigRevision(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	// The neighbour is compared as the plain record read from its stored text
	// (O18 T1c: Go's encoding with always-written members, not the source bytes).
	var other model.Router
	if err := json.Unmarshal(otherRaw, &other); err != nil {
		t.Fatal(err)
	}
	if revision != before+1 || len(rows) != 2 || !testjson.Equal(replacement, rows[0]) || !testjson.Equal(other, rows[1]) {
		t.Fatal("failed probe not saved in isolation", actual)
	}
}

func TestR05StepBRawStaticRequests(t *testing.T) {
	capture := make(chan stepBWire, 32)
	d, _, _ := stepBIkuai(t, routerfake.IkuaiState{}, false, capture)
	undo, err := d.Apply(t.Context(), mustJSON(t, routeAdd("198.51.100.0/24", "cf-stepb")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Apply(t.Context(), undo); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"add": `{"action":"add","func_name":"static_rt","param":{"comment":"cf-stepb","dst_addr":"198.51.100.0","enabled":"yes","gateway":"192.0.2.4","interface":"auto","ip_type":"4","netmask":"255.255.255.0","prio":1,"tagname":"cf_rt_b991faeee"}}`,
		"del": `{"action":"del","func_name":"static_rt","param":{"id":100}}`,
	}
	for len(capture) > 0 {
		req := <-capture
		var body struct {
			Func   string `json:"func_name"`
			Action string `json:"action"`
		}
		if err := json.Unmarshal([]byte(req.body), &body); err != nil {
			t.Fatal(err)
		}
		if body.Func != "static_rt" || body.Action == "show" {
			continue
		}
		if req.method != "POST" || req.path != "/Action/call" || req.contentType != "application/json" || req.body != expected[body.Action] {
			t.Fatalf("raw request differs: %+v", req)
		}
		delete(expected, body.Action)
	}
	if len(expected) != 0 {
		t.Fatal("write requests were not observed", expected)
	}
}
