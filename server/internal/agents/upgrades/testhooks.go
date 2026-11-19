package upgrades

import (
	"context"
	"time"

	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

// SetRequestForTest replaces update control transport with a typed seam.
func SetRequestForTest(fn func(context.Context, *model.Agent, string, string, []byte) (*agents.Response, error)) func() {
	prev := requestOverride
	requestOverride = fn
	return func() { requestOverride = prev }
}

// SetArtifactsForTest replaces the native artifact catalog.
func SetArtifactsForTest(fn func(context.Context) (model.Map[model.UpgradeArtifact], error)) func() {
	prev := artifactsFunc
	artifactsFunc = fn
	return func() { artifactsFunc = prev }
}

// SetReachesForTest replaces the authenticated address probe.
func SetReachesForTest(fn func(*model.Agent, string) bool) func() {
	prev := reachesOverride
	reachesOverride = fn
	return func() { reachesOverride = prev }
}

// SetAskForTest replaces the address device endpoint.
func SetAskForTest(fn func(*model.Agent, string, string, []byte) (int, model.Fields)) func() {
	prev := askOverride
	askOverride = fn
	return func() { askOverride = prev }
}

// RunInlineForTest waits for a tracked worker so callers can observe its result synchronously.
func RunInlineForTest() func() {
	previous := startThread
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
	runner.Start(context.Background())
	startThread = func(ctx context.Context, name string, fn func(context.Context)) error {
		done := make(chan struct{})
		if err := runner.Spawn(ctx, name, func(worker context.Context) { defer close(done); fn(worker) }); err != nil {
			return err
		}
		<-done
		return nil
	}
	return func() { startThread = previous; runner.Shutdown(context.Background()) }
}

// SetRetrySecondsForTest temporarily changes the address retry throttle.
func SetRetrySecondsForTest(seconds float64) func() {
	previous := RetrySeconds
	RetrySeconds = seconds
	return func() { RetrySeconds = previous }
}
