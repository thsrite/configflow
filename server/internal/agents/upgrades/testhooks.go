package upgrades

import (
	"configflow/internal/agents"
	"configflow/internal/jsonx"
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

// RunInlineForTest runs the background address check inside the request.
func RunInlineForTest() func() {
	previous := startThread
	startThread = func(fn func()) { fn() }
	return func() { startThread = previous }
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
