package agents

import (
	"bytes"
	"encoding/json"

	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/redact"
)

// publicAgentModel projects caller-owned native records through one redaction
// boundary. Credentials are captured before private task trees are removed.
func publicAgentModel(record *model.Agent) (map[string]json.RawMessage, error) {
	return publicAgentWithCredentialsModel(record, nil)
}

func publicAgentWithCredentialsModel(record *model.Agent, secrets redact.Secrets) (map[string]json.RawMessage, error) {
	raw, err := model.Marshal(record)
	if err != nil {
		return nil, err
	}
	secrets = redact.CollectCredentials(json.RawMessage(raw), secrets)
	var public map[string]json.RawMessage
	if err = jsondecode.Unmarshal(raw, &public); err != nil {
		return nil, err
	}
	for _, key := range []string{"token", "applied_content", "dns_follow_job"} {
		delete(public, key)
	}
	if record.OxiDNSHAPublication != nil && record.OxiDNSHAPublication.HasMembers() {
		summary, err := agents.HASummaryModel(record.OxiDNSHAPublication)
		if err != nil {
			return nil, err
		}
		public["oxidns_ha_publication"], err = model.Marshal(summary)
		if err != nil {
			return nil, err
		}
	}
	if raw, present := public["deployments"]; present && len(raw) > 0 && raw[0] == '{' {
		public["deployments"], err = publicDeploymentsModel(record, raw)
		if err != nil {
			return nil, err
		}
	}
	if record.LatestDeployment != nil {
		public["latest_deployment"], err = publicDeploymentModel(record.LatestDeployment)
		if err != nil {
			return nil, err
		}
	}
	if record.LatestUpgrade != nil {
		state, err := agents.PublicUpgradeStateModel(record.LatestUpgrade)
		if err != nil {
			return nil, err
		}
		public["latest_upgrade"], err = model.Marshal(state)
		if err != nil {
			return nil, err
		}
	}
	// SanitizeCredentials owns all secret discovery and text redaction. The HTTP
	// writer may perform its independent stored-secret pass afterwards.
	publicRaw, err := json.Marshal(public)
	if err != nil {
		return nil, err
	}
	sanitized := redact.SanitizeCredentials(json.RawMessage(publicRaw), secrets)
	if err = jsondecode.Unmarshal(sanitized, &public); err != nil {
		return nil, err
	}
	return public, nil
}

func publicDeploymentModel(record *model.Deployment) (json.RawMessage, error) {
	if record == nil {
		return json.RawMessage("null"), nil
	}
	out := *record
	if out.HAPublication != nil && out.HAPublication.HasMembers() {
		aggregated, err := agents.HAAggregateModel(&out, out.HAPublication)
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
// object) from the typed entries, instead of decoding every encoded entry
// again. Entries that are not valid tasks keep the encoded-entry rules: a
// non-object becomes null, an object is decoded as before. A history with a
// repeated id uses the encoded object, whose decoding keeps the last value.
func publicDeploymentsModel(record *model.Agent, raw json.RawMessage) (json.RawMessage, error) {
	history := record.Deployments
	keys := history.Keys()
	seen := make(map[string]bool, len(keys))
	for _, id := range keys {
		if seen[id] {
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
