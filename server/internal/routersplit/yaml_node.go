package routersplit

import (
	"configflow/internal/gateway/gwbase"
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

func nodeTruthy(node *yamlx.Node) bool {
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
	return jsonx.Truthy(value)
}

func nodeTunFilters(tun *yamlx.Node) []string {
	filters := []string{}
	for _, key := range gwbase.TUNFilterKeys {
		if nodeTruthy(yamlx.Get(tun, key)) {
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
		return nil, gatewayError("Mihomo tun 必须是对象")
	}
	if len(nodeTunFilters(tun)) != 0 {
		// Reuse the established message and key ordering, with only the small
		// filter projection crossing the existing JSON configuration boundary.
		projection := jsonx.Obj()
		for _, key := range nodeTunFilters(tun) {
			projection.Set(key, true)
		}
		_, err := FullTunDocument(jsonx.Obj("tun", projection))
		return nil, err
	}
	for _, key := range RouteKeysRemoved {
		yamlx.Delete(tun, key)
	}
	data, err := jsonx.Compact(FullTun())
	if err != nil {
		return nil, err
	}
	full, err := yamlx.FromJSON(data)
	if err != nil {
		return nil, err
	}
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
	found := false
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
