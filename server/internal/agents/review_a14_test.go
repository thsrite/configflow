package agents

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"configflow/internal/lifecycle"
)

func startReviewRuntime(t *testing.T) *lifecycle.Runtime {
	t.Helper()
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(context.Background())
	t.Cleanup(func() { rt.Stop(8 * time.Second) })
	return rt
}

// A14 review M1: a manual rollback reservation whose submission was lost at
// shutdown (or by a process exit) must not wedge the Agent. While this process
// still owns the submission a poll keeps waiting; afterwards it reissues the
// guarded, idempotent rollback exactly as for an observed unknown outcome.
func TestReviewA14ManualRollbackWithoutLiveOwnerIsReissued(t *testing.T) {
	e := newEnv(t)
	id := successfulDeployment(e, "manual-test")
	rt := startReviewRuntime(t)
	ctx := rt.BindRequest(e.ctx)
	livePolls := 0
	e.fake.set(func(req Request) (*Response, error) {
		if req.Method == "GET" {
			return reply(map[string]any{"success": true, "status": "succeeded"}, 200), nil
		}
		if livePolls == 0 {
			// A poll while this process still owns the submission waits.
			livePolls++
			polled := e.poll(id)
			if !deploymentFlagTrue(polled, "pending") || derefStr(polled.Message) != "Rollback submission awaiting Agent confirmation" {
				t.Errorf("live owner poll %v", polled)
			}
		}
		// Shutdown cancels the bound request; the connection is reset before
		// the Agent reads the rollback request.
		rt.Stop(8 * time.Second)
		return nil, &TransportError{Err: context.Canceled}
	})
	if _, err := e.m.RollbackDeploymentModel(ctx, e.id(), id); !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown result", err)
	}
	reserved, _ := e.get(e.id()).Deployments.Get(id)
	if reserved.Status != "rolling_back" || !deploymentFlagTrue(&reserved, "manual_rollback") {
		t.Fatal("reservation not durable", reserved)
	}
	posts := 0
	e.fake.set(func(req Request) (*Response, error) {
		if req.Method == "GET" {
			return reply(map[string]any{"success": true, "status": "succeeded"}, 200), nil
		}
		posts++
		if !strings.HasSuffix(req.URL, "/deployments/"+id+"/rollback") {
			t.Errorf("url %s", req.URL)
		}
		return reply(map[string]any{"success": true, "status": "rolled_back", "manual_rollback": true, "config_version": "old"}, 200), nil
	})
	rt.Start(context.Background())
	restarted := newManager(t, e.repo)
	restarted.SetClient(e.fake)
	result, err := restarted.GetDeploymentModel(rt.BindRequest(e.ctx), e.id(), id)
	if err != nil {
		t.Fatal(err)
	}
	if posts != 1 || result.Status != "rolled_back" || !deploymentFlagTrue(result, "manual_rollback") {
		t.Fatalf("posts %d result %v", posts, result)
	}
	if !deploymentFlagTrue(e.begin("after-rollback"), "success") {
		t.Fatal("Agent stayed blocked")
	}
	e.finish("after-rollback")
}
