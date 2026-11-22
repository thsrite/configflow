package convert

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
	"strings"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// ClientNodeDecoder is the temporary N8-9 substore projection, not a second parser.
type ClientNodeDecoder func([]*yamlx.Node) ([]model.Node, error)

// ClientProxyText renders subscription responses for Surge or Loon.
func ClientProxyText(proxies []*yamlx.Node, decode ClientNodeDecoder, target string) (string, error) {
	for _, proxy := range proxies {
		if proxy == nil {
			return "", fmt.Errorf("%s", msgClientProxyObject)
		}
		if proxy.Kind == yaml.AliasNode {
			data, err := yamlx.ToJSON(proxy)
			if err != nil {
				return "", err
			}
			if data = bytes.TrimSpace(data); len(data) == 0 || data[0] != '{' {
				return "", fmt.Errorf("%s", msgClientProxyObject)
			}
		} else if proxy.Kind != yaml.MappingNode {
			return "", fmt.Errorf("%s", msgClientProxyObject)
		}
		if dialer := yamlx.Get(proxy, "dialer-proxy"); dialer != nil && !nullNode(dialer) {
			if target == "surge" {
				return "", dialerError(msgSurgeDeliveryDialer)
			}
			return "", dialerError(msgLoonDeliveryDialer)
		}
	}
	if decode == nil {
		return "", &HookMissingError{Hook: "proxies_to_nodes", Provider: "substore"}
	}
	// A proxy whose type or server is not text is skipped like any failing
	// node (Python failed on it); the typed node would read it as empty.
	valid := make([]*yamlx.Node, 0, len(proxies))
	for _, proxy := range proxies {
		name := ""
		if value := yamlx.Get(proxy, "name"); value != nil {
			name = value.Value
		}
		if err := parsedNodeText(proxy, name, "type", "server"); err != nil {
			if target == "surge" {
				surgeLog.Warningf(msgSurgeNodeFailure, name, err)
			} else {
				loonLog.Warningf(msgLoonNodeFailure, name, err)
			}
			continue
		}
		valid = append(valid, proxy)
	}
	nodes, err := decode(valid)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, node := range nodes {
		var line, section string
		if target == "surge" {
			line, section, err = SurgeNode(node)
		} else {
			line, err = LoonNode(node)
		}
		if err != nil {
			if target == "surge" {
				surgeLog.Warningf(msgSurgeNodeFailure, node.Name, err)
			} else {
				loonLog.Warningf(msgLoonNodeFailure, node.Name, err)
			}
			continue
		}
		if line != "" && section == "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// ClientGroupNodes returns filtered subscription names followed by explicit members.
func ClientGroupNodes(group model.ProxyGroup, config *model.Config) ([]string, error) {
	if config == nil {
		return nil, fmt.Errorf("%s", msgClientConfig)
	}
	var names []string
	subscriptions := map[string]bool{}
	for _, id := range stringList(group.Subscriptions) {
		subscriptions[id] = true
	}
	if len(subscriptions) > 0 {
		var nodes []model.Node
		for _, node := range config.Nodes {
			if subscriptions[node.SubscriptionID] && enabled(node.Enabled) {
				nodes = append(nodes, node)
			}
		}
		filter := CurrentMihomoEnvironment().Clients.FilterNodes
		if filter == nil {
			return nil, &HookMissingError{Hook: "filter_candidates", Provider: "rules"}
		}
		nodes, err := filter(nodes, group.Regex, group.ExcludeRegex)
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			names = append(names, node.Name)
		}
	}
	names = append(names, stringList(group.Proxies)...)
	result := []string{}
	seen := map[string]bool{}
	for _, name := range names {
		if !seen[name] {
			result = append(result, name)
			seen[name] = true
		}
	}
	return result, nil
}
