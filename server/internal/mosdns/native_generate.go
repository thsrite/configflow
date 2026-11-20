package mosdns

import (
	"errors"
	"gopkg.in/yaml.v3"
	"strings"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func stringList(values ...string) *yamlx.Node {
	result := yamlx.Seq()
	for _, value := range values {
		result.Content = append(result.Content, yamlx.Str(value))
	}
	return result
}
func instruction(action string) *yamlx.Node { return yamlx.Map(yamlx.Str("exec"), yamlx.Str(action)) }
func condition(match, action string) *yamlx.Node {
	return yamlx.Map(yamlx.Str("matches"), stringList(match), yamlx.Str("exec"), yamlx.Str(action))
}
func resource(tag, kind string, args *yamlx.Node) *yamlx.Node {
	return yamlx.Map(yamlx.Str("tag"), yamlx.Str(tag), yamlx.Str("type"), yamlx.Str(kind), yamlx.Str("args"), args)
}
func settingInt(value *model.Int, fallback int64) int64 {
	if value == nil {
		return fallback
	}
	return int64(*value)
}
func effectiveOrigin(config *model.Config, baseURL string) string {
	if value := strings.TrimSpace(config.SystemConfig.ServerDomain); value != "" {
		return value
	}
	return baseURL
}

// Downloads lists the remote resources referenced by the generated document.
func Downloads(config *model.Config, baseURL string) ([]Download, error) {
	layout, err := layoutFor(config)
	if err != nil {
		return nil, err
	}
	result := []Download{}
	settings := config.Profile.MosDNS
	selected := idSet(append(append([]string{}, settings.DirectRulesets...), settings.ProxyRulesets...))
	for _, id := range layout.order {
		item, exists := layout.sets[id]
		if !exists || !selected[id] || !flag(&item, "enabled", item.Enabled) {
			continue
		}
		files, err := RuleFiles(config, effectiveOrigin(config, baseURL), item, layout.stems[id])
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.URL != nil {
				result = append(result, Download{Name: item.Name, URL: *file.URL, LocalPath: file.LocalPath, Behavior: textValue(item.Behavior, "domain")})
			}
		}
	}
	return result, nil
}

// CustomFiles returns the hosts and adjacent single-rule resources.
func CustomFiles(config *model.Config) ([]File, error) {
	result := []File{}
	if config == nil || config.Profile == nil {
		return result, nil
	}
	hosts := convert.ParseHostsPairs(config.Profile.MosDNS.CustomHosts)
	if len(hosts) > 0 {
		lines := make([]string, len(hosts))
		for i, h := range hosts {
			lines[i] = h.Domain + " " + h.Address
		}
		result = append(result, File{Path: "./rules/custom_hosts.txt", Content: strings.Join(lines, "\n")})
	}
	groups, err := SingleRuleBatches(config)
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		result = append(result, File{Path: group.Path, Content: strings.Join(group.Lines, "\n")})
	}
	return result, nil
}

// Generate builds MosDNS YAML from one captured, typed repository view.
func Generate(config *model.Config, baseURL string) (string, error) {
	if config == nil || config.Profile == nil {
		return "", errors.New("mosdns: missing profile")
	}
	root, err := document(config, baseURL)
	if err != nil {
		return "", err
	}
	yamlx.MaterializeAliases(root)
	custom := config.Profile.MosDNS.CustomConfig
	if original, parseErr := yamlx.Parse([]byte(custom)); parseErr == nil && original.Kind == yaml.MappingNode {
		data, err := yamlx.Edit([]byte(custom), func(node *yamlx.Node) error { *node = *root; return nil })
		return string(data), err
	}
	data, err := yamlx.Marshal(root)
	return string(data), err
}

