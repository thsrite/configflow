package monitor

import (
	"configflow/internal/version"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/model"

	"golang.org/x/text/encoding/charmap"
)

// Registry supplies owned typed Agent records and the monitoring HTTP transport.
type Registry interface {
	WithReadSnapshot(context.Context, func(context.Context) error) error
	ReadAgent(context.Context, string, bool) (*model.Agent, error)
	DataDir() string
	Client() agents.Client
}

// TypedFromAgents adapts the registry without a dynamic JSON round trip.
func TypedFromAgents(m *agents.Manager) Registry { return agentsManager{m} }

func originalString(raw json.RawMessage, present bool, err error) (string, bool) {
	if err != nil || !present {
		return "", false
	}
	var value *string
	if jsondecode.Unmarshal(raw, &value) != nil || value == nil {
		return "", false
	}
	return *value, true
}
func originalText(raw json.RawMessage, present bool, err error) string {
	value, _ := originalString(raw, present, err)
	return value
}

func modelVersionAtLeast(agent *model.Agent, minimum string) bool {
	text, valid := agent.Version, true
	if !valid {
		return false
	}
	for _, r := range text {
		if r > 127 {
			return false
		}
	}
	return version.CompareAgent(text, minimum) >= 0
}

// Origin validates the original registered endpoint. R5 string projections and
// case aliases cannot become an endpoint credential or service identity.
func Origin(agent *model.Agent) (string, error) {
	if agent == nil {
		return "", unreachable()
	}
	host := agent.Host
	if host == "" || strings.ContainsAny(host, "/?#@\\") {
		return "", unreachable()
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if canonical, v6, valid := parseIP(host); valid {
		host = canonical
		if v6 {
			host = "[" + host + "]"
		}
	} else if !hostnameRE.MatchString(host) {
		return "", unreachable()
	}
	port := int64(agent.Port)
	if port < 1 || port > 65535 {
		return "", unreachable()
	}
	return "http://" + host + ":" + strconv.FormatInt(port, 10), nil
}

func monitorHeaders(token string) (http.Header, error) {
	encoded, err := charmap.ISO8859_1.NewEncoder().String(token)
	if err != nil || strings.ContainsAny(token, "\r\n") {
		return nil, agents.ErrHeaderEncoding
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+encoded)
	header.Set("Accept", "application/json")
	return header, nil
}

func fieldText(fields model.Fields, key string) string {
	raw, present := fields.Get(key)
	return originalText(raw, present, nil)
}
func answerBool(fields model.Fields, key string) (bool, bool, bool) {
	raw, present := fields.Get(key)
	if !present {
		return false, false, true
	}
	var flag *bool
	if jsondecode.Unmarshal(raw, &flag) != nil || flag == nil {
		return false, true, false
	}
	return *flag, true, true
}

func protocolAnswer(response *agents.Response, limit int, fail func(string, int) *Error) (model.Fields, error) {
	var fields model.Fields
	if response == nil {
		return fields, fail("INVALID_RESPONSE", 0)
	}
	status := response.StatusCode
	switch {
	case status == 401 || status == 403:
		return fields, fail("AGENT_AUTH_FAILED", 0)
	case status >= 300 && status < 400:
		return fields, fail("INVALID_RESPONSE", 0)
	case len(response.Body) > limit:
		return fields, fail("INVALID_RESPONSE", 0)
	case response.BodyError != nil:
		return fields, fail("AGENT_UNREACHABLE", 0)
	}
	text, valid := agents.DecodeJSONBytes(response.Body)
	normalized, _ := jsonresp.NormalizeNonFinite([]byte(text))
	if !valid || jsondecode.Unmarshal(normalized, &fields) != nil {
		if status == 404 && (!valid || !json.Valid([]byte(text))) {
			return fields, fail("UPGRADE_REQUIRED", 409)
		}
		return fields, fail("INVALID_RESPONSE", 0)
	}
	return fields, nil
}

type agentsManager struct{ *agents.Manager }

func (m agentsManager) WithReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	return m.Repository().WithReadSnapshot(ctx, fn)
}

func (m agentsManager) DataDir() string { return m.Repository().DataDir() }

func (m agentsManager) RegistryManager() *agents.Manager { return m.Manager }
