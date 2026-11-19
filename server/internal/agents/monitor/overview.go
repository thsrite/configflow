package monitor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

// UpstreamLabel associates a metrics series with its public address label.
type UpstreamLabel struct{ PluginTag, Upstream, Address string }

// Snapshot holds validated data from one OxiDNS overview response.
type Snapshot struct {
	Identity     SnapshotIdentity
	Core         CoreStatus
	Recorders    []string
	Observations *Observations
	Labels       []UpstreamLabel
}

func readOverviewSnapshot(ctx context.Context, m Registry, a *model.Agent, dashboard bool) (*Snapshot, error) {
	minimum := MinAgentVersion
	if dashboard {
		minimum = MinDashboardAgentVersion
	}
	fields, err := requestNative(ctx, m, a, "overview", nil, minimum)
	if err != nil {
		return nil, err
	}
	invalid := NewError("INVALID_RESPONSE", 0, "")
	raw, _ := fields.Get("health")
	var health model.Fields
	if jsondecode.Unmarshal(raw, &health) != nil {
		return nil, invalid
	}
	raw, _ = fields.Get("capabilities")
	var capabilities model.Fields
	if jsondecode.Unmarshal(raw, &capabilities) != nil {
		return nil, invalid
	}
	raw, present := fields.Get("metrics")
	metrics, valid := originalString(raw, present, nil)
	if !valid {
		return nil, invalid
	}
	observations, err := ReadMetrics(metrics)
	if err != nil {
		return nil, invalid
	}
	snapshot := &Snapshot{Recorders: []string{}, Observations: observations, Labels: []UpstreamLabel{}}
	snapshot.Core = CoreStatus{Version: pySlice(fieldText(health, "version"), 64), Status: pySlice(fieldText(health, "status"), 32), InstanceID: pySlice(fieldText(health, "instance_id"), 128)}
	raw, _ = health.Get("uptime_ms")
	snapshot.Core.UptimeMS = protocolNumber(raw, MaxInteger)
	if raw, present := capabilities.Get("query_recorders"); present {
		if jsondecode.Unmarshal(raw, &snapshot.Recorders) != nil || snapshot.Recorders == nil || len(snapshot.Recorders) > 64 {
			return nil, invalid
		}
		// json.Unmarshal into []string accepts null elements as an empty string;
		// exact member decoding below requires original string tokens instead.
		var tags []json.RawMessage
		_ = jsondecode.Unmarshal(raw, &tags)
		for _, tag := range tags {
			if _, valid := originalString(tag, true, nil); !valid {
				return nil, invalid
			}
		}
	}
	if raw, present := fields.Get("upstream_labels"); present {
		var labels []json.RawMessage
		if jsondecode.Unmarshal(raw, &labels) != nil || labels == nil || len(labels) > ClientSampleLimit {
			return nil, invalid
		}
		seen := map[groupKey]bool{}
		for _, raw := range labels {
			var fields model.Fields
			if jsondecode.Unmarshal(raw, &fields) != nil {
				return nil, invalid
			}
			var label UpstreamLabel
			for _, field := range []struct {
				name   string
				target *string
			}{{"plugin_tag", &label.PluginTag}, {"upstream", &label.Upstream}, {"address", &label.Address}} {
				raw, present := fields.Get(field.name)
				value, valid := originalString(raw, present, nil)
				if !valid || runeLen(value) > 16384 {
					return nil, invalid
				}
				*field.target = value
			}
			key := groupKey{label.PluginTag, label.Upstream}
			if seen[key] {
				return nil, invalid
			}
			seen[key] = true
			snapshot.Labels = append(snapshot.Labels, label)
		}
	}
	if dashboard {
		for _, tag := range snapshot.Recorders {
			if !validRecorderTag(tag) {
				return nil, invalid
			}
		}
		collected, valid := protocolInt(fields, "collected_at_ms", 0, 253402300799999)
		source, runtime := fieldText(fields, "source_revision"), fieldText(fields, "runtime_revision")
		if !valid || !instanceRE.MatchString(fieldText(health, "instance_id")) || !hex64.MatchString(source) || !hex64.MatchString(runtime) {
			return nil, invalid
		}
		snapshot.Identity = SnapshotIdentity{CollectedAtMS: collected, SourceRevision: source, RuntimeRevision: runtime}
	}
	return snapshot, nil
}

