package ipv6

import (
	"configflow/internal/jsonresp"
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/agents/diag"
	"configflow/internal/redact"
	"configflow/internal/yamlx"
)

func (s *Service) credentials(ctx context.Context, config Document) (redact.Secrets, error) {
	system, e := s.systemSecrets(ctx)
	if e != nil {
		return nil, e
	}
	seeds := redact.CollectCredentials(jsonresp.Value(config), system)
	all, e := s.all(ctx)
	if e != nil {
		return nil, e
	}
	seeds = redact.CollectCredentials(jsonresp.Value(all), seeds)
	_, doc := ReadConfiguration(config)
	if doc != nil {
		if b, e := yamlx.ToJSON(doc); e == nil {
			seeds = redact.CollectCredentials(json.RawMessage(b), seeds)
		}
	}
	return seeds, nil
}

var diagnosticFields = map[string]bool{"label": true, "message": true, "error": true, "reason": true, "details": true, "command": true, "conflicts": true, "evidence": true, "source_note": true}

func diagnosticProjection(b json.RawMessage, seeds redact.Secrets) json.RawMessage {
	if obj := object(b); obj != nil {
		for key, value := range obj {
			if diagnosticFields[key] {
				obj[key] = redact.SanitizeCredentials(value, seeds)
			} else if key != "check_token" && diag.IsCredentialKey(key) {
				obj[key] = jsonresp.Value(redact.Redacted)
			} else {
				obj[key] = diagnosticProjection(value, seeds)
			}
		}
		return jsonresp.Value(obj)
	}
	var list []json.RawMessage
	if json.Unmarshal(b, &list) == nil && list != nil {
		for i, v := range list {
			list[i] = diagnosticProjection(v, seeds)
		}
		return jsonresp.Value(list)
	}
	return b
}

// Public applies system secrets across the entire response and user credentials
// only to diagnostics and credential fields, preserving protocol values and IDs.
// Seeds include every peer before projection, preventing cross-device echoes.
func (s *Service) Public(ctx context.Context, profile string, value any) (json.RawMessage, error) {
	config, _, e := s.snapshot(ctx, profile)
	if e != nil {
		return nil, e
	}
	seeds, e := s.credentials(ctx, config)
	if e != nil {
		return nil, e
	}
	system, e := s.systemSecrets(ctx)
	if e != nil {
		return nil, e
	}
	projected := diagnosticProjection(jsonresp.Value(value), seeds)
	return json.Marshal(redact.SanitizeWithSecrets(projected, system, false))
}

// capturedPublic retains credential seeds from the observation's immutable
// source even when a concurrent edit removes them from the live repository.
func (s *Service) capturedPublic(ctx context.Context, config, a Document, value any) (json.RawMessage, error) {
	seeds, e := s.credentials(ctx, config)
	if e != nil {
		return nil, e
	}
	seeds = redact.CollectCredentials(jsonresp.Value(a), seeds)
	system, e := s.systemSecrets(ctx)
	if e != nil {
		return nil, e
	}
	system = redact.NewSecrets(system, []string{text(a["token"])})
	for _, peer := range documents(config["agents"]) {
		system = redact.NewSecrets(system, []string{text(peer["token"])})
	}
	projected := diagnosticProjection(jsonresp.Value(value), seeds)
	return json.Marshal(redact.SanitizeWithSecrets(projected, system, false))
}

func (s *Service) capturedError(ctx context.Context, config, a Document, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var boundary *Error
	if !errors.As(err, &boundary) {
		return err
	}
	b, e := s.capturedPublic(ctx, config, a, Document{"message": jsonresp.Value(boundary.Message)})
	if e != nil {
		return e
	}
	copy := *boundary
	copy.Message = text(object(b)["message"])
	return &copy
}