func document(config *model.Config, baseURL string) (*yamlx.Node, error) {
	layout, err := layoutFor(config)
	if err != nil {
		return nil, err
	}
	s := config.Profile.MosDNS
	var original *yamlx.Node
	if strings.TrimSpace(s.CustomConfig) != "" {
		original, err = loadCustomConfig(s.CustomConfig)
		if err != nil {
			return nil, err
		}
	}
	root := yamlx.Map()
	if flag(&s, "log_enabled", s.LogEnabled) {
		yamlx.Set(root, "log", yamlx.Map(yamlx.Str("level"), yamlx.Str(textValue(s.LogLevel, "info")), yamlx.Str("file"), yamlx.Str(textValue(s.LogFile, "./mosdns.log"))))
	}
	if flag(&s, "api_enabled", s.APIEnabled) {
		yamlx.Set(root, "api", yamlx.Map(yamlx.Str("http"), yamlx.Str(textValue(s.APIAddress, "0.0.0.0:8338"))))
	}
	plugins := yamlx.Seq()
	add := func(tag, kind string, args *yamlx.Node) {
		plugins.Content = append(plugins.Content, resource(tag, kind, args))
	}
	directSets, proxySets := idSet(s.DirectRulesets), idSet(s.ProxyRulesets)
	for _, id := range layout.order {
		item, exists := layout.sets[id]
		if !exists || (!directSets[id] && !proxySets[id]) || !flag(&item, "enabled", item.Enabled) {
			continue
		}
		files, err := RuleFiles(config, effectiveOrigin(config, baseURL), item, layout.stems[id])
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			tag, kind := layout.tags[id], "domain_set"
			if file.Part == PartIP {
				kind = "ip_set"
				if textValue(item.Behavior, "domain") == "classical" {
					tag += "_ip"
				}
			}
			add(tag, kind, yamlx.Map(yamlx.Str("files"), stringList(file.LocalPath)))
		}
	}
	if flag(&s, "cache_enabled", s.CacheEnabled) {
		args := yamlx.Map(yamlx.Str("size"), yamlx.Int(settingInt(s.CacheSize, 10240)), yamlx.Str("lazy_cache_ttl"), yamlx.Int(settingInt(s.CacheLazyTTL, 21600)))
		if flag(&s, "cache_dump_enabled", s.CacheDumpEnabled) {
			interval := settingInt(s.CacheDumpInterval, 300)
			if interval <= 0 {
				interval = 300
			}
			file := textValue(s.CacheDumpFile, "./cache.dump")
			if file == "" {
				file = "./cache.dump"
			}
			yamlx.Set(args, "dump_file", yamlx.Str(strings.TrimSpace(file)))
			yamlx.Set(args, "dump_interval", yamlx.Int(interval))
		}
		add("lazy_cache", "cache", args)
	}
	local, err := Upstreams(s.LocalDNS)
	if err != nil {
		return nil, err
	}
	if len(local) == 0 {
		local = []*yamlx.Node{yamlx.Map(yamlx.Str("addr"), yamlx.Str("223.5.5.5")), yamlx.Map(yamlx.Str("addr"), yamlx.Str("119.29.29.29"))}
	}
	remote, err := Upstreams(s.RemoteDNS)
	if err != nil {
		return nil, err
	}
	if len(remote) == 0 {
		remote = []*yamlx.Node{yamlx.Map(yamlx.Str("addr"), yamlx.Str("https://1.1.1.1/dns-query")), yamlx.Map(yamlx.Str("addr"), yamlx.Str("https://dns.google/dns-query"))}
	}
	fallback, err := Upstreams(s.FallbackDNS)
	if err != nil {
		return nil, err
	}
	if len(fallback) == 0 {
		copied := yamlx.CopyGraph(yamlx.Seq(local...))
		fallback = copied.Content
	}
	for _, forward := range []struct {
		tag         string
		concurrency int64
		upstreams   []*yamlx.Node
	}{{"forward_local", 10, local}, {"forward_remote", 5, remote}, {"forward_remote_fallback", 10, fallback}} {
		add(forward.tag, "forward", yamlx.Map(yamlx.Str("concurrent"), yamlx.Int(forward.concurrency), yamlx.Str("upstreams"), yamlx.Seq(forward.upstreams...)))
	}
	hosts := len(convert.ParseHostsPairs(s.CustomHosts)) > 0
	if hosts {
		add("custom_hosts", "hosts", yamlx.Map(yamlx.Str("files"), stringList("./rules/custom_hosts.txt")))
	}
	add("remote_dns", "sequence", yamlx.Seq(instruction("query_summary forward_remote"), instruction("$forward_remote"), condition("!has_resp", "reject 3"), instruction("ttl 1")))
	add("remote_dns_fallback", "sequence", yamlx.Seq(instruction("query_summary forward_remote_fallback"), instruction("$forward_remote_fallback"), condition("!has_resp", "reject 3")))
	add("proxy_dns", "fallback", yamlx.Map(yamlx.Str("primary"), yamlx.Str("remote_dns"), yamlx.Str("secondary"), yamlx.Str("remote_dns_fallback"), yamlx.Str("threshold"), yamlx.Int(500), yamlx.Str("always_standby"), yamlx.Bool(false)))
	add("proxy_dns_seq", "sequence", yamlx.Seq(instruction("$proxy_dns"), instruction("accept")))
	china := yamlx.Seq()
	if flag(&s, "cache_enabled", s.CacheEnabled) {
		china.Content = append(china.Content, instruction("$lazy_cache"), condition("has_resp", "accept"))
	}
	china.Content = append(china.Content, instruction("query_summary forward_local"), instruction("$forward_local"), condition("!has_resp", "reject 3"))
	add("china_dns", "sequence", china)
	batchByID := map[string]RuleBatch{}
	for _, batch := range layout.batches {
		for _, id := range batch.IDs {
			batchByID[id] = batch
		}
		kind := "domain_set"
		if batch.Part == PartIP {
			kind = "ip_set"
		}
		add(batch.Tag, kind, yamlx.Map(yamlx.Str("files"), stringList(batch.Path)))
	}
	main := yamlx.Seq(instruction("metrics_collector metrics"), instruction("prefer_ipv4"))
	if hosts {
		main.Content = append(main.Content, instruction("$custom_hosts"), condition("has_resp", "accept"))
	}
	var domainEntries, ipEntries []*yamlx.Node
	added := map[string]bool{}
	directRules, proxyRules := idSet(s.DirectRules), idSet(s.ProxyRules)
	route := func(tag string, direct bool, part string) {
		action := "goto proxy_dns_seq"
		if direct {
			action = "goto china_dns"
		}
		if part == PartIP {
			action = "goto ip_requery_proxy"
			if direct {
				action = "accept"
			}
			ipEntries = append(ipEntries, condition("resp_ip $"+tag, action))
		} else {
			domainEntries = append(domainEntries, condition("qname $"+tag, action))
		}
	}
	for _, item := range config.Profile.RuleConfigs {
		if item.ID == "" || !flag(&item, "enabled", item.Enabled) || !flag(&item, "library_enabled", item.LibraryEnabled) {
			continue
		}
		switch item.ItemType {
		case "ruleset":
			if !directSets[item.ID] && !proxySets[item.ID] {
				continue
			}
			rs := layout.sets[item.ID]
			tag := layout.tags[item.ID]
			for _, part := range Parts(textValue(rs.Behavior, "domain")) {
				t := tag
				if part == PartIP && textValue(rs.Behavior, "domain") == "classical" {
					t += "_ip"
				}
				route(t, directSets[item.ID], part)
			}
		case "rule":
			batch, exists := batchByID[item.ID]
			if !exists || added[batch.Tag] || (!directRules[item.ID] && !proxyRules[item.ID]) {
				continue
			}
			added[batch.Tag] = true
			route(batch.Tag, directRules[item.ID], batch.Part)
		}
	}
	matches := MatchEntries(s.CustomMatches)
	if textValue(s.CustomMatchPosition, "tail") == "head" {
		main.Content = append(main.Content, matches...)
		main.Content = append(main.Content, domainEntries...)
	} else {
		main.Content = append(main.Content, domainEntries...)
		main.Content = append(main.Content, matches...)
	}
	localDefault := textValue(s.DefaultForward, "forward_remote") == "forward_local"
	if len(ipEntries) > 0 {
		add("ip_requery_proxy", "sequence", yamlx.Seq(instruction("drop_resp"), instruction("goto proxy_dns_seq")))
		main.Content = append(main.Content, instruction("query_summary forward_local"), instruction("$forward_local"))
		main.Content = append(main.Content, ipEntries...)
		if localDefault {
			main.Content = append(main.Content, condition("has_resp", "accept"))
		} else {
			main.Content = append(main.Content, instruction("drop_resp"))
		}
	}
	if localDefault {
		main.Content = append(main.Content, instruction("goto china_dns"))
	} else {
		main.Content = append(main.Content, instruction("goto proxy_dns_seq"))
	}
	add("sequence_main", "sequence", main)
	listen := textValue(s.ListenAddress, ":53")
	if listen == "" {
		listen = ":53"
	}
	for _, kind := range []string{"udp_server", "tcp_server"} {
		plugins.Content = append(plugins.Content, yamlx.Map(yamlx.Str("type"), yamlx.Str(kind), yamlx.Str("args"), yamlx.Map(yamlx.Str("entry"), yamlx.Str("sequence_main"), yamlx.Str("listen"), yamlx.Str(listen))))
	}
	yamlx.Set(root, "plugins", plugins)
	if original != nil {
		for key, value := range yamlx.AllPairs(original) {
			k := yamlx.Resolve(key)
			if k.Kind == yaml.ScalarNode && k.ShortTag() == "!!str" {
				switch k.Value {
				case "log", "api", "plugins", "servers", "data_providers":
					continue
				}
			}
			root.Content = append(root.Content, key, value)
		}
	}
	return root, nil
}
