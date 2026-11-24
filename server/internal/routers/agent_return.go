package routers

import (
	"configflow/internal/routers/device"
	"context"
	"encoding/json"

	"configflow/internal/agents"
	"configflow/internal/agents/diag"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
)

// GatewayAgent carries a native Agent view and its opaque A01 transport handle.
type GatewayAgent struct {
	*model.Agent
	Record *model.Agent     `json:"-"`
	DNS    *device.DNSAgent `json:"-"`
}

// AgentSource supplies Agent records and their owned return-routing client.
type AgentSource interface {
	Agent(context.Context, string) (*GatewayAgent, error)
	ReturnAgent(*GatewayAgent) ReturnAgent
}

// ManagerAgentSource adapts A01 without exposing legacy JSON to domain logic.
type ManagerAgentSource struct{ Manager *agents.Manager }

// Agent reads the registry/liveness view once and decodes its native fields.
// The view is a shallow copy of the borrowed record: read only below the top
// level, like the record itself.
func (s ManagerAgentSource) Agent(ctx context.Context, id string) (*GatewayAgent, error) {
	if id == "" {
		return nil, nil
	}
	record, err := s.Manager.ReadAgent(ctx, id, false)
	if err != nil || record == nil {
		return nil, err
	}
	dns, err := dnsAgentOf(record)
	if err != nil {
		return nil, err
	}
	view := *record
	return &GatewayAgent{&view, record, dns}, nil
}

// dnsAgentMembers are the members DNSAgent decodes.
var dnsAgentMembers = []string{"id", "host", "service_type", "status", "oxidns_ha", "oxidns_ha_state", "oxidns_ha_publication"}

// dnsAgentOf decodes the DNS projection from only the members it reads, with
// the same strict decoder (and errors) as decoding the encoded record.
func dnsAgentOf(record *model.Agent) (*device.DNSAgent, error) {
	var members model.Fields
	for _, name := range dnsAgentMembers {
		raw, present, err := model.EncodeMember(record, name)
		if err != nil {
			return nil, err
		}
		if present {
			if err = members.Set(name, raw); err != nil {
				return nil, err
			}
		}
	}
	data, err := members.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var dns device.DNSAgent
	if err = jsondecode.Unmarshal(data, &dns); err != nil {
		return nil, err
	}
	return &dns, nil
}

// ReturnAgent constructs the A01 client for the same inspected Agent record.
func (s ManagerAgentSource) ReturnAgent(a *GatewayAgent) ReturnAgent {
	return NewReturnAgent(s.Manager, a)
}

// NewReturnAgent binds an Agent manager to one inspected gateway.
func NewReturnAgent(m *agents.Manager, a *GatewayAgent) ReturnAgent { return &managerReturnAgent{m, a} }

type managerReturnAgent struct {
	manager *agents.Manager
	agent   *GatewayAgent
}

func (a *managerReturnAgent) Status(ctx context.Context) (*ReturnStatus, error) {
	if a.agent == nil || a.agent.Record == nil {
		return nil, device.Error(messageNoReturnAgent)
	}
	result, err := a.manager.ReturnRoutingStatusModel(ctx, a.agent.Record)
	if err != nil {
		return nil, a.safeError(err)
	}
	b, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var s ReturnStatus
	if err = json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
func (a *managerReturnAgent) Apply(ctx context.Context, spec json.RawMessage) error {
	if a.agent == nil || a.agent.Record == nil {
		return device.Error(messageNoReturnAgent)
	}
	_, err := a.manager.ReturnRoutingApplyModel(ctx, a.agent.Record, spec)
	return a.safeError(err)
}
func (a *managerReturnAgent) Remove(ctx context.Context, restore bool) error {
	if a.agent == nil || a.agent.Record == nil {
		return device.Error(messageNoReturnAgent)
	}
	_, err := a.manager.ReturnRoutingRemoveModel(ctx, a.agent.Record, restore)
	return a.safeError(err)
}

type returnAgentError struct {
	cause   error
	message string
}

func (e *returnAgentError) Error() string { return e.message }
func (e *returnAgentError) Unwrap() error { return e.cause }
func (a *managerReturnAgent) safeError(err error) error {
	if err == nil {
		return nil
	}
	secrets := []any{}
	if a.agent != nil && a.agent.Agent != nil {
		secrets = append(secrets, a.agent.Token)
	}
	return &returnAgentError{err, diag.FailureText(err.Error(), secrets, 0)}
}
