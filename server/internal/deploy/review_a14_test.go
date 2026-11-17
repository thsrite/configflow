package deploy

import (
	"testing"

	"configflow/internal/agents/agentfake"
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
