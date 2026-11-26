package mcp

import (
	"context"
	"encoding/json"
)

type section struct{ name, path string }

var mosdnsSections = []section{{"rulesets", "/api/mosdns/rulesets"}, {"custom_matches", "/api/mosdns/custom-matches"}, {"dns_servers", "/api/mosdns/dns-servers"}, {"log", "/api/mosdns/log-settings"}, {"api", "/api/mosdns/api-settings"}, {"cache", "/api/mosdns/cache-settings"}}
var settingsSections = []section{{"server_domain", "/api/server-domain"}, {"config_token", "/api/config-token"}, {"subscription_aggregation", "/api/settings/subscription-aggregation"}, {"backup", "/api/backup/config"}, {"github_proxy", "/api/rule-library/proxy-domains"}, {"version", "/api/version"}}

func sectionPath(sections []section, name string) string {
	for _, s := range sections {
		if s.name == name {
			return s.path
		}
	}
	return ""
}
func (i *invocation) getSections(ctx context.Context, sections []section, name string) (json.RawMessage, error) {
	if name != "" {
		path := sectionPath(sections, name)
		if path == "" {
			return nil, bad(msgSection, name)
		}
		return i.get(ctx, path)
	}
	out := object{}
	for _, s := range sections {
		v, err := i.get(ctx, s.path)
		if err != nil {
			return nil, err
		}
		out[s.name] = v
	}
	return packed(out), nil
}
func (i *invocation) updateSettings(ctx context.Context) (json.RawMessage, error) {
	a := i.args
	if err := i.require("section"); err != nil {
		return nil, err
	}
	if err := i.require("data"); err != nil {
		return nil, err
	}
	path := sectionPath(settingsSections, a.Section)
	if path == "" {
		return nil, bad(msgSection, a.Section)
	}
	if a.Section == "version" {
		return nil, bad(msgReadonly, a.Section)
	}
	data := copyObject(a.Data)
	if a.Section == "backup" {
		v, err := i.get(ctx, path)
		if err != nil {
			return nil, err
		}
		current := asObject(v)
		for k, v := range data {
			current[k] = v
		}
		data = current
	}
	read, write := "", ""
	if a.Section == "server_domain" {
		read, write = "server_domain", "new_domain"
	}
	if a.Section == "config_token" {
		read, write = "config_token", "token"
	}
	if read != "" {
		v, ok := data[read]
		_, hasWrite := data[write]
		if ok && !hasWrite {
			data[write] = v
			delete(data, read)
		}
	}
	return i.post(ctx, path, data)
}
func mergeObjects(previous, changes object, depth int) (object, error) {
	if depth > 128 {
		return nil, bad(msgType)
	}
	result := copyObject(previous)
	for k, v := range changes {
		if len(v) > 0 && v[0] == '{' {
			merged, err := mergeObjects(asObject(result[k]), asObject(v), depth+1)
			if err != nil {
				return nil, err
			}
			v = packed(merged)
		}
		result[k] = v
	}
	return result, nil
}
