package substore

import (
	"configflow/internal/convert"
	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"strings"
)

// ProxyShapeError identifies an invalid subscription graph without engine-specific exceptions.
type ProxyShapeError struct{ Message string }

// Error returns the user-facing shape diagnostic.
func (e *ProxyShapeError) Error() string { return e.Message }
func proxyShape(format string, n *yamlx.Node) error {
	data, err := yamlx.ToJSON(n)
	if err != nil {
		return err
	}
	return &ProxyShapeError{Message: fmt.Sprintf(format, data)}
}

// ParseProxyNodes reads the provider's physical YAML graph once and returns owned proxies.
func ParseProxyNodes(text string) ([]*yamlx.Node, error) {
	root, err := yamlx.Parse([]byte(text))
	if err != nil {
		return nil, fmt.Errorf(msgInvalidYAML, err)
	}
	if err := yamlx.ProjectJSONKeys(root, yamlx.StringifyScalarKeys); err != nil {
		return nil, err
	}
	root = yamlx.Resolve(root)
	if root == nil || root.Kind != yaml.MappingNode {
		return []*yamlx.Node{}, nil
	}
	list := yamlx.Get(root, "proxies")
	if list == nil {
		return []*yamlx.Node{}, nil
	}
	if _, err := yamlx.ToJSON(list); err != nil {
		return nil, err
	}
	list = yamlx.Resolve(list)
	if list.Kind != yaml.SequenceNode {
		return nil, proxyShape(msgProxyList, list)
	}
	out := make([]*yamlx.Node, 0, len(list.Content))
	for _, item := range list.Content {
		item = yamlx.Resolve(item)
		if item == nil || item.Kind != yaml.MappingNode {
			return nil, proxyShape(msgProxyObject, item)
		}
		node := yamlx.Clone(item)
		convert.FixProxyNode(node)
		out = append(out, node)
	}
	return out, nil
}

// ProxyNodes builds typed stored-node records while retaining opaque protocol parameters.
func ProxyNodes(proxies []*yamlx.Node) ([]model.Node, error) {
	out := make([]model.Node, 0, len(proxies))
	for _, proxy := range proxies {
		proxy = yamlx.Resolve(proxy)
		if proxy == nil || proxy.Kind != yaml.MappingNode {
			return nil, proxyShape(msgProxyObject, proxy)
		}
		typeNode := yamlx.Resolve(yamlx.Get(proxy, "type"))
		if typeNode != nil && typeNode.ShortTag() != "!!null" && (typeNode.Kind != yaml.ScalarNode || typeNode.ShortTag() != "!!str") {
			return nil, proxyShape(msgProxyType, typeNode)
		}
		data, err := yamlx.ToJSON(proxy)
		if err != nil {
			return nil, err
		}
		var source model.Node
		if err := json.Unmarshal(data, &source); err != nil {
			return nil, err
		}
		if yamlx.Get(proxy, "name") == nil {
			source.Name = "Unnamed"
		}
		params := yamlx.Clone(proxy)
		for _, key := range []string{"name", "type", "server", "port"} {
			yamlx.Delete(params, key)
		}
		var dynamic model.Dynamic
		if err := dynamic.SetNode(params); err != nil {
			return nil, err
		}
		nameJSON, err := jsonresp.Encode(source.Name)
		if err != nil {
			return nil, err
		}
		serverJSON, err := jsonresp.Encode(source.Server)
		if err != nil {
			return nil, err
		}
		portJSON, err := jsonresp.Encode(source.Port)
		if err != nil {
			return nil, err
		}
		issues := model.Issues(&source)
		for _, field := range []struct {
			name   string
			target *[]byte
		}{{"name", &nameJSON}, {"server", &serverJSON}, {"port", &portJSON}} {
			if model.IssueUnder(issues, field.name) {
				raw, err := yamlx.ToJSON(yamlx.Get(proxy, field.name))
				if err != nil {
					return nil, err
				}
				*field.target = raw
			}
		}
		encoded, err := jsonresp.Encode(struct {
			Name   json.RawMessage `json:"name"`
			Type   string          `json:"type"`
			Server json.RawMessage `json:"server"`
			Port   json.RawMessage `json:"port"`
			Params model.Dynamic   `json:"params"`
		}{nameJSON, strings.ToLower(source.Type), serverJSON, portJSON, dynamic})
		if err != nil {
			return nil, err
		}
		var record model.Node
		if err := json.Unmarshal(encoded, &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}
