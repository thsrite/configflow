package routers

import (
	"bytes"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"configflow/internal/model"
	"configflow/internal/routers/routerfake"
)

const r10YAML = "ipv6: true\ndns:\n  enable: true\n  ipv6: true\n  enhanced-mode: fake-ip\n  fake-ip-range6: fd00:face::1/64\ntun:\n  enable: true\n"

func r10Config(raw string) *model.Profile {
	return &model.Profile{Mihomo: model.MihomoSettings{CustomConfig: raw}}
}
func r10Agent() *model.Agent {
	return &model.Agent{ID: "gateway", Host: "192.0.2.4", ServiceType: "mihomo", Enabled: new(true)}
}
func r10Host() ipv6.IPv6Host {
	return ipv6.IPv6Host{Interface: "eth0", Addresses: []ipv6.IPv6HostAddress{{Address: "fd00:1::4", Usable: true}, {Address: "fe80::4"}}}
}
func r10Router() *spec.Router {
	return &spec.Router{RouterSettings: spec.RouterSettings{ID: "synthetic", Type: "routeros"}, Extra: map[string]json.RawMessage{}}
}
func r10State() routerfake.State {
	return routerfake.State{IPv6Wire: true, Tables: map[string][]routerfake.Row{
		ros.PathIPv6Routes: {}, rosIPv6Addresses: {{".id": "*A1", "address": "fd00:1::1/64", "interface": "bridge"}}, rosIPv4Addresses: {{".id": "*B1", "address": "192.0.2.1/24", "interface": "bridge", "actual-interface": "bridge"}}, "/ipv6/firewall/filter": {}, "/ipv6/firewall/address-list": {}, "/interface": {{".id": "*C1", "name": "bridge"}}, "/interface/list": {}, "/interface/list/member": {},
	}}
}
func r10Wanted(n int) *spec.Desired {
	d := &spec.Desired{ProxyCIDRs: []spec.ProxyCIDR{}, Warnings: []string{}, IPv6CIDRs: []spec.ProxyCIDR{}, BlockingWarnings: []string{}}
	for i := 0; i < n; i++ {
		d.IPv6CIDRs = append(d.IPv6CIDRs, spec.ProxyCIDR{CIDR: fmt.Sprintf("2001:db8:%x::/48", i+10), Sources: []string{"Synthetic"}})
	}
	return d
}
func r10Route(id, cidr, gateway string) routerfake.Row {
	return routerfake.Row{".id": id, "dst-address": cidr, "gateway": gateway, "routing-table": "main", "distance": "1", "disabled": "false", "active": "true", "dynamic": "false", "comment": "foreign"}
}

type r10Step struct {
	Action   string                    `json:"action"`
	Config   *model.Profile            `json:"config"`
	Desired  *spec.Desired             `json:"desired"`
	Agent    *model.Agent              `json:"agent"`
	Host     ipv6.IPv6Host             `json:"host"`
	Mutation *routerfake.ExternalWrite `json:"mutation,omitempty"`
}
type r10Case struct {
	Name    string           `json:"name"`
	Feature string           `json:"feature"`
	State   routerfake.State `json:"state"`
	Router  *spec.Router     `json:"router"`
	Steps   []r10Step        `json:"steps"`
}

