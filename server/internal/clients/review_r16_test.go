package clients

import (
	"configflow/internal/routers/clientlist"
	"configflow/internal/routers/device"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/routerdoc"
)

// A router may echo another router's credential, not merely its own. Sanitize
// after joining saved rows, live metadata and the cache's refresh diagnostic.
func TestReviewR16DiscoveryCredentialReferences(t *testing.T) {
	const firstSecret = "synthetic-first-password"
	const secondSecret = "synthetic-second-password+/@"
	const agentSecret = "synthetic-agent-token"
	secrets := redact.NewSecrets([]string{firstSecret, secondSecret, agentSecret})
	for _, mode := range []string{"live", "cached", "refresh-failure", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			rt := testRuntime(t)
			var failed atomic.Bool
			s := &DiscoveryService{Spawner: rt, Open: func(r *spec.Router) clientlist.Reader {
				return reader{read: func(context.Context, clientlist.ReadOptions) (*routerdoc.Reading, error) {
					if failed.Load() {
						return nil, &device.RouterError{Message: "peer diagnostic " + secondSecret}
					}
					reading := snapshot(url.QueryEscape(secondSecret)).Readings[0].Reading
					reading.Notes = []string{"agent diagnostic " + agentSecret}
					return reading, nil
				}}
			}}
			config := raw(map[string]any{
				"routers": map[string]any{"items": []any{
					map[string]any{"id": "first", "type": "routeros", "name": "synthetic-user", "password": firstSecret},
					map[string]any{"id": "second", "type": "routeros", "name": firstSecret, "password": secondSecret},
				}},
				"agents":       []any{map[string]any{"id": "agent", "token": agentSecret}},
				"device_notes": map[string]string{"192.0.2.20": firstSecret},
			})
			if mode == "unsupported" {
				config = json.RawMessage(strings.ReplaceAll(string(config), "routeros", "unsupported"))
			}
			original := string(config)
			if mode != "unsupported" && url.QueryEscape(secondSecret) == secondSecret {
				t.Fatal("encoded credential fixture did not encode")
			}
			opts := DiscoveryOptions{Cache: NewDiscoveryCache(rt, nil), ProfileID: "synthetic"}
			out, err := s.Discover(rt.BindRequest(t.Context()), config, opts)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "cached" || mode == "refresh-failure" {
				failed.Store(true)
				opts.Refresh = mode == "refresh-failure"
				out, err = s.Discover(rt.BindRequest(t.Context()), config, opts)
				if err != nil || !out.Cached {
					t.Fatalf("cached result missing: %v", err)
				}
				if opts.Refresh && out.RefreshError == "" {
					t.Fatal("failure control did not execute")
				}
			}
			if redact.ContainsEncodedSecret(string(raw(out)), secrets) {
				t.Error("discovery disclosed a known credential reference")
			}
			if mode != "unsupported" && (out.State != "ok" || out.Sources[0].Name != "synthetic-user" || len(out.Discovered) != 1) {
				t.Fatal("healthy evidence or public username lost")
			}
			if original != string(config) {
				t.Fatal("public projection changed private configuration")
			}
		})
	}
}

