package configgen

import (
	"configflow/internal/jsonx"
	"configflow/internal/oxidns"
)

// dnsProfile is the remaining Render compatibility boundary; Build reads the
// native repository directly and never enters this transcoder.
func dnsProfile(config *jsonx.Object) (*oxidns.Profile, error) {
	selected := jsonx.NewObject()
	for _, key := range []string{"oxidns", "mosdns", "rule_configs", "rules", "rule_sets", "proxy_groups", "device_notes", "profile_id", "system_config", "rule_library", "agents", "nodes", "subscriptions", "subscription_aggregations"} {
		if value, ok := config.Get(key); ok {
			selected.Set(key, value)
		}
	}
	data, err := jsonx.Compact(selected)
	if err != nil {
		return nil, err
	}
	return oxidns.ProfileFromJSON(data)
}
