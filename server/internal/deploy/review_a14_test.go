package deploy

import (
	"context"
	"testing"
	"time"

	"configflow/internal/agents/agentfake"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
)

// A14 review L1: unforced provider details share the one-write-per-second
// throttle of Python's preparation_detail; forced phases always persist.
func TestReviewA14UnforcedProgressIsThrottled(t *testing.T) {
	f := newPublish(t, agentfake.Options{}, nil)
	if _, err := f.m.ReserveDeployment(f.ctx, "publish-agent", "throttle"); err != nil {
		t.Fatal(err)
	}
	defer f.m.FinishDeploymentPreparation("publish-agent", "throttle")
	message := func() string {
		d, _ := f.agent(t).Deployments.Get("throttle")
		return textValue(d.Message)
	}
	clock := lifecycletest.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Clock: clock, Mode: lifecycle.ModeManual})
	runtime.Start(f.ctx)
	defer runtime.Shutdown(context.Background())
	p := publishProgress(runtime.BindRequest(f.ctx), f.m, "publish-agent", "throttle", "")
	p.Message("正在准备节点来源「A」")
	p.Message("节点来源「A」已准备")
	p.Message("正在准备节点来源「B」")
	if got := message(); got != "正在准备节点来源「A」" {
		t.Fatal("unforced details were not throttled", got)
	}
	clock.Advance(999 * time.Millisecond)
	p.Message("节点来源「B」已准备")
	if got := message(); got != "正在准备节点来源「A」" {
		t.Fatal("detail persisted before one second", got)
	}
	clock.Advance(time.Millisecond)
	p.Message("节点来源「B」已准备")
	if got := message(); got != "节点来源「B」已准备" {
		t.Fatal("detail did not persist at one second", got)
	}
	p.Phase("正在校验节点来源与拨号引用关系（2 个文件）")
	if got := message(); got != "正在校验节点来源与拨号引用关系（2 个文件）" {
		t.Fatal("forced phase was throttled", got)
	}
}
