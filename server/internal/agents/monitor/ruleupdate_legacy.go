package monitor

import (
	"configflow/internal/jsonx"
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// HABusyError is the ValueError of oxidns_ha_publish.rules: the route
// answers SERVICE_BUSY 409 with its text.
type HABusyError struct{ Message string }

func (e *HABusyError) Error() string { return e.Message }

// ErrHARulesNotPorted is the default pair rule update until A18 (DNS HA)
// registers oxidns_ha_publish.rules with SetHARules.
var ErrHARulesNotPorted = errors.New("oxidns_ha_publish.rules is not ported yet (A18)")

// HARulesFunc is oxidns_ha_publish.rules(manager, agent_id, method): the
// task answer and its HTTP status; a *HABusyError for its ValueErrors.
type HARulesFunc func(ctx context.Context, m Manager, agentID, method string) (*jsonx.Object, int, error)

var (
	haRulesMu sync.RWMutex
	haRules   HARulesFunc = func(context.Context, Manager, string, string) (*jsonx.Object, int, error) {
		return nil, 0, ErrHARulesNotPorted
	}
)

// SetHARules installs oxidns_ha_publish.rules (A18) and returns a function
// restoring the previous one.
func SetHARules(fn HARulesFunc) (restore func()) {
	haRulesMu.Lock()
	previous := haRules
	haRules = fn
	haRulesMu.Unlock()
	return func() {
		haRulesMu.Lock()
		haRules = previous
		haRulesMu.Unlock()
	}
}

func currentHARules() HARulesFunc {
	haRulesMu.RLock()
	defer haRulesMu.RUnlock()
	return haRules
}

var haTerminal = map[string]bool{"succeeded": true, "failed": true, "rollback_failed": true}

// SupportsPaths is the legacy model adapter.
func SupportsPaths(agent *jsonx.Object) bool {
	return versionAtLeast(agent, RuleUpdatePathsMinAgentVersion)
}

func strIn(value any, options []string) bool {
	s, ok := value.(string)
	return ok && contains(options, s)
}

func ruleUpdateTask(value any) (*jsonx.Object, error) {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return nil, err
	}
	task, err := decodeRuleTask(raw)
	if err != nil {
		return nil, err
	}
	raw, err = json.Marshal(task)
	if err != nil {
		return nil, err
	}
	value, err = jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	return value.(*jsonx.Object), nil
}

func pairRules(ctx context.Context, m Registry, id, method string) (json.RawMessage, int, error) {
	var old Manager
	switch m := m.(type) {
	case Manager:
		old = m
	case legacyRegistry:
		old = m.Manager
	default:
		return nil, 0, ErrHARulesNotPorted
	}
	result, status, err := currentHARules()(ctx, old, id, method)
	if err != nil {
		return nil, status, err
	}
	raw, err := jsonx.Compact(result)
	return raw, status, err
}

// RuleUpdateCall converts the legacy package representation once.
func RuleUpdateCall(ctx context.Context, m Manager, id, method string, value any) (*jsonx.Object, int, error) {
	var paths []string
	if value != nil {
		var ok bool
		paths, ok = nativeTags(value)
		if !ok {
			return nil, 0, RuleUpdateFail("INVALID_REQUEST", 400)
		}
	}
	result, status, err := CallRuleUpdate(ctx, registryFor(m), id, method, paths)
	if err != nil {
		return nil, status, err
	}
	var raw []byte
	if result.Pair != nil {
		raw = result.Pair
	} else {
		raw, err = json.Marshal(result)
	}
	if err != nil {
		return nil, 0, err
	}
	decoded, err := jsonx.Parse(raw)
	if err != nil {
		return nil, 0, err
	}
	return decoded.(*jsonx.Object), status, nil
}

func ruleUpdateAgent(ctx context.Context, m Manager, id string) (*jsonx.Object, error) {
	agent, err := agentFor(ctx, m, id, RuleUpdateMinAgentVersion, false)
	return agent, err
}
