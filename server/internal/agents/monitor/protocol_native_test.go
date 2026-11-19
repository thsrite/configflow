package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

type nativeRegistry struct {
	agent  model.Agent
	client agents.Client
}

func (m *nativeRegistry) ReadAgent(context.Context, string, bool) (*model.Agent, error) {
	a := m.agent
	return &a, nil
}
func (m *nativeRegistry) WithReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (m *nativeRegistry) DataDir() string       { return "" }
func (m *nativeRegistry) Client() agents.Client { return m.client }

type nativeClient struct {
	response *agents.Response
	request  agents.Request
	calls    int
	before   func(context.Context)
}

func (c *nativeClient) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.calls++
	c.request = r
	if c.before != nil {
		c.before(ctx)
	}
	return c.response, nil
}
func nativeFixture(t testing.TB, body string) (*nativeRegistry, *nativeClient) {
	t.Helper()
	var a model.Agent
	if err := json.Unmarshal([]byte(`{"id":"dns","host":"127.0.0.1","port":8082,"token":"synthetic-token","service_type":"oxidns","version":"1.8.60-go"}`), &a); err != nil {
		t.Fatal(err)
	}
	c := &nativeClient{response: &agents.Response{StatusCode: 200, Body: []byte(body)}}
	return &nativeRegistry{agent: a, client: c}, c
}

func TestNativeMonitorReplyUsesExactKeysAndRejectsDuplicateMembers(t *testing.T) {
	for _, tc := range []struct {
		body     string
		accepted bool
	}{
		{`{"success":true,"caches":[]}`, true},
		{`{"success":true,"caches":["cache"],"Caches":["alias"]}`, true},
		{`{"Success":true,"caches":["cache"]}`, false},
		{`{"success":true,"Caches":["cache"]}`, false},
		{`{"success":1,"caches":["cache"]}`, false},
		{`{"success":true,"caches":[null]}`, false},
		{`{"success":true,"caches":["cache","cache"]}`, false},
		{`{"success":true,"caches":["cache"],"caches":[]}`, false},
		{`{"success":true,"caches":["cache"],"ca\u0063hes":[]}`, false},
		{`{"success":true,"caches":[],"unused":{"x":1,"x":2}}`, false},
		{`[]`, false}, {`null`, false}, {`{"success":true}`, false}, {`invalid`, false},
	} {
		t.Run(tc.body, func(t *testing.T) {
			m, _ := nativeFixture(t, tc.body)
			got, err := CallCache(t.Context(), m, "dns", "GET", nil)
			if (err == nil) != tc.accepted {
				t.Fatalf("reply accepted=%v want=%v err=%v", err == nil, tc.accepted, err)
			}
			if err == nil && len(got.Caches) > 0 && got.Caches[0] != "cache" {
				t.Fatal("case alias changed result", got)
			}
		})
	}
}

func TestNativeMonitorStoredIdentityCannotUseR5Projection(t *testing.T) {
	for _, field := range []string{"host", "token", "service_type", "version"} {
		t.Run(field, func(t *testing.T) {
			m, c := nativeFixture(t, `{"success":true,"caches":[]}`)
			if err := m.agent.SetJSON(field, []byte(`7`)); err != nil {
				t.Fatal(err)
			}
			_, err := CallCache(t.Context(), m, "dns", "GET", nil)
			if err == nil || c.calls != 0 {
				t.Fatal("numeric identity authorized request", err, c.calls)
			}
		})
	}
	m, c := nativeFixture(t, `{"success":true,"caches":[]}`)
	if err := m.agent.SetJSON("version", []byte(`"１.８.６０-go"`)); err != nil {
		t.Fatal(err)
	}
	if _, err := CallCache(t.Context(), m, "dns", "GET", nil); err == nil || c.calls != 0 {
		t.Fatal("Unicode version authorized request", err)
	}
}

