package routers

import (
	"bytes"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/rospolicy"
	"configflow/internal/routers/routeedit"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/routers/routerfake"
)

func o36bRuntime(t *testing.T) (*lifecycle.Runtime, context.Context, context.CancelFunc) {
	t.Helper()
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(t.Context())
	t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	request, disconnect := context.WithCancel(runtime.BindRequest(t.Context()))
	t.Cleanup(disconnect)
	return runtime, request, disconnect
}

func TestO36bLegacyWriteRuntimeBoundary(t *testing.T) {
	for _, vendor := range []string{"routeros", "ikuai"} {
		for _, operation := range []string{"apply", "restore", "edit", "plan", "empty-plan", "write", "batch"} {
			if vendor == "routeros" && (operation == "write" || operation == "batch") {
				continue
			}
			for _, shutdown := range []bool{true, false} {
				name := vendor + "/" + operation + "/disconnect"
				if shutdown {
					name = vendor + "/" + operation + "/shutdown"
				}
				t.Run(name, func(t *testing.T) {
					tc := r06Base(vendor, "add")
					var driver Driver
					var state func() []byte
					var calls func() int
					if vendor == "routeros" {
						fake := routerfake.New(tc.ROS.Tables)
						server := httptest.NewServer(fake)
						t.Cleanup(server.Close)
						d := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
						t.Cleanup(d.Close)
						driver = d
						state = func() []byte { data, _ := json.Marshal(fake.Snapshot().Tables); return data }
						calls = func() int { return len(fake.Calls()) }
					} else {
						fake := routerfake.NewIkuai(tc.IK)
						server := httptest.NewServer(fake)
						t.Cleanup(server.Close)
						d := NewIkuai(server.URL, routerfake.IkuaiUser, routerfake.IkuaiPassword, false)
						t.Cleanup(d.Close)
						driver = d
						state = func() []byte { data, _ := json.Marshal(fake.Snapshot().Tables); return data }
						calls = func() int { return len(fake.Snapshot().Calls) }
					}
					op := json.RawMessage(`{"kind":"route_add","cidr":"198.51.100.0/24","gateway":"192.0.2.3","comment":"cf-o36b"}`)
					if operation == "restore" {
						var err error
						op, err = driver.Apply(t.Context(), op)
						if err != nil {
							t.Fatal("prepare undo", err)
						}
					}
					runtime, request, disconnect := o36bRuntime(t)
					before, count := state(), calls()
					if shutdown {
						runtime.Shutdown(context.Background())
					} else {
						disconnect()
					}
					var err error
					callback := false
					switch operation {
					case "apply", "restore":
						_, err = driver.Apply(request, op)
					case "edit":
						_, err = driver.(routeedit.RouteEditor).MutateRoute(request, "add", nil, tc.Values)
					case "plan", "empty-plan":
						plan := &routeedit.RoutePlan{Action: "add", Values: tc.Values, Empty: operation == "empty-plan"}
						_, err = routeedit.ExecuteRoutePlan(request, driver.(routeedit.RouteEditor), nil, plan)
					case "write":
						_, err = driver.(*Ikuai).Write(request, "static_rt", "del", ros.Row{"id": "1"}, 0)
					case "batch":
						err = driver.(*Ikuai).Batch(request, true, func(context.Context) error { callback = true; return nil })
					}
					if shutdown {
						if !errors.Is(err, context.Canceled) || calls() != count || !bytes.Equal(before, state()) || callback {
							t.Fatalf("stopped owner performed I/O/state commit: err=%v calls=%d->%d callback=%t", err, count, calls(), callback)
						}
					} else {
						if err != nil {
							t.Fatal("disconnect interrupted work", err)
						}
						if operation != "empty-plan" && operation != "batch" && bytes.Equal(before, state()) {
							t.Fatal("disconnected write did not change the device")
						}
						if operation == "batch" && !callback {
							t.Fatal("disconnected batch did not run")
						}
					}
				})
			}
		}
	}
}

func TestO36bApplyManyRuntimeBoundary(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			fake := routerfake.New(nil)
			server := httptest.NewServer(fake)
			defer server.Close()
			d := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
			defer d.Close()
			runtime, request, disconnect := o36bRuntime(t)
			ops := make([]json.RawMessage, 150)
			for i := range ops {
				ops[i], _ = json.Marshal(ros.Operation{Kind: "route_add", CIDR: fmt.Sprintf("198.51.100.%d/32", i), Gateway: "192.0.2.1", Comment: "cf-o36b"})
			}
			recorded := 0
			err := d.ApplyMany(request, ops, func(_, _ json.RawMessage) {
				recorded++
				if recorded == 100 {
					if shutdown {
						runtime.Shutdown(context.Background())
					} else {
						disconnect()
					}
				}
			})
			want := 150
			if shutdown {
				want = 100
				if !errors.Is(err, context.Canceled) {
					t.Fatal("shutdown did not stop the next batch", err)
				}
			} else if err != nil {
				t.Fatal("disconnect stopped the next batch", err)
			}
			written := len(fake.Snapshot().Tables[ros.PathRoutes])
			if written != want || recorded != written {
				t.Fatalf("written=%d recorded=%d want=%d; every write must be proven and recorded", written, recorded, want)
			}
		})
	}
}

func TestO36bPolicyRestoreCommitRuntimeBoundary(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			d, fake := r08Driver(t)
			previous := json.RawMessage(`{"spec":null,"resources":[],"synthetic":"retained"}`)
			d.SetPolicyManifest(previous)
			runtime, request, disconnect := o36bRuntime(t)
			stopped := false
			d.SetHTTP(&r09AfterReadHTTP{DeviceHTTP: d.HTTP(), after: func(method, target string) {
				if method == "GET" && target == d.Endpoint()+"/rest"+rospolicy.SettingsPath {
					stopped = true
					if shutdown {
						runtime.Shutdown(context.Background())
					} else {
						disconnect()
					}
				}
			}})
			before, _ := json.Marshal(fake.Snapshot().Tables)
			err := rospolicy.RestoreROSPolicy(request, d.Client, rospolicy.ROSPolicyUndo{Kind: "ros_policy_restore", Previous: json.RawMessage(`null`)})
			after, _ := json.Marshal(fake.Snapshot().Tables)
			if !stopped || !bytes.Equal(before, after) {
				t.Fatal("fixture did not hit manifest-only restore", stopped)
			}
			if shutdown {
				if !errors.Is(err, context.Canceled) || !bytes.Equal(d.PolicyManifest(), previous) {
					t.Fatalf("shutdown committed policy proof: err=%v manifest=%s", err, d.PolicyManifest())
				}
			} else if err != nil || string(d.PolicyManifest()) != "null" {
				t.Fatalf("disconnect interrupted policy proof: err=%v manifest=%s", err, d.PolicyManifest())
			}
		})
	}
}
