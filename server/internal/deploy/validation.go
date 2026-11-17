package deploy

import (
	"configflow/internal/agents"
	"configflow/internal/oxidns"
	"configflow/internal/version"
	"configflow/internal/yamlx"
	"encoding/json"
	"runtime/debug"
)

// RulesFingerprint identifies the Go schemas AND validator build. Unlike the
// content fingerprint this intentionally invalidates Python-era ready receipts.
func RulesFingerprint() string {
	build := version.Version
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			build = info.Main.Version
		}
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				build += "\x00" + s.Value
			}
		}
	}
	return rulesFingerprint(oxidns.SchemaFingerprint(), build)
}
func rulesFingerprint(schema, build string) string {
	return digest([]byte(schema + "\x00" + build + "\x00"))
}

// SettingsDigest is the Go-native canonical digest of stored OxiDNS settings.
func SettingsDigest(settings json.RawMessage) string {
	encoded, err := nativeCanonical(settings)
	if err != nil {
		return ""
	}
	return digest(encoded)
}

// ValidateBundle inspects the actual final archive before attesting OxiDNS files.
func ValidateBundle(agentID string, b *Bundle) (ValidationReceipt, error) {
	m := b.Manifest()
	fingerprint, err := Fingerprint(m)
	if err != nil {
		return ValidationReceipt{}, err
	}
	if m.ServiceType != "oxidns" {
		return ValidationReceipt{}, preparation("Invalid OxiDNS deployment archive digest")
	}
	archive, err := b.Archive()
	if err != nil {
		return ValidationReceipt{}, err
	}
	sha, err := b.SHA256()
	if err != nil {
		return ValidationReceipt{}, err
	}
	if digest(archive) != sha {
		return ValidationReceipt{}, preparation("Invalid OxiDNS deployment archive digest")
	}
	files, err := readArchive(m, archive)
	if err != nil {
		return ValidationReceipt{}, &PreparationError{Message: "Invalid OxiDNS deployment archive", cause: err}
	}
	document, err := oxidns.LoadNativeYAML(string(files[m.ConfigPath].Data), "")
	if err != nil {
		return ValidationReceipt{}, err
	}
	if truthNodeInclude(document) {
		return ValidationReceipt{}, preparation(msgUnexpandedInclude)
	}
	if _, err = oxidns.RequireValidDocument(document); err != nil {
		return ValidationReceipt{}, err
	}
	return ValidationReceipt{Version: 1, RulesSHA256: RulesFingerprint(), AgentID: agentID, DeploymentID: m.DeploymentID, ProfileID: m.ProfileID, ServiceType: "oxidns", SHA256: sha, Fingerprint: fingerprint}, nil
}
func truthNodeInclude(n *oxidns.Node) bool { return truth(yamlx.Get(n, "include")) }

// ReceiptMatches accepts only server-owned evidence for the current resolved
// binding and the exact task archive/content identity. Missing identities fail.
func ReceiptMatches(agent *agents.Record, state *agents.Record) bool {
	return receiptMatches(agent, state)
}

// PublicDeployment returns the public localized task without private baselines.
func PublicDeployment(record *agents.Record) *agents.Record { return publicDeployment(record) }
