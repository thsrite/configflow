package dnsfollow

import (
	"testing"

	"configflow/internal/agents/agentfake"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

func TestC07RunnerAndLoopSpec(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.RegisterPeriodic(f.s.Loop())
	names := rt.Names()
	count := 0
	for _, name := range names {
		if name == "dns-follow" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("duplicate/missing periodic owner", names)
	}
	runner := f.s.Runner()
	if runner == nil {
		t.Fatal("C07 DNS follow runner missing")
	}
	f.editAgent(t, "target", func(a *model.Agent) { _ = model.SetStoredMember(&a, "dns_follow_state", []byte(`{"status":"failed"}`)) })
	if err := runner(f.ctx, "dns_follow", "target"); err != nil {
		t.Fatal(err)
	}
	if st := state(f.agent(t)); st.Status != "waiting" || !value(st.RetryRequested) {
		t.Fatal(st)
	}
	if err := runner(f.ctx, "dns_follow", "target"); err == nil {
		t.Fatal("runner allowed waiting retry")
	}
}
