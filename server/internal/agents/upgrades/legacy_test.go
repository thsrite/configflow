package upgrades

// Frozen Python-shape fixtures enter only through this test codec. State-machine
// decisions and all writes still run through the production native owners.
import (
	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"context"
	"encoding/json"
	"errors"
)

var requestHook func(*jsonx.Object, string, string, *jsonx.Object) (*agents.Response, error)

func legacyAgent(a *jsonx.Object) *model.Agent {
	if a == nil {
		return nil
	}
	raw, _ := jsonx.Compact(a)
	var out model.Agent
	_ = jsondecode.Unmarshal(raw, &out)
	return &out
}
func legacyObject(value any) *jsonx.Object {
	if value == nil {
		return nil
	}
	raw, _ := model.Marshal(value)
	parsed, _ := jsonx.Parse(raw)
	out, _ := parsed.(*jsonx.Object)
	return out
}
func syncHook() {
	requestOverride = nil
	if requestHook != nil {
		requestOverride = func(_ context.Context, a *model.Agent, method, path string, body []byte) (*agents.Response, error) {
			var payload *jsonx.Object
			if len(body) > 0 {
				v, _ := jsonx.Parse(body)
				payload, _ = v.(*jsonx.Object)
			}
			return requestHook(legacyObject(a), method, path, payload)
		}
	}
}
func Start(ctx context.Context, m *agents.Manager, id string) (*jsonx.Object, error) {
	syncHook()
	state, err := StartModel(ctx, m, id)
	return legacyObject(state), err
}
func Poll(ctx context.Context, m *agents.Manager, id string) (*jsonx.Object, error) {
	syncHook()
	state, err := PollModel(ctx, m, id)
	return legacyObject(state), err
}
func ReportLegacy(ctx context.Context, m *agents.Manager, id string, payload any) (*jsonx.Object, error) {
	raw, err := jsonx.Compact(payload)
	if err != nil {
		return nil, err
	}
	state, err := ReportLegacyJSON(ctx, m, id, raw)
	return legacyObject(state), err
}
func Artifacts() (*jsonx.Object, error) { out, err := ArtifactsModel(); return legacyObject(out), err }
func Follow(ctx context.Context, m *agents.Manager, a *jsonx.Object, host string) error {
	return FollowModel(ctx, m, legacyAgent(a), host)
}
func reaches(ctx context.Context, m *agents.Manager, a *jsonx.Object, host string) (bool, error) {
	return reachesModel(ctx, m, legacyAgent(a), host)
}
func ask(ctx context.Context, m *agents.Manager, a *jsonx.Object, host, method string, body *jsonx.Object) (int, *jsonx.Object, error) {
	var raw []byte
	if body != nil {
		raw, _ = jsonx.Default(body)
	}
	status, fields, err := askModel(ctx, m, legacyAgent(a), host, method, raw)
	return status, legacyObject(fields), err
}
func request(ctx context.Context, m *agents.Manager, a *jsonx.Object, method, path string, payload *jsonx.Object) (*agents.Response, error) {
	syncHook()
	var raw json.RawMessage
	if payload != nil {
		raw, _ = jsonx.Compact(payload)
	}
	if len(raw) == 0 {
		return requestModel(ctx, m, legacyAgent(a), method, path, nil)
	}
	return requestModel(ctx, m, legacyAgent(a), method, path, raw)
}
func save(ctx context.Context, m *agents.Manager, id string, updateID any, patch *jsonx.Object, queued bool) (*jsonx.Object, error) {
	raw, _ := jsonx.Compact(patch)
	var fields model.Fields
	_ = jsondecode.Unmarshal(raw, &fields)
	p := upgradePatch{Status: patch.GetString("status")}
	if patch.Has("error") {
		p.Error = new(rawText(mustField(fields, "error")))
	}
	if patch.Has("message") {
		p.Message = new(rawText(mustField(fields, "message")))
	}
	p.AgentUpdatedAt = new(patch.GetString("agent_updated_at"))
	if events, ok := fields.Get("events"); ok {
		_ = jsondecode.Unmarshal(events, &p.Events)
	}
	text, _ := updateID.(string)
	state, err := saveModel(ctx, m, id, text, p, queued)
	return legacyObject(state), err
}
func mustField(f model.Fields, key string) json.RawMessage { raw, _ := f.Get(key); return raw }
func ErrorClass(err error) string {
	var corrupt *CorruptStateError
	if errors.As(err, &corrupt) {
		return "CorruptStateError"
	}
	if errors.Is(err, agents.ErrHeaderEncoding) {
		return "UnicodeEncodeError"
	}
	if errors.Is(err, agents.ErrAgentEndpoint) {
		return "EndpointError"
	}
	if errors.Is(err, ErrInvalidReport) {
		return "ValueError"
	}
	if IsValueError(err) {
		return "ValueError"
	}
	return ""
}
func AgentResponse(payload any, status int) *agents.Response {
	body, _ := jsonx.Default(payload)
	return &agents.Response{StatusCode: status, Header: map[string][]string{"Content-Type": {"application/json"}}, Body: body}
}
func setLegacyArtifactsForTest(fn func() (*jsonx.Object, error)) func() {
	return SetArtifactsForTest(func() (model.Map[model.UpgradeArtifact], error) {
		v, err := fn()
		if err != nil {
			return model.Map[model.UpgradeArtifact]{}, err
		}
		raw, _ := jsonx.Compact(v)
		var out model.Map[model.UpgradeArtifact]
		err = jsondecode.Unmarshal(raw, &out)
		return out, err
	})
}
