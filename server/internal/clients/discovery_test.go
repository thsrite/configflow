package clients

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/routerdoc"
	"configflow/internal/routers"
)

type reader struct {
	read   func(context.Context, routers.ClientReadOptions) (*routerdoc.Reading, error)
	closed *atomic.Int32
}

func (r reader) ReadClients(ctx context.Context, o routers.ClientReadOptions) (*routerdoc.Reading, error) {
	return r.read(ctx, o)
}
func (r reader) Close() {
	if r.closed != nil {
		r.closed.Add(1)
	}
}
func testRuntime(t *testing.T) *lifecycle.Runtime {
	t.Helper()
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual, Grace: 40 * time.Millisecond})
	rt.Start(context.Background())
	t.Cleanup(func() { rt.Shutdown(context.Background()) })
	return rt
}
func TestDiscoveryMergesAndRejoinsCurrentState(t *testing.T) {
	rt := testRuntime(t)
	var calls, closed atomic.Int32
	service := &DiscoveryService{Spawner: rt, GatewayHost: func(context.Context, string) string { return "192.0.2.254" }}
	service.Open = func(r *routers.Router) routers.ClientReader {
		return reader{closed: &closed, read: func(ctx context.Context, o routers.ClientReadOptions) (*routerdoc.Reading, error) {
			calls.Add(1)
			if !o.Priority["192.0.2.10"] {
				t.Error("saved priority missing")
			}
			reading := snapshot("first").Readings[0].Reading
			reading.Own["192.0.2.1"] = true
			reading.LAN = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}
			s := routerdoc.NewSighting("192.0.2.11")
			s.MAC = "AA:BB:CC:00:00:11"
			s.Presence = "recent"
			s.AddSource("arp")
			reading.Sightings[s.IP] = s
			return reading, nil
		}}
	}
	cache := NewDiscoveryCache(rt, nil)
	config := json.RawMessage(`{"routers":{"items":[{"id":"r1","type":"routeros","endpoint":"http://192.0.2.1","proxy_gateway_agent_id":"a","ros_policy_rule_sets":["rule"]}]},"device_notes":{"192.0.2.10":"旧名称","192.0.2.1":"主路由","192.0.2.99":"缺失","198.51.100.1":"外部"}}`)
	first, err := service.Discover(rt.BindRequest(context.Background()), config, DiscoveryOptions{ProfileID: "office", Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	if first.State != "ok" || len(first.Discovered) != 1 || first.Discovered[0].IP != "192.0.2.11" {
		t.Fatalf("first %+v", first)
	}
	evidence := map[string]ObservedDevice{}
	for _, d := range first.Devices {
		evidence[d.IP] = d
	}
	if evidence["192.0.2.1"].Basis != "router" || evidence["192.0.2.99"].Presence != "offline" || evidence["198.51.100.1"].Basis != "outside" {
		t.Fatal("saved evidence", evidence)
	}
	if first.Sources[0].Gateway == nil || *first.Sources[0].Gateway != "192.0.2.254" {
		t.Fatal("gateway")
	}
	updated := json.RawMessage(strings.ReplaceAll(string(config), "旧名称", "新名称"))
	second, err := service.Discover(rt.BindRequest(context.Background()), updated, DiscoveryOptions{ProfileID: "office", Cache: cache, AgentHosts: []string{"192.0.2.11"}})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Cached || len(second.Discovered) != 0 || calls.Load() != 1 || closed.Load() != 1 {
		t.Fatal("cache must rejoin current names/hosts")
	}
	found := false
	for _, d := range second.Devices {
		if d.IP == "192.0.2.10" {
			found = d.Name == "新名称"
		}
	}
	if !found {
		t.Fatal("cached old identity")
	}
	_, err = service.Discover(context.Background(), json.RawMessage(`{}`), DiscoveryOptions{ProfileID: "office", Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Discover(context.Background(), config, DiscoveryOptions{ProfileID: "office", Cache: cache})
	if err != nil || calls.Load() != 2 {
		t.Fatalf("unsupported must invalidate: %v", err)
	}
}
func TestDiscoveryTimeoutAndPanicIsolation(t *testing.T) {
	for _, mode := range []string{"timeout", "panic", "error"} {
		t.Run(mode, func(t *testing.T) {
			rt := testRuntime(t)
			var closed atomic.Int32
			service := &DiscoveryService{Spawner: rt, Open: func(r *routers.Router) routers.ClientReader {
				return reader{closed: &closed, read: func(ctx context.Context, _ routers.ClientReadOptions) (*routerdoc.Reading, error) {
					if r.ID == "slow" {
						switch mode {
						case "panic":
							panic("synthetic reader bug")
						case "error":
							return nil, errors.New("synthetic read failure")
						default:
							<-ctx.Done()
							return nil, ctx.Err()
						}
					}
					return snapshot("good").Readings[0].Reading, nil
				}}
			}}
			cfg := json.RawMessage(`{"routers":{"items":[{"id":"slow","type":"routeros"},{"id":"fast","type":"ikuai"}]},"device_notes":{"192.0.2.99":"缺失"}}`)
			result, err := service.Discover(rt.BindRequest(context.Background()), cfg, DiscoveryOptions{Timeout: 80 * time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			if result.State != "partial" || result.Sources[0].OK || !result.Sources[1].OK || result.Devices[0].Presence != "unknown" {
				t.Fatalf("isolation: %+v", result)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err = rt.WaitIdle(ctx); err != nil || closed.Load() != 2 {
				t.Fatalf("workers leaked: %d %v", closed.Load(), err)
			}
		})
	}
}
func TestOwnersDetachDisconnectAndRejectShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			rt := testRuntime(t)
			cache := NewDiscoveryCache(rt, nil)
			request, cancel := context.WithCancel(context.Background())
			ctx := rt.BindRequest(request)
			started := make(chan struct{})
			release := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				_, _, err := cache.Read(ctx, "office", cacheRouters("synthetic"), func(work context.Context) (Snapshot, error) {
					close(started)
					if shutdown {
						<-work.Done()
					} else {
						<-release
					}
					return snapshot("loaded"), nil
				}, false)
				done <- err
			}()
			<-started
			if shutdown {
				rt.Shutdown(context.Background())
			} else {
				cancel()
				close(release)
			}
			err := <-done
			cancel()
			if shutdown {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("shutdown: %v", err)
				}
				cache.mu.Lock()
				for _, e := range cache.entries {
					if e.snapshot != nil {
						t.Error("snapshot committed after shutdown")
					}
				}
				cache.mu.Unlock()
				if _, err = UpdateDevice(ctx, json.RawMessage(`{}`), Change{IP: "192.0.2.10", Name: "x"}, ""); !errors.Is(err, context.Canceled) {
					t.Fatalf("post-shutdown update: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err = UpdateDevice(ctx, json.RawMessage(`{}`), Change{IP: "192.0.2.10", Name: "x"}, ""); err != nil {
					t.Fatalf("disconnect update: %v", err)
				}
			}
		})
	}
}
func TestDeviceLosslessRenameAndRemoval(t *testing.T) {
	config := json.RawMessage(`{"device_notes":{"192.0.2.20":"second","192.0.2.10":"first"},"oxidns":{"future":{"preserve":true}},"future":{"nested":[1,2]},"client_fixed_ips":{"192.0.2.20":{"future":true}}}`)
	next, err := UpdateDevice(context.Background(), config, Change{IP: "192.0.2.20", Name: "renamed", PreviousName: "second"}, "")
	if err != nil {
		t.Fatal(err)
	}
	list, err := ListDevices(next)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Devices) != 2 || list.Devices[0].IP != "192.0.2.20" || list.Devices[0].Name != "renamed" {
		t.Fatal("rename changed ordering", list)
	}
	var before, after map[string]json.RawMessage
	_ = json.Unmarshal(config, &before)
	_ = json.Unmarshal(next, &after)
	for _, key := range []string{"oxidns", "future", "client_fixed_ips"} {
		equalJSON(t, key, before[key], after[key])
	}
	final, err := UpdateDevice(context.Background(), next, Change{IP: "192.0.2.20", PreviousName: "renamed"}, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(final, &after)
	equalJSON(t, "removed binding", json.RawMessage(`{}`), after["client_fixed_ips"])
}

func TestRepositoryDeviceWriteRespectsRuntime(t *testing.T) {
	rt := testRuntime(t)
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	request, cancel := context.WithCancel(context.Background())
	ctx := rt.BindRequest(request)
	cancel()
	save := func(c Change) error {
		_, err := r.UpdateProfile(ctx, "default", func(work context.Context, current *model.Profile) error {
			next, err := UpdateDevice(work, raw(current), c, "")
			if err != nil {
				return err
			}
			var decoded model.Profile
			if err := json.Unmarshal(next, &decoded); err != nil {
				return err
			}
			*current = decoded
			return nil
		})
		return err
	}
	if err = save(Change{IP: "192.0.2.10", Name: "saved after disconnect"}); err != nil {
		t.Fatal(err)
	}
	before := configJSON(t, r)
	list, err := ListDevices(before)
	if err != nil || len(list.Devices) != 1 {
		t.Fatalf("write lost after disconnect: %v", err)
	}
	rt.Shutdown(context.Background())
	if err = save(Change{IP: "192.0.2.10", Name: "must not save", PreviousName: "saved after disconnect"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown write accepted: %v", err)
	}
	equalJSON(t, "shutdown database unchanged", before, configJSON(t, r))
}