func r10StepFor(action string, n int) r10Step {
	return r10Step{Action: action, Config: r10Config(r10YAML), Desired: r10Wanted(n), Agent: r10Agent(), Host: r10Host()}
}
func r10Run(t *testing.T, f *routerfake.Fake, endpoint string, tc r10Case) r08Output {
	t.Helper()
	f.Reset(tc.State)
	d := NewRouterOS(endpoint, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	ctx := context.Background()
	out := r08Output{Results: []json.RawMessage{}}
	var undo json.RawMessage
	var fakePlan *ipv6.IPv6FakeIPPlan
	var routesPlan *ipv6.IPv6RoutesPlan
	var err error
	add := func(value any) {
		raw, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		out.Results = append(out.Results, raw)
	}
	for _, step := range tc.Steps {
		if step.Mutation != nil {
			f.ApplyExternal(*step.Mutation)
		}
		if step.Action == "undo" {
			_, err = d.Apply(ctx, undo)
			if err == nil {
				add(ros.Row{})
			}
		} else if step.Action == "apply" {
			var raw json.RawMessage
			if tc.Feature == "fake" {
				raw = device.JSON(fakePlan.Ops[0])
			} else {
				raw = device.JSON(routesPlan.Ops[0])
			}
			undo, err = d.Apply(ctx, raw)
			if err == nil {
				add(undo)
			}
		} else {
			r := r09Copy(*tc.Router)
			if r.Extra == nil {
				r.Extra = map[string]json.RawMessage{}
			}
			blocked := false
			var ops []device.Op
			if tc.Feature == "fake" {
				if d.IPv6FakeIPManifestSet() {
					r.Extra["ipv6_fakeip_state"] = ros.GuardRaw(d.IPv6FakeIPManifest())
				}
				fakePlan = ipv6.InspectIPv6FakeIP(ctx, step.Config, &r, step.Agent, step.Host, d.Client)
				add(fakePlan)
				ops = fakePlan.Ops
				blocked = len(fakePlan.Blockers) > 0
			} else {
				if d.IPv6RoutesManifestSet() {
					r.Extra["ipv6_routes_state"] = ros.GuardRaw(d.IPv6RoutesManifest())
				}
				routesPlan = ipv6.InspectIPv6Routes(ctx, step.Config, &r, step.Desired, step.Agent, step.Host, d.Client)
				add(routesPlan)
				ops = routesPlan.Ops
				blocked = len(routesPlan.Skip) > 0
			}
			if step.Action == "report" {
				state, e := d.ReadState(ctx)
				if e != nil {
					err = e
					break
				}
				if tc.Feature == "fake" {
					state.IPv6FakeIPPlan = fakePlan
				} else {
					state.IPv6RoutesPlan = routesPlan
				}
				report, e := BuildReport(step.Desired, state)
				if e != nil {
					err = e
					break
				}
				add(report)
				continue
			}
			if step.Action == "plan" || blocked {
				continue
			}
			if len(ops) > 0 {
				undo, err = d.Apply(ctx, device.JSON(ops[0]))
				if err == nil {
					add(undo)
				}
			}
			if err == nil {
				if tc.Feature == "fake" {
					err = ipv6.VerifyIPv6FakeIP(ctx, d.Client, &fakePlan.Check)
				} else {
					err = ipv6.VerifyIPv6Routes(ctx, d.Client, routesPlan.Check)
				}
			}
			if err == nil && step.Action == "rollback" && len(ops) > 0 {
				_, err = d.Apply(ctx, undo)
				if err == nil {
					add(ros.Row{})
				}
			}
		}
		if err != nil {
			break
		}
	}
	if err != nil {
		class := "RouterError"
		if e, ok := errors.AsType[*device.RouterError](err); ok && e.Kind == device.KindRollback {
			class = "RouterRollbackError"
		}
		out.Error = &r08Error{class, err.Error()}
	}
	if tc.Feature == "fake" {
		out.Manifest = ros.GuardRaw(d.IPv6FakeIPManifest())
	} else {
		out.Manifest = ros.GuardRaw(d.IPv6RoutesManifest())
	}
	return out
}

func TestR10ManifestCopies(t *testing.T) {
	d := NewRouterOSWithHTTP("", nil)
	raw := json.RawMessage(`{"future":{"keep":true}}`)
	d.SetIPv6FakeIPManifest(raw)
	d.SetIPv6RoutesManifest(raw)
	raw[2] = 'X'
	a := d.Manifests()
	if bytes.Equal(a.IPv6FakeIP.Value, raw) {
		t.Fatal("aliased input")
	}
	a.IPv6Routes.Value[2] = 'Y'
	if bytes.Equal(a.IPv6Routes.Value, d.Manifests().IPv6Routes.Value) {
		t.Fatal("aliased output")
	}
	if !a.IPv6FakeIP.Set || !a.IPv6Routes.Set {
		t.Fatal("presence lost")
	}
	d.SetIPv6FakeIPManifest(nil)
	if !bytes.Equal(d.Manifests().IPv6FakeIP.Value, []byte("null")) {
		t.Fatal("withdrawal not explicit null")
	}
}
