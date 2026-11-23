package routers

import (
	"bytes"
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/ros"
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"configflow/internal/devhttp"
	"configflow/internal/lifecycle"
	"configflow/internal/routers/routerfake"
)

func TestR09RuntimeShutdownAndRequestDisconnect(t *testing.T) {
	for _, action := range []string{"shutdown_apply", "shutdown_restore", "shutdown_empty_restore", "disconnect"} {
		t.Run(action, func(t *testing.T) {
			tc := r09Base(action)
			f := routerfake.New(tc.State.Tables)
			srv := httptest.NewServer(f)
			defer srv.Close()
			d := NewRouterOS(srv.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			plan := r09Plan(t, d, tc.Settings, nil)
			r := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
			r.Start(t.Context())
			defer r.Shutdown(t.Context())
			request, cancel := context.WithCancel(t.Context())
			defer cancel()
			bound := r.BindRequest(request)
			undo := &DNSGuardUndo{Kind: "dns_guard_restore"}
			if action == "shutdown_restore" {
				var err error
				undo, err = dnsguard.Apply(bound, d.Client, *plan.Ops[0].(*DNSGuardReplace))
				if err != nil {
					t.Fatal(err)
				}
			}
			if action == "disconnect" {
				cancel()
				undo, err := dnsguard.Apply(bound, d.Client, *plan.Ops[0].(*DNSGuardReplace))
				if err != nil {
					t.Fatalf("request disconnect stopped publication: %v", err)
				}
				if err := dnsguard.Restore(bound, d.Client, *undo); err != nil {
					t.Fatalf("request disconnect stopped restore: %v", err)
				}
				if holder := d.Manifests(); !holder.DNSGuard.Set || len(holder.DNSGuard.Value) != 0 {
					t.Fatalf("disconnected request lost completed withdrawal: %+v", holder)
				}
				return
			}
			r.Shutdown(t.Context())
			proof := bytes.Clone(d.DNSGuardManifest())
			set := d.DNSGuardManifestSet()
			before := len(f.Calls())
			var err error
			if action == "shutdown_apply" {
				_, err = dnsguard.Apply(bound, d.Client, *plan.Ops[0].(*DNSGuardReplace))
			} else {
				err = dnsguard.Restore(bound, d.Client, *undo)
			}
			if !errors.Is(err, context.Canceled) || len(f.Calls()) != before || !bytes.Equal(proof, d.DNSGuardManifest()) || set != d.DNSGuardManifestSet() {
				t.Fatalf("shutdown owner performed I/O or committed a manifest: %v", err)
			}
		})
	}
}

type r09AfterReadHTTP struct {
	DeviceHTTP
	after func(method, url string)
}

func (c *r09AfterReadHTTP) Do(ctx context.Context, method, url string, body any, timeout time.Duration) (*devhttp.Response, error) {
	response, err := c.DeviceHTTP.Do(ctx, method, url, body, timeout)
	c.after(method, url)
	return response, err
}

func TestR09ShutdownAfterSnapshotCannotClearManifest(t *testing.T) {
	f, srv, d, previous := r09Seed(t)
	// The previous objects disappeared externally. The resulting withdrawal is
	// a zero-device-write fast path that must still honor runtime shutdown.
	f.Reset(r09Base("missing_previous").State)
	settings := r09Base("withdraw").Settings
	settings.Enabled = false
	plan := r09Plan(t, d, settings, previous)
	if len(plan.Blockers) != 0 || len(plan.Ops) != 1 || len(plan.Ops[0].(*DNSGuardReplace).Owned) != 0 {
		t.Fatalf("not a manifest-only withdrawal: %s", r09Raw(plan))
	}
	r := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
	r.Start(t.Context())
	defer r.Shutdown(t.Context())
	bound := r.BindRequest(t.Context())
	shutdown := false
	d.SetHTTP(&r09AfterReadHTTP{DeviceHTTP: d.HTTP(), after: func(method, url string) {
		if method == "GET" && url == srv.URL+"/rest/tool/netwatch" {
			shutdown = true
			r.Shutdown(t.Context())
		}
	}})
	before := len(f.Calls())
	_, err := dnsguard.Apply(bound, d.Client, *plan.Ops[0].(*DNSGuardReplace))
	if !shutdown || err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) || !bytes.Equal(d.DNSGuardManifest(), previous) {
		t.Fatalf("shutdown after snapshot cleared proof: shutdown=%t err=%v manifest=%s", shutdown, err, d.DNSGuardManifest())
	}
	for _, call := range f.Calls()[before:] {
		if call.Method != "GET" && !strings.HasSuffix(call.Path, "/print") {
			t.Fatalf("shutdown wrote a device object: %+v", call)
		}
	}
}

func TestR09ShutdownAfterDeleteProofCannotWrite(t *testing.T) {
	f, srv, d, previous := r09Seed(t)
	settings := r09Base("replace_bypass").Settings
	settings.Bypass = []string{"192.0.2.60/32"}
	plan := r09Plan(t, d, settings, previous)
	if len(plan.Blockers) != 0 || len(plan.Ops) != 1 {
		t.Fatalf("not a replacement plan: %s", r09Raw(plan))
	}
	r := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Mode: lifecycle.ModeManual})
	r.Start(t.Context())
	defer r.Shutdown(t.Context())
	bound := r.BindRequest(t.Context())
	shutdown := false
	watchReads := 0
	d.SetHTTP(&r09AfterReadHTTP{DeviceHTTP: d.HTTP(), after: func(method, url string) {
		if method == "GET" && url == srv.URL+"/rest"+ros.PathNetwatch {
			// Netwatch has no single-row REST read in the shared driver: the
			// first collection read is preview, the second proves the delete.
			watchReads++
			if watchReads == 2 {
				shutdown = true
				r.Shutdown(t.Context())
			}
		}
	}})
	before := len(f.Calls())
	_, err := dnsguard.Apply(bound, d.Client, *plan.Ops[0].(*DNSGuardReplace))
	if !shutdown || err == nil || !bytes.Equal(d.DNSGuardManifest(), previous) {
		t.Fatalf("shutdown after ownership proof committed: shutdown=%t err=%v", shutdown, err)
	}
	for _, call := range f.Calls()[before:] {
		if call.Method != "GET" && !strings.HasSuffix(call.Path, "/print") {
			t.Fatalf("shutdown wrote after its last proof: %+v", call)
		}
	}
}
