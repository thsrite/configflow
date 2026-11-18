package mosdns

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"configflow/internal/model"
	"configflow/internal/weburl"
	"configflow/internal/yamlx"
)

// SourceFile is a generated rule resource. An empty URL means inline content.
type SourceFile struct {
	Part      string  `json:"part"`
	URL       *string `json:"url"`
	LocalPath string  `json:"local_path"`
}

// RuleBatch groups adjacent selected rules that share one route and matcher.
type RuleBatch struct {
	Tag   string
	Part  string
	Path  string
	IDs   []string
	Lines []string
}

type ruleLists struct {
	Rules    []model.RuleConfig `json:"rules"`
	RuleSets []model.RuleConfig `json:"rule_sets"`
	Fields   model.Fields
	raw      model.Raw
}

func (r *ruleLists) UnmarshalJSON(data []byte) error { return model.DecodeRecord(data, r, &r.raw) }

func flag(value *bool) bool { return value == nil || *value }
func textValue(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}
func idSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// SplitRules returns enabled rules and providers from the captured typed view.
// Historical rules/rule_sets members are decoded with the same shared R5 codec.
func SplitRules(config *model.Config) ([]model.RuleConfig, []model.RuleConfig, error) {
	rules, sets := []model.RuleConfig{}, []model.RuleConfig{}
	if config == nil || config.Profile == nil {
		return rules, sets, nil
	}
	for _, item := range config.Profile.RuleConfigs {
		if !flag(item.Enabled) || !flag(item.LibraryEnabled) {
			continue
		}
		switch item.ItemType {
		case "rule":
			rules = append(rules, item)
		case "ruleset":
			sets = append(sets, item)
		}
	}
	if len(config.Profile.RuleConfigs) > 0 {
		return rules, sets, nil
	}
	var fields model.Fields
	for _, name := range []string{"rules", "rule_sets"} {
		if data, ok := config.Profile.Fields.Get(name); ok {
			if err := fields.Set(name, data); err != nil {
				return nil, nil, err
			}
		}
	}
	data, err := fields.MarshalJSON()
	if err != nil {
		return nil, nil, err
	}
	var old ruleLists
	if err = json.Unmarshal(data, &old); err != nil {
		return nil, nil, err
	}
	if old.Rules == nil {
		old.Rules = []model.RuleConfig{}
	}
	if old.RuleSets == nil {
		old.RuleSets = []model.RuleConfig{}
	}
	return old.Rules, old.RuleSets, nil
}

func orderedRuleIDs(config *model.Config, sets []model.RuleConfig) []string {
	result := []string{}
	if config != nil && config.Profile != nil {
		for _, item := range config.Profile.RuleConfigs {
			if item.ItemType == "ruleset" && item.ID != "" {
				result = append(result, item.ID)
			}
		}
	}
	if len(result) == 0 {
		for _, item := range sets {
			if item.ID != "" {
				result = append(result, item.ID)
			}
		}
		return result
	}
	known := idSet(result)
	for _, item := range sets {
		if item.ID != "" && !known[item.ID] {
			result = append(result, item.ID)
			known[item.ID] = true
		}
	}
	return result
}

// SingleRuleBatches returns ordered resource groups without widening adjacency.
func SingleRuleBatches(config *model.Config) ([]RuleBatch, error) {
	result := []RuleBatch{}
	if config == nil || config.Profile == nil {
		return result, nil
	}
	settings := config.Profile.MosDNS
	direct, proxy := idSet(settings.DirectRules), idSet(settings.ProxyRules)
	entries := config.Profile.RuleConfigs
	if len(entries) == 0 {
		var err error
		entries, _, err = SplitRules(config)
		if err != nil {
			return nil, err
		}
	}
	counts := map[string]int{}
	previous := ""
	for _, item := range entries {
		if (item.ItemType != "" && item.ItemType != "rule") || !flag(item.Enabled) || !flag(item.LibraryEnabled) || (!direct[item.ID] && !proxy[item.ID]) {
			previous = ""
			continue
		}
		part, value := "", item.Value
		if prefix, ok := clashDomainPrefix(item.RuleType); ok {
			part = PartDomain
			value = prefix + ":" + value
		} else if inList(item.RuleType, clashIPTypes) {
			part = PartIP
		} else {
			previous = ""
			continue
		}
		base := "proxy"
		if direct[item.ID] {
			base = "direct"
		}
		if part == PartIP {
			base += "_ip_rules"
		} else {
			base += "_rules"
		}
		if previous != base {
			counts[base]++
			tag := base
			if counts[base] > 1 {
				tag += "_" + strconv.Itoa(counts[base])
			}
			result = append(result, RuleBatch{Tag: tag, Part: part, Path: "./rules/" + tag + ".txt", IDs: []string{}, Lines: []string{}})
		}
		last := &result[len(result)-1]
		last.IDs = append(last.IDs, item.ID)
		last.Lines = append(last.Lines, value)
		previous = base
	}
	return result, nil
}

type sourceIdentity struct{ LibraryID, Name, Behavior, URL string }
type nativeLayout struct {
	sets        map[string]model.RuleConfig
	order       []string
	tags, stems map[string]string
	batches     []RuleBatch
}

