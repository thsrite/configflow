package agents

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
)

func startReviewRuntime(t *testing.T) {
	t.Helper()
	lifecycle.Stop(8 * time.Second)
	if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	lifecycle.Start(context.Background())
	t.Cleanup(func() {
		lifecycle.Stop(8 * time.Second)
		if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
			t.Error("restore idle Runtime", err)
		}
	})
}

// A14 review M1: a manual rollback reservation whose submission was lost at
// shutdown (or by a process exit) must not wedge the Agent. While this process
// still owns the submission a poll keeps waiting; afterwards it reissues the
// guarded, idempotent rollback exactly as for an observed unknown outcome.
func TestReviewA14ManualRollbackWithoutLiveOwnerIsReissued(t *testing.T) {
	e := newEnv(t)
	id := successfulDeployment(e, "manual-test")
	startReviewRuntime(t)
	ctx := lifecycle.BindRequest(e.ctx)
	livePolls := 0
	e.fake.set(func(req Request) (*Response, error) {
		if req.Method == "GET" {
			return reply(jsonx.Obj("success", true, "status", "succeeded"), 200), nil
		}
		if livePolls == 0 {
			// A poll while this process still owns the submission waits.
			livePolls++
			polled := e.poll(id)
			if !polled.GetBool("pending") || polled.Value("message") != "Rollback submission awaiting Agent confirmation" {
				t.Errorf("live owner poll %v", polled)
			}
		}
		// Shutdown cancels the bound request; the connection is reset before
		// the Agent reads the rollback request.
		lifecycle.Stop(8 * time.Second)
		return nil, &TransportError{Err: context.Canceled}
	})
	if _, err := e.m.RollbackDeployment(ctx, e.id(), id); !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown result", err)
	}
	reserved := e.get(e.id()).GetObject("deployments").GetObject(id)
	if reserved.Value("status") != "rolling_back" || !reserved.GetBool("manual_rollback") {
		t.Fatal("reservation not durable", reserved)
	}
	posts := 0
	e.fake.set(func(req Request) (*Response, error) {
		if req.Method == "GET" {
			return reply(jsonx.Obj("success", true, "status", "succeeded"), 200), nil
		}
		posts++
		if !strings.HasSuffix(req.URL, "/deployments/"+id+"/rollback") {
			t.Errorf("url %s", req.URL)
		}
		return reply(jsonx.Obj("success", true, "status", "rolled_back", "manual_rollback", true, "config_version", "old"), 200), nil
	})
	lifecycle.Start(context.Background())
	restarted := newManager(t, e.repo)
	restarted.SetClient(e.fake)
	result, err := restarted.GetDeployment(lifecycle.BindRequest(e.ctx), e.id(), id)
	if err != nil {
		t.Fatal(err)
	}
	if posts != 1 || result.Value("status") != "rolled_back" || !result.GetBool("manual_rollback") {
		t.Fatalf("posts %d result %v", posts, result)
	}
	if !e.begin("after-rollback").GetBool("success") {
		t.Fatal("Agent stayed blocked")
	}
	e.finish("after-rollback")
}
