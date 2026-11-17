// Package configexport prepares shareable configuration copies. Restorable
// administrator backups bypass this policy; repository identity and writes
// remain owned by repo.
package configexport

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// Sanitize returns an external-safe configuration without mutating its source.
func Sanitize(ctx context.Context, config *jsonx.Object) (*jsonx.Object, error) {
	return sanitize(ctx, config, config.GetObject("system_config"))
}

func sanitize(ctx context.Context, payload, settings *jsonx.Object) (*jsonx.Object, error) {
	result, err := redact.SanitizeExternalPayload(ctx, payload, settings)
	if err != nil {
		return nil, err
	}
	out, ok := jsonx.AsObject(result)
	if !ok {
		return nil, fmt.Errorf("sanitize configuration: object expected")
	}
	return out, nil
}

// RedactOxiDNS removes opaque native text and upstream credentials while
// retaining rule identities and the shape of pinned upstream actions.
func RedactOxiDNS(settings model.Dynamic) (model.Dynamic, error) {
	root, err := settings.Node()
	if err != nil {
		return model.Dynamic{}, err
	}
	root = redactNode(root, 0)
	var out model.Dynamic
	if err := out.SetNode(root); err != nil {
		return out, err
	}
	return out, nil
}

var opaque = map[string]bool{"custom_config": true, "native_config": true, "template_base_config": true, "legacy_native_config": true, "local_dns": true, "remote_dns": true, "fallback_dns": true}
var credentials = map[string]bool{"password": true, "username": true, "token": true, "secret": true, "authorization": true, "private_key": true, "private-key": true, "key": true, "socks5": true, "proxy": true, "url": true, "urls": true, "headers": true, "bootstrap_rules": true}

func nonempty(n *yamlx.Node) bool {
	if n == nil {
		return false
	}
	switch n.Kind {
	case yaml.MappingNode, yaml.SequenceNode:
		return len(n.Content) > 0
	default:
		if n.ShortTag() == "!!int" || n.ShortTag() == "!!float" {
			v, _ := strconv.ParseFloat(n.Value, 64)
			return v != 0
		}
		return n.ShortTag() != "!!null" && !(n.ShortTag() == "!!bool" && n.Value == "false") && !(n.ShortTag() == "!!int" && n.Value == "0") && !(n.ShortTag() == "!!float" && (n.Value == "0.0" || n.Value == "0")) && n.Value != ""
	}
}
func redactNode(n *yamlx.Node, depth int) *yamlx.Node {
	if depth >= 64 {
		return yamlx.Str(redact.Redacted)
	}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, item := n.Content[i].Value, n.Content[i+1]
			switch {
			case key == "additional_files":
				item = yamlx.Seq()
			case opaque[key]:
				item = yamlx.Str("")
			case credentials[strings.ToLower(key)] && nonempty(item):
				item = yamlx.Str(redact.Redacted)
			case (key == "rule_actions" || key == "rule_dns_parked") && item.Kind == yaml.MappingNode && depth+1 < 62:
				for j := 1; j < len(item.Content); j += 2 {
					action := redactNode(item.Content[j], depth+2)
					upstream := yamlx.Get(action, "upstream")
					if nonempty(yamlx.Get(upstream, "addr")) {
						yamlx.Set(upstream, "addr", yamlx.Str(redact.Redacted))
					}
					item.Content[j] = action
				}
			default:
				item = redactNode(item, depth+1)
			}
			n.Content[i+1] = item
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			n.Content[i] = redactNode(item, depth+1)
		}
	}
	return n
}

// Wire installs the export hooks. Only this adapter uses legacy repo values;
// the arbitrary OxiDNS subtree is represented by model.Dynamic internally.
func Wire() {
	repo.RegisterHooks(repo.Hooks{SanitizeExternalPayload: sanitize, RedactOxidnsSettings: func(settings any) any {
		data, err := json.Marshal(settings)
		if err != nil {
			return redact.Redacted
		}
		var input model.Dynamic
		if err = input.UnmarshalJSON(data); err != nil {
			return redact.Redacted
		}
		out, err := RedactOxiDNS(input)
		if err != nil {
			return redact.Redacted
		}
		data, err = json.Marshal(out)
		if err != nil {
			return redact.Redacted
		}
		value, err := jsonx.Parse(data)
		if err != nil {
			return redact.Redacted
		}
		return value
	}})
}
