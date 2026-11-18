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

func TestN89StoredRouterPreservesWrongTypesAndOpaqueFields(t *testing.T) {
	source := []byte(`{"id":"r","name":true,"type":"routeros","endpoint":"http://192.0.2.1","username":"operator","password":"opaque", "verify_tls":"bad","rule_sync_interval":1.5,"dns_guard":{"enabled":"bad","failover":false,"unknown":9223372036854775808123},"custom":9223372036854775808123}`)
	var r Router
	if err := json.Unmarshal(source, &r); err != nil {
		t.Fatal(err)
	}
	if r.Name != "" || !r.VerifyTLS || r.RuleSyncInterval != 60 || r.DNSGuard != nil {
		t.Fatalf("wrong runtime defaults: %#v", r)
	}
	unchanged, err := r.MarshalJSON()
	if err != nil || !bytes.Equal(unchanged, source) {
		t.Fatalf("unchanged record lost provenance: %s %v", unchanged, err)
	}
	r.Username = "changed"
	changed, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var old, next map[string]json.RawMessage
	_ = json.Unmarshal(source, &old)
	_ = json.Unmarshal(changed, &next)
	for key, value := range old {
		if key != "username" && !bytes.Equal(value, next[key]) {
			t.Fatalf("unrelated stored member %s changed: %s -> %s", key, value, next[key])
		}
	}
}

func TestN89StoredIdentityCannotSelectDevice(t *testing.T) {
	for _, field := range []string{"id", "type", "endpoint", "username", "password", "proxy_gateway_agent_id", "dns_agent_id"} {
		for _, value := range []string{`12`, `true`, `[]`, `{}`, `null`} {
			t.Run(field+value, func(t *testing.T) {
				if (field == "proxy_gateway_agent_id" || field == "dns_agent_id") && value == `null` {
					return
				}
				fields := map[string]json.RawMessage{"id": json.RawMessage(`"r"`), "type": json.RawMessage(`"routeros"`), "endpoint": json.RawMessage(`"http://192.0.2.1"`)}
				fields[field] = json.RawMessage(value)
				raw, _ := json.Marshal(fields)
				var r Router
				if err := json.Unmarshal(raw, &r); err != nil {
					t.Fatal(err)
				}
				_, release, err := RouterJobLock(t.Context(), "p", "r", &r)
				if release != nil {
					release()
				}
				if err == nil {
					t.Fatal("invalid identity obtained device write lock")
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

func BenchmarkSyncEnvironment(b *testing.B) {
	for _, n := range []int{1, 16, 64} {
		for _, padding := range []int{0, 1 << 20} {
			b.Run(fmt.Sprintf("rules%d/padding%d", n, padding), func(b *testing.B) {
				raw, _ := json.Marshal(map[string]any{"mihomo": map[string]string{"custom_config": strings.Repeat("x", padding)}, "system_config": map[string]string{"rule_fetch_proxy": ""}, "rule_library": []any{}})
				ctx, err := withSyncEnvironment(context.Background(), raw)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					for i := 0; i < n; i++ {
						if _, err = syncEnvironmentConfig(ctx, raw); err != nil {
							b.Fatal(err)
						}
						_ = syncTransportKey(ctx, "https://example.com/rule")
					}
				}
			})
		}
	}
}
func TestN89SyncEnvironmentAllocations(t *testing.T) {
	measure := func(n, padding int) int64 {
		raw, _ := json.Marshal(map[string]any{"mihomo": map[string]string{"custom_config": strings.Repeat("x", padding)}})
		ctx, err := withSyncEnvironment(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		result := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				for i := 0; i < n; i++ {
					if _, err := syncEnvironmentConfig(ctx, raw); err != nil {
						b.Fatal(err)
					}
					_ = syncTransportKey(ctx, "https://example.com/rule")
				}
			}
		})
		t.Logf("rules=%d padding=%d B/op=%d allocs/op=%d", n, padding, result.AllocedBytesPerOp(), result.AllocsPerOp())
		return result.AllocedBytesPerOp()
	}
	for _, n := range []int{1, 64} {
		small, large := measure(n, 0), measure(n, 1<<20)
		if large-small > 32<<10 {
			t.Fatalf("full config decoded per rule: delta=%d", large-small)
		}
	}
}