// TargetIdentity hashes only original target identity fields, never R5 projections.
func TargetIdentity(a *model.Agent) (string, error) {
	identity := make([]json.RawMessage, 0, 5)
	for _, key := range []string{"id", "host", "port", "token", "service_type"} {
		raw, present, err := a.MemberJSON(key)
		if err != nil {
			return "", err
		}
		if !present {
			raw = []byte("null")
		}
		identity = append(identity, raw)
	}
	return digestIdentity(identity)

}
func verifyNativeTarget(ctx context.Context, m Registry, id, expected string) error {
	current, err := m.ReadAgent(ctx, id, true)
	if err != nil {
		return err
	}
	if current == nil {
		return NewError("DASHBOARD_CHANGED", 409, "")
	}
	digest, err := TargetIdentity(current)
	if err != nil {
		return err
	}
	if digest != expected {
		return NewError("DASHBOARD_CHANGED", 409, "")
	}
	return nil
}
func compareDashboardState(ctx context.Context, m Registry, store *StateStore, id, expected string, replacement *DashboardState, verify func(context.Context) error) error {
	return m.WithReadSnapshot(ctx, func(held context.Context) error {
		if err := held.Err(); err != nil {
			return err
		}
		if err := verify(held); err != nil {
			return err
		}
		_, err := store.CompareState(held, id, expected, replacement)
		return err
	})
}

// PublicAgentSummary retains the selected public field values without credentials.
type PublicAgentSummary struct {
	ID        json.RawMessage `json:"id"`
	Name      json.RawMessage `json:"name"`
	Host      json.RawMessage `json:"host"`
	ProfileID json.RawMessage `json:"profile_id"`
	Version   json.RawMessage `json:"version"`
	Status    json.RawMessage `json:"status"`
}

func publicAgent(a *model.Agent) PublicAgentSummary {
	read := func(key string) json.RawMessage {
		raw, present, err := a.MemberJSON(key)
		if err != nil || !present {
			return []byte("null")
		}
		return raw
	}
	return PublicAgentSummary{read("id"), read("name"), read("host"), read("profile_id"), read("version"), read("status")}
}

// PublicUpstream identifies one series without upstream credentials or query text.
type PublicUpstream struct {
	ID           string        `json:"id"`
	Tag          string        `json:"tag"`
	Upstream     string        `json:"upstream"`
	Address      string        `json:"address"`
	Requests     *MetricNumber `json:"requests"`
	Failed       *MetricNumber `json:"failed"`
	AvgLatencyMS *MetricNumber `json:"avg_latency_ms"`
}

func publicUpstreamRows(rows []UpstreamSummary, labels []UpstreamLabel) ([]PublicUpstream, error) {
	addresses := map[groupKey]string{}
	for _, label := range labels {
		addresses[groupKey{label.PluginTag, label.Upstream}] = label.Address
	}
	out := make([]PublicUpstream, 0, len(rows))
	for _, row := range rows {
		tag, _ := json.Marshal(row.Tag)
		upstream, _ := json.Marshal(row.Upstream)
		digest, err := digestIdentity([]json.RawMessage{tag, upstream})
		if err != nil {
			return nil, err
		}

		out = append(out, PublicUpstream{digest, displayAddress(row.Tag), displayAddress(row.Upstream), displayAddress(addresses[groupKey{row.Tag, row.Upstream}]), row.Requests, row.Failed, row.AvgLatencyMS})
	}
	return out, nil
}

// StatisticsBoundary describes the current dashboard period.
type StatisticsBoundary struct {
	Generation       string `json:"generation"`
	StartedAtMS      int64  `json:"started_at_ms"`
	Reason           string `json:"reason"`
	Partial          bool   `json:"partial"`
	PublicationCount int64  `json:"publication_count,omitempty"`
}

// Dashboard is the credential-free metrics, ranking and statistics response.
type Dashboard struct {
	Success     bool                `json:"success"`
	Agent       PublicAgentSummary  `json:"agent"`
	CollectedAt string              `json:"collected_at"`
	Core        CoreStatus          `json:"core"`
	Recorders   []string            `json:"recorders"`
	Metrics     MetricTotals        `json:"metrics"`
	Caches      []CacheSummary      `json:"caches"`
	Servers     []ServerSummary     `json:"servers"`
	Upstreams   []PublicUpstream    `json:"upstreams"`
	Statistics  *StatisticsBoundary `json:"statistics"`
	Rankings    *Rankings           `json:"rankings"`
}

func dashboardResult(a *model.Agent, snapshot *Snapshot, summary *MetricSummary, collected string) (*Dashboard, error) {
	upstreams, err := publicUpstreamRows(summary.Upstreams, snapshot.Labels)
	if err != nil {
		return nil, err
	}
	return &Dashboard{Success: true, Agent: publicAgent(a), CollectedAt: collected, Core: snapshot.Core, Recorders: snapshot.Recorders, Metrics: summary.Metrics, Caches: summary.Caches, Servers: summary.Servers, Upstreams: upstreams}, nil
}

