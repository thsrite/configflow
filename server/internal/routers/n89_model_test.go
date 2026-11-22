package routers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

func projectionRepository(t testing.TB, n, padding int) *repo.Repository {
	t.Helper()
	db, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.UpdateProfile(context.Background(), "default", func(_ context.Context, p *model.Profile) error {
		p.Mihomo.CustomConfig = strings.Repeat("x", padding)
		for i := 0; i < n; i++ {
			p.Routers.Items = append(p.Routers.Items, model.Router{ID: fmt.Sprintf("r%d", i), Name: "router", Type: "routeros", Endpoint: fmt.Sprintf("http://192.0.2.1:%d", 8000+i), Username: "operator", Password: "synthetic-password"})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = routerProfiles(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func BenchmarkRouterProjection(b *testing.B) {
	for _, n := range []int{1, 16, 64} {
		for _, padding := range []int{0, 1 << 20} {
			b.Run(fmt.Sprintf("routers%d/padding%d", n, padding), func(b *testing.B) {
				db := projectionRepository(b, n, padding)
				// Allocation measurements exclude the test-only view guard's encodings.
				db.SetViewGuard(nil)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					rows, err := routerProfiles(context.Background(), db)
					if err != nil || len(rows) != 1 || len(rows[0].Routers) != n {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// A large unrelated YAML section must not be decoded once per router. This
// measures bytes allocated, not wall time, and includes the real repo read.
func TestN89RouterProjectionAllocations(t *testing.T) {
	for _, n := range []int{1, 16} {
		measure := func(padding int) int64 {
			db := projectionRepository(t, n, padding)
			// Allocation measurements exclude the test-only view guard's encodings.
			db.SetViewGuard(nil)
			result := testing.Benchmark(func(b *testing.B) {
				for b.Loop() {
					if _, err := routerProfiles(context.Background(), db); err != nil {
						b.Fatal(err)
					}
				}
			})
			t.Logf("routers=%d padding=%d B/op=%d allocs/op=%d", n, padding, result.AllocedBytesPerOp(), result.AllocsPerOp())
			return result.AllocedBytesPerOp()
		}
		small, large := measure(0), measure(1<<20)
		if large-small > 64<<10 {
			t.Fatalf("unrelated configuration decoded in router loop: delta=%d", large-small)
		}
	}
}

// TestN89StoredRouterReadsWrongTypesLeniently (O18 T1c; was
// TestN89StoredRouterPreservesWrongTypesAndOpaqueFields, which kept the stored
// bytes and wrong-typed values): wrong-typed stored settings read as their
// runtime defaults or by the switch rule, undeclared members are dropped, and
// an edit changes only the edited member of the Go encoding.
func TestN89StoredRouterReadsWrongTypesLeniently(t *testing.T) {
	source := []byte(`{"id":"r","name":true,"type":"routeros","endpoint":"http://192.0.2.1","username":"operator","password":"opaque", "verify_tls":"bad","rule_sync_interval":1.5,"dns_guard":{"enabled":"bad","failover":false,"unknown":9223372036854775808123},"custom":9223372036854775808123}`)
	var r Router
	if err := json.Unmarshal(source, &r); err != nil {
		t.Fatal(err)
	}
	// verify_tls is switch:"on" (wrong type reads as on); dns_guard.enabled is
	// switch:"off" (wrong type reads as off); 1.5 is not an interval.
	if r.Name != "" || !r.VerifyTLS || r.RuleSyncInterval != 60 || r.DNSGuard == nil || r.DNSGuard.Enabled || r.DNSGuard.Failover {
		t.Fatalf("wrong runtime defaults: %+v %+v", r.RouterSettings, r.DNSGuard)
	}
	unchanged, err := r.MarshalJSON()
	if err != nil || bytes.Contains(unchanged, []byte("9223372036854775808123")) || bytes.Contains(unchanged, []byte(`"bad"`)) {
		t.Fatalf("undeclared or wrong-typed value written back: %s %v", unchanged, err)
	}
	r.Username = "changed"
	changed, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var old, next map[string]json.RawMessage
	_ = json.Unmarshal(unchanged, &old)
	_ = json.Unmarshal(changed, &next)
	for key, value := range old {
		if key != "username" && !bytes.Equal(value, next[key]) {
			t.Fatalf("unrelated stored member %s changed: %s -> %s", key, value, next[key])
		}
	}
	if string(next["username"]) != `"changed"` {
		t.Fatalf("edit not written: %s", changed)
	}
}

func TestN89StoredIdentityCannotOpenDevice(t *testing.T) {
	for _, field := range []string{"id", "type", "endpoint", "username", "password", "proxy_gateway_agent_id", "dns_agent_id"} {
		for _, value := range []string{`12`, `true`, `[]`, `{}`, `null`} {
			t.Run(field+value, func(t *testing.T) {
				optionalNull := (field == "proxy_gateway_agent_id" || field == "dns_agent_id") && value == `null`
				fields := map[string]json.RawMessage{"id": json.RawMessage(`"r"`), "type": json.RawMessage(`"routeros"`), "endpoint": json.RawMessage(`"http://192.0.2.1"`)}
				fields[field] = json.RawMessage(value)
				raw, _ := json.Marshal(fields)
				var r Router
				if err := json.Unmarshal(raw, &r); err != nil {
					t.Fatal(err)
				}
				err := r.ValidateForDevice()
				if optionalNull {
					if err != nil {
						t.Fatal("null optional link rejected", err)
					}
				} else if err == nil {
					t.Fatal("invalid identity allowed device access")
				}
			})
		}
	}
}

func FuzzN89StoredRouter(f *testing.F) {
	f.Add([]byte(`{"id":"r","name":14,"verify_tls":"bad","opaque":9223372036854775808123}`))
	f.Add([]byte(`{"dns_guard":{"enabled":true,"extra":[1,2]},"password":"bytes\u001c"}`))
	f.Fuzz(func(t *testing.T, source []byte) {
		var r Router
		if json.Unmarshal(source, &r) != nil {
			return
		}
		out, err := r.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		var expected model.Router
		if json.Unmarshal(source, &expected) != nil {
			t.Fatal("model disagrees")
		}
		want, err := model.Marshal(&expected)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, want) {
			t.Fatalf("read changed stored record: %s -> %s", source, out)
		}
		r.Name = "edited"
		out, err = r.MarshalJSON()
		if err != nil || !json.Valid(out) {
			t.Fatalf("invalid edit: %s %v", out, err)
		}
	})
}

// TestN89UnknownEditsAndStateRemoval (O18 T1c): Extra keeps only the
// declared device-state members. Editing or adding an undeclared member is
// dropped on write (it used to be written back with the opaque members);
// editing or removing a declared state member is written; the shared stored
// record is not mutated.
func TestN89UnknownEditsAndStateRemoval(t *testing.T) {
	source := []byte(`{"id":"r","future":1,"last_probe":{"ok":true},"opaque":9223372036854775808123}`)
	for _, tc := range []struct {
		name string
		edit func(*Router)
		key  string
		want string
	}{
		{"modify-undeclared", func(r *Router) { r.Extra["future"] = json.RawMessage(`2`) }, "future", ""},
		{"add-undeclared", func(r *Router) { r.Extra["new_field"] = json.RawMessage(`{"x":true}`) }, "new_field", ""},
		{"modify-state", func(r *Router) { r.Extra["last_probe"] = json.RawMessage(`{"at":"t","ok":false}`) }, "last_probe", `{"at":"t","ok":false}`},
		{"drop-state", func(r *Router) { delete(r.Extra, "last_probe") }, "last_probe", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r Router
			if err := json.Unmarshal(source, &r); err != nil {
				t.Fatal(err)
			}
			before, err := model.Marshal(r.stored)
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(&r)
			out, err := r.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(out, &fields)
			if string(fields[tc.key]) != tc.want || fields["opaque"] != nil {
				t.Fatalf("bad edit: %s", out)
			}
			unchanged, err := model.Marshal(r.stored)
			if err != nil || !bytes.Equal(unchanged, before) {
				t.Fatal("edit mutated the shared stored record")
			}
		})
	}
}

// TestN89MalformedActiveGuardBlocksDeviceAccess: a guard whose stored settings
// have the wrong type must not authorize device management.
func TestN89MalformedActiveGuardBlocksDeviceAccess(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		blocked bool // false: accepted O18 T1c semantics (see below)
	}{
		// Suspected regression handed back to the T1c author: an enabled guard
		// whose bypass list has the wrong type now reads as an enabled guard
		// with no bypass (ValidateForDevice checks model.Issues, which is
		// always empty for plain records).
		{`{"dns_guard":{"enabled":true,"bypass":"x"}}`, true},
		// Accepted: a disabled guard (switch:"off" reads a wrong type as off)
		// with a malformed list may be withdrawn using its stored state.
		{`{"dns_guard":{"enabled":false,"bypass":"x"},"dns_guard_state":{"resources":[]}}`, false},
		{`{"ipv6_pd_guard":{"enabled":"x"},"ipv6_pd_guard_state":{"resources":[]}}`, false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			var r Router
			if err := json.Unmarshal([]byte(tc.raw), &r); err != nil {
				t.Fatal(err)
			}
			err := r.ValidateForDevice()
			if !tc.blocked {
				if err != nil {
					t.Fatalf("disabled guard blocked: %v", err)
				}
				return
			}
			if err == nil || err.Error() != messageStoredGuardInvalid {
				t.Fatalf("malformed enabled guard not blocked: %v", err)
			}
		})
	}
}