func TestNativeMonitorWritesDetachAndRespectShutdown(t *testing.T) {
	for _, operation := range []string{"cache", "rules", "panel"} {
		for _, shutdown := range []bool{false, true} {
			t.Run(operation+map[bool]string{false: "-disconnect", true: "-shutdown"}[shutdown], func(t *testing.T) {
				m, c := nativeFixture(t, `{"success":true,"flushed":["cache"]}`)
				if operation == "rules" {
					c.response.Body = []byte(`{"success":true,"task":{"id":"rule-1","status":"running","phase":"downloading","downloaded":0,"download_total":1,"reloaded":0,"reload_total":1}}`)
				}
				if operation == "panel" {
					if err := m.agent.SetJSON("service_type", []byte(`"mihomo"`)); err != nil {
						t.Fatal(err)
					}
					c.response.Body = []byte(`{}`)
				}
				runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				runtime.Start(context.Background())
				defer runtime.Shutdown(context.Background())
				bound := runtime.BindRequest(context.Background())
				disconnected, cancel := context.WithCancel(bound)
				cancel()
				if shutdown {
					c.before = func(context.Context) { runtime.Shutdown(context.Background()) }
				}
				call := func(ctx context.Context) error {
					switch operation {
					case "cache":
						_, err := CallCache(ctx, m, "dns", "POST", []string{"cache"})
						return err
					case "rules":
						_, _, err := CallRuleUpdate(ctx, m, "dns", "POST", nil)
						return err
					default:
						_, err := CallPanel(ctx, m, "dns", "configs", "PATCH", nil, []byte(`{"mode":"rule"}`), "")
						return err
					}
				}
				err := call(disconnected)
				if c.calls != 1 {
					t.Fatalf("disconnect aborted operation: %v", err)
				}
				if shutdown {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("shutdown reply accepted: %v", err)
					}
					if err := call(bound); !errors.Is(err, context.Canceled) || c.calls != 1 {
						t.Fatal("stopped generation sent request", err, c.calls)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func FuzzMonitorCacheProtocol(f *testing.F) {
	for _, seed := range []string{`{"success":true,"caches":["cache"]}`, `{"Success":true,"caches":[]}`, `{"success":true,"caches":[],"x":{"a":0,"a":1}}`, `null`, `[]`} {
		f.Add(seed)
	}
	f.Add("\ufeff{\"success\":true,\"caches\":[]}")
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > CacheMaxResponseBytes+1 {
			return
		}
		m, c := nativeFixture(t, body)
		result, err := CallCache(t.Context(), m, "dns", "GET", nil)
		if err == nil {
			if result == nil || !result.Success || !validTags(result.Caches, false) {
				t.Fatal("unvalidated cache result")
			}
			var fields model.Fields
			decoded, valid := agents.DecodeJSONBytes([]byte(body))
			if !valid {
				t.Fatal("accepted unsupported response bytes")
			}
			normalized, _ := jsonresp.NormalizeNonFinite([]byte(decoded))
			if json.Unmarshal(normalized, &fields) != nil {
				t.Fatal("invalid/duplicate reply accepted")
			}
			success, present, valid := answerBool(fields, "success")
			if !success || !present || !valid {
				t.Fatal("success alias accepted")
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "synthetic-token") {
				t.Fatal("credential leaked")
			}
		}
		if c.request.Body != nil || c.request.Method != "GET" || c.request.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Fatal("request bytes changed")
		}
	})
}

func TestNativePanelRejectsNonJSONUnicodeWhitespace(t *testing.T) {
	for _, body := range []string{"\u00a0", "\u2000", "\u3000"} {
		t.Run(body, func(t *testing.T) {
			m, _ := nativeFixture(t, body)
			if err := m.agent.SetJSON("service_type", []byte(`"mihomo"`)); err != nil {
				t.Fatal(err)
			}
			_, err := CallPanel(t.Context(), m, "dns", "version", "GET", nil, nil, "")
			wantMonitorError(t, err, "INVALID_RESPONSE", 502)
		})
	}
}
