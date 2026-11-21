package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"os"

	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

// CollectAgentCredentials snapshots credential values from one Agent record
// without its deployment and IPv6 verification histories. Only those two
// history members are dropped (on a local copy); every other declared or
// stored member is collected exactly as redact.CollectCredentials would on the
// full record, so a snapshot pays for the small record, not its history.
func CollectAgentCredentials(record *model.Agent, seeds redact.Secrets) redact.Secrets {
	secrets := redact.NewSecrets(seeds, []string{os.Getenv("AGENT_REGISTRATION_KEY")})
	return collectAgentInto(record, secrets)
}

// CollectAllAgentCredentials is CollectAgentCredentials over every record.
func CollectAllAgentCredentials(records []model.Agent, seeds redact.Secrets) redact.Secrets {
	secrets := redact.NewSecrets(seeds, []string{os.Getenv("AGENT_REGISTRATION_KEY")})
	for i := range records {
		secrets = collectAgentInto(&records[i], secrets)
	}
	return secrets
}

func collectAgentInto(record *model.Agent, secrets redact.Secrets) redact.Secrets {
	if record == nil {
		return secrets
	}
	// Raw/Fields are copy-on-write: Clear changes this local value only.
	selected := *record
	if selected.Clear("deployments") != nil || selected.Clear("ipv6_verifications") != nil {
		return redact.NewSecrets(secrets, []string{record.Token})
	}
	raw, err := model.Marshal(selected)
	if err != nil {
		return redact.NewSecrets(secrets, []string{record.Token})
	}
	return redact.CollectCredentials(json.RawMessage(raw), secrets)
}

// SystemSecrets computes external payload secrets from the repository's
// sensitive system_config string members without copying or marshaling the
// System or SystemConfig record.
func SystemSecrets(ctx context.Context, r *repo.Repository) (redact.Secrets, error) {
	fields := map[string]any{}
	if err := r.View(ctx, func(document *model.Document) error {
		for field := range redact.SensitiveSystemConfigFields {
			raw, present, err := document.System.SystemConfig.MemberJSON(field)
			if err != nil {
				return err
			}
			trimmed := bytes.TrimSpace(raw)
			if !present || len(trimmed) == 0 || trimmed[0] != '"' {
				continue
			}
			var text string
			if json.Unmarshal(trimmed, &text) == nil && text != "" {
				fields[field] = text
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return redact.ExternalPayloadSecrets(ctx, fields)
}

type rowSecretsKey struct{}

type rowSecrets struct {
	agents    redact.Secrets
	perAgent  map[*model.Agent]redact.Secrets
	system    redact.Secrets
	systemErr error
}

// withRowSecrets attaches the credentials of records (each collected once)
// and the system secrets to ctx for one task-row projection pass, so row
// builders neither re-read Agents and system settings nor re-encode a record.
// Credential discovery does not depend on its seeds, so the union of the
// per-record sets equals one collection seeded with the others.
func withRowSecrets(ctx context.Context, records []model.Agent, systemSecrets redact.Secrets, systemErr error) context.Context {
	perAgent := make(map[*model.Agent]redact.Secrets, len(records))
	lists := make([][]string, 0, len(records))
	for i := range records {
		secrets := CollectAgentCredentials(&records[i], nil)
		perAgent[&records[i]] = secrets
		lists = append(lists, secrets)
	}
	return context.WithValue(ctx, rowSecretsKey{}, &rowSecrets{
		agents:    redact.NewSecrets(lists...),
		perAgent:  perAgent,
		system:    systemSecrets,
		systemErr: systemErr,
	})
}

// RowAgentCredentials returns record's credentials joined with seeds. Within
// a task-row pass it reuses the pass's collection for that record; otherwise
// it collects them (CollectAgentCredentials).
func RowAgentCredentials(ctx context.Context, record *model.Agent, seeds redact.Secrets) redact.Secrets {
	if s, ok := ctx.Value(rowSecretsKey{}).(*rowSecrets); ok && s != nil {
		if own, ok := s.perAgent[record]; ok {
			return redact.NewSecrets(seeds, own)
		}
	}
	return CollectAgentCredentials(record, seeds)
}

// RowAgentSecrets returns the pre-collected credentials of all Agents for the
// current task-row pass, when present.
func RowAgentSecrets(ctx context.Context) (redact.Secrets, bool) {
	s, ok := ctx.Value(rowSecretsKey{}).(*rowSecrets)
	if !ok || s == nil {
		return nil, false
	}
	return s.agents, true
}

// RowSystemSecrets returns the pre-collected system secrets for the current
// task-row pass, when present.
func RowSystemSecrets(ctx context.Context) (redact.Secrets, error, bool) {
	s, ok := ctx.Value(rowSecretsKey{}).(*rowSecrets)
	if !ok || s == nil {
		return nil, nil, false
	}
	return s.system, s.systemErr, true
}
