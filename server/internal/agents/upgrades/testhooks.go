package upgrades

import (
	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"context"
	"time"
)

// Test seams for route tests in other packages (the Python tests
// monkeypatch upgrades._request, upgrades.artifacts,
// address_follow._reaches/_ask and threading.Thread). Each returns a
// function restoring the previous value.

// SetRequestForTest replaces _request(agent, method, path, json=payload).
func SetRequestForTest(fn func(agent *jsonx.Object, method, path string, payload *jsonx.Object) (*agents.Response, error)) func() {
	previous := requestHook
	requestHook = fn
	return func() { requestHook = previous }
}

// SetArtifactsForTest replaces artifacts().
func SetArtifactsForTest(fn func() (*jsonx.Object, error)) func() {
	previous := artifactsFunc
	artifactsFunc = fn
	return func() { artifactsFunc = previous }
}

// SetReachesForTest replaces address_follow._reaches.
func SetReachesForTest(fn func(agent *jsonx.Object, host string) bool) func() {
	previous := reachesHook
	reachesHook = fn
	return func() { reachesHook = previous }
}

// SetAskForTest replaces address_follow._ask (status 0 is None).
func SetAskForTest(fn func(agent *jsonx.Object, host, method string, body *jsonx.Object) (int, *jsonx.Object)) func() {
	previous := askHook
	askHook = fn
	return func() { askHook = previous }
}

// RunInlineForTest waits for an independently spawned test-runtime worker,
// preserving the old seam's synchronous observation without unmanaged work.
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

// SetRetrySecondsForTest sets RETRY_SECONDS.
func SetRetrySecondsForTest(seconds float64) func() {
	previous := RetrySeconds
	RetrySeconds = seconds
	return func() { RetrySeconds = previous }
}

// AgentResponse builds a fake Agent answer: JSON of payload with status.
func AgentResponse(payload any, status int) *agents.Response {
	body, err := jsonx.Default(payload)
	if err != nil {
		panic(err)
	}
	return &agents.Response{StatusCode: status, Header: map[string][]string{"Content-Type": {"application/json"}}, Body: body}
}
