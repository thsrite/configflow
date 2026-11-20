package oxidns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"configflow/internal/ctxx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

func c27Snapshot(t *testing.T, native bool) *Profile {
	t.Helper()
	p, err := ProfileFromJSON([]byte(`{"profile_id":"default","system_config":{"server_domain":"https://panel.example.com"},
	"agents":[{"id":"dns","name":"DNS","service_type":"oxidns"}],
	"rule_configs":[{"id":"a","itemType":"rule","rule_type":"DOMAIN","value":"a.example.com","policy":"DIRECT"},
	{"id":"b","itemType":"rule","rule_type":"DOMAIN","value":"b.example.com","policy":"DIRECT"}],
	"oxidns":{"local_dns":"udp://192.0.2.53","remote_dns":"udp://192.0.2.54",
	"rule_actions":{"a":{"action":"blackhole"}},"extra":{"retain":["original"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if native {
		settings, err := PrepareEditor(p.Settings(), p, "https://panel.example.com")
		if err != nil {
			t.Fatal(err)
		}
		set(p.doc, "oxidns", settings)
	}
	return p
}

func TestC27ReviewConcurrentSnapshots(t *testing.T) {
	t.Parallel()
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			p := c27Snapshot(t, native)
			before := ToJSON(p.doc)
			const workers = 8
			failures := make(chan error, workers)
			var wg sync.WaitGroup
			for worker := 0; worker < workers; worker++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for range 4 {
						state, err := ReadRuleDNS(p, RuleDNSReadOptions{})
						if err != nil {
							failures <- err
							return
						}
						result, err := ApplyRuleDNS(p, p, []DNSAssignment{{"a", "proxy"}}, RuleDNSApplyOptions{})
						if err != nil {
							failures <- err
							return
						}
						if len(state.Engine.Agents) != 1 || state.Engine.Agents[0].Name == nil {
							failures <- errors.New("missing Agent view")
							return
						}
						state.Engine.Agents[0].Name[0] = 'x'
						at(result.Settings, "extra", "retain").Content[0].Value = "changed"
						agents := ProfileAgents(p)
						set(agents[0], "name", S("changed"))
					}
				}()
			}
			wg.Wait()
			close(failures)
			for err := range failures {
				t.Error(err)
			}
			if !bytes.Equal(before, ToJSON(p.doc)) {
				t.Fatal("parallel operations or returned values changed the shared snapshot")
			}
		})
	}
}

func TestC27ReviewNativeIdentitiesRemainEditable(t *testing.T) {
	t.Parallel()
	for _, separator := range []string{"\u2028", "\u2029"} {
		for _, source := range []string{"local", "remote", "fallback"} {
			t.Run(fmt.Sprintf("%U/%s", []rune(separator)[0], source), func(t *testing.T) {
				p := c27Snapshot(t, false)
				settings := clone(p.Settings())
				entry := M("addr", "udp://192.0.2.53", "tag", "slot"+separator+"one")
				text, err := dump(L(entry))
				if err != nil {
					t.Fatal(err)
				}
				set(settings, source+"_dns", S(text))
				set(settings, "rule_actions", M("a", M("action", "upstream", "source", source, "upstream", entry)))
				set(p.doc, "oxidns", settings)
				settings, err = PrepareEditor(settings, p, "https://panel.example.com")
				if err != nil {
					t.Fatal(err)
				}
				set(p.doc, "oxidns", settings)
				state, err := ReadRuleDNS(p, RuleDNSReadOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if !state.Engine.Editable || state.Rules["a"].UpstreamLabel != upstreamLabel(source, "udp://192.0.2.53") {
					t.Fatalf("new native hash lost its source or became locked: %+v", state)
				}
				if _, err := ApplyRuleDNS(p, p, []DNSAssignment{{"b", "proxy"}}, RuleDNSApplyOptions{}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func c27ProfileFromModel(p *model.Profile) *Profile {
	raw, err := json.Marshal(p)
	if err != nil {
		empty, _ := NewTypedProfile(nil)
		return empty
	}
	out, err := ProfileFromJSON(raw)
	if err != nil {
		empty, _ := NewTypedProfile(nil)
		return empty
	}
	return out
}

func c27Repository(t *testing.T, native bool) (*repo.Repository, context.Context) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	p := c27Snapshot(t, native)
	fields, err := json.Marshal(map[string]json.RawMessage{
		"rule_configs": ToJSON(p.get("rule_configs")),
		"oxidns":       ToJSON(p.Settings()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.SaveProfileFieldsJSON(ctx, "default", fields, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "other", Name: "Other"}, "default"); err != nil {
		t.Fatal(err)
	}
	return r, ctx
}

func c27Transaction(r *repo.Repository, ctx context.Context, id, choice string) error {
	scope := repo.ScopeOf(ctx)
	_, err := r.UpdateProfile(ctx, "default", func(held context.Context, current *model.Profile) error {
		if repo.ScopeOf(held) != scope {
			return fmt.Errorf("transaction owner lost the request Scope")
		}
		before := c27ProfileFromModel(current)
		if id != "a" && id != "b" {
			var rule model.RuleConfig
			rawRule, err := json.Marshal(map[string]any{
				"id": id, "itemType": "rule", "rule_type": "DOMAIN",
				"value": id + ".example.com", "policy": "DIRECT",
			})
			if err != nil {
				return err
			}
			if err := json.Unmarshal(rawRule, &rule); err != nil {
				return err
			}
			current.RuleConfigs = append(current.RuleConfigs, rule)
		}
		after := c27ProfileFromModel(current)
		result, err := ApplyRuleDNS(before, after, []DNSAssignment{{id, choice}}, RuleDNSApplyOptions{})
		if err != nil {
			return err
		}
		if result.Changed {
			if err := current.OxiDNS.SetNode(result.Settings); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// The repository owner detaches requests before transaction admission (O3-6a).
// Shutdown still cancels that admission. Compute using locked before/after
// snapshots and install settings only after success, as required by C32.
func TestC27ReviewCanceledTransactionWaitsAndCommits(t *testing.T) {
	r, base := c27Repository(t, false)
	locked, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	first := make(chan error, 1)
	go func() {
		_, err := r.UpdateProfile(base, "default", func(_ context.Context, current *model.Profile) error {
			close(locked)
			<-release
			node, err := current.OxiDNS.Node()
			if err != nil {
				return err
			}
			set(node, "retained_first_writer", B(true))
			return current.OxiDNS.SetNode(node)
		})
		first <- err
	}()
	select {
	case <-locked:
	case <-time.After(5 * time.Second):
		t.Fatal("first transaction did not take lock")
	}
	request, cancel := context.WithCancel(repo.WithScope(base))
	scope := repo.ScopeOf(request)
	cancel()
	// Request cancellation is ignored by the owner; cancellation of its work
	// parent must still stop a real contended lock wait immediately.
	work, stop := context.WithCancel(base)
	stop()
	if err := c27Transaction(r, ctxx.WithParent(request, work), "a", "proxy"); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown-cancellable lock returned %v", err)
	}
	detached := ctxx.Detach(request)
	if repo.ScopeOf(detached) != scope {
		t.Fatal("detaching lost the request Scope")
	}
	second := make(chan error, 1)
	go func() { second <- c27Transaction(r, request, "a", "proxy") }()
	select {
	case err := <-second:
		t.Fatalf("detached request did not wait for the lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	for _, done := range []chan error{first, second} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("transaction did not finish")
		}
	}
	saved, err := r.Config(base, "default", repo.ResourceRoots{})
	if err != nil {
		t.Fatal(err)
	}
	settingsRaw, err := json.Marshal(saved.Profile.OxiDNS)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := testjson.Parse(settingsRaw)
	if err != nil {
		t.Fatal(err)
	}
	retained, _ := testjson.Path(settings, "retained_first_writer")
	proxyRules, _ := testjson.Path(settings, "proxy_rules")
	if retained != true || !testjson.Equal(proxyRules, []any{"a"}) {
		t.Fatal("canceled transaction lost its DNS edit or the preceding writer")
	}
}

func TestC27ReviewConcurrentTransactionsMerge(t *testing.T) {
	t.Parallel()
	r, base := c27Repository(t, false)
	original, err := r.Profile(base, "default")
	if err != nil {
		t.Fatal(err)
	}
	other, err := r.Profile(base, "other")
	if err != nil {
		t.Fatal(err)
	}
	const workers = 12
	failures := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			request, cancel := context.WithCancel(repo.WithScope(base))
			cancel()
			choice := "direct"
			if index%2 != 0 {
				choice = "proxy"
			}
			failures <- c27Transaction(r, request, fmt.Sprintf("added%d", index), choice)
		}(worker)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	saved, err := r.Profile(base, "default")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.RuleConfigs) != len(original.RuleConfigs)+workers || int64(saved.Revision) != int64(original.Revision)+workers {
		t.Fatal("concurrent transactions lost rule edits or revisions")
	}
	settingsNode, err := saved.OxiDNS.Node()
	if err != nil {
		t.Fatal(err)
	}
	if len(items(get(settingsNode, "direct_rules"))) != workers/2 || len(items(get(settingsNode, "proxy_rules"))) != workers/2 {
		t.Fatal("concurrent DNS assignments were lost")
	}
	nextOther, err := r.Profile(base, "other")
	if err != nil {
		t.Fatal(err)
	}
	if !testjson.Equal(other, nextOther) {
		t.Fatal("default-profile transaction changed another profile")
	}
}

func TestC27ReviewLockedTransactionRollsBack(t *testing.T) {
	t.Parallel()
	r, base := c27Repository(t, true)
	_, err := r.UpdateProfile(base, "default", func(_ context.Context, current *model.Profile) error {
		settings, err := current.OxiDNS.Node()
		if err != nil {
			return err
		}
		document, err := LoadNativeYAML(str(get(settings, "native_config")), NativeLabel)
		if err != nil {
			return err
		}
		for _, plugin := range items(get(document, "plugins")) {
			if isOneOf(get(plugin, "tag"), BlackholeTag) {
				set(get(plugin, "args"), "mode", S("nxdomain"))
			}
		}
		if err := setNativeConfig(settings, document); err != nil {
			return err
		}
		return current.OxiDNS.SetNode(settings)
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := r.Profile(base, "default")
	if err != nil {
		t.Fatal(err)
	}
	request, cancel := context.WithCancel(repo.WithScope(base))
	cancel()
	err = c27Transaction(r, request, "added", "proxy")
	if !errors.Is(err, ErrRoutingLocked) {
		t.Fatalf("expected routing conflict, got %v", err)
	}
	var locked *RoutingLockedError
	if !errors.As(err, &locked) || len(locked.Usages) != 1 {
		t.Fatalf("conflict lost typed usages: %v", err)
	}
	after, err := r.Profile(base, "default")
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	if !bytes.Equal(beforeJSON, afterJSON) {
		t.Fatal("conflict committed rule data, settings or revision")
	}
}

func TestC27ReviewIssueKeysAndRuleClassification(t *testing.T) {
	t.Parallel()
	a := Issue{Code: "code", Message: "first\x00second"}
	b := Issue{Code: "code\x00first", Message: "second"}
	if validationIssueKey(a) == validationIssueKey(b) {
		t.Fatal("issue tuple key collided")
	}
	tests := []struct {
		behavior        *Node
		convertible, ip bool
	}{
		{nil, true, false}, {Null(), true, false}, {B(false), true, false}, {I(0), true, false},
		{L("ipcidr"), true, false}, {M("kind", "ipcidr"), true, false}, {S(""), true, false},
		{S("domain"), true, false}, {S("ipcidr"), false, true}, {S("classical"), true, true},
		{S("ipcidr\x00"), true, false},
	}
	for _, test := range tests {
		item := M("itemType", "ruleset")
		if test.behavior != nil {
			set(item, "behavior", test.behavior)
		}
		if Convertible(item) != test.convertible || HasIPPart(item, nil) != test.ip {
			t.Fatalf("unexpected classification for %s", ToJSON(test.behavior))
		}
	}
}
