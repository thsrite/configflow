package agents

import (
	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

// publicAgent adapts the legacy registry record to the public API. Keep this
// boundary shared by list/detail and client tools so private history cannot
// escape through an alternate endpoint. The source record is never modified.
func publicAgent(record *agents.Record) *agents.Record {
	public := record.Clone()
	for key, value := range record.All() {
		if key == "token" || key == "applied_content" || key == "dns_follow_job" || value == repo.AgentHistoryNotRead {
			public.Delete(key)
		}
	}
	if job := public.GetObject("oxidns_ha_publication"); job != nil && job.Len() > 0 {
		public.Set("oxidns_ha_publication", agents.HASummary(job))
	}
	if records := public.GetObject("deployments"); records != nil {
		cleaned := records.Clone()
		for key := range records.All() {
			cleaned.Set(key, deploy.PublicDeployment(records.GetObject(key)))
		}
		public.Set("deployments", cleaned)
	}
	if record := public.GetObject("latest_deployment"); record != nil {
		public.Set("latest_deployment", deploy.PublicDeployment(record))
	}
	if record := public.GetObject("latest_upgrade"); record != nil {
		public.Set("latest_upgrade", agents.PublicUpgradeState(record))
	}
	return redact.SanitizeCredentials(public, redact.NewSecrets([]string{record.GetString("token")}))
}
