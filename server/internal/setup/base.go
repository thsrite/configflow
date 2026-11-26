package setup

import (
	"fmt"
	"strings"

	"configflow/internal/convert"
	"configflow/internal/routersplit"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
	"strconv"
)

// ControllerSecretError returns the public reason a newly entered controller
// password is unsafe. Existing private base credentials are retained unchanged.
func ControllerSecretError(value string) error {
	if value == "123456" {
		return failure(msgTemplateSecret, 400)
	}
	if len(value) < 8 || len(value) > 64 {
		return failure(msgSecretFormat, 400)
	}
	for _, c := range value {
		if c < '!' || c > '~' {
			return failure(msgSecretFormat, 400)
		}
	}
	return nil
}

func nodeString(node *yamlx.Node) string {
	node = yamlx.Resolve(node)
	if node != nil && node.Kind == yaml.ScalarNode && node.ShortTag() == "!!str" {
		return node.Value
	}
	return ""
}

func nodeDescription(node *yamlx.Node, stringForm bool) (string, error) {
	node = yamlx.Resolve(node)
	if node != nil && node.Kind == yaml.ScalarNode && node.ShortTag() == "!!str" {
		return node.Value, nil
	}
	raw, err := yamlx.ToJSON(node)
	return string(raw), err
}

func (s *Service) mihomoBase(source string, fullTUN bool, reserved map[string]string, secret *string) (string, []string, error) {
	if !s.owners.DNS.ready() {
		return "", nil, unavailable()
	}
	empty := strings.TrimSpace(source) == ""
	if empty {
		source = s.catalog.Template.Mihomo.CustomConfig
	}
	original, err := yamlx.Parse([]byte(source))
	if err != nil {
		return "", nil, failure(msgBaseParse, 400)
	}
	if original.Kind != yaml.MappingNode {
		return "", nil, failure(msgBaseMapping, 400)
	}
	// Encode validates the alias budget and copies the graph without expanding
	// aliases. The DNS owner may mutate its independent temporary document.
	prepared, err := yamlx.Encode(original)
	if err != nil {
		return "", nil, ownerValidation(err)
	}
	document, err := s.owners.DNS.PrepareMihomoDNS(prepared)
	if err != nil {
		return "", nil, ownerValidation(err)
	}
	changes := []string{}
	if empty {
		changes = append(changes, msgEmptyBase)
	}
	if !yamlx.Equal(yamlx.Get(yamlx.Get(original, "dns"), "nameserver"), yamlx.Get(yamlx.Get(document, "dns"), "nameserver")) {
		changes = append(changes, msgNameservers+strings.Join(s.owners.DNS.Nameservers(), "、"))
	}
	oldSecret := nodeString(yamlx.Get(document, "secret"))
	hasSecret := oldSecret != "" && oldSecret != "123456"
	if secret != nil {
		if err := ControllerSecretError(*secret); err != nil {
			return "", nil, err
		}
		if oldSecret != *secret {
			change := msgSecretSet
			if hasSecret {
				change = msgSecretUpdated
			}
			changes = append(changes, change)
			yamlx.Set(document, "secret", yamlx.Str(*secret))
		}
	} else if !hasSecret {
		return "", nil, failure(msgSecretRequired, 400)
	}
	dns := yamlx.Resolve(yamlx.Get(document, "dns"))
	for _, setting := range []struct {
		key   string
		value *yamlx.Node
	}{{"enable", yamlx.Bool(true)}, {"listen", yamlx.Str(s.owners.DNS.DNSTarget)}, {"enhanced-mode", yamlx.Str("fake-ip")}} {
		before := yamlx.Get(dns, setting.key)
		if yamlx.Equal(before, setting.value) {
			continue
		}
		next, err := nodeDescription(setting.value, false)
		if err != nil {
			return "", nil, err
		}
		if before == nil {
			changes = append(changes, fmt.Sprintf(msgDNSSet, setting.key, next))
		} else {
			old, err := nodeDescription(before, false)
			if err != nil {
				return "", nil, err
			}
			changes = append(changes, fmt.Sprintf(msgDNSChanged, setting.key, old, next))
		}
		yamlx.Set(dns, setting.key, setting.value)
	}
	if mode := yamlx.Get(document, "mode"); mode != nil && !yamlx.Equal(mode, yamlx.Str("rule")) {
		shown, err := nodeDescription(mode, true)
		if err != nil {
			return "", nil, err
		}
		changes = append(changes, fmt.Sprintf(msgModeChanged, shown))
		yamlx.Set(document, "mode", yamlx.Str("rule"))
	}
	tun := yamlx.Resolve(yamlx.Get(document, "tun"))
	if tun != nil && tun.ShortTag() != "!!null" && tun.Kind != yaml.MappingNode {
		return "", nil, failure(msgTunMapping, 400)
	}
	if fullTUN {
		next, err := routersplit.FullTunNode(document)
		if err != nil {
			return "", nil, ownerValidation(err)
		}
		document = next
		if !yamlx.Equal(tun, yamlx.Get(document, "tun")) {
			change := msgFullTUN
			removed := []string{}
			for _, key := range routersplit.RouteKeysRemoved {
				if yamlx.Get(tun, key) != nil {
					removed = append(removed, "tun."+key)
				}
			}
			if len(removed) > 0 {
				change += msgRemoved + strings.Join(removed, "、")
			}
			changes = append(changes, change)
		}
	} else if tun != nil && tun.Kind == yaml.MappingNode {
		if filters := routersplit.TunFilterPaths(tun); len(filters) > 0 {
			return "", nil, failure(fmt.Sprintf(msgTunFilter, filters[0]), 400)
		}
		if routersplit.TunActive(yamlx.Get(tun, "enable")) {
			yamlx.Set(tun, "enable", yamlx.Bool(false))
			changes = append(changes, msgTunDisabled)
		}
	}
	ports := convert.ListenPorts(document)
	for _, port := range ports {
		if label, exists := reserved[strconv.FormatInt(port.Port, 10)]; exists {
			return "", nil, failure(fmt.Sprintf(msgPortCollision, port.Owner, strconv.FormatInt(port.Port, 10), label), 400)
		}
	}
	data, err := yamlx.Marshal(document)
	return string(data), changes, err
}
