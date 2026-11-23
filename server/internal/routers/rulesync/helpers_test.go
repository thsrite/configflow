package rulesync

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/routers"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/rospolicy"
	"configflow/internal/routers/routerfake"
	"configflow/internal/routers/spec"
)

const r08Gateway = "192.0.2.3"
const r08Target = "198.51.100.0/25"

func r08State() routerfake.State {
	return routerfake.State{PolicyWire: true, Tables: map[string][]routerfake.Row{
		ros.PathTables: {{".id": "*0", "name": "main", "fib": ""}},
		ros.PathRoutes: {
			{".id": "*1", "dst-address": "192.0.2.0/24", "gateway": "bridge", "routing-table": "main", "immediate-gw": "bridge", "active": "true", "connect": "true", "dynamic": "true"},
			{".id": "*2", "dst-address": "198.18.0.0/16", "gateway": r08Gateway, "routing-table": "main", "active": "true", "immediate-gw": r08Gateway + "%bridge", "disabled": "false", "comment": "cf"},
		},
		ros.PathNAT: {{".id": "*3", "chain": "srcnat", "action": "masquerade", "out-interface": "wan", "disabled": "false"}},
	}}
}

func r08Desired(selected bool) *spec.Desired {
	d := &spec.Desired{FakeIPRange: "198.18.0.0/16", ProxyGateway: r08Gateway, ProxyCIDRs: []spec.ProxyCIDR{{CIDR: "198.18.0.0/16", Sources: []string{"fake-ip"}}}, Warnings: []string{}}
	p := &spec.ROSPolicyDesired{Selected: []string{}, Sources: []string{}, CIDRs: []spec.ProxyCIDR{}, Owner: "cf-ros-policy-test"}
	if selected {
		p.Selected = []string{"synthetic"}
		p.Sources = []string{"192.0.2.101/32"}
		p.CIDRs = []spec.ProxyCIDR{{CIDR: r08Target, Sources: []string{"Synthetic"}}}
	} else {
		d.ProxyCIDRs = append(d.ProxyCIDRs, spec.ProxyCIDR{CIDR: r08Target, Sources: []string{"Synthetic"}})
	}
	d.ROSPolicy = p
	return d
}

func r08InspectState(ctx context.Context, d *routers.RouterOS) (*routers.RouterState, error) {
	state, err := d.ReadState(ctx)
	if err != nil {
		return nil, err
	}
	state.ROSPolicy, err = rospolicy.SnapshotROSPolicy(ctx, d.Client)
	if err != nil {
		return nil, err
	}
	state.NatContext.RoutingRules = (*state.ROSPolicy)["/routing/rule"]
	state.NatContext.VRFs = (*state.ROSPolicy)["/ip/vrf"]
	if state.NatContext.Mangle == nil {
		state.NatContext.Mangle = (*state.ROSPolicy)[ros.PathMangle]
	}
	return state, nil
}

func r08Plan(ctx context.Context, d *routers.RouterOS, desired *spec.Desired) (*rospolicy.ROSPolicyPlan, error) {
	desired.ROSPolicy.Previous = slices.Clone(d.PolicyManifest())
	state, err := r08InspectState(ctx, d)
	if err != nil {
		return nil, err
	}
	return rospolicy.BuildROSPolicy(desired, state.NatContext, state.ROSPolicy), nil
}

func r08Comparable(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal comparable: %v", err)
	}
	var out any
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal comparable: %v", err)
	}
	return out
}

func r08Writes(t *testing.T, calls []routerfake.Call) any {
	t.Helper()
	result := []routerfake.Call{}
	for _, call := range calls {
		if call.Method == "GET" {
			continue
		}
		if call.Path == "/execute" {
			body, _ := call.Body.(map[string]any)
			script, _ := body["script"].(string)
			if !strings.Contains(script, "cf-done:") && !strings.Contains(script, "cf-id:") {
				continue
			}
		}
		result = append(result, call)
	}
	return r08Comparable(t, result)
}

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
		ros.PathIPv6Routes: {}, ipv6.PathIPv6Addresses: {{".id": "*A1", "address": "fd00:1::1/64", "interface": "bridge"}}, ipv6.PathIPv4Addresses: {{".id": "*B1", "address": "192.0.2.1/24", "interface": "bridge", "actual-interface": "bridge"}}, "/ipv6/firewall/filter": {}, "/ipv6/firewall/address-list": {}, "/interface": {{".id": "*C1", "name": "bridge"}}, "/interface/list": {}, "/interface/list/member": {},
	}}
}

func r12Compare(t *testing.T, label string, a, b any) {
	t.Helper()
	left, right := r08Comparable(t, a), r08Comparable(t, b)
	if !reflect.DeepEqual(left, right) {
		t.Errorf("%s differ:\n left=%v\nright=%v", label, fmt.Sprint(left), fmt.Sprint(right))
	}
}

type r12Source struct {
	gateway *routers.GatewayAgent
	client  routers.ReturnAgent
}

func (s r12Source) Agent(_ context.Context, id string) (*routers.GatewayAgent, error) {
	if s.gateway != nil && s.gateway.ID == id {
		return s.gateway, nil
	}
	return nil, nil
}
func (s r12Source) ReturnAgent(*routers.GatewayAgent) routers.ReturnAgent { return s.client }

type r13RejectSpawner struct{}

func (r13RejectSpawner) Spawn(context.Context, string, func(context.Context)) error {
	return lifecycle.ErrStopping
}

func effectful(raw json.RawMessage) bool {
	var head struct {
		Kind string `json:"kind"`
	}
	return json.Unmarshal(raw, &head) == nil && head.Kind != "manual" && head.Kind != "advisory"
}
