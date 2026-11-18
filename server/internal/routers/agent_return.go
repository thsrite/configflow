package routers

import (
	"context"
	"encoding/json"

	"configflow/internal/agents"
	"configflow/internal/agents/diag"
	"configflow/internal/model"
)

// GatewayAgent carries a native Agent view and its opaque A01 transport handle.
type GatewayAgent struct {
	*model.Agent
	Record *agents.Record `json:"-"`
	DNS    *DNSAgent      `json:"-"`
}

// AgentSource supplies Agent records and their owned return-routing client.
type AgentSource interface {
	Agent(context.Context, string) (*GatewayAgent, error)
	ReturnAgent(*GatewayAgent) ReturnAgent
}

// ManagerAgentSource adapts A01 without exposing legacy JSON to domain logic.
type ManagerAgentSource struct{ Manager *agents.Manager }

// Agent reads the registry/liveness view once and decodes its native fields.
func (s ManagerAgentSource) Agent(ctx context.Context, id string) (*GatewayAgent, error) {
	if id == "" {
		return nil, nil
	}
	record, err := s.Manager.GetAgentByID(ctx, id, false)
	if err != nil || record == nil {
		return nil, err
	}
	b, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	var view model.Agent
	var dns DNSAgent
	if err = json.Unmarshal(b, &view); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &dns); err != nil {
		return nil, err
	}
	return &GatewayAgent{&view, record, &dns}, nil
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
		return nil, routerError(messageNoReturnAgent)
	}
	result, err := a.manager.ReturnRoutingStatus(ctx, a.agent.Record)
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
		return routerError(messageNoReturnAgent)
	}
	var payload agents.Record
	if err := json.Unmarshal(spec, &payload); err != nil {
		return err
	}
	_, err := a.manager.ReturnRoutingApply(ctx, a.agent.Record, &payload)
	return a.safeError(err)
}
func (a *managerReturnAgent) Remove(ctx context.Context, restore bool) error {
	if a.agent == nil || a.agent.Record == nil {
		return routerError(messageNoReturnAgent)
	}
	_, err := a.manager.ReturnRoutingRemove(ctx, a.agent.Record, restore)
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
