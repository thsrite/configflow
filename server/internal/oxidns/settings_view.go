package oxidns

import (
	"encoding/json"
	"fmt"

	"configflow/internal/jsondecode"
)

// adguardView is the leniently decoded adguard section of a settings object.
type adguardView struct {
	Enabled member[bool]             `json:"enabled"`
	Content member[string]           `json:"content"`
	URLs    member[[]member[string]] `json:"urls"`
}

func (a adguardView) urls() []string {
	if !a.URLs.OK {
		return nil
	}
	out := make([]string, 0, len(a.URLs.Value))
	for _, item := range a.URLs.Value {
		if item.OK {
			out = append(out, item.Value)
		}
	}
	return out
}

// commonDisabledView is the leniently decoded renamed_plugins map inside
// _common_disabled. Parked native plugin trees remain on the nativeDoc boundary.
type commonDisabledView struct {
	RenamedPlugins member[map[string]member[string]] `json:"renamed_plugins"`
}

// settingsView is a leniently decoded read view of the OxiDNS settings object
// using jsondecode.Member semantics: missing or wrong-typed members do not fail
// decoding, and callers preserve their exact validation and fallback rules.
type settingsView struct {
	ConfigMode          member[string]                     `json:"config_mode"`
	NativeConfig        member[string]                     `json:"native_config"`
	CustomHosts         member[string]                     `json:"custom_hosts"`
	DefaultForward      member[string]                     `json:"default_forward"`
	PreferIPv4          member[bool]                       `json:"prefer_ipv4"`
	CustomMatchPosition member[string]                     `json:"custom_match_position"`
	LocalDNS            member[string]                     `json:"local_dns"`
	RemoteDNS           member[string]                     `json:"remote_dns"`
	FallbackDNS         member[string]                     `json:"fallback_dns"`
	AdGuard             member[adguardView]                `json:"adguard"`
	CommonDisabled      member[commonDisabledView]         `json:"_common_disabled"`
	TemplateSettings    member[map[string]json.RawMessage] `json:"template_settings"`
	LegacyNativeConfig  member[json.RawMessage]            `json:"legacy_native_config"`
}

func readSettingsView(settings *Node) settingsView {
	if !isMap(settings) {
		return settingsView{}
	}
	var v settingsView
	_ = jsondecode.Unmarshal(ToJSON(settings), &v)
	return v
}

func (v settingsView) isNative() bool {
	return v.ConfigMode.OK && v.ConfigMode.Value == "native"
}

func (v settingsView) loadNative(label string) (*Node, error) {
	if !v.NativeConfig.OK {
		return nil, fail(fmt.Sprintf("OxiDNS %s必须为不超过 2 MiB 的 YAML 文本", label))
	}
	return LoadNativeYAML(v.NativeConfig.Value, label)
}

func (v settingsView) loadNativeOrEmpty(label string) (*Node, error) {
	if !v.NativeConfig.Present() {
		return LoadNativeYAML("", label)
	}
	return v.loadNative(label)
}

func (v settingsView) adguardEnabled() bool {
	return v.AdGuard.OK && v.AdGuard.Value.Enabled.OK && v.AdGuard.Value.Enabled.Value
}

func (v settingsView) adguardURLs() []string {
	if !v.AdGuard.OK {
		return nil
	}
	return v.AdGuard.Value.urls()
}

func (v settingsView) renamedPlugin(tag string) (string, bool) {
	if !v.CommonDisabled.OK || !v.CommonDisabled.Value.RenamedPlugins.OK {
		return "", false
	}
	entry, ok := v.CommonDisabled.Value.RenamedPlugins.Value[tag]
	return entry.Value, ok && entry.OK
}

func (v settingsView) sourceDNS(source string) string {
	switch source {
	case "local":
		return v.LocalDNS.Value
	case "remote":
		return v.RemoteDNS.Value
	case "fallback":
		return v.FallbackDNS.Value
	}
	return ""
}

func (v settingsView) preferIPv4() bool {
	return !(v.PreferIPv4.OK && !v.PreferIPv4.Value)
}

func memberTextNodeOr(m member[string], fallback string) *Node {
	if !m.Present() {
		return S(fallback)
	}
	if m.OK {
		return S(m.Value)
	}
	if node, err := FromJSON(m.Raw); err == nil {
		return node
	}
	return S(fallback)
}
