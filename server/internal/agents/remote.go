package agents

// The AgentManager methods that call an Agent's HTTP API directly (legacy
// configuration push, restart, status, logs, uninstall, update, logging
// configuration, domain discovery probes, return routing, DNS HA).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"golang.org/x/text/encoding/charmap"
)

// Call adapts a remaining gateway/deployment caller to CallAgent.
func (m *Manager) Call(ctx context.Context, agent *jsonx.Object, method, path string, header http.Header, body []byte, timeout Timeout) (*Response, error) {
	a, err := agentModel(agent)
	if err != nil {
		return nil, err
	}
	return m.CallAgent(ctx, a, method, path, header, body, timeout)
}

// ErrHeaderEncoding rejects a header outside the Agent's Latin-1 wire contract.
var ErrHeaderEncoding = errors.New(msgAgentHeaderEncoding)

// EncodeHeaderLatin1 uses the native ISO-8859-1 codec. Invalid values are
// rejected without repeating credentials or interpreter exception details.
func EncodeHeaderLatin1(value string) (string, error) {
	encoded, err := charmap.ISO8859_1.NewEncoder().String(value)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrHeaderEncoding, err)
	}
	return encoded, nil
}

// jsonBody is requests' json= encoding (json.dumps, ASCII, ", " separators)
// with its Content-Type. Like requests 2.31 (allow_nan=False), NaN and
// infinities are refused before anything is sent with InvalidJSONError, a
// RequestException: a *TransportError.
func jsonBody(value any) (http.Header, []byte, error) {
	body, err := jsonresp.EncodeDevice(value)
	if err != nil {
		return nil, nil, &TransportError{Err: errors.New(msgInvalidDevicePayload)}
	}
	return http.Header{"Content-Type": {"application/json"}}, body, nil
}

// PushConfig carries the legacy publication options retained for W6.
type PushConfig struct {
	// Extra is extra_data without the bundle (directories,
	// ruleset_downloads, activate, base_url, resource_config,
	// source_snapshot, ...): merged into the legacy JSON payload.
	Extra               *jsonx.Object
	ConfigRevision      any // int or nil
	ExpectedProfileID   *string
	ExpectedServiceType *string
}

// remoteLegacyValue only encodes/decodes the API facade's opaque JSON values.
func remoteLegacyValue(raw json.RawMessage, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(raw)
}
func remoteLegacyObject(raw json.RawMessage, err error) (*jsonx.Object, error) {
	value, err := remoteLegacyValue(raw, err)
	if err != nil {
		return nil, err
	}
	result, ok := value.(*jsonx.Object)
	if !ok {
		return nil, ErrInvalidAgentReply
	}
	return result, nil
}

// PushLegacyConfig adapts legacy publication data to its native owner.
func (m *Manager) PushLegacyConfig(ctx context.Context, id, content string, push PushConfig) (*jsonx.Object, error) {
	var extra model.Fields
	if push.Extra != nil {
		data, err := json.Marshal(push.Extra)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &extra); err != nil {
			return nil, err
		}
	}
	var revision json.RawMessage
	if push.ConfigRevision != nil {
		var err error
		revision, err = json.Marshal(push.ConfigRevision)
		if err != nil {
			return nil, err
		}
	}
	return remoteLegacyObject(m.PushLegacyConfigModel(ctx, id, content, LegacyConfigOptions{Extra: extra, Revision: revision, ExpectedProfileID: push.ExpectedProfileID, ExpectedServiceType: push.ExpectedServiceType}))
}

// RestartAgentService serializes the native operation with the HA guard.
func (m *Manager) RestartAgentService(ctx context.Context, id string) (*jsonx.Object, error) {
	return m.serviceOperation(ctx, id, func() (*jsonx.Object, error) {
		return remoteLegacyObject(m.RunDirectOperation(ctx, id, RestartService, nil, nil))
	}, false)
}
func (m *Manager) serviceOperation(ctx context.Context, id string, operation func() (*jsonx.Object, error), uninstall bool) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if hook := currentHooks().HAServiceOperationTyped; hook != nil {
		raw, err := hook(ctx, m, id, func() (json.RawMessage, error) {
			result, err := operation()
			if err != nil {
				return nil, err
			}
			return json.Marshal(result)
		}, uninstall)
		return remoteLegacyObject(raw, err)
	}
	return defaultServiceOperation(ctx, m, id, operation, uninstall)
}

// quote encodes Agent query values while retaining path separators.
func quote(value string) string {
	plus := url.QueryEscape(value)
	// quote_plus encodes "/" and " "; quote keeps "/" and writes %20.
	out := make([]byte, 0, len(plus))
	for i := 0; i < len(plus); i++ {
		switch {
		case plus[i] == '+':
			out = append(out, "%20"...)
		case plus[i] == '%' && i+2 < len(plus) && plus[i+1] == '2' && (plus[i+2] == 'F' || plus[i+2] == 'f'):
			out = append(out, '/')
			i += 2
		default:
			out = append(out, plus[i])
		}
	}
	return string(out)
}

// RemoteError carries a sanitized user-facing Agent refusal.
type RemoteError struct{ Message string }

func (e *RemoteError) Error() string { return e.Message }

// ReturnRoutingStatus adapts the remaining router caller to the native owner.
func (m *Manager) ReturnRoutingStatus(ctx context.Context, agent *jsonx.Object) (*jsonx.Object, error) {
	a, err := agentModel(agent)
	if err != nil {
		return nil, err
	}
	fields, err := m.ReturnRoutingStatusModel(ctx, a)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(fields)
	return remoteLegacyObject(raw, err)
}

// ReturnRoutingApply adapts the remaining router caller to the native owner.
func (m *Manager) ReturnRoutingApply(ctx context.Context, agent *jsonx.Object, spec any) (*jsonx.Object, error) {
	a, err := agentModel(agent)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	fields, err := m.ReturnRoutingApplyModel(ctx, a, payload)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(fields)
	return remoteLegacyObject(raw, err)
}

// ReturnRoutingRemove adapts the remaining router caller to the native owner.
func (m *Manager) ReturnRoutingRemove(ctx context.Context, agent *jsonx.Object, restore bool) (*jsonx.Object, error) {
	a, err := agentModel(agent)
	if err != nil {
		return nil, err
	}
	fields, err := m.ReturnRoutingRemoveModel(ctx, a, restore)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(fields)
	return remoteLegacyObject(raw, err)
}
