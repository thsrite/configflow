package ipv6

import (
	"context"
	"encoding/json"

	"configflow/internal/agents/diag"
	"configflow/internal/redact"
	"configflow/internal/yamlx"
)

func (s *Service) credentials(ctx context.Context, config Document) (redact.Secrets, error) {
	system, e := s.systemSecrets(ctx)
	if e != nil {
		return nil, e
	}
	seeds := redact.CollectCredentials(raw(config), system)
	all, e := s.all(ctx)
	if e != nil {
		return nil, e
	}
	seeds = redact.CollectCredentials(raw(all), seeds)
	_, doc := ReadConfiguration(config)
	if doc != nil {
		if b, e := yamlx.ToJSON(doc); e == nil {
			seeds = redact.CollectCredentials(json.RawMessage(b), seeds)
		}
	}
	return seeds, nil
}

var diagnosticFields = map[string]bool{"checks": true, "message": true, "error": true, "reason": true, "details": true, "command": true, "conflicts": true, "evidence": true, "refresh_error": true, "source_note": true}

func diagnosticProjection(b json.RawMessage, seeds redact.Secrets) json.RawMessage {
	if obj := object(b); obj != nil {
		for key, value := range obj {
			if diagnosticFields[key] {
				obj[key] = redact.SanitizeCredentials(value, seeds)
			} else if key != "check_token" && diag.IsCredentialKey(key) {
				obj[key] = raw(redact.Redacted)
			} else {
				obj[key] = diagnosticProjection(value, seeds)
			}
		}
		return raw(obj)
	}
	var list []json.RawMessage
	if json.Unmarshal(b, &list) == nil && list != nil {
		for i, v := range list {
			list[i] = diagnosticProjection(v, seeds)
		}
		return raw(list)
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
	projected := diagnosticProjection(raw(value), seeds)
	return json.Marshal(redact.SanitizeWithSecrets(projected, system, false))
}
