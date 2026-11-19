package monitor

import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
)

// Advance is the legacy pure state-transition codec.
func Advance(previous, snapshot, core, observations *jsonx.Object, target string, agent *jsonx.Object, manual bool) (*jsonx.Object, error) {
	var prev *DashboardState
	if previous != nil {
		raw, err := jsonx.Compact(previous)
		if err != nil {
			return nil, err
		}
		prev, err = DecodeState(raw)
		if err != nil {
			return nil, err
		}
	}
	var a *model.Agent
	if agent != nil {
		a = new(model.Agent)
		if err := metricJSON(agent, a); err != nil {
			return nil, err
		}
	}
	var stamp SnapshotIdentity
	if err := metricJSON(snapshot, &stamp); err != nil {
		return nil, err
	}
	var status CoreStatus
	if err := metricJSON(core, &status); err != nil {
		return nil, err
	}
	state, err := AdvanceState(prev, &stamp, &status, nativeObservations(observations), target, a, manual)
	if err != nil {
		return nil, err
	}
	if state == prev && previous != nil {
		return previous, nil
	}
	return metricObject(state), nil
}

// Totals is the legacy period-total codec.
func Totals(state, observations *jsonx.Object) (*jsonx.Object, bool, error) {
	raw, err := jsonx.Compact(state)
	if err != nil {
		return nil, false, err
	}
	s, err := DecodeState(raw)
	if err != nil {
		return nil, false, err
	}
	adjusted, partial, err := StateTotals(s, nativeObservations(observations))
	if err != nil {
		return nil, false, err
	}
	return metricObject(adjusted), partial, nil
}
