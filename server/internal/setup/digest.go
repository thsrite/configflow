package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"configflow/internal/jsonx"
)

// OwnedDigest identifies the stored profile sections controlled by the wizard.
// withoutBase excludes only Mihomo custom_config; unrelated metadata is ignored.
// Existing Python-written session hashes remain valid through the shared legacy
// fingerprint encoder; no new JSON encoder or Python compatibility code lives here.
func OwnedDigest(profile json.RawMessage, withoutBase bool) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(profile, &fields); err != nil || fields == nil {
		return "", fmt.Errorf("read setup ownership: %w", ErrCorruptRecord)
	}
	owned := make(map[string]json.RawMessage, 4)
	for _, key := range []string{"proxy_groups", "rule_configs", "mihomo", "oxidns"} {
		owned[key] = fields[key]
		if len(owned[key]) == 0 {
			owned[key] = json.RawMessage(`null`)
		}
	}
	if withoutBase {
		var mihomo map[string]json.RawMessage
		if json.Unmarshal(owned["mihomo"], &mihomo) == nil && mihomo != nil {
			delete(mihomo, "custom_config")
			owned["mihomo"], _ = json.Marshal(mihomo)
		}
	}
	raw, err := json.Marshal(owned)
	if err != nil {
		return "", fmt.Errorf("encode setup ownership fields: %w", err)
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("read setup ownership fields: %w", err)
	}
	raw, err = jsonx.Encode(value, jsonx.Options{SortKeys: true})
	if err != nil {
		return "", fmt.Errorf("encode stored setup fingerprint: %w", err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
