package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/oplock"
	"configflow/internal/routers/routeedit"
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/lockx"
	"configflow/internal/routers/routerfake"
)

type r06CancelHTTP struct {
	client   device.DeviceHTTP
	cancel   context.CancelFunc
	vendor   string
	written  bool
	observed int
}

func (c *r06CancelHTTP) Do(ctx context.Context, method, target string, body any, timeout time.Duration) (*devhttp.Response, error) {
	response, err := c.client.Do(ctx, method, target, body, timeout)
	isWrite := c.vendor == "routeros" && (method == "PUT" || method == "PATCH" || method == "DELETE")
	if c.vendor == "ikuai" && strings.HasSuffix(target, "/Action/call") {
		if b, ok := body.(map[string]any); ok {
			isWrite = b["action"] != "show"
		}
	}
	if c.written {
		c.observed++
		if ctx.Err() != nil {
			return nil, &devhttp.Error{Class: devhttp.ConnectionError, Err: context.Canceled}
		}
	}
	if isWrite && !c.written {
		c.written = true
		c.cancel()
	}
	return response, err
}
func (c *r06CancelHTTP) Close() { c.client.Close() }

func TestR06CancellationFinishesReadbackAndCompensation(t *testing.T) {
	for _, vendor := range []string{"routeros", "ikuai"} {
		for _, rollback := range []bool{false, true} {
			t.Run(vendor+map[bool]string{false: "/success", true: "/compensation"}[rollback], func(t *testing.T) {
				tc := r06Base(vendor, "update")
				var endpoint string
				var final func() any
				if vendor == "routeros" {
					if rollback {
						tc.ROS.Faults = []routerfake.Fault{{Method: "GET", Path: "/ip/route", Count: 3, Status: 503}}
					}
					fake := routerfake.New(nil)
					fake.Reset(tc.ROS)
					server := httptest.NewServer(fake)
					defer server.Close()
					endpoint = server.URL
					final = func() any { return fake.Snapshot().Tables["/ip/route"][0]["comment"] }
				} else {
					if rollback {
						tc.IK.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: "show", Count: 3, Drop: true}}
					}
					fake := routerfake.NewIkuai(tc.IK)
					server := httptest.NewServer(fake)
					defer server.Close()
					endpoint = server.URL
					final = func() any { return fake.Snapshot().Tables["static_rt"][0]["comment"] }
				}
				type key struct{}
				request, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "scope"))
				defer cancel()
				transport := &r06CancelHTTP{client: devhttp.New(devhttp.Config{Cookies: vendor == "ikuai"}), cancel: cancel, vendor: vendor}
				var editor routeedit.RouteEditor
				if vendor == "routeros" {
					editor = NewRouterOSWithHTTP(endpoint, transport)
				} else {
					editor = NewIkuaiWithHTTP(endpoint, "synthetic-admin", "synthetic-secret", transport)
				}
				defer transport.Close()
				rows, err := editor.ReadRoutes(request)
				if err != nil {
					t.Fatal(err)
				}
				result, err := editor.MutateRoute(request, "update", rows[0], tc.Values)
				if request.Err() == nil || transport.observed < 1 {
					t.Fatal("test did not cancel accepted write and observe later requests")
				}
				if rollback {
					if e, ok := errors.AsType[*device.RouterError](err); !ok || e.Kind == device.KindRollback {
						t.Fatalf("recovery did not complete: %v", err)
					}
					if final() != "office" {
						t.Fatal("original configuration was not restored")
					}
				} else {
					if err != nil || !result.Changed {
						t.Fatalf("readback abandoned: %v", err)
					}
				}
			})
		}
	}
}

func TestR06CancelledEntryStillWrites(t *testing.T) {
	for _, vendor := range []string{"routeros", "ikuai"} {
		t.Run(vendor, func(t *testing.T) {
			tc := r06Base(vendor, "add")
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var editor routeedit.RouteEditor
			if vendor == "routeros" {
				f := routerfake.New(tc.ROS.Tables)
				s := httptest.NewServer(f)
				defer s.Close()
				d := NewRouterOS(s.URL, "synthetic-admin", "synthetic-secret", false)
				defer d.Close()
				editor = d
			} else {
				f := routerfake.NewIkuai(tc.IK)
				s := httptest.NewServer(f)
				defer s.Close()
				d := NewIkuai(s.URL, "synthetic-admin", "synthetic-secret", false)
				defer d.Close()
				editor = d
			}
			result, err := editor.MutateRoute(ctx, "add", nil, tc.Values)
			if err != nil || !result.Changed {
				t.Fatalf("cancelled request abandoned operation: %v", err)
			}
		})
	}
}

func TestR06ConnectionLock(t *testing.T) {
	tc := r06Base("routeros", "read")
	violations := lockx.Violations()
	type scopeKey struct{}
	root := context.WithValue(context.Background(), scopeKey{}, "repo-scope")
	ctx, release, err := oplock.RouterOperationLock(root, tc.Router, true)
	if err != nil {
		t.Fatal(err)
	}
	other := tc.Router
	other.Endpoint += "///"
	other.ID = "other-profile-router"
	if ctx.Value(scopeKey{}) != "repo-scope" {
		t.Fatal("scope was lost")
	}
	read, cancel := context.WithCancel(root)
	cancel()
	if _, unlock, err := oplock.RouterOperationLock(read, other, false); !errors.Is(err, context.Canceled) {
		if unlock != nil {
			unlock()
		}
		t.Fatalf("same endpoint did not block: %v", err)
	}
	ready := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		close(ready)
		c, u, e := oplock.RouterOperationLock(read, other, true)
		if e == nil {
			defer u()
			if c.Err() != nil {
				e = c.Err()
			}
		}
		finished <- e
	}()
	<-ready
	select {
	case e := <-finished:
		t.Fatalf("write lock was not serialized: %v", e)
	case <-time.After(20 * time.Millisecond):
	}
	release()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	release() // release is idempotent
	if oplock.OperationLockHeld(tc.Router) {
		t.Fatal("connection lock still held")
	}
	if lockx.Violations() != violations {
		t.Fatal("router lock order violation")
	}
}

