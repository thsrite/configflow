package routers_test

// Go-only R05 checks the Python corpus cannot observe: request cancellation
// (PORTING rule 4b), malformed device data (no panic in a request path), and
// credentials never reaching an error message.

import (
	"configflow/internal/routers/device"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/routers"
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/routerfake"
)

func ikuaiDriver(t *testing.T, state routerfake.IkuaiState) (*routers.Ikuai, *routerfake.Ikuai) {
	t.Helper()
	fake := routerfake.NewIkuai(state)
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	driver := routers.NewIkuai(server.URL, routerfake.IkuaiUser, routerfake.IkuaiPassword, false)
	t.Cleanup(driver.Close)
	return driver, fake
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func cancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func lastCall(state routerfake.IkuaiState) string {
	call := state.Calls[len(state.Calls)-1]
	body, _ := call.Body.(map[string]any)
	return fmt.Sprintf("%v/%v", body["func_name"], body["action"])
}

// A cancelled request context never abandons a started write: the add is
// read back, a batch proves its routes at the end, the DHCP restore after a
// lost reply runs, and a rollback runs every undo.
func TestIkuaiCancellationFinishesStartedWork(t *testing.T) {
	t.Run("route add read back", func(t *testing.T) {
		driver, fake := ikuaiDriver(t, routerfake.IkuaiState{})
		if _, err := driver.Apply(cancelled(), mustJSON(t, routeAdd("198.51.100.0/24", "cf-x"))); err != nil {
			t.Fatal(err)
		}
		state := fake.Snapshot()
		if len(state.Tables["static_rt"]) != 1 || lastCall(state) != "static_rt/show" {
			t.Fatalf("route not proven: %d rows, last %s", len(state.Tables["static_rt"]), lastCall(state))
		}
	})
	t.Run("batch proves adds after cancellation", func(t *testing.T) {
		driver, fake := ikuaiDriver(t, routerfake.IkuaiState{})
		ctx, cancel := context.WithCancel(context.Background())
		err := driver.Batch(ctx, false, func(ctx context.Context) error {
			for _, cidr := range []string{"198.51.100.0/25", "198.51.100.128/25"} {
				if _, err := driver.Apply(ctx, mustJSON(t, routeAdd(cidr, "cf-x"))); err != nil {
					return err
				}
			}
			cancel() // the browser went away after the writes
			return nil
		})
		if err != nil || lastCall(fake.Snapshot()) != "static_rt/show" {
			t.Fatalf("batch end read-back skipped: %v, last %s", err, lastCall(fake.Snapshot()))
		}
	})
	t.Run("dhcp local restore", func(t *testing.T) {
		server := ikServer(nil)
		driver, fake := ikuaiDriver(t, dhcpState(rows{server}, nil,
			routerfake.IkuaiFault{Func: "dhcp_server", Action: "edit", Count: 1, LoseReply: true}))
		_, err := driver.Apply(cancelled(), mustJSON(t, dhcpOp(server, nil)))
		if err == nil || !strings.Contains(err.Error(), "无法连接 iKuai") {
			t.Fatalf("err = %v", err)
		}
		if dns := fake.Snapshot().Tables["dhcp_server"][0]["dns1"]; dns != "198.51.100.5" {
			t.Fatalf("DHCP DNS not restored after cancellation: %v", dns)
		}
	})
	t.Run("rollback runs every undo", func(t *testing.T) {
		driver, fake := ikuaiDriver(t, routerfake.IkuaiState{})
		var undos []json.RawMessage
		for _, cidr := range []string{"198.51.100.0/25", "198.51.100.128/25"} {
			undo, err := driver.Apply(context.Background(), mustJSON(t, routeAdd(cidr, "cf-x")))
			if err != nil {
				t.Fatal(err)
			}
			undos = append(undos, undo)
		}
		ctx, cancel := context.WithCancel(context.Background())
		err := driver.Batch(ctx, true, func(ctx context.Context) error {
			for _, undo := range undos {
				cancel()
				if _, err := driver.Apply(ctx, undo); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil || len(fake.Snapshot().Tables["static_rt"]) != 0 {
			t.Fatalf("rollback interrupted: %v, %d routes left", err, len(fake.Snapshot().Tables["static_rt"]))
		}
	})
}

// odd is a value of every JSON shape a device could send where a string or
// number is expected.
var odd = []any{nil, true, 0, 1.5, "", "x", []any{}, []any{map[string]any{}}, map[string]any{"custom": "x"},
	map[string]any{"custom": []any{[]any{}, map[string]any{}}, "object": []any{1, map[string]any{"gid": []any{}, "gp_name": map[string]any{}}}}}

// Device rows with odd values in every field, and operations naming them,
// fail cleanly (an error or a result) instead of panicking.
func TestIkuaiMalformedDeviceDataNeverPanics(t *testing.T) {
	fields := map[string][]string{
		"static_rt":     {"enabled", "tagname", "dst_addr", "gateway", "netmask", "prio", "comment"},
		"nat_rule":      {"enabled", "tagname", "comment", "action", "src_addr", "dst_addr", "src_port", "dst_port"},
		"stream_ipport": {"enabled", "tagname", "comment", "src_addr", "dst_addr", "time", "type", "nexthop", "prio", "src_addr_inv"},
		"route_object":  {"group_name", "group_value", "group_id", "ref_count"},
		"dhcp_server":   {"enabled", "addr_pool", "netmask", "gateway", "dns1", "dns2", "status", "check_relay_only", "phy_ifnames"},
		"dhcp_static":   {"enabled", "ip_addr", "dns1", "dns2", "mac"},
	}
	for v, value := range odd {
		state := routerfake.IkuaiState{Tables: map[string][]routerfake.Row{}}
		for table, keys := range fields {
			row := routerfake.Row{"id": 1}
			if table == "nat_rule" {
				row["action"] = "filter"
			}
			for _, key := range keys {
				row[key] = value
			}
			second := routerfake.Row{"id": 2, "group_name": "cf_pa_1", "group_id": value, "tagname": value,
				"group_value": []any{map[string]any{"ip": value, "comment": "cf-mihomo"}}}
			state.Tables[table] = rows{row, second}
		}
		ops := []map[string]any{
			byID("nat_update", 1, "add_dst", []string{"198.51.100.0/24"}, "comment", "cf-x"),
			byID("nat_update", 1, "comment", "cf-x", "groups", []string{"cf_pa_1"}, "keep_foreign", true),
			byID("policy_route_update", 1, "gateway", ikGateway, "dst", []string{"198.51.100.0/24"}, "comment", "cf-x"),
			byID("route_comment", 1, "comment", "cf-x"), byID("route_delete", 1), byID("nat_delete", 1),
			{"kind": "nat_restore", "record": map[string]any{"id": 1, "dst_addr": value, "src_addr": value}},
			{"kind": "policy_route_readd", "record": map[string]any{"id": 1, "tagname": value, "comment": value,
				"dst_addr": map[string]any{"object": []any{map[string]any{"gid": value, "gp_name": "cf_pa_1"}}}}},
			{"kind": "add_undo", "func": "static_rt", "match": map[string]any{"tagname": value, "comment": value}, "confirmed": true},
			groupOp("ikuai_group_update", "cf_pa_1", "pa", []string{"198.51.100.0/24"}, "x"),
			groupOp("ikuai_group_delete", "cf_pa_1", "pa", nil, "x"),
			{"kind": "ikuai_group_restore", "name": "cf_pa_1", "role": "pa", "entries": []any{}, "expected": "x"},
			{"kind": "dhcp_dns_update", "id": "1", "dns": []any{ikDNS}, "expected": value,
				"scope": map[string]any{"id": "1", "network": "192.0.2.0/24", "gateway": ikGateway}},
			{"kind": "dhcp_dns_restore", "record": map[string]any{"id": value}},
			{"kind": value, "id": value, "record": value},
		}
		t.Run(fmt.Sprintf("value_%d", v), func(t *testing.T) {
			driver, _ := ikuaiDriver(t, state)
			call := func(name string, fn func() error) {
				t.Helper()
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s panicked: %v", name, r)
					}
				}()
				_ = fn()
			}
			call("ReadState", func() error { _, err := driver.ReadState(context.Background()); return err })
			call("ReadDHCPDNS", func() error { _, err := driver.ReadDHCPDNS(context.Background()); return err })
			call("Probe", func() error { _, err := driver.Probe(context.Background()); return err })
			for i, op := range ops {
				call(fmt.Sprintf("op %d", i), func() error { _, err := driver.Apply(context.Background(), mustJSON(t, op)); return err })
			}
			call("oversized", func() error {
				ikuai.IkuaiOversizedRequests([]json.RawMessage{mustJSON(t, map[string]any{"kind": "nat_update", "groups": value})})
				return nil
			})
		})
	}
}

// Passwords, their login digests, session details and redirect secrets never
// appear in an error message.
func TestIkuaiErrorsNeverCarryCredentials(t *testing.T) {
	password := "pa55-secret-word"
	sum := md5.Sum([]byte(password))
	secrets := []string{password, hex.EncodeToString(sum[:]), base64.StdEncoding.EncodeToString([]byte("salt_11" + password)),
		"secret-token", "secret-pass", "secret-detail"}
	states := []routerfake.IkuaiState{
		{Password: "other"},
		{Faults: []routerfake.IkuaiFault{{Func: "login", Status: 302, Location: "https://user:secret-pass@192.0.2.1:8443/x?token=secret-token"}}},
		{Faults: []routerfake.IkuaiFault{{Func: "login", Status: 500, RawBody: strp("secret-detail " + password)}}},
		{Faults: []routerfake.IkuaiFault{{Func: "login", RawBody: strp("<html>secret-detail</html>")}}},
		{Faults: []routerfake.IkuaiFault{{Func: "login", Drop: true}}},
	}
	for i, state := range states {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if state.Password == "" {
				state.Username, state.Password = routerfake.IkuaiUser, password
			}
			fake := routerfake.NewIkuai(state)
			server := httptest.NewServer(fake)
			defer server.Close()
			driver := routers.NewIkuai(server.URL, routerfake.IkuaiUser, password, false)
			defer driver.Close()
			_, err := driver.Probe(context.Background())
			if err == nil {
				t.Fatal("expected a failure")
			}
			for _, secret := range secrets {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error %q contains %q", err, secret)
				}
			}
		})
	}
	for _, class := range []string{devhttp.SSLError, devhttp.ConnectTimeout, devhttp.ReadTimeout, devhttp.ConnectionError,
		devhttp.ChunkedEncodingError, devhttp.InvalidURL} {
		driver := routers.NewIkuaiWithHTTP("http://192.0.2.1", routerfake.IkuaiUser, password, classHTTP{class})
		_, err := driver.Write(context.Background(), "nat_rule", "edit", map[string]any{"id": 1}, 0)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("%s: %v", class, err)
		}
	}
}

