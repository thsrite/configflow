package routersplit

import (
	"configflow/internal/gateway/gwbase"
	"configflow/internal/yamlx"
	"fmt"

	"gopkg.in/yaml.v3"
)

// nodeFilterActive interprets a user TUN exclusion: only empty/zero values
// disable it. Opaque tagged scalars remain active so takeover fails closed.
func nodeFilterActive(node *yamlx.Node) bool {
	node = yamlx.Resolve(node)
	if node == nil {
		return false
	}
	if node.Kind == yaml.MappingNode || node.Kind == yaml.SequenceNode {
		return len(node.Content) > 0
	}
	if node.ShortTag() == "!!null" {
		return false
	}
	var value any
	if err := yamlx.Decode(node, &value); err != nil {
		return true
	}
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case int:
		return v != 0
	case int64:
		return v != 0
	case uint64:
		return v != 0
	case float64:
		return v != 0
	}
	// Opaque tagged scalars and undecodable extensions are active filters: a
	// takeover must fail closed rather than silently bypass user exclusions.
	return true
}

func nodeTunFilters(tun *yamlx.Node) []string {
	filters := []string{}
	for _, key := range gwbase.TUNFilterKeys {
		if nodeFilterActive(yamlx.Get(tun, key)) {
			filters = append(filters, key)
		}
	}
	return filters
}

func fullTunNode(document *yamlx.Node) (*yamlx.Node, error) {
	document, err := yamlx.Encode(document)
	if err != nil {
		return nil, err
	}
	tun := yamlx.Resolve(yamlx.Get(document, "tun"))
	if tun == nil || tun.ShortTag() == "!!null" {
		tun = yamlx.Map()
	}
	if tun.Kind != yaml.MappingNode {
		return nil, gatewayError(messageTunObject)
	}
	if filters := nodeTunFilters(tun); len(filters) > 0 {
		return nil, gatewayError(fmt.Sprintf(messageTunFilter, filters[0]))
	}
	for _, key := range RouteKeysRemoved {
		yamlx.Delete(tun, key)
	}
	full := yamlx.Map(yamlx.Str("enable"), yamlx.Bool(true), yamlx.Str("stack"), yamlx.Str("mixed"), yamlx.Str("auto-route"), yamlx.Bool(true), yamlx.Str("auto-redirect"), yamlx.Bool(true), yamlx.Str("auto-detect-interface"), yamlx.Bool(true), yamlx.Str("dns-hijack"), yamlx.Seq(yamlx.Str("any:53"), yamlx.Str("tcp://any:53")))
	for key, value := range yamlx.Pairs(full) {
		if key != "stack" || yamlx.Get(tun, key) == nil {
			yamlx.Set(tun, key, value)
		}
	}
	yamlx.Set(document, "tun", tun)
	return document, nil
}

func fullTunNodeShape(document *yamlx.Node) bool {
	document = yamlx.Resolve(document)
	if document == nil || document.Kind != yaml.MappingNode || yamlx.Get(document, GatewayMarker) != nil {
		return false
	}
	tun := yamlx.Resolve(yamlx.Get(document, "tun"))
	if tun == nil || tun.Kind != yaml.MappingNode {
		return false
	}
	for _, key := range []string{"enable", "auto-route", "auto-redirect"} {
		node := yamlx.Resolve(yamlx.Get(tun, key))
		var value bool
		if node == nil || node.ShortTag() != "!!bool" || yamlx.Decode(node, &value) != nil || !value {
			return false
		}
	}
	hijack := yamlx.Resolve(yamlx.Get(tun, "dns-hijack"))
	if hijack == nil || hijack.Kind != yaml.SequenceNode {
		return false
	}
	// New AIO setups explicitly disable hijacking; existing full-TUN profiles use any:53.
	found := len(hijack.Content) == 0
	for _, node := range hijack.Content {
		node = yamlx.Resolve(node)
		if node != nil && node.ShortTag() == "!!str" && node.Value == "any:53" {
			found = true
		}
	}
	if !found {
		return false
	}
	for _, key := range RouteKeysRemoved {
		if yamlx.Get(tun, key) != nil {
			return false
		}
	}
	return true
}
