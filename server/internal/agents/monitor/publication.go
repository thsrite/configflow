package monitor

// The pure part of backend/agents/oxidns_publication_stats.py that the
// overview needs (advance, totals, _task, _new_state, PublicationPending).
// checkpoint and complete (publication hooks) belong to A07 and are added to
// this package there.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"configflow/internal/jsonx"
)

// terminal is TERMINAL.
var terminal = map[string]bool{"succeeded": true, "failed": true, "rolled_back": true, "rollback_failed": true}

// ErrPublicationPending is PublicationPending: do not discard a checkpoint
// while the activation outcome is unknown.
var ErrPublicationPending = errors.New("PublicationPending")

// tokenHex is secrets.token_hex(16) (a variable for tests).
var tokenHex = func() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// attributeError is the AttributeError of value.get(...) on a truthy value
// that is not a dict (Python answers 500).
func attributeError(value any) error {
	return fmt.Errorf("AttributeError: %s object has no attribute 'get'", pyTypeName(value))
}

func pyTypeName(value any) string {
	switch v := value.(type) {
	case string:
		return "'str'"
	case bool:
		return "'bool'"
	case []any:
		return "'list'"
	case jsonx.Number:
		if v.IsInt() {
			return "'int'"
		}
		return "'float'"
	}
	return "'object'"
}

// orDict is `value or {}` followed by .get: a falsy value is an empty dict,
// a truthy non-dict an AttributeError.
func orDict(value any) (*jsonx.Object, error) {
	if !jsonx.Truthy(value) {
		return jsonx.NewObject(), nil
	}
	o, ok := value.(*jsonx.Object)
	if !ok {
		return nil, attributeError(value)
	}
	return o, nil
}

// newState is _new_state.
func newState(snapshot, core, observations *jsonx.Object, target, reason string) *jsonx.Object {
	return jsonx.Obj("schema", 1, "generation", tokenHex(),
		"started_at_ms", snapshot.Value("collected_at_ms"), "reason", reason, "target", target,
		"instance_id", core.Value("instance_id"), "source_revision", snapshot.Value("source_revision"),
		"runtime_revision", snapshot.Value("runtime_revision"), "baseline", observations)
}

func pyEqualStr(value any, s string) bool {
	v, ok := value.(string)
	return ok && v == s
}

// publicationTask is _task: the deployment record of the pending
// checkpoint, or an empty dict when it was superseded or belongs elsewhere.
func publicationTask(agent *jsonx.Object, pending *jsonx.Object) (*jsonx.Object, error) {
	var agentValue any
	if agent != nil {
		agentValue = agent
	}
	a, err := orDict(agentValue)
	if err != nil {
		return nil, err
	}
	ident := pending.Value("deployment_id")
	deployments, err := orDict(a.Value("deployments"))
	if err != nil {
		return nil, err
	}
	var entry any
	if id, ok := ident.(string); ok {
		entry = deployments.Value(id)
	}
	task, err := orDict(entry)
	if err != nil {
		return nil, err
	}
	latest, err := orDict(a.Value("latest_deployment"))
	if err != nil {
		return nil, err
	}
	in := func(value any, options ...string) bool {
		for _, option := range options {
			if pyEqualStr(value, option) {
				return true
			}
		}
		return false
	}
	superseded := !jsonx.Equal(latest.Value("deployment_id"), ident) && !(in(task.Value("status"), "succeeded", "rolled_back") &&
		in(latest.Value("status"), "preparing", "uploading", "ready"))
	profile := a.Value("profile_id")
	if !jsonx.Truthy(profile) {
		profile = "default"
	}
	if superseded || !pyEqualStr(task.Value("service_type"), "oxidns") || !jsonx.Equal(task.Value("profile_id"), profile) {
		return jsonx.NewObject(), nil
	}
	return task, nil
}

// unhashable reports a list or dict (TypeError in a set membership test).
func unhashable(value any) bool {
	switch value.(type) {
	case []any, *jsonx.Object:
		return true
	}
	return false
}

