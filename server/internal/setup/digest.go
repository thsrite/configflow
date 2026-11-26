package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"bytes"
	"configflow/internal/model"
)

// OwnedDigest identifies the stored profile sections controlled by the wizard.
// withoutBase excludes only Mihomo custom_config; unrelated metadata is ignored.
// Versioned session evidence uses canonical native JSON; old hashes fail closed.
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
	raw, err = canonicalDigestJSON(raw)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// CoreFingerprint identifies the published core content, excluding routers and
// release receipts that may advance revisions without changing generated cores.
func CoreFingerprint(config json.RawMessage) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(config, &fields); err != nil || fields == nil {
		return "", ErrCorruptRecord
	}
	selected := map[string]json.RawMessage{}
	for _, key := range []string{"proxy_groups", "rule_configs", "mihomo", "oxidns", "subscriptions", "nodes", "subscription_aggregations", "rule_library", "system_config"} {
		selected[key] = fields[key]
		if len(selected[key]) == 0 {
			selected[key] = json.RawMessage(`null`)
		}
	}
	raw, err := json.Marshal(selected)
	if err != nil {
		return "", err
	}
	raw, err = canonicalDigestJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// digestVersion marks native encoding/json fingerprints. Every object is decoded
// recursively and Marshal sorts string map keys; number token spelling is kept.
const digestVersion model.Int = 1

func canonicalDigestJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
func currentDigest(record *Record) bool {
	return record.DigestVersion != nil && *record.DigestVersion == digestVersion
}
