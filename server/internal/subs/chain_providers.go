package subs

import (
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

type chainOriginal struct {
	Name   string `json:"name"`
	Fields model.Fields
	Raw    model.Raw
}

func (r *chainOriginal) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, r, &r.Raw) }
func (r chainOriginal) MarshalJSON() ([]byte, error)  { return model.EncodeRecord(&r, &r.Raw) }

type chainFilters struct {
	Filter        *string   `json:"filter"`
	ExcludeFilter *string   `json:"exclude-filter"`
	ExcludeType   *string   `json:"exclude-type"`
	Use           *[]string `json:"use"`
	Fields        model.Fields
	Raw           model.Raw
}

func (f *chainFilters) UnmarshalJSON(b []byte) error { return model.DecodeRecord(b, f, &f.Raw) }
func readChainFilters(group *yamlx.Node) (chainFilters, error) {
	projection := yamlx.Map()
	for _, field := range []string{"filter", "exclude-filter", "exclude-type", "use"} {
		if value := yamlx.Get(group, field); value != nil {
			projection.Content = append(projection.Content, yamlx.Str(field), value)
		}
	}
	data, err := yamlx.ToJSON(projection)
	if err != nil {
		return chainFilters{}, err
	}
	var out chainFilters
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	if issues := model.Issues(&out); len(issues) > 0 {
		return out, fmt.Errorf(msgChainFieldInvalid, issues[0].Path)
	}
	return out, nil
}
func chainCandidateType(node *yamlx.Node, kind string) (string, error) {
	value := yamlx.Get(node, "type")
	if dnsNull(value) {
		return "", nil
	}
	if text, ok := dnsText(value); ok {
		return text, nil
	}
	raw, err := yamlx.ToJSON(value)
	if err != nil {
		return "", err
	}
	label := msgChainGroupType
	if kind == "node" {
		label = msgChainNodeType
	}
	return "", fmt.Errorf(msgChainIdentityType, label, raw)
}
func ownedProviders(main *yamlx.Node) (*yamlx.Node, error) {
	value := yamlx.Get(main, "proxy-providers")
	if value == nil {
		return yamlx.Map(), nil
	}
	resolved := yamlx.Resolve(value)
	if resolved == nil || resolved.Kind != yaml.MappingNode {
		return nil, fmt.Errorf(msgChainFieldInvalid, "proxy-providers")
	}
	validated, err := yamlx.Encode(value)
	if err != nil {
		return nil, err
	}
	return yamlx.CopyGraph(validated), nil
}

