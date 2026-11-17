package deploy

import "encoding/json"

// UnmarshalJSON treats wrong-typed optional capability members as absent (N8 R5).
// Old Agents and malformed optional additions therefore fail the protocol gate.
func (c *Capabilities) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(raw, &fields); e != nil {
		return e
	}
	*c = Capabilities{}
	protocols := func(key string) []int {
		var entries []json.RawMessage
		if json.Unmarshal(fields[key], &entries) != nil {
			return nil
		}
		out := []int{}
		for _, entry := range entries {
			var n int
			if json.Unmarshal(entry, &n) == nil {
				out = append(out, n)
			}
		}
		return out
	}
	c.DeploymentProtocols = protocols("deployment_protocols")
	c.FinalConfigProtocols = protocols("final_config_protocols")
	c.ReloadOnlyProtocols = protocols("reload_only_protocols")
	_ = json.Unmarshal(fields["service_type"], &c.ServiceType)
	_ = json.Unmarshal(fields["config_path"], &c.ConfigPath)
	_ = json.Unmarshal(fields["config_directory"], &c.ConfigDirectory)
	var resources []json.RawMessage
	if json.Unmarshal(fields["mihomo_resources"], &resources) == nil {
		if resources != nil {
			c.MihomoResources = []string{}
		}
		for _, r := range resources {
			var s string
			if json.Unmarshal(r, &s) == nil {
				c.MihomoResources = append(c.MihomoResources, s)
			}
		}
	}
	return nil
}
