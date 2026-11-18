package convert_test

import (
	"context"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/substore"
)

func executeClientBoundary(t *testing.T, entry *jsonx.Object) (any, error) {
	t.Helper()
	fn := entry.GetString("fn")
	if fn == "surge" || fn == "loon" {
		args := entry.GetObject("args")
		profile := args.GetObject("profile").DeepCopy()
		if fn == "surge" {
			return convert.GenerateSurgeConfig(context.Background(), profile, args.GetString("base_url"))
		}
		return convert.GenerateLoonConfig(context.Background(), profile, args.GetString("base_url"))
	}
	args := entry.GetList("args")
	arg := func(i int) any { return jsonx.DeepCopy(args[i]) }
	switch fn {
	case "node_surge":
		line, section, err := convert.ConvertNodeToSurge(arg(0).(*jsonx.Object))
		return []any{noneString(line), noneString(section)}, err
	case "node_loon":
		line, err := convert.ConvertNodeToLoon(arg(0).(*jsonx.Object))
		return noneString(line), err
	case "proxies_surge":
		return convert.ConvertProxiesToSurgeText(arg(0).([]any), substore.ProxiesToNodes)
	case "proxies_loon":
		return convert.ConvertProxiesToLoonText(arg(0).([]any), substore.ProxiesToNodes)
	case "group_surge":
		smart, _ := arg(3).(*jsonx.Object)
		line, err := convert.ConvertProxyGroupToSurge(arg(0), arg(1).(*jsonx.Object), args[2].(string), smart)
		return noneString(line), err
	case "group_loon":
		var members []string
		for _, m := range args[1].([]any) {
			members = append(members, m.(string))
		}
		line, err := convert.ConvertProxyGroupToLoon(arg(0), members)
		return noneString(line), err
	case "group_nodes":
		return convert.GetProxyGroupNodes(arg(0), arg(1).(*jsonx.Object))
	case "build_rules":
		lines, remote, err := convert.BuildLoonRules(arg(0).(*jsonx.Object), args[1].(string))
		return []any{stringsAny(lines), stringsAny(remote)}, err
	case "sanitize":
		return convert.SanitizeLoonName(arg(0))
	}
	t.Fatalf("unknown client call %s", fn)
	return nil, nil
}
func clientEntryIssue(t *testing.T, entry *jsonx.Object) string {
	t.Helper()
	fn := entry.GetString("fn")
	profileIssue := func(profile *jsonx.Object) string {
		if issue := convert.ClientProfileReplayIssue(profile, fn); strings.HasPrefix(issue, "pending shared") {
			return issue
		}
		if issue := convert.CommonReplayIssue(profile); issue != "" {
			return issue
		}
		if issue := convert.NodeProfileReplayIssue(profile); issue != "" {
			return issue
		}
		if issue := convert.DialerProfileReplayIssue(profile); issue != "" {
			return issue
		}
		return convert.ClientProfileReplayIssue(profile, fn)
	}
	if fn == "surge" || fn == "loon" {
		return profileIssue(entry.GetObject("args").GetObject("profile"))
	}
	args := entry.GetList("args")
	switch fn {
	case "node_surge", "node_loon":
		return convert.ClientNodeReplayIssue(args[0])
	case "group_surge", "group_nodes":
		if issue := convert.ClientGroupReplayIssue(args[0], "surge"); issue != "" {
			return issue
		}
		return profileIssue(args[1].(*jsonx.Object))
	case "group_loon":
		return convert.ClientGroupReplayIssue(args[0], "loon")
	case "build_rules":
		return profileIssue(args[0].(*jsonx.Object))
	case "sanitize":
		if _, ok := args[0].(string); !ok {
			return "sanitize string"
		}
	case "proxies_surge", "proxies_loon":
		proxies, _ := args[0].([]any)
		for _, proxy := range proxies {
			if object, ok := proxy.(*jsonx.Object); !ok || object == nil {
				return "subscription proxy object"
			}
		}
		nodes, err := substore.ProxiesToNodes(proxies)
		if err != nil {
			return ""
		}
		for _, node := range nodes {
			if issue := convert.ClientNodeReplayIssue(node); issue != "" {
				return "subscription " + issue
			}
		}
	}
	return ""
}
