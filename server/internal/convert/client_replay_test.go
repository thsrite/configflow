package convert_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/substore"
)

func executeClientBoundary(t *testing.T, entry *convert.JObj) (any, error) {
	t.Helper()
	fn := entry.GetString("fn")
	if fn == "surge" || fn == "loon" {
		args := entry.GetObject("args")
		cfg, err := decodeClientConfig(args.GetObject("profile").DeepCopy())
		if err != nil {
			return "", err
		}
		if fn == "surge" {
			return convert.SurgeConfig(context.Background(), cfg, args.GetString("base_url"))
		}
		return convert.LoonConfig(context.Background(), cfg, args.GetString("base_url"))
	}
	args := entry.GetList("args")
	arg := func(i int) any { return convert.DeepCopyJ(args[i]) }
	switch fn {
	case "node_surge":
		node, err := convert.DecodeJ[model.Node](arg(0))
		if err != nil {
			return nil, err
		}
		line, section, err := convert.SurgeNode(node)
		return []any{noneString(line), noneString(section)}, err
	case "node_loon":
		node, err := convert.DecodeJ[model.Node](arg(0))
		if err != nil {
			return nil, err
		}
		line, err := convert.LoonNode(node)
		return noneString(line), err
	case "proxies_surge":
		nodes, err := decodeYAMLProxies(arg(0).([]any))
		if err != nil {
			return "", err
		}
		return convert.ClientProxyText(nodes, substore.ProxyNodes, "surge")
	case "proxies_loon":
		nodes, err := decodeYAMLProxies(arg(0).([]any))
		if err != nil {
			return "", err
		}
		return convert.ClientProxyText(nodes, substore.ProxyNodes, "loon")
	case "group_surge":
		group, err := convert.DecodeJ[model.ProxyGroup](arg(0))
		if err != nil {
			return nil, err
		}
		cfg, err := convert.DecodeConfigJ(arg(1).(*convert.JObj))
		if err != nil {
			return nil, err
		}
		var smart map[string]string
		if arg(3) != nil {
			smart, err = convert.DecodeJ[map[string]string](arg(3))
			if err != nil {
				return nil, err
			}
		}
		line, err := convert.SurgeGroup(group, cfg, args[2].(string), smart)
		return noneString(line), err
	case "group_loon":
		var members []string
		for _, m := range args[1].([]any) {
			members = append(members, m.(string))
		}
		group, err := convert.DecodeJ[model.ProxyGroup](arg(0))
		if err != nil {
			return nil, err
		}
		line, err := convert.LoonGroup(group, members)
		return noneString(line), err
	case "group_nodes":
		group, err := convert.DecodeJ[model.ProxyGroup](arg(0))
		if err != nil {
			return nil, err
		}
		cfg, err := convert.DecodeConfigJ(arg(1).(*convert.JObj))
		if err != nil {
			return nil, err
		}
		return convert.ClientGroupNodes(group, cfg)
	case "build_rules":
		cfg, err := convert.DecodeConfigJ(arg(0).(*convert.JObj))
		if err != nil {
			return nil, err
		}
		lines, remote, err := convert.LoonRules(cfg, args[1].(string))
		return []any{stringsAny(lines), stringsAny(remote)}, err
	case "sanitize":
		text, err := convert.DecodeJ[string](arg(0))
		if err != nil {
			return "", err
		}
		return convert.ClientSanitizeNameForTest(text), nil
	}
	t.Fatalf("unknown client call %s", fn)
	return nil, nil
}

func clientEntryIssue(t *testing.T, entry *convert.JObj) (issue string) {
	t.Helper()
	fn := entry.GetString("fn")
	// Keep all production decoder/shape diagnoses first. Only a formerly
	// failing, otherwise valid row can use the accepted numeric-text category.
	defer func() {
		if issue == "" && rowOf(entry).errClass != "" {
			issue = clientAcceptedNumericText(entry)
		}
	}()
	target := fn
	if index := strings.LastIndexByte(fn, '_'); index >= 0 {
		target = fn[index+1:]
	}
	profileIssue := func(profile *convert.JObj) string {
		if issue := convert.CommonReplayIssue(profile); issue != "" {
			return issue
		}
		if issue := convert.NodeProfileReplayIssue(profile); issue != "" {
			return issue
		}
		if issue := convert.DialerProfileReplayIssue(profile); issue != "" {
			return issue
		}
		return convert.ClientProfileReplayIssue(profile, target)
	}
	if fn == "surge" || fn == "loon" {
		return profileIssue(entry.GetObject("args").GetObject("profile"))
	}
	args := entry.GetList("args")
	switch fn {
	case "node_surge", "node_loon":
		return convert.ClientNodeReplayIssue(args[0], target)
	case "group_surge", "group_nodes":
		if issue := convert.ClientGroupReplayIssue(args[0], "surge"); issue != "" {
			return issue
		}
		return profileIssue(args[1].(*convert.JObj))
	case "group_loon":
		return convert.ClientGroupReplayIssue(args[0], "loon")
	case "build_rules":
		return profileIssue(args[0].(*convert.JObj))
	case "sanitize":
		if _, ok := args[0].(string); !ok {
			return "sanitize string"
		}
	case "proxies_surge", "proxies_loon":
		proxies, _ := args[0].([]any)
		for _, proxy := range proxies {
			if object, ok := proxy.(*convert.JObj); !ok || object == nil {
				return "subscription proxy object"
			}
		}
		yamlProxies, err := decodeYAMLProxies(proxies)
		if err != nil {
			return ""
		}
		nodes, err := substore.ProxyNodes(yamlProxies)
		if err != nil {
			return ""
		}
		for _, node := range nodes {
			data, err := model.Marshal(node)
			if err != nil {
				continue
			}
			parsed, err := convert.ParseJ(data)
			if err != nil {
				continue
			}
			if issue := convert.ClientNodeReplayIssue(parsed, target); issue != "" {
				return "subscription " + issue
			}
		}
	}
	return ""
}

// The production scalar decoder is the authority; the index records input shape,
// never a native business output. Type discriminators are deliberately excluded.
func clientAcceptedNumericText(entry *convert.JObj) string {
	numeric := func(object *convert.JObj, fields ...string) string {
		if object == nil {
			return ""
		}
		for _, field := range fields {
			raw, _ := object.Get(field)
			if _, ok := raw.(json.Number); !ok {
				continue
			}
			data, err := convert.CompactJ(raw)
			if err != nil || !json.Valid(data) {
				continue
			}
			var value model.String
			if json.Unmarshal(data, &value) == nil {
				return field
			}
		}
		return ""
	}
	fn := entry.GetString("fn")
	if fn == "node_surge" || fn == "node_loon" {
		node, _ := entry.GetList("args")[0].(*convert.JObj)
		if field := numeric(node, "name", "server"); field != "" {
			return "native numeric text node." + field
		}
	}
	if fn == "surge" || fn == "loon" {
		profile := entry.GetObject("args").GetObject("profile")
		if field := numeric(profile.GetObject(fn), "custom_config"); field != "" {
			return "native numeric text " + fn + "." + field
		}
	}
	return ""
}
