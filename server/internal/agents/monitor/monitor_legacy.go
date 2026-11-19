package monitor

import (
	"context"
	"encoding/json"

	"configflow/internal/agents"
	"configflow/internal/jsonx"

	"golang.org/x/text/encoding/charmap"
)

// Manager is what the monitor needs of the Agent manager.
type Manager interface {
	// WithReadSnapshot excludes configuration writers during verification and leaf CAS.
	WithReadSnapshot(context.Context, func(context.Context) error) error
	GetAgentByID(ctx context.Context, agentID string, history bool) (*jsonx.Object, error)
	DataDir() string
	Client() agents.Client
}

type agentsManager struct{ *agents.Manager }

func (m agentsManager) WithReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	return m.Repository().WithReadSnapshot(ctx, fn)
}

func (m agentsManager) DataDir() string { return m.Repository().DataDir() }

// FromAgents adapts the registry's Manager.
func FromAgents(m *agents.Manager) Manager { return agentsManager{m} }

func agentFor(ctx context.Context, m Manager, id, minimum string, history bool) (*jsonx.Object, error) {
	a, err := readDNSAgent(ctx, registryFor(m), id, minimum, history)
	if err != nil {
		return nil, err
	}
	return legacyAgent(a), nil
}
func versionAtLeast(raw *jsonx.Object, minimum string) bool {
	a, err := typedAgent(agentMembers(raw, "version"))
	return err == nil && a != nil && modelVersionAtLeast(a, minimum)
}

// AgentOrigin is the legacy registered-endpoint codec.
func AgentOrigin(raw *jsonx.Object) (string, error) {
	a, err := typedAgent(agentMembers(raw, "host", "port"))
	if err != nil {
		return "", unreachable()
	}
	return Origin(a)
}

// agentMembers projects only fields consumed by a legacy scalar read. Lazy
// history sentinels and unrelated opaque values must never enter its JSON codec.
func agentMembers(raw *jsonx.Object, names ...string) *jsonx.Object {
	if raw == nil {
		return nil
	}
	out := jsonx.NewObject()
	for _, name := range names {
		if value, present := raw.Get(name); present {
			out.Set(name, value)
		}
	}
	return out
}

var doRequest = request

func request(ctx context.Context, m Manager, raw *jsonx.Object, endpoint string, params query, minimum string) (*jsonx.Object, error) {
	a, err := typedAgent(raw)
	if err != nil {
		return nil, err
	}
	fields, err := monitorRequest(ctx, registryFor(m), a, endpoint, params, minimum)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, NewError("INVALID_RESPONSE", 0, "")
	}
	value, err := jsonx.Parse(encoded)
	if err != nil {
		return nil, NewError("INVALID_RESPONSE", 0, "")
	}
	return value.(*jsonx.Object), nil
}
func latin1(value string) (string, bool) {
	encoded, err := charmap.ISO8859_1.NewEncoder().String(value)
	return encoded, err == nil
}
func text(value any, limit int) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return pySlice(s, limit)
}

func displayUpstream(value any) string { return displayAddress(text(value, 2048)) }

func target(raw *jsonx.Object) (string, error) {
	a, err := typedAgent(raw)
	if err != nil {
		return "", err
	}
	return TargetIdentity(a)
}

type snapshotResult struct {
	data, core, observations *jsonx.Object
	recorders, labels        []any
}

