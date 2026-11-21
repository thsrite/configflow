package agents

import (
	"bytes"
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
	public := make(map[string]json.RawMessage, len(agentMemberNames)+record.Fields.Len())
	for _, name := range agentMemberNames {
		if privateAgentMembers[name] {
			continue
		}
		var err error
		switch {
		case name == "deployments" && record.Deployments.Len() > 0:
			public[name], err = publicDeploymentsModel(record, nil)
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
			raw, present, memberErr := record.MemberJSON(name)
			if memberErr != nil || !present {
				err = memberErr
				break
			}
			// An empty, null or wrongly typed history: the encoded rules.
			if name == "deployments" && len(raw) > 0 && raw[0] == '{' {
				raw, err = publicDeploymentsModel(record, raw)
			}
			public[name] = raw
		}
		if err != nil {
			return nil, err
		}
	}
	for name, raw := range record.Fields.All() {
		if !privateAgentMembers[name] {
			public[name] = raw
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
// masks: those of every stored Agent record of the current generation
// (remembered per store head by the repository) and those in the members a
// read lays over the stored record (heartbeat fields, liveness).
func liveAgentSecrets(ctx context.Context, record *model.Agent) (redact.Secrets, error) {
	stored, err := redact.AgentRecordCredentials(ctx, nil)
	if err != nil {
		return nil, err
	}
	live := make(map[string]json.RawMessage, len(liveMembers))
	for _, name := range liveMembers {
		raw, present, err := record.MemberJSON(name)
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
	for _, key := range []string{"prepared_content", "oxidns_validation_receipt"} {
		if err := out.Clear(key); err != nil {
			return nil, err
		}
	}
	localized, err := agents.PublicDeploymentMessagesModel(&out)
	if err != nil {
		return nil, err
	}
	return model.Marshal(localized)
}

// publicDeploymentsModel projects the deployment history (raw is its encoded
// object, or nil to encode it only when needed) from the typed entries, instead of decoding every encoded entry
// again. Entries that are not valid tasks keep the encoded-entry rules: a
// non-object becomes null, an object is decoded as before. A history with a
// repeated id uses the encoded object, whose decoding keeps the last value.
func publicDeploymentsModel(record *model.Agent, raw json.RawMessage) (json.RawMessage, error) {
	history := record.Deployments
	keys := history.Keys()
	seen := make(map[string]bool, len(keys))
	for _, id := range keys {
		if seen[id] {
			if raw == nil {
				var err error
				if raw, _, err = record.MemberJSON("deployments"); err != nil {
					return nil, err
				}
			}
			return publicEncodedDeploymentsModel(raw)
		}
		seen[id] = true
	}
	deployments := make(map[string]json.RawMessage, len(keys))
	for id, task := range history.All() {
		if !history.EntryValid(id) {
			entry, _, err := history.MemberJSON(id)
			if err != nil {
				return nil, err
			}
			deployments[id], err = publicEncodedDeploymentModel(entry)
			if err != nil {
				return nil, err
			}
			continue
		}
		var err error
		deployments[id], err = publicDeploymentModel(&task)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(deployments)
}

func publicEncodedDeploymentsModel(raw json.RawMessage) (json.RawMessage, error) {
	var deployments map[string]json.RawMessage
	if err := jsondecode.Unmarshal(raw, &deployments); err != nil {
		return nil, err
	}
	for id, entry := range deployments {
		var err error
		deployments[id], err = publicEncodedDeploymentModel(entry)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(deployments)
}

func publicEncodedDeploymentModel(raw json.RawMessage) (json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return json.RawMessage("null"), nil
	}
	var task model.Deployment
	if err := jsondecode.Unmarshal(raw, &task); err != nil {
		return nil, err
	}
	return publicDeploymentModel(&task)
}