// Malformed list members must not hide valid clients from that same router.
// A truncated table cannot prove that an absent saved host is offline.
func TestReviewR16MalformedRowsRetainHealthyClients(t *testing.T) {
	for _, table := range []string{"/ip/arp", "/ip/address", "/ip/dhcp-server", "/ip/dhcp-server/lease"} {
		t.Run(table, func(t *testing.T) {
			wire := rosWire()
			var rows []json.RawMessage
			if err := json.Unmarshal(wire.tables[table], &rows); err != nil {
				t.Fatal(err)
			}
			rows = append(rows, json.RawMessage(`null`), json.RawMessage(`false`), json.RawMessage(`"bad row"`))
			wire.tables[table] = raw(rows)
			server := httptest.NewServer(wire)
			defer server.Close()
			cfg := raw(map[string]any{"routers": map[string]any{"items": []any{map[string]any{
				"id": "router", "type": "routeros", "endpoint": server.URL,
			}}}, "device_notes": map[string]string{"192.0.2.99": "absent", "192.0.2.1": "router"}})
			out, err := NewDiscoveryService().Discover(t.Context(), cfg, DiscoveryOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if out.State != "ok" || len(out.Discovered) != 2 || out.Discovered[0].IP != "192.0.2.10" {
				t.Fatalf("healthy clients lost: state=%s count=%d", out.State, len(out.Discovered))
			}
			devices := map[string]ObservedDevice{}
			for _, d := range out.Devices {
				devices[d.IP] = d
			}
			if devices["192.0.2.99"].Presence != "unknown" || len(out.Sources[0].Notes) == 0 {
				t.Fatal("incomplete table incorrectly proved absence")
			}
			if devices["192.0.2.1"].Basis != "router" || out.Discovered[0].Hostname != "租约设备" {
				t.Fatal("healthy address/lease evidence discarded")
			}
		})
	}
}

func TestReviewR16RouterRecordsAndIkuaiNulls(t *testing.T) {
	for _, table := range []string{"monitor_lanip", "dhcp_lease", "lan"} {
		t.Run(table, func(t *testing.T) {
			wire := &discoveryWire{tables: map[string]json.RawMessage{
				"monitor_lanip": json.RawMessage(`[{"ip_addr":"192.0.2.10","mac":"aa:bb:cc:00:00:10"},null,false,{"ip_addr":null}]`),
				"dhcp_lease":    json.RawMessage(`[{"ip_addr":"192.0.2.11","mac":"aa:bb:cc:00:00:11"}]`),
				"lan":           json.RawMessage(`[{"ip_mask":"192.0.2.1/24"},null]`),
			}, faults: map[string]int{}}
			wire.tables[table] = json.RawMessage(`null`)
			server := httptest.NewServer(wire)
			defer server.Close()
			cfg := raw(map[string]any{"routers": map[string]any{"items": []any{
				nil, false, "invalid", map[string]any{"id": "bad", "type": nil},
				map[string]any{"id": "good", "type": "ikuai", "name": nil, "endpoint": server.URL, "username": "synthetic-user", "rule_sync_interval": "bad"},
			}}})
			out, err := NewDiscoveryService().Discover(t.Context(), cfg, DiscoveryOptions{})
			if err != nil || out.State != "ok" || len(out.Sources) != 1 || out.Sources[0].Name != "good" || len(out.Discovered) == 0 {
				t.Fatalf("null/fallback hid healthy records: %v %+v", err, out)
			}
		})
	}
}

func TestReviewR16RealDriverPeerCredential(t *testing.T) {
	const secret = "synthetic-peer-password"
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"detail": "peer credential " + secret})
	}))
	defer bad.Close()
	good := httptest.NewServer(rosWire())
	defer good.Close()
	cfg := raw(map[string]any{"routers": map[string]any{"items": []any{
		map[string]any{"id": "bad", "type": "routeros", "endpoint": bad.URL, "password": "synthetic-own-password"},
		map[string]any{"id": "good", "type": "routeros", "endpoint": good.URL, "password": secret},
	}}})
	out, err := NewDiscoveryService().Discover(t.Context(), cfg, DiscoveryOptions{})
	if err != nil || out.State != "partial" || out.Sources[0].OK || !out.Sources[1].OK || len(out.Discovered) != 2 {
		t.Fatalf("peer isolation failed: %v", err)
	}
	if !strings.Contains(out.Sources[0].Error, redact.Redacted) || strings.Contains(string(raw(out)), secret) {
		t.Fatalf("real driver peer diagnostic was not redacted: %q", out.Sources[0].Error)
	}
}

func TestReviewR16InvalidARPContainerRemainsFailure(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `false`} {
		t.Run(body, func(t *testing.T) {
			wire := rosWire()
			wire.tables["/ip/arp"] = json.RawMessage(body)
			server := httptest.NewServer(wire)
			defer server.Close()
			cfg := raw(map[string]any{"routers": map[string]any{"items": []any{map[string]any{"id": "bad", "type": "routeros", "endpoint": server.URL}}}})
			out, err := NewDiscoveryService().Discover(t.Context(), cfg, DiscoveryOptions{})
			if err != nil || out.State != "failed" || out.Sources[0].OK || len(out.Discovered) != 0 {
				t.Fatal("malformed container silently became a successful empty scan")
			}
		})
	}
}

func TestReviewR16CancellationAtRepositoryCommit(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			rt := testRuntime(t)
			r, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			before := configJSON(t, r)
			revision, err := r.ConfigRevision(t.Context(), "default")
			if err != nil {
				t.Fatal(err)
			}
			files := reviewR16Files(t, r.DataDir())
			request, cancel := context.WithCancel(t.Context())
			defer cancel()
			reached := false
			_, err = r.UpdateProfile(rt.BindRequest(request), "default", func(work context.Context, current *model.Profile) error {
				next, e := UpdateDevice(work, raw(current), Change{IP: "192.0.2.10", Name: "synthetic client"}, "")
				if e != nil {
					return e
				}
				var decoded model.Profile
				if e := json.Unmarshal(next, &decoded); e != nil {
					return e
				}
				reached = true
				if shutdown {
					rt.Shutdown(context.Background())
				} else {
					cancel()
				}
				*current = decoded
				return nil
			})
			if !reached {
				t.Fatal("commit checkpoint was not reached")
			}
			afterRevision, e := r.ConfigRevision(t.Context(), "default")
			if e != nil {
				t.Fatal(e)
			}
			if shutdown {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("shutdown commit: %v", err)
				}
				equalJSON(t, "shutdown DB", before, configJSON(t, r))
				if afterRevision != revision || !reflect.DeepEqual(files, reviewR16Files(t, r.DataDir())) {
					t.Fatal("shutdown changed revision/files")
				}
			} else {
				if err != nil || afterRevision != revision+1 {
					t.Fatalf("disconnect commit: revision delta=%d err=%v", afterRevision-revision, err)
				}
				devices, e := ListDevices(configJSON(t, r))
				if e != nil || len(devices.Devices) != 1 || devices.Devices[0].Name != "synthetic client" {
					t.Fatal("disconnected write lost")
				}
			}
		})
	}
}

func reviewR16Files(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.Contains(entry.Name(), ".sqlite3") || strings.HasSuffix(entry.Name(), ".lock") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