func collectedLess(a, b any) bool {
	x, okA := asNum(a)
	y, okB := asNum(b)
	return okA && okB && x.less(y)
}

// Advance is advance: the pure transition of the dashboard state; the
// caller commits it with the generation compare-and-set. previous nil is
// None. The result is previous itself when nothing changes.
func Advance(previous, snapshot, core, observations *jsonx.Object, target string, agent *jsonx.Object, manual bool) (*jsonx.Object, error) {
	reason := ""
	switch {
	case manual:
		reason = "manual"
	case previous == nil:
		reason = "initial"
	case !pyEqualStr(previous.Value("target"), target):
		reason = "target_changed"
	case !jsonx.Equal(previous.Value("instance_id"), core.Value("instance_id")):
		reason = "core_changed"
	case !jsonx.Equal(previous.Value("source_revision"), snapshot.Value("source_revision")):
		reason = "configuration_changed"
	case !jsonx.Equal(previous.Value("runtime_revision"), snapshot.Value("runtime_revision")):
		reason = "core_changed"
	case MetricsCountersRestarted(observations, previous.GetObject("baseline")):
		reason = "counters_changed"
	}
	var pending *jsonx.Object
	if previous != nil {
		pending, _ = previous.Value("pending").(*jsonx.Object)
	}
	if pending != nil && pending.Len() > 0 && !manual && pyEqualStr(previous.Value("target"), target) {
		task, err := publicationTask(agent, pending)
		if err != nil {
			return nil, err
		}
		status := task.Value("status")
		if collectedLess(snapshot.Value("collected_at_ms"), pending.Value("collected_at_ms")) {
			return nil, ErrPublicationPending
		}
		if jsonx.Truthy(status) {
			if unhashable(status) {
				return nil, fmt.Errorf("TypeError: unhashable type: %s", pyTypeName(status))
			}
			if s, ok := status.(string); !ok || !terminal[s] {
				if reason != "" {
					return nil, ErrPublicationPending
				}
				return previous, nil
			}
		}
		var expected any
		switch {
		case pyEqualStr(status, "succeeded"):
			expected = pending.Value("expected_revision")
		case pyEqualStr(status, "rolled_back"):
			expected = previous.Value("source_revision")
		}
		if reason != "" && expected != nil && jsonx.Equal(expected, snapshot.Value("source_revision")) &&
			!jsonx.Truthy(task.Value("manual_rollback")) {
			// Counts observed since the new runtime started belong to the
			// same dashboard period.
			count, _ := asNum(previous.Value("publication_count"))
			next, _ := count.add(intNum(1))
			state := previous.Clone()
			state.Set("generation", tokenHex())
			state.Set("reason", "publication")
			state.Set("instance_id", core.Value("instance_id"))
			state.Set("source_revision", snapshot.Value("source_revision"))
			state.Set("runtime_revision", snapshot.Value("runtime_revision"))
			state.Set("baseline", ZeroBaseline(observations))
			state.Set("carried", pending.Value("totals"))
			state.Set("carried_partial", pending.Value("partial"))
			state.Set("pending", nil)
			state.Set("publication_count", next.value())
			return state, nil
		}
		if reason == "" {
			// Rejection before mutation, or a publication which did not
			// reload: keep the original baseline.
			state := previous.Clone()
			state.Set("generation", tokenHex())
			state.Set("pending", nil)
			return state, nil
		}
	}
	if reason != "" {
		return newState(snapshot, core, observations, target, reason), nil
	}
	return previous, nil
}

// Totals is totals: the adjusted observations of a state and whether they
// are partial.
func Totals(state, observations *jsonx.Object) (*jsonx.Object, bool, error) {
	adjusted, partial, err := ApplyMetricsBaseline(observations, state.GetObject("baseline"))
	if err != nil {
		return nil, false, err
	}
	if pyNumberEqual(state.Value("schema"), jsonx.Int(2)) {
		adjusted = AddObservations(adjusted, state.GetObject("carried"))
		carried, _ := state.Value("carried_partial").(bool)
		partial = partial || carried
	}
	return adjusted, partial, nil
}
