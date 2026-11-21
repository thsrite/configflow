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
	agentsErr error
	system    redact.Secrets
	systemErr error
}

// withRowSecrets attaches the stored Agent record credentials and the system
// secrets to ctx for one task-row projection pass, so row builders neither
// re-read Agents and system settings nor re-encode a record.
func withRowSecrets(ctx context.Context, agentSecrets redact.Secrets, agentsErr error, systemSecrets redact.Secrets, systemErr error) context.Context {
	return context.WithValue(ctx, rowSecretsKey{}, &rowSecrets{
		agents:    agentSecrets,
		agentsErr: agentsErr,
		system:    systemSecrets,
		systemErr: systemErr,
	})
}

// RowAgentSecrets returns the credential values of every stored Agent record,
// deployment and IPv6 verification histories included, as task rows collected
// them before O10 (redact.AgentRecordCredentials, remembered per store head).
// Within a task-row pass it reuses the pass's single read.
func RowAgentSecrets(ctx context.Context) (redact.Secrets, error) {
	if s, ok := ctx.Value(rowSecretsKey{}).(*rowSecrets); ok && s != nil {
		return s.agents, s.agentsErr
	}
	return redact.AgentRecordCredentials(ctx, nil)
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