func TestR06PreviewBindsFullSnapshotAndConnection(t *testing.T) {
	tc := r06Base("routeros", "read")
	row := routeedit.EditROSEntry(deviceRow(tc.ROS.Tables["/ip/route"][0]), nil)
	payload := r06JSON(map[string]any{"action": "update", "id": row.ID, "revision": row.Public().Revision, "values": tc.Values})
	original, err := routeedit.BuildRoutePlan(tc.Router, []*routeedit.EditableRoute{row}, payload)
	if err != nil {
		t.Fatal(err)
	}
	password := tc.Router
	password.Password = "changed-secret"
	same, err := routeedit.BuildRoutePlan(password, []*routeedit.EditableRoute{row}, payload)
	if err != nil || original.Fingerprint != same.Fingerprint {
		t.Fatal("password changed fingerprint")
	}
	endpoint := tc.Router
	endpoint.Endpoint = "http://other.example.com"
	changed, _ := routeedit.BuildRoutePlan(endpoint, []*routeedit.EditableRoute{row}, payload)
	if changed.Fingerprint == original.Fingerprint {
		t.Fatal("connection omitted from fingerprint")
	}
	other := *row
	other.ID = "*99"
	other.CIDR = "192.0.2.0/24"
	same, err = routeedit.BuildRoutePlan(tc.Router, []*routeedit.EditableRoute{row, &other}, payload)
	if err != nil || same.Fingerprint != original.Fingerprint {
		t.Fatal("unrelated route changed fingerprint")
	}
	drifted := deviceRow(tc.ROS.Tables["/ip/route"][0])
	drifted["distance"] = "99"
	changedRow := routeedit.EditROSEntry(drifted, nil)
	if _, err := routeedit.BuildRoutePlan(tc.Router, []*routeedit.EditableRoute{changedRow}, payload); device.RouterErrorKind(err) != device.KindConflict {
		t.Fatal("advanced drift not rejected")
	}
	for _, field := range []string{"active", "inactive", "immediate-gw"} {
		runtime := deviceRow(tc.ROS.Tables["/ip/route"][0])
		runtime[field] = "different"
		if routeedit.EditROSEntry(runtime, nil).Public().Revision != row.Public().Revision {
			t.Fatalf("runtime %s altered revision", field)
		}
	}
	noop := &routeedit.RouteValues{CIDR: row.CIDR, Gateway: row.Gateway, Enabled: row.Enabled, Comment: row.Comment}
	plan, err := routeedit.BuildRoutePlan(tc.Router, []*routeedit.EditableRoute{row}, r06JSON(map[string]any{"action": "update", "id": row.ID, "revision": row.Public().Revision, "values": noop}))
	if err != nil || !plan.Empty {
		t.Fatal("no-op not recognized")
	}
	result, err := routeedit.ExecuteRoutePlan(context.Background(), nil, nil, plan)
	if err != nil || result.Changed {
		t.Fatal("no-op contacted driver")
	}
}

func TestR06DeviceCredentialEchoIsHidden(t *testing.T) {
	for _, vendor := range []string{"routeros", "ikuai"} {
		t.Run(vendor, func(t *testing.T) {
			tc := r06Base(vendor, "add")
			var editor routeedit.RouteEditor
			if vendor == "routeros" {
				tc.ROS.Faults = []routerfake.Fault{{Method: "PUT", Path: "/ip/route", Count: 1, Status: 400, Body: routerfake.Row{"message": "synthetic-secret"}}}
				f := routerfake.New(nil)
				f.Reset(tc.ROS)
				s := httptest.NewServer(f)
				defer s.Close()
				d := NewRouterOS(s.URL, "synthetic-admin", "synthetic-secret", false)
				defer d.Close()
				editor = d
			} else {
				code := 2
				tc.IK.Faults = []routerfake.IkuaiFault{{Func: "static_rt", Action: "add", Count: 1, Code: &code, Message: "synthetic-secret"}}
				f := routerfake.NewIkuai(tc.IK)
				s := httptest.NewServer(f)
				defer s.Close()
				d := NewIkuai(s.URL, "synthetic-admin", "synthetic-secret", false)
				defer d.Close()
				editor = d
			}
			_, err := editor.MutateRoute(context.Background(), "add", nil, tc.Values)
			if e, ok := errors.AsType[*device.RouterError](err); !ok || e.Message != routeedit.MessageEditSecret {
				t.Fatalf("device echo leaked: %v", err)
			}
		})
	}
}

func TestR06SecretRedactionKeepsRecoveryStatus(t *testing.T) {
	for _, kind := range []device.ErrorKind{device.KindRollback, device.KindUncertainWrite} {
		err := routeedit.SafeError(device.KindError(kind, "device echoes synthetic-secret"), "http://example.com", "synthetic-secret")
		e, ok := errors.AsType[*device.RouterError](err)
		if !ok || e.Kind != kind || strings.Contains(e.Message, "synthetic-secret") {
			t.Fatal("unsafe redaction", err)
		}
		if kind == device.KindRollback && !strings.Contains(e.Message, "补偿未完成") {
			t.Fatal("redaction hid failed compensation")
		}
		if kind == device.KindUncertainWrite && !strings.Contains(e.Message, "无法确认") {
			t.Fatal("redaction hid uncertain write")
		}
	}
}
