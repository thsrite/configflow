package health

import (
	"context"
	"encoding/json"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func needsRemoteProxyString(text string) bool {
	if text == "" {
		return false
	}
	parsed, err := convert.StructuredProxy(text)
	return err == nil && parsed == nil
}

func convertProbeNode(ctx context.Context, raw json.RawMessage, converter convert.NodeConverter) (model.Fields, bool) {
	var node model.Node
	if json.Unmarshal(raw, &node) != nil {
		return model.Fields{}, false
	}
	var original rawObject
	if json.Unmarshal(raw, &original) != nil {
		return model.Fields{}, false
	}
	var proxy *yamlx.Node
	var err error
	if node.ProxyString != nil && *node.ProxyString != "" || original.Fields.Has("params") {
		proxy, err = convert.NodeProxy(ctx, node, converter)
	} else {
		// Already materialized protocol records retain all opaque protocol extensions.
		proxy, err = yamlx.FromJSON(raw)
		if err == nil {
			convert.FixProxyNode(proxy)
		}
	}
	if err != nil || proxy == nil || proxy.ShortTag() != "!!map" {
		return model.Fields{}, false
	}
	kind := yamlx.Get(proxy, "type")
	if kind == nil || kind.ShortTag() != "!!str" || kind.Value == "" {
		return model.Fields{}, false
	}
	yamlx.Set(proxy, "name", yamlx.Str(node.Name))
	encoded, err := yamlx.ToJSON(proxy)
	if err != nil {
		return model.Fields{}, false
	}
	var fields model.Fields
	if json.Unmarshal(encoded, &fields) != nil {
		return model.Fields{}, false
	}
	return fields, true
}