// ReadOverview reads one dashboard and atomically advances its statistics period.
func ReadOverview(ctx context.Context, m Registry, id string, args [][2]string) (*Dashboard, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	params, err := dashboardParams(args)
	if err != nil {
		return nil, err
	}
	result, err := readDashboard(ctx, m, id, params, false)
	return result, nativeStateError(err)
}

// ResetStatistics starts a new period, explicitly discarding an old checkpoint.
// This remains the recovery action for damaged deployment records in monitoring.
func ResetStatistics(ctx context.Context, m Registry, id string, body json.RawMessage) (*Dashboard, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var fields model.Fields
	if jsondecode.Unmarshal(body, &fields) != nil || fields.Len() > 1 || fields.Len() == 1 && !fields.Has("recorder") {
		return nil, invalidQuery()
	}
	var params query
	if raw, present := fields.Get("recorder"); present {
		tag, valid := originalString(raw, present, nil)
		if !valid || !validRecorderTag(tag) {
			return nil, invalidQuery()
		}
		params = query{{"recorder", tag}}
	}
	result, err := readDashboard(ctx, m, id, params, true)
	return result, nativeStateError(err)
}
func readDashboard(ctx context.Context, m Registry, id string, params query, manual bool) (*Dashboard, error) {
	minimum := MinAgentVersion
	if manual {
		minimum = MinDashboardAgentVersion
	}
	a, err := readDNSAgent(ctx, m, id, minimum, false)
	if err != nil {
		return nil, err
	}
	if !modelVersionAtLeast(a, MinDashboardAgentVersion) {
		snapshot, err := readOverviewSnapshot(ctx, m, a, false)
		if err != nil {
			return nil, err
		}
		result, err := dashboardResult(a, snapshot, Summarize(snapshot.Observations), isoUTC(nowUTC()))
		if err != nil {
			return nil, err
		}
		result.Rankings = unavailableRankings("upgrade_required")
		return result, nil
	}
	store := NewStateStore(m.DataDir())
	previous, err := store.ReadState(ctx, id)
	if err != nil {
		return nil, err
	}
	snapshot, err := readOverviewSnapshot(ctx, m, a, true)
	if err != nil {
		return nil, err
	}
	digest, err := TargetIdentity(a)
	if err != nil {
		return nil, err
	}
	if err := verifyNativeTarget(ctx, m, id, digest); err != nil {
		return nil, err
	}
	current, err := m.ReadAgent(ctx, id, true)
	if err != nil {
		return nil, err
	}
	state, err := AdvanceState(previous, &snapshot.Identity, &snapshot.Core, snapshot.Observations, digest, current, manual)
	if err != nil {
		return nil, err
	}
	if snapshot.Identity.CollectedAtMS < state.StartedAtMS {
		return nil, NewError("SNAPSHOT_CHANGED", 409, "")
	}
	adjusted, partial, err := StateTotals(state, snapshot.Observations)
	if err != nil {
		return nil, err
	}
	result, err := dashboardResult(a, snapshot, Summarize(adjusted), isoUTC(fromTimestampMS(snapshot.Identity.CollectedAtMS)))
	if err != nil {
		return nil, err
	}
	result.Rankings, err = overviewRanking(ctx, m, a, params, &snapshot.Identity, state, snapshot.Recorders, manual)
	if err != nil {
		return nil, err
	}
	expected := ""
	if previous != nil {
		expected = previous.Generation
	}
	var replacement *DashboardState
	if state != previous {
		replacement = state
	}
	if err := compareDashboardState(ctx, m, store, id, expected, replacement, func(held context.Context) error { return verifyNativeTarget(held, m, id, digest) }); err != nil {
		return nil, err
	}
	result.Statistics = &StatisticsBoundary{state.Generation, state.StartedAtMS, state.Reason, partial, state.PublicationCount}
	return result, nil
}

// mapStateErrors translates state failures only at the HTTP-facing operation.
func nativeStateError(err error) error {
	var state *StateError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrPublicationPending):
		return NewError("SNAPSHOT_CHANGED", 409, "")
	case errors.As(err, &state):
		return NewError("DASHBOARD_STATE_ERROR", 500, "")
	case errors.Is(err, ErrStateConflict):
		return NewError("DASHBOARD_CHANGED", 409, "")
	}
	return err
}

func digestIdentity(parts []json.RawMessage) (string, error) {
	encoded, err := jsonresp.EncodeDevice(parts)
	if err != nil {
		return "", err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, encoded); err != nil {
		return "", err
	}
	digest := sha256.Sum256(compact.Bytes())
	return hex.EncodeToString(digest[:]), nil
}
