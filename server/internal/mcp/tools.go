package mcp

import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"
)

var listPaths = map[string]string{
	"list_profiles": "/api/profiles", "list_subscriptions": "/api/subscriptions", "list_nodes": "/api/nodes", "list_aggregations": "/api/aggregations",
	"list_rules": "/api/rules", "list_rule_library": "/api/rule-library", "list_proxy_groups": "/api/proxy-groups", "list_agents": "/api/agents",
	"get_rule_dns": "/api/rules/dns", "get_oxidns_settings": "/api/oxidns/settings", "get_overview": "/api/stats/overview",
}

func knownTool(name string) bool {
	var defs []Definition
	if jsondecode.Unmarshal(catalog, &defs) != nil {
		return false
	}
	for _, d := range defs {
		if d.Name == name {
			return true
		}
	}
	return false
}
func (i *invocation) run(ctx context.Context, name string) (json.RawMessage, error) {
	result, err := i.runTool(ctx, name)
	if err != nil {
		return nil, err
	}
	if b := asObject(result); len(result) > 0 && result[0] == '{' {
		if _, ok := b["profile_id"]; !ok {
			b["profile_id"] = text(i.profile)
		}
		return packed(b), nil
	}
	return result, nil
}
func (i *invocation) runTool(ctx context.Context, name string) (json.RawMessage, error) {
	if path, ok := listPaths[name]; ok {
		return i.get(ctx, path)
	}
	a := i.args
	// Identifiers occupy exactly one route segment, never a route chosen by args.
	if a.ID != "" {
		if _, err := pathPart(a.ID); err != nil {
			return nil, err
		}
	}
	need := func(keys ...string) error {
		for _, key := range keys {
			if err := i.require(key); err != nil {
				return err
			}
		}
		return nil
	}
	switch name {
	case "manage_subscription":
		return i.crud(ctx, "/api/subscriptions", "sub", "订阅")
	case "manage_node":
		return i.crud(ctx, "/api/nodes", "", "节点")
	case "manage_rule_library":
		return i.crud(ctx, "/api/rule-library", "lib", "规则仓库条目")
	case "manage_proxy_group":
		return i.crud(ctx, "/api/proxy-groups", "group", "策略组")
	case "manage_rule":
		return i.manageRule(ctx)
	case "reorder_items":
		if err := need("collection", "ids"); err != nil {
			return nil, err
		}
		return i.reorder(ctx, a.Collection, a.IDs, value(a.Position, "top"))
	case "get_profile":
		if err := need("id"); err != nil {
			return nil, err
		}
		return i.get(ctx, "/api/profiles/"+a.ID)
	case "manage_profile":
		if err := need("action"); err != nil {
			return nil, err
		}
		if a.Action == "create" {
			return i.post(ctx, "/api/profiles", a.Data)
		}
		if err := need("id"); err != nil {
			return nil, err
		}
		path := "/api/profiles/" + a.ID
		switch a.Action {
		case "update":
			return i.put(ctx, path, a.Data)
		case "delete":
			_, err := i.delete(ctx, path)
			if err != nil {
				return nil, err
			}
			return packed(object{"success": flag(true), "profile_id": text(a.ID)}), nil
		}
		return nil, bad(msgAction, a.Action)
	case "clone_profile":
		if err := need("source_profile_id", "data"); err != nil {
			return nil, err
		}
		id, err := pathPart(a.SourceProfileID)
		if err != nil {
			return nil, err
		}
		return i.post(ctx, "/api/profiles/"+id+"/clone", a.Data)
	case "import_client_config":
		return i.post(ctx, "/api/profiles/import-client", i.original)
	case "bind_agent_profile":
		if err := need("id", "profile_id"); err != nil {
			return nil, err
		}
		current, err := i.get(ctx, "/api/agents/"+a.ID)
		if err != nil {
			return nil, err
		}
		body := asObject(current)
		body["profile_id"] = text(i.profile)
		return i.put(ctx, "/api/agents/"+a.ID, body)
	case "fetch_subscription":
		if err := need("id"); err != nil {
			return nil, err
		}
		return i.post(ctx, "/api/subscriptions/"+a.ID+"/fetch", object{"preview": flag(a.Preview)})
	case "get_subscription_nodes":
		if a.ID != "" {
			return i.get(ctx, "/api/subscriptions/"+a.ID+"/nodes")
		}
		return i.get(ctx, "/api/subscriptions/proxies")
	case "manage_aggregation":
		return i.manageAggregation(ctx)
	case "preview_aggregation":
		if err := need("id"); err != nil {
			return nil, err
		}
		mode := value(a.Mode, "nodes")
		if a.CountOnly {
			mode = "count"
		}
		path := "/api/aggregations/" + a.ID
		switch mode {
		case "nodes":
			return i.get(ctx, path+"/preview")
		case "count":
			return i.get(ctx, path+"/count")
		case "provider":
			v, err := i.get(ctx, path+"/provider")
			if err != nil {
				return nil, err
			}
			return packed(object{"id": text(a.ID), "provider": v}), nil
		}
		return nil, bad(msgMode, mode)
	case "compose_rule":
		body := object{}
		for _, key := range []string{"source", "rule", "dns", "dns_ip"} {
			if v, ok := i.original[key]; ok && string(v) != "null" {
				body[key] = v
			}
		}
		return i.post(ctx, "/api/rules/compose", body)
	case "set_rule_dns":
		if err := need("routes"); err != nil {
			return nil, err
		}
		body := object{"routes": i.original["routes"]}
		if a.SkipUnsupported != nil {
			body["skip_unsupported"] = flag(*a.SkipUnsupported)
		}
		return i.put(ctx, "/api/rules/dns", body)
	case "batch_add_rules":
		if err := need("rule_type", "domains", "policy"); err != nil {
			return nil, err
		}
		return i.post(ctx, "/api/rules/batch", object{"rule_type": text(a.RuleType), "domains": i.original["domains"], "policy": text(a.Policy)})
	case "test_rule_match":
		if err := need("query"); err != nil {
			return nil, err
		}
		return i.post(ctx, "/api/rules/match-test", object{"query": text(a.Query)})
	case "find_duplicate_rules":
		return i.post(ctx, "/api/rules/find-duplicates", object{})
	case "get_rule_library_content":
		if err := need("id"); err != nil {
			return nil, err
		}
		v, err := i.get(ctx, "/api/rule-library/content/"+a.ID)
		if err != nil {
			return nil, err
		}
		return packed(object{"id": text(a.ID), "content": v}), nil
	case "cache_rule_library":
		if err := need("ids"); err != nil {
			return nil, err
		}
		return i.post(ctx, "/api/rule-library/cache", object{"rule_ids": i.original["ids"]})
	case "test_rule_library":
		if a.URL != "" {
			return i.post(ctx, "/api/rule-library/test-single", object{"url": text(a.URL)})
		}
		return i.post(ctx, "/api/rule-library/test", object{})
	case "preview_proxy_group_regex":
		if err := need("regex", "source"); err != nil {
			return nil, err
		}
		source := rawString(a.Source)
		if source != "subscription" && source != "aggregation" {
			return nil, bad(msgSource)
		}
		subs, aggs := i.original["subscriptions"], i.original["aggregations"]
		if subs == nil {
			subs = json.RawMessage(`[]`)
		}
		if aggs == nil {
			aggs = json.RawMessage(`[]`)
		}
		return i.post(ctx, "/api/proxy-groups/preview-regex", object{"regex": text(a.Regex), "source": text(source), "subscriptions": subs, "aggregations": aggs})
	case "preview_config", "generate_config":
		if err := need("target"); err != nil {
			return nil, err
		}
		if !validTarget(a.Target) {
			return nil, bad(msgTarget)
		}
		path := "/api/generate/" + a.Target
		if name == "preview_config" {
			path += "/preview"
		}
		v, err := i.post(ctx, path, object{"base_url": text(a.BaseURL)})
		if err != nil {
			return nil, err
		}
		if name == "preview_config" {
			return v, nil
		}
		size := json.RawMessage(`null`)
		var s string
		if json.Unmarshal(v, &s) == nil {
			size = number(utf8.RuneCountInString(s))
		} else if b := asObject(v); string(b["binary"]) == "true" {
			size = b["size"]
		}
		saved := a.Target != "mosdns" && a.Target != "oxidns"
		message := fmt.Sprintf(msgPacked, a.Target)
		if saved {
			message = fmt.Sprintf(msgSaved, a.Target)
		}
		return packed(object{"success": flag(true), "target": text(a.Target), "saved": flag(saved), "size": size, "message": text(message)}), nil
	case "manage_config_backup":
		return i.configBackup(ctx)
	case "get_agent":
		if err := need("id"); err != nil {
			return nil, err
		}
		v, err := i.get(ctx, "/api/agents/"+a.ID)
		if err != nil {
			return nil, err
		}
		o := object{"agent": v}
		if a.IncludeStatus {
			o["status"], err = i.get(ctx, "/api/agents/"+a.ID+"/status")
		}
		return packed(o), err
	case "manage_agent":
		return i.manageAgent(ctx)
	case "get_agent_logs":
		if err := need("id"); err != nil {
			return nil, err
		}
		q := [][2]string{{"lines", strconv.Itoa(intValue(a.Lines, 100))}}
		addQuery(&q, "log_path", a.LogPath)
		return i.call(ctx, "GET", "/api/agents/"+a.ID+"/logs", nil, q)
	case "get_agent_metrics":
		if err := need("id"); err != nil {
			return nil, err
		}
		scope := value(a.Scope, "current")
		paths := map[string]string{"current": "/metrics", "summary": "/metrics/summary", "history": "/metrics/history", "traffic": "/traffic/stats", "traffic_trend": "/traffic/trend"}
		suffix, ok := paths[scope]
		if !ok {
			return nil, bad(msgScope, scope)
		}
		var q [][2]string
		if a.Hours != nil && (scope == "history" || scope == "traffic_trend") {
			q = append(q, [2]string{"hours", strconv.Itoa(*a.Hours)})
		}
		return i.call(ctx, "GET", "/api/agents/"+a.ID+suffix, nil, q)
	case "get_agent_install_info":
		return i.installInfo(ctx)
	case "get_mosdns_settings":
		return i.getSections(ctx, mosdnsSections, a.Section)
	case "update_mosdns_settings":
		if err := need("section", "data"); err != nil {
			return nil, err
		}
		path := sectionPath(mosdnsSections, a.Section)
		if path == "" {
			return nil, bad(msgSection, a.Section)
		}
		v, err := i.get(ctx, path)
		if err != nil {
			return nil, err
		}
		o := asObject(v)
		for k, v := range a.Data {
			o[k] = v
		}
		return i.post(ctx, path, o)
	case "convert_mosdns_rule":
		if err := need("url"); err != nil {
			return nil, err
		}
		v, err := i.call(ctx, "GET", "/api/mosdns/rule-proxy", nil, [][2]string{{"url", a.URL}})
		if err != nil {
			return nil, err
		}
		return packed(object{"url": text(a.URL), "content": v}), nil
	case "update_oxidns_settings":
		if err := need("data"); err != nil {
			return nil, err
		}
		v, err := i.get(ctx, "/api/oxidns/settings")
		if err != nil {
			return nil, err
		}
		body, err := mergeObjects(asObject(v), a.Data, 0)
		if err != nil {
			return nil, err
		}
		return i.post(ctx, "/api/oxidns/settings", body)
	case "get_settings":
		return i.getSections(ctx, settingsSections, a.Section)
	case "update_settings":
		return i.updateSettings(ctx)
	case "run_backup":
		if err := need("action"); err != nil {
			return nil, err
		}
		path := "/api/backup/now"
		if a.Action == "test_connection" {
			path = "/api/backup/test"
		} else if a.Action != "backup_now" {
			return nil, bad(msgAction, a.Action)
		}
		v, err := i.get(ctx, "/api/backup/config")
		if err != nil {
			return nil, err
		}
		return i.post(ctx, path, asObject(v))
	case "get_app_logs":
		q := [][2]string{{"lines", strconv.Itoa(intValue(a.Lines, 100))}}
		addQuery(&q, "search", a.Search)
		addQuery(&q, "level", a.Level)
		return i.call(ctx, "GET", "/api/logs/tail", nil, q)
	case "manage_app_logs":
		if err := need("action"); err != nil {
			return nil, err
		}
		if a.Action == "info" {
			return i.get(ctx, "/api/logs/info")
		}
		if a.Action == "clear" {
			return i.post(ctx, "/api/logs/clear", object{})
		}
		return nil, bad(msgAction, a.Action)
	case "manage_custom_config":
		if err := need("target", "action"); err != nil {
			return nil, err
		}
		if !validTarget(a.Target) {
			return nil, bad(msgTarget)
		}
		path := "/api/custom-config/" + a.Target
		if a.Action == "get" {
			return i.get(ctx, path)
		}
		if a.Action == "set" {
			if a.Content == nil {
				return nil, bad(msgContent)
			}
			return i.post(ctx, path, object{"config": text(*a.Content)})
		}
		return nil, bad(msgAction, a.Action)
	}
	return nil, fmt.Errorf("MCP tool has no adapter: %s", name)
}
func (i *invocation) manageAggregation(ctx context.Context) (json.RawMessage, error) {
	a := i.args
	if err := i.require("action"); err != nil {
		return nil, err
	}
	if a.Action == "create" {
		if len(a.Data) == 0 {
			return nil, bad(msgCreate, "聚合")
		}
		v, err := i.post(ctx, "/api/aggregations", a.Data)
		if err != nil {
			return nil, err
		}
		return resultAction("create", "", unwrap(v, packed(a.Data))), nil
	}
	if err := i.require("id"); err != nil {
		return nil, err
	}
	path := "/api/aggregations/" + a.ID
	if a.Action == "delete" {
		_, err := i.delete(ctx, path)
		if err != nil {
			return nil, err
		}
		return resultAction("delete", a.ID, nil), nil
	}
	if a.Action != "update" {
		return nil, bad(msgAction, a.Action)
	}
	if len(a.Data) == 0 {
		return nil, bad(msgUpdate, "聚合")
	}
	v, err := i.get(ctx, path)
	if err != nil {
		return nil, err
	}
	body := mergeResource(asObject(v), a.Data, a.ID)
	v, err = i.put(ctx, path, body)
	if err != nil {
		return nil, err
	}
	return resultAction("update", a.ID, unwrap(v, packed(body))), nil
}
func (i *invocation) configBackup(ctx context.Context) (json.RawMessage, error) {
	a := i.args
	if err := i.require("action"); err != nil {
		return nil, err
	}
	scope := value(a.Scope, "system")
	if scope != "system" && scope != "profile" {
		return nil, bad(msgScope, scope)
	}
	prefix := "/api/config"
	if scope == "profile" {
		id, err := pathPart(i.profile)
		if err != nil {
			return nil, err
		}
		prefix = "/api/profiles/" + id
	}
	switch a.Action {
	case "export":
		var q [][2]string
		if scope == "system" && a.Desensitize != nil {
			q = append(q, [2]string{"desensitize", strconv.FormatBool(*a.Desensitize)})
		}
		return i.call(ctx, "GET", prefix+"/export", nil, q)
	case "import":
		if len(a.Data) == 0 {
			return nil, bad(msgImport)
		}
		return i.post(ctx, prefix+"/import", a.Data)
	case "reset":
		if scope == "system" {
			return i.post(ctx, prefix+"/reset", object{})
		}
	}
	if scope == "profile" {
		return nil, bad(msgProfileAction, a.Action)
	}
	return nil, bad(msgAction, a.Action)
}
func (i *invocation) manageAgent(ctx context.Context) (json.RawMessage, error) {
	a := i.args
	if err := i.require("action"); err != nil {
		return nil, err
	}
	if err := i.require("id"); err != nil {
		return nil, err
	}
	path := "/api/agents/" + a.ID
	switch a.Action {
	case "update":
		if len(a.Data) == 0 {
			return nil, bad(msgUpdate, " Agent ")
		}
		v, err := i.get(ctx, path)
		if err != nil {
			return nil, err
		}
		o := asObject(v)
		for k, v := range a.Data {
			o[k] = v
		}
		return i.put(ctx, path, o)
	case "delete":
		return i.delete(ctx, path)
	case "restart", "uninstall":
		return i.post(ctx, path+"/"+a.Action, object{})
	case "upgrade":
		return i.post(ctx, path+"/update", object{})
	case "push_config":
		return i.post(ctx, path+"/push-config", object{"base_url": text(a.BaseURL)})
	case "clear_log", "validate_log_path":
		if err := i.require("log_path"); err != nil {
			return nil, err
		}
		key, suffix := "log_path", "/logs/clear"
		if a.Action == "validate_log_path" {
			key, suffix = "path", "/logs/validate"
		}
		return i.post(ctx, path+suffix, object{key: text(value(a.LogPath, ""))})
	case "get_logging":
		return i.get(ctx, path+"/config/logging")
	case "set_logging":
		if a.Enabled == nil {
			return nil, bad(msgEnabled)
		}
		return i.post(ctx, path+"/config/logging", object{"enabled": flag(*a.Enabled)})
	}
	return nil, bad(msgAction, a.Action)
}
func (i *invocation) installInfo(ctx context.Context) (json.RawMessage, error) {
	a := i.args
	if err := i.require("method"); err != nil {
		return nil, err
	}
	paths := map[string]string{"install_script": "/api/agents/install-script", "docker_compose": "/api/agents/docker-agent-compose", "docker_run": "/api/agents/docker-agent-run", "latest_version": "/api/agents/latest-version"}
	path, ok := paths[a.Method]
	if !ok {
		return nil, bad(msgInstallMethod, a.Method)
	}
	var q [][2]string
	for k, v := range a.Params {
		if string(v) == "null" {
			continue
		}
		s := rawString(v)
		if s == "" && string(v) != "\"\"" {
			s = string(v)
			if s == "true" {
				s = "True"
			}
			if s == "false" {
				s = "False"
			}
		}
		q = append(q, [2]string{k, s})
	}
	v, err := i.call(ctx, "GET", path, nil, q)
	if err != nil {
		return nil, err
	}
	if len(v) > 0 && v[0] == '{' {
		return v, nil
	}
	return packed(object{"method": text(a.Method), "content": v}), nil
}