// ChainProviders clones source providers for lowered chain exits and filters
// original names before overrides. It uses the shared dialer and filter owner;
// no configuration or resource catalog is re-decoded inside candidate loops.
func ChainProviders(config *model.Config, main *yamlx.Node) (failure error) {
	ops := sharedDialers()
	if ops.Resolve == nil || ops.Filter == nil {
		return &CollaborationError{Operation: "chain providers"}
	}
	providers, err := ownedProviders(main)
	if err != nil {
		return err
	}
	groups, err := ownedSequence(main, "proxy-groups")
	if err != nil {
		return err
	}
	hadGroups := yamlx.Get(main, "proxy-groups") != nil
	// Keep the original prefix on a business failure, including an empty provider
	// object; only successful cleanup removes an unused empty provider collection.
	defer func() {
		if hadGroups {
			yamlx.Set(main, "proxy-groups", groups)
		}
		if failure == nil && len(providers.Content) == 0 {
			yamlx.Delete(main, "proxy-providers")
		} else {
			yamlx.Set(main, "proxy-providers", providers)
		}
		yamlx.MaterializeAliases(main)
	}()
	groupByName, proxyByName := map[string]*yamlx.Node{}, map[string]*yamlx.Node{}
	for _, group := range groups.Content {
		if resolved := yamlx.Resolve(group); resolved != nil && resolved.Kind == yaml.MappingNode {
			groupByName[proxyName(group)] = group
		}
	}
	proxies := yamlx.Resolve(yamlx.Get(main, "proxies"))
	if proxies != nil && proxies.Kind == yaml.SequenceNode {
		for _, proxy := range proxies.Content {
			if resolved := yamlx.Resolve(proxy); resolved != nil && resolved.Kind == yaml.MappingNode {
				proxyByName[proxyName(proxy)] = proxy
			}
		}
	}
	occupied := map[string]bool{}
	for name := range yamlx.Pairs(providers) {
		occupied[name] = true
	}
	for name := range groupByName {
		occupied[name] = true
	}
	for name := range proxyByName {
		occupied[name] = true
	}
	nodeIDs := map[string]bool{}
	if config != nil {
		for _, node := range config.Nodes {
			nodeIDs[node.ID] = true
		}
	}
	for _, source := range groupList(config) {
		dialer, err := opaqueGroupRef(source, "_chain_dialer")
		if err != nil {
			return err
		}
		if dialer == nil {
			continue
		}
		group := groupByName[source.Name]
		if group == nil {
			return &ChainResourceError{ID: source.Name}
		}
		filters, err := readChainFilters(group)
		if err != nil {
			return err
		}
		filterValue := textValue(filters.Filter)
		yamlx.Delete(group, "filter")
		yamlx.Delete(group, "exclude-filter")
		yamlx.Delete(group, "exclude-type")
		var patterns []string
		if strings.Contains(filterValue, "`") {
			patterns = strings.Split(filterValue, "`")
		}
		originals := model.Map[chainOriginal]{}
		if data, present := source.Fields.Get("_chain_original_candidates"); present {
			if err := json.Unmarshal(data, &originals); err != nil {
				return err
			}
		}
		if issues := model.Issues(&originals); len(issues) > 0 {
			return fmt.Errorf(msgChainFieldInvalid, "_chain_original_candidates")
		}
		static := []FilterCandidate{}
		for id, original := range originals.All() {
			kind := "group"
			if nodeIDs[id] {
				kind = "node"
			}
			emitted, err := ops.Resolve(config, model.Node{DialerRef: &model.ResourceRef{Type: kind, ID: id}})
			if err != nil {
				return err
			}
			if emitted == nil {
				return &ChainResourceError{ID: id}
			}
			table := groupByName
			if kind == "node" {
				table = proxyByName
			}
			converted := table[*emitted]
			if converted == nil {
				return &ChainResourceError{ID: *emitted}
			}
			typ, err := chainCandidateType(converted, kind)
			if err != nil {
				return err
			}
			static = append(static, FilterCandidate{Name: original.Name, Type: typ, Key: *emitted})
		}
		static, err = ops.Filter(static, "", textValue(filters.ExcludeFilter), textValue(filters.ExcludeType))
		if err != nil {
			return referenceFailure(err)
		}
		if members := yamlx.Resolve(yamlx.Get(group, "proxies")); members != nil && members.Kind == yaml.SequenceNode && len(members.Content) > 0 {
			allowed := map[string]bool{}
			for _, candidate := range static {
				allowed[candidate.Key] = true
			}
			kept := yamlx.Seq()
			for _, member := range members.Content {
				data, err := yamlx.ToJSON(member)
				if err != nil {
					return err
				}
				var name model.String
				if json.Unmarshal(data, &name) == nil && allowed[string(name)] {
					kept.Content = append(kept.Content, member)
				}
			}
			yamlx.Set(group, "proxies", kept)
		}
		use := yamlx.Seq()
		var captures []int
		const orderTag = "排序_"
		for index, originalName := range textList(filters.Use) {
			base := source.Name + "_" + originalName
			name, suffix := base, 2
			for occupied[name] {
				name = fmt.Sprintf("%s (%d)", base, suffix)
				suffix++
			}
			occupied[name] = true
			definition := yamlx.Resolve(yamlx.Get(providers, originalName))
			if definition == nil || definition.Kind != yaml.MappingNode {
				return &ChainResourceError{ID: originalName}
			}
			validated, err := yamlx.Encode(definition)
			if err != nil {
				return err
			}
			provider := yamlx.CopyGraph(validated)
			yamlx.Set(provider, "path", yamlx.Str(fmt.Sprintf("./providers/%s_provider_%d.yaml", source.ID, index)))
			dialerName, err := ops.Resolve(config, model.Node{DialerRef: dialer})
			if err != nil {
				return err
			}
			if dialerName == nil {
				return &ChainResourceError{ID: dialer.ID}
			}
			override := yamlx.Map(yamlx.Str("additional-prefix"), yamlx.Str(name+"_"), yamlx.Str("dialer-proxy"), yamlx.Str(*dialerName))
			yamlx.Set(provider, "override", override)
			if filterValue != "" {
				yamlx.Set(provider, "filter", yamlx.Str(filterValue))
			}
			if len(patterns) > 0 {
				parts := make([]string, 0, len(patterns))
				for _, pattern := range patterns {
					parts = append(parts, "(?:"+pattern+")")
				}
				yamlx.Set(provider, "filter", yamlx.Str(strings.Join(parts, "|")))
				if captures == nil {
					for _, pattern := range patterns {
						compiled, err := rules.CompilePyRegex(pattern, false)
						if err != nil {
							return fmt.Errorf("compile chain provider filter: %w", err)
						}
						captures = append(captures, compiled.Groups())
					}
				}
				substitutions := yamlx.Seq()
				for rank, pattern := range patterns {
					substitutions.Content = append(substitutions.Content, yamlx.Map(yamlx.Str("pattern"), yamlx.Str(fmt.Sprintf(`^(?!%s)(?=[\s\S]*?(?:%s))([\s\S]*)$`, quoteProviderLiteral(orderTag), pattern)), yamlx.Str("target"), yamlx.Str(fmt.Sprintf("%s%d_$%d", orderTag, rank+1, captures[rank]+1))))
				}
				yamlx.Set(override, "proxy-name", substitutions)
			}
			if filters.ExcludeFilter != nil {
				existing := yamlx.Get(provider, "exclude-filter")
				if current, ok := dnsText(existing); ok && current != "" {
					if *filters.ExcludeFilter != "" {
						yamlx.Set(provider, "exclude-filter", yamlx.Str(current+"`"+*filters.ExcludeFilter))
					}
				} else {
					yamlx.Set(provider, "exclude-filter", yamlx.Str(*filters.ExcludeFilter))
				}
			}
			if filters.ExcludeType != nil {
				yamlx.Set(provider, "exclude-type", yamlx.Str(*filters.ExcludeType))
			}
			yamlx.Set(providers, name, provider)
			use.Content = append(use.Content, yamlx.Str(name))
		}
		if len(use.Content) > 0 {
			yamlx.Set(group, "use", use)
		}
		if len(patterns) > 0 && len(use.Content) > 0 {
			ordered := make([]string, 0, len(patterns))
			for rank, pattern := range patterns {
				var dynamic []string
				for _, name := range use.Content {
					dynamic = append(dynamic, "^"+namePattern(name.Value+"_"+orderTag+fmt.Sprint(rank+1)+"_"))
				}
				matching, err := ops.Filter(static, pattern, "", "")
				if err != nil {
					return referenceFailure(err)
				}
				for _, candidate := range matching {
					dynamic = append(dynamic, "^"+namePattern(candidate.Key)+"$")
				}
				ordered = append(ordered, strings.Join(dynamic, "|"))
			}
			yamlx.Set(group, "filter", yamlx.Str(strings.Join(ordered, "`")))
		}
	}
	return nil
}