func layoutFor(config *model.Config) (nativeLayout, error) {
	_, sets, err := SplitRules(config)
	if err != nil {
		return nativeLayout{}, err
	}
	batches, err := SingleRuleBatches(config)
	if err != nil {
		return nativeLayout{}, err
	}
	out := nativeLayout{sets: map[string]model.RuleConfig{}, order: orderedRuleIDs(config, sets), tags: map[string]string{}, stems: map[string]string{}, batches: batches}
	for _, item := range sets {
		if item.ID != "" {
			out.sets[item.ID] = item
		}
	}
	used := idSet(builtinTags)
	for _, batch := range batches {
		used[batch.Tag] = true
	}
	sources := map[sourceIdentity]string{}
	for _, id := range out.order {
		item := out.sets[id]
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = strings.TrimPrefix(id, "ruleset_")
		}
		suffixes := []string{""}
		if textValue(item.Behavior, "domain") == "classical" {
			suffixes = append(suffixes, "_ip")
		}
		tag := name
		for index := 2; ; index++ {
			taken := false
			for _, suffix := range suffixes {
				if used[tag+suffix] {
					taken = true
					break
				}
			}
			if !taken {
				break
			}
			tag = name + "_" + strconv.Itoa(index)
		}
		for _, suffix := range suffixes {
			used[tag+suffix] = true
		}
		out.tags[id] = tag
		source := sourceIdentity{item.LibraryRuleID, item.Name, textValue(item.Behavior, "domain"), textValue(item.URL, "")}
		if existing, ok := sources[source]; ok {
			out.stems[id] = existing
		} else {
			out.stems[id] = tag
			sources[source] = tag
		}
	}
	return out, nil
}

// RuleProxyURL constructs a profile-scoped provider URL in protocol query order.
func RuleProxyURL(config *model.Config, baseURL, sourceURL, part, sourceID string) (string, error) {
	if config == nil || config.SystemConfig.RuleProxyToken == "" {
		return "", ErrRuleProxyToken
	}
	endpoint := "/api/mosdns/rule-proxy"
	if config.ProfileID != "" {
		endpoint = "/api/profiles/" + url.PathEscape(config.ProfileID) + "/mosdns/rule-proxy"
	}
	pairs := [][2]string{{"url", sourceURL}}
	if sourceID != "" {
		pairs = append(pairs, [2]string{"source_id", sourceID})
	}
	if part != "" {
		pairs = append(pairs, [2]string{"part", part})
	}
	pairs = append(pairs, [2]string{"token", config.SystemConfig.RuleProxyToken})
	parsed, err := weburl.Parse(baseURL + endpoint)
	if err != nil {
		return "", err
	}
	query := strings.ReplaceAll(weburl.EncodeQuery(pairs), "+", "%20")
	if parsed.RawQuery != "" {
		query = parsed.RawQuery + "&" + query
	}
	parsed.RawQuery = query
	return parsed.String(), nil
}

// RuleFiles describes each native domain/IP part required by one provider.
func RuleFiles(config *model.Config, baseURL string, item model.RuleConfig, stem string) ([]SourceFile, error) {
	behavior := textValue(item.Behavior, "domain")
	sourceURL := textValue(item.URL, "")
	if strings.HasPrefix(sourceURL, "/") && baseURL != "" {
		sourceURL = baseURL + sourceURL
	}
	sourceID := item.LibraryRuleID
	if sourceID == "" {
		sourceID = item.ID
	}
	if stem == "" {
		stem = item.Name
	}
	files := []SourceFile{}
	for _, part := range Parts(behavior) {
		suffix := ""
		if part == PartIP && behavior == "classical" {
			suffix = "_ip"
		}
		file := SourceFile{Part: part, LocalPath: "./rules/" + stem + suffix + ".txt"}
		if sourceURL != "" {
			queryPart := part
			if behavior != "ipcidr" && behavior != "classical" {
				queryPart = ""
			}
			value, err := RuleProxyURL(config, baseURL, sourceURL, queryPart, sourceID)
			if err != nil {
				return nil, err
			}
			file.URL = &value
		}
		files = append(files, file)
	}
	return files, nil
}

// MatchEntries projects typed editor matches to MosDNS sequence nodes.
func MatchEntries(matches []model.MosDNSMatch) []*yamlx.Node {
	out := []*yamlx.Node{}
	for _, item := range matches {
		if !flag(item.Enabled) {
			continue
		}
		action := strings.TrimSpace(item.Exec)
		if action == "" {
			continue
		}
		lines := item.Matches.Lines
		if item.Matches.Text != nil {
			lines = strings.Split(strings.ReplaceAll(*item.Matches.Text, "\r\n", "\n"), "\n")
		}
		values := []*yamlx.Node{}
		for _, line := range lines {
			if value := strings.TrimSpace(line); value != "" {
				values = append(values, yamlx.Str(value))
			}
		}
		if len(values) > 0 {
			out = append(out, yamlx.Map(yamlx.Str("matches"), yamlx.Seq(values...), yamlx.Str("exec"), yamlx.Str(action)))
		}
	}
	return out
}

// PublishGuard identifies the draft revision a later publish is allowed to use.
type PublishGuard struct {
	ProfileID      string `json:"expected_profile_id"`
	ServiceType    string `json:"expected_service_type"`
	ConfigRevision int64  `json:"expected_config_revision"`
}

// ErrRuleProxyToken identifies a missing rule-provider capability.
var ErrRuleProxyToken=errors.New(msgProxyTokenRequired)
