package agents

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/redact"
)

// publicAgentModel projects one record through the redaction boundary, with
// the credentials collected from the record itself (histories included).
func publicAgentModel(record *model.Agent) (map[string]json.RawMessage, error) {
	raw, err := model.Marshal(record)
	if err != nil {
		return nil, err
	}
	return publicAgentView(record, redact.CollectCredentials(json.RawMessage(raw), nil))
}

// agentMemberNames are the declared members of a stored Agent record.
var agentMemberNames = func() []string {
	var names []string
	typ := reflect.TypeFor[model.Agent]()
	for i := range typ.NumField() {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			names = append(names, name)
		}
	}
	return names
}()

// privateAgentMembers never leave the server.
var privateAgentMembers = map[string]bool{"token": true, "applied_content": true, "dns_follow_job": true}

// publicAgentView is the public projection of record, built member by member:
// private members are dropped, task histories and states are projected from
// their typed values, every other member is copied as encoded. secrets must
// hold every credential found in record (callers pass the registry's
// per-generation credentials plus the live members' own). SanitizeCredentials
// then masks credential fields, credential-bearing text and those secrets.
func publicAgentView(record *model.Agent, secrets redact.Secrets) (map[string]json.RawMessage, error) {
	public := make(map[string]json.RawMessage, len(agentMemberNames))
	for _, name := range agentMemberNames {
		if privateAgentMembers[name] {
			continue
		}
		var err error
		switch {
		case name == "deployments" && record.Deployments.Len() > 0:
			public[name], err = publicDeploymentsModel(record)
		case name == "latest_deployment" && record.LatestDeployment != nil:
			public[name], err = publicDeploymentModel(record.LatestDeployment)
		case name == "latest_upgrade" && record.LatestUpgrade != nil:
			var state *model.AgentUpgrade
			if state, err = agents.PublicUpgradeStateModel(record.LatestUpgrade); err == nil {
				public[name], err = model.Marshal(state)
			}
		case name == "oxidns_ha_publication" && record.OxiDNSHAPublication != nil && record.OxiDNSHAPublication.HasMembers():
			var summary *model.HAPublicationSummary
			if summary, err = agents.HASummaryModel(record.OxiDNSHAPublication); err == nil {
				public[name], err = model.Marshal(summary)
			}
		default:
			raw, present, memberErr := model.EncodeMember(record, name)
			if memberErr != nil || !present {
				err = memberErr
				break
			}
			public[name] = raw
		}
		if err != nil {
			return nil, err
		}
	}
	payload := make(map[string]any, len(public))
	for name, raw := range public {
		payload[name] = raw
	}
	// SanitizeCredentials owns all secret discovery and text redaction. The HTTP
	// writer may perform its independent stored-secret pass afterwards.
	sanitized := redact.SanitizeCredentials(payload, secrets)
	var result map[string]json.RawMessage
	if err := jsondecode.Unmarshal(sanitized, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// liveAgentSecrets are the credentials a public view of the live record
// masks: those of every stored Agent record of the generation the record was
// read from (collected from that generation's own records, remembered per
// store head by the repository) and those in the members a read lays over
// the stored record (heartbeat fields, liveness).
// The stored credentials are read once per request (cache may be nil).
func liveAgentSecrets(ctx context.Context, m *agents.Manager, cache *contentSources, record *model.Agent) (redact.Secrets, error) {
	var stored redact.Secrets
	var err error
	if cache != nil && cache.storedRead {
		stored, err = cache.stored, cache.storedErr
	} else {
		var gen agents.Generation
		if cache != nil {
			gen = cache.gen
		}
		stored, err = m.GenerationCredentials(ctx, gen)
		if cache != nil {
			cache.stored, cache.storedErr, cache.storedRead = stored, err, true
		}
	}
	if err != nil {
		return nil, err
	}
	live := make(map[string]json.RawMessage, len(liveMembers))
	for _, name := range liveMembers {
		raw, present, err := model.EncodeMember(record, name)
		if err != nil {
			return nil, err
		}
		if present {
			live[name] = raw
		}
	}
	data, err := json.Marshal(live)
	if err != nil {
		return nil, err
	}
	return redact.CollectCredentials(json.RawMessage(data), stored), nil
}

func publicDeploymentModel(record *model.Deployment) (json.RawMessage, error) {
	if record == nil {
		return json.RawMessage("null"), nil
	}
	out := *record
	if out.HAPublication != nil && out.HAPublication.HasMembers() {
		aggregated, err := agents.HAAggregateView(&out, out.HAPublication)
		if err != nil {
			return nil, err
		}
		out = *aggregated
	}
	out.PreparedContent, out.OxiDNSValidationReceipt = nil, nil
	localized, err := agents.PublicDeploymentMessagesModel(&out)
	if err != nil {
		return nil, err
	}
	return model.Marshal(localized)
}

// publicDeploymentsModel projects the deployment history from its typed entries.
func publicDeploymentsModel(record *model.Agent) (json.RawMessage, error) {
	deployments := make(map[string]json.RawMessage, record.Deployments.Len())
	for id, task := range record.Deployments.All() {
		var err error
		deployments[id], err = publicDeploymentModel(&task)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(deployments)
}
