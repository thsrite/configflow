package deploy

import (
	"context"
	"testing"

	"configflow/internal/agents/agentfake"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/routersplit"
)

// A14 review L1: unforced provider details share the one-write-per-second
// throttle of Python's preparation_detail; forced phases always persist.
func TestReviewA14UnforcedProgressIsThrottled(t *testing.T) {
	f := newPublish(t, agentfake.Options{}, nil)
	if _, err := f.m.BeginDeployment(f.ctx, "publish-agent", "throttle"); err != nil {
		t.Fatal(err)
	}
	defer f.m.FinishDeploymentPreparation("publish-agent", "throttle")
	message := func() string {
		return f.agent(t).GetObject("deployments").GetObject("throttle").GetString("message")
	}
	p := publishProgress(f.ctx, f.m, "publish-agent", "throttle", "")
	p.Message("正在准备节点来源「A」")
	p.Message("节点来源「A」已准备")
	p.Message("正在准备节点来源「B」")
	if got := message(); got != "正在准备节点来源「A」" {
		t.Fatal("unforced details were not throttled", got)
	}
	p.Phase("正在校验节点来源与拨号引用关系（2 个文件）")
	if got := message(); got != "正在校验节点来源与拨号引用关系（2 个文件）" {
		t.Fatal("forced phase was throttled", got)
	}
}

// A14 + A20: deploy.Wire must not register a second setup-session reader;
// setupflow.Wire owns routersplit's hook.
func TestWireKeepsTheSingleSetupSessionReader(t *testing.T) {
	ctx := testContext(t)
	r, err := repo.GetRepository(ctx)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	restore := routersplit.SetProfileSessions(func(context.Context, *repo.Repository, string) ([]*jsonx.Object, error) {
		calls++
		return nil, nil
	})
	t.Cleanup(restore)
	Wire()
	if _, err = routersplit.LegacyContext(ctx, r, "default"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("deploy.Wire replaced the shared setup-session reader")
	}
}
