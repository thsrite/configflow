package routers

import (
	"bufio"
	"compress/gzip"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"encoding/json"
	"net/netip"
	"os"
	"testing"
)

// TestR03Corpus runs every row of testdata/r03_corpus.jsonl.gz
// (testdata/gen_r03.py: the real Python entry points on synthetic devices)
// through the Go entry points and compares the results structurally.
func TestR03Corpus(t *testing.T) {
	file, err := os.Open("testdata/r03_corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 1<<20), 1<<28)
	counts, differ, intended := map[string]int{}, 0, 0
	for scanner.Scan() {
		var row struct {
			Fn  string          `json:"fn"`
			In  json.RawMessage `json:"in"`
			Out json.RawMessage `json:"out"`
			// Python's result with its repack bug fixed (gen_r03.py FIXED_LAYOUT):
			// an intended difference, Go must match this one.
			OutFixed json.RawMessage `json:"out_fixed"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		counts[row.Fn]++
		got := r03Call(t, row.Fn, row.In)
		if row.OutFixed != nil {
			intended++
			if sameJSON(t, got, row.Out) {
				t.Errorf("%s: Go reproduces the Python repack bug (in %.300s)", row.Fn, row.In)
			}
			row.Out = row.OutFixed
		}
		if row.Fn == "ikuai_read" {
			row.Out = withoutGroupType(t, row.Out)
		}
		if !sameJSON(t, got, row.Out) {
			differ++
			if differ <= 15 {
				g, _ := json.Marshal(got)
				in := string(row.In)
				if len(in) > 3000 {
					in = in[:3000] + "…"
				}
				want := string(row.Out)
				if len(want) > 3000 {
					want = want[:3000] + "…"
				}
				if len(g) > 3000 {
					g = append(g[:3000], "…"...)
				}
				t.Errorf("%s(%s)\n got  %s\n want %s", row.Fn, in, g, want)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if intended == 0 {
		t.Error("no corpus row exercises the fixed repack bug")
	}
	t.Logf("rows %v; %d differ; %d intended differences (Python repack bug fixed)", counts, differ, intended)
}

type r03State struct {
	NatContext   *NatContext         `json:"nat_context"`
	AddressSets  map[string][]string `json:"address_sets"`
	Retiring     []string            `json:"retiring"`
	Routes       []*RouteEntry       `json:"routes"`
	NatBypass    []*NatBypassEntry   `json:"nat_bypass"`
	PolicyRoutes []*PolicyRouteEntry `json:"policy_routes"`
	Retired      []*PolicyRouteEntry `json:"retired_policy_routes"`
}

type r03Ikuai struct {
	Groups []ikuaiRow `json:"groups"`
	NAT    []ikuaiRow `json:"nat"`
	Policy []ikuaiRow `json:"policy"`
}

func (k r03Ikuai) state() *GroupState {
	return ReadGroupRules(GroupsFromRows(k.Groups), k.NAT, k.Policy)
}

func prefixes(t *testing.T, texts []string) []netip.Prefix {
	out := make([]netip.Prefix, len(texts))
	for i, text := range texts {
		out[i] = netip.MustParsePrefix(text)
	}
	return out
}

func texts(list []netip.Prefix) []string {
	out := []string{}
	for _, p := range list {
		out = append(out, p.String())
	}
	return out
}

func groupView(g *IkuaiGroup) map[string]any {
	return map[string]any{"id": g.ID, "gid": g.GID, "name": g.Name, "comment": g.Comment, "values": g.Values,
		"entries": g.Entries, "refs": g.Refs, "role": g.Role, "index": g.Index, "users": g.Users,
		"dup": g.Dup, "adopted": g.Adopted}
}

func r03Call(t *testing.T, fn string, in json.RawMessage) any {
	var v struct {
		CIDR          string              `json:"cidr"`
		Gateway       string              `json:"gateway"`
		State         r03State            `json:"state"`
		PolicyRouting bool                `json:"policy_routing"`
		Retiring      []string            `json:"retiring"`
		Ops           []map[string]any    `json:"ops"`
		Rows          []Row               `json:"rows"`
		Sources       []string            `json:"sources"`
		Destinations  []string            `json:"destinations"`
		Interface     *string             `json:"interface"`
		Context       *NatContext         `json:"context"`
		AddressSets   map[string][]string `json:"address_sets"`
		Mangle        [][2]string         `json:"mangle"`
		Firewall      [][3]string         `json:"firewall"`
		Desired       *Desired            `json:"desired"`
		Ikuai         *r03Ikuai           `json:"ikuai"`
		Steps         []r03Step           `json:"steps"`
		Check         GroupStageCheck     `json:"check"`
		Missing       bool                `json:"missing"`
		Name          string              `json:"name"`
		Role          string              `json:"role"`
		Values        []string            `json:"values"`
		Notes         map[string]string   `json:"notes"`
		ID            *int64              `json:"id"`
		r03Ikuai
	}
	switch fn {
	case "span", "digest", "networks":
		var list []string
		decodeInto(t, in, &list)
		switch fn {
		case "span":
			return GroupSpan(list)
		case "digest":
			return GroupDigest(list)
		}
		return texts(device.Networks(list))
	case "canon", "client_sources":
		var s string
		decodeInto(t, in, &s)
		if fn == "client_sources" {
			return texts(ros.ClientSourceNetworks(netip.MustParseAddr(s)))
		}
		return CanonAddress(s)
	}
	decodeInto(t, in, &v)
	state := &RouterState{NatContext: v.State.NatContext, AddressSets: v.State.AddressSets}
	switch fn {
	case "assess_nat":
		a := ros.AssessNAT(v.CIDR, v.Gateway, state.NatContext, v.PolicyRouting, nil)
		return map[string]string{"status": a.Status, "detail": a.Detail}
	case "assess_bypass":
		state.PolicyNATRetiring = v.Retiring
		return ros.AssessBypass(v.CIDR, v.Gateway, state.AddressSets, state.NatContext, state.PolicyNATRetiring, nil)
	case "planned":
		var expansions []BypassExpansion
		for _, op := range v.Ops {
			e := BypassExpansion{ID: op["id"].(string), Comment: op["comment"].(string)}
			for _, d := range op["add_dst"].([]any) {
				e.AddDst = append(e.AddDst, d.(string))
			}
			expansions = append(expansions, e)
		}
		return ros.PlannedBypassConflicts(expansions, state.AddressSets, state.NatContext, v.Gateway)
	case "preserves":
		accepted := []string{}
		iface := ""
		if v.Interface != nil {
			iface = *v.Interface
		}
		ok := ros.PreservesSources(v.Rows, prefixes(t, v.Sources), prefixes(t, v.Destinations), iface, v.Context,
			v.AddressSets, &accepted, nil)
		return map[string]any{"result": ok, "accepted": accepted}
	case "inspect_rule":
		var others []ListRef
		for _, m := range v.Mangle {
			others = append(others, ListRef{Path: PathMangle + "/" + m[0], Name: m[1]})
		}
		for _, f := range v.Firewall {
			others = append(others, ListRef{Path: f[0] + "/" + f[1], Name: f[2]})
		}
		found := ros.InspectNATListRule(v.Rows, others)
		return map[string]any{"rule": found.Rule, "problem": found.Problem, "out_interface": found.OutInterface}
	case "report":
		state = &RouterState{Routes: v.State.Routes, NatBypass: v.State.NatBypass, AddressSets: v.State.AddressSets,
			NatContext: v.State.NatContext, PolicyRoutes: v.State.PolicyRoutes, RetiredPolicyRoutes: v.State.Retired}
		if v.Ikuai != nil {
			state.IkuaiGroups = v.Ikuai.state()
		}
		report, err := BuildReport(v.Desired, state)
		if err != nil {
			t.Fatal(err)
		}
		return report
	case "ikuai_read":
		s := v.r03Ikuai.state()
		groups := []any{}
		for _, g := range s.Groups {
			groups = append(groups, groupView(g))
		}
		rules := []any{}
		for _, r := range s.Rules {
			rules = append(rules, map[string]any{"key": r.Key, "func": r.Func, "id": r.ID, "comment": r.Comment,
				"enabled": r.Enabled, "action": r.Action, "custom": r.Custom, "objects": r.Objects, "src_objects": r.SrcObjects})
		}
		return map[string]any{"groups": groups, "rules": rules}
	case "ikuai_plan":
		return runSteps(v.r03Ikuai.state(), v.Steps)
	case "verify":
		state = &RouterState{}
		if !v.Missing {
			state.IkuaiGroups = v.r03Ikuai.state()
		}
		if err := VerifyGroups(v.Check, state.IkuaiGroups); err != nil {
			return err.Error()
		}
		return nil
	case "payload":
		entries := make([]GroupEntry, len(v.Values))
		for i, value := range v.Values {
			entries[i] = GroupEntry{value, "n"}
		}
		return map[string]any{"payload": GroupPayload(v.Name, v.Role, v.Values, v.ID, v.Notes),
			"bytes":         GroupBytes(v.Name, v.Role, v.Values, v.Notes),
			"entry_payload": EntryPayload(v.Name, entries, v.ID)}
	}
	t.Fatalf("unknown fn %s", fn)
	return nil
}

type r03Step struct {
	Op      string           `json:"op"`
	Role    string           `json:"role"`
	Want    []string         `json:"want"`
	Subject *string          `json:"subject"`
	Key     *string          `json:"key"`
	Keep    bool             `json:"keep"`
	Kind    string           `json:"kind"`
	Rule    string           `json:"rule"`
	Names   []string         `json:"names"`
	Roles   []string         `json:"roles"`
	Rules   []GroupRuleCheck `json:"rules"`
	Removed []string         `json:"removed"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func runSteps(state *GroupState, steps []r03Step) any {
	warnings, blockers := []string{"earlier"}, []string{}
	p := NewGroupPlanner(state, &warnings, &blockers)
	results := []any{}
	for _, s := range steps {
		key := deref(s.Key)
		switch s.Op {
		case "family":
			final, ops := p.Family(s.Role, s.Want, deref(s.Subject))
			results = append(results, map[string]any{"final": final, "ops": nonNilOps(ops), "contents": p.Contents[s.Role]})
		case "needs_rewrite":
			results = append(results, p.NeedsRewrite(key, s.Role, s.Keep))
		case "dropped":
			results = append(results, p.DroppedForeign(key))
		case "migration":
			results = append(results, p.Migration(key, s.Kind, s.Rule))
		case "deleted":
			p.DeletedRules[key] = true
			results = append(results, nil)
		case "rewritten":
			p.Rewritten[key] = s.Names
			results = append(results, nil)
		case "referenced":
			results = append(results, nonNil(p.ReferencedValues(key)))
		case "cleanup":
			results = append(results, p.Cleanup(s.Roles...))
		default:
			results = append(results, p.Check(s.Roles, s.Rules, s.Removed))
		}
	}
	var migration any
	if w := p.MigrationWarning(); w != "" {
		migration = w
	}
	return map[string]any{"results": results, "warnings": warnings, "blockers": blockers, "migration_warning": migration}
}

func nonNilOps(ops []GroupOp) []GroupOp {
	if ops == nil {
		return []GroupOp{}
	}
	return ops
}

func decodeInto(t *testing.T, raw json.RawMessage, v any) {
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

// withoutGroupType drops each group's "type" from a Python ikuai_read result:
// group_from_row copied it, but nothing ever read it, so Go does not keep it.
func withoutGroupType(t *testing.T, raw json.RawMessage) json.RawMessage {
	var out struct {
		Groups []map[string]any `json:"groups"`
		Rules  any              `json:"rules"`
	}
	decodeInto(t, raw, &out)
	for _, g := range out.Groups {
		delete(g, "type")
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