// IkuaiRequestBytes never undercounts the body devhttp sends.
func TestIkuaiRequestBytesBoundsTheBody(t *testing.T) {
	payloads := []map[string]any{
		{"comment": "中文 😀 \"q\" \\ \x01\b\f\n\t</>&  é", "tagname": "a,b:c"},
		{"group_value": []any{map[string]any{"ip": "198.51.100.0/24", "comment": "x"}}, "id": 9999999999},
		{},
	}
	for _, payload := range payloads {
		body, err := devhttp.EncodeJSON(map[string]any{"func_name": "nat_rule", "action": "edit", "param": payload})
		if err != nil {
			t.Fatal(err)
		}
		if got := ikuai.IkuaiRequestBytes("nat_rule", "edit", payload); got < len(body) {
			t.Fatalf("IkuaiRequestBytes = %d < %d sent", got, len(body))
		}
	}
	if device.UndoOf(errors.New("plain")) != nil || device.UndoOf(nil) != nil {
		t.Fatal("UndoOf of a non-router error")
	}
}

// classHTTP fails every /Action/call with one devhttp failure class.
type classHTTP struct{ class string }

func (c classHTTP) Do(_ context.Context, _, url string, _ any, _ time.Duration) (*devhttp.Response, error) {
	if strings.HasSuffix(url, "/Action/login") {
		return &devhttp.Response{Status: 200, Body: []byte(`{"Result":10000,"code":0}`)}, nil
	}
	return nil, &devhttp.Error{Class: c.class, Err: errors.New("synthetic-secret-detail")}
}

func (classHTTP) Close() {}