func overviewSnapshot(ctx context.Context, m Manager, raw *jsonx.Object, dashboard bool) (*snapshotResult, error) {
	a, err := typedAgent(raw)
	if err != nil {
		return nil, err
	}
	snapshot, err := readOverviewSnapshot(ctx, registryFor(m), a, dashboard)
	if err != nil {
		return nil, err
	}
	tags := make([]any, len(snapshot.Recorders))
	for i, tag := range snapshot.Recorders {
		tags[i] = tag
	}
	labels := make([]any, len(snapshot.Labels))
	for i, label := range snapshot.Labels {
		labels[i] = jsonx.Obj("plugin_tag", label.PluginTag, "upstream", label.Upstream, "address", label.Address)
	}
	return &snapshotResult{data: metricObject(snapshot.Identity), core: metricObject(snapshot.Core), observations: metricObject(snapshot.Observations), recorders: tags, labels: labels}, nil
}
func rankingsUnavailable(code string, fields ...any) *jsonx.Object {
	r := unavailableRankings(code)
	out := metricObject(r)
	for i := 0; i+1 < len(fields); i += 2 {
		out.Set(fields[i].(string), fields[i+1])
	}
	return out
}
func rankingSection(value any, kind string) (*jsonx.Object, error) {
	return rankingSectionRows(value, kind, 10)
}
func rankingSectionRows(value any, kind string, limit int) (*jsonx.Object, error) {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return nil, err
	}
	section, err := decodeRankingSection(raw, kind, limit)
	if err != nil {
		return nil, err
	}
	return metricObject(section), nil
}
func verifyTarget(ctx context.Context, m Manager, id, expected string) error {
	return verifyNativeTarget(ctx, registryFor(m), id, expected)
}
func compareState(ctx context.Context, m Manager, store *StateStore, id, expected string, replacement *jsonx.Object, verify func(context.Context) error) error {
	var state *DashboardState
	if replacement != nil {
		raw, err := jsonx.Compact(replacement)
		if err != nil {
			return err
		}
		state, err = DecodeState(raw)
		if err != nil {
			return err
		}
	}
	return compareDashboardState(ctx, registryFor(m), store, id, expected, state, verify)
}
func mapStateErrors(err error) error { return nativeStateError(err) }

// ValidRecorderTag validates a legacy recorder value.
func ValidRecorderTag(value any) bool {
	text, ok := value.(string)
	return ok && validRecorderTag(text)
}

// Overview is the legacy response codec.
func Overview(ctx context.Context, m Manager, agentID string, args [][2]string) (*jsonx.Object, error) {
	result, err := ReadOverview(ctx, registryFor(m), agentID, args)
	if err != nil {
		return nil, err
	}
	return metricObject(result), nil
}

// ResetDashboard is the legacy response codec.
func ResetDashboard(ctx context.Context, m Manager, agentID string, body any) (*jsonx.Object, error) {
	raw, err := jsonx.Compact(body)
	if err != nil {
		return nil, invalidQuery()
	}
	result, err := ResetStatistics(ctx, registryFor(m), agentID, raw)
	if err != nil {
		return nil, err
	}
	return metricObject(result), nil
}

// Queries is the legacy response codec.
func Queries(ctx context.Context, m Manager, agentID string, args [][2]string) (*jsonx.Object, error) {
	result, err := ReadQueries(ctx, registryFor(m), agentID, args)
	if err != nil {
		return nil, err
	}
	return metricObject(result), nil
}

// Query is the legacy response codec.
func Query(ctx context.Context, m Manager, agentID, recordID string, args [][2]string) (*jsonx.Object, error) {
	result, err := ReadQuery(ctx, registryFor(m), agentID, recordID, args)
	if err != nil {
		return nil, err
	}
	return metricObject(result), nil
}

// Clients is the legacy response codec.
func Clients(ctx context.Context, m Manager, agentID string, args [][2]string) (*jsonx.Object, error) {
	result, err := ReadClients(ctx, registryFor(m), agentID, args)
	if err != nil {
		return nil, err
	}
	return metricObject(result), nil
}

func record(value any, detail bool) (*jsonx.Object, error) {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return nil, err
	}
	out, err := queryRecord(raw, detail)
	if err != nil {
		return nil, err
	}
	decoded, err := jsonx.Parse(out)
	if err != nil {
		return nil, err
	}
	return decoded.(*jsonx.Object), nil
}
func (m agentsManager) RegistryManager() *agents.Manager { return m.Manager }
