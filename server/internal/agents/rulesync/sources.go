package rulesync

import (
	"net/url"
	"slices"
	"strings"

	"configflow/internal/deploy"
	"configflow/internal/yamlx"
)

// RuleEntry describes one generated provider download.
type RuleEntry struct {
	Name      string `json:"name,omitempty"`
	URL       string `json:"url"`
	LocalPath string `json:"local_path"`
}

// RuleSourceFingerprint describes served source identity without fetching it.
// Unknown or recursive callbacks return nil so callers refresh conservatively.
func RuleSourceFingerprint(rawURL string, source *deploy.Source, baseURL string) []string {
	return sourceFingerprint(rawURL, source, baseURL, 0)
}
func sourceFingerprint(rawURL string, source *deploy.Source, baseURL string, depth int) []string {
	if depth > 3 || rawURL == "" {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	if (parsed.Scheme != "" || parsed.Host != "") && !source.InternalRuleURL(rawURL, baseURL) {
		return []string{"url", rawURL}
	}
	if strings.HasSuffix(parsed.Path, "/mosdns/rule-proxy") || strings.HasSuffix(parsed.Path, "/oxidns/rule-proxy") {
		inner := sourceFingerprint(parsed.Query().Get("url"), source, baseURL, depth+1)
		if inner == nil {
			return nil
		}
		return append([]string{"proxy"}, inner...)
	}
	rule, err := deploy.LocalRule(rawURL, source, baseURL, false)
	if err != nil || rule == nil {
		return nil
	}
	shape := []string{rule.Format, rule.Behavior}
	if rule.SourceType == "content" {
		return append(append([]string{"content"}, shape...), digest([]byte(rule.Content)))
	}
	if rule.URL == rawURL {
		return nil
	}
	inner := sourceFingerprint(rule.URL, source, baseURL, depth+1)
	if inner == nil {
		return nil
	}
	return append(append([]string{"library"}, shape...), inner...)
}

// UnchangedRulePaths finds unique files with an unchanged URL and served source.
func UnchangedRulePaths(oldEntries, newEntries []RuleEntry, base, current *deploy.Source, baseURL string) map[string]bool {
	old := map[string]map[string]bool{}
	for _, e := range oldEntries {
		p, err := deploy.RelativePath(e.LocalPath)
		if err != nil {
			continue
		}
		if old[p] == nil {
			old[p] = map[string]bool{}
		}
		old[p][e.URL] = true
	}
	counts := map[string]int{}
	for _, e := range newEntries {
		p, err := deploy.RelativePath(e.LocalPath)
		if err == nil {
			counts[p]++
		}
	}
	out := map[string]bool{}
	for _, e := range newEntries {
		p, err := deploy.RelativePath(e.LocalPath)
		if err != nil || counts[p] != 1 || len(old[p]) != 1 || !old[p][e.URL] {
			continue
		}
		before := RuleSourceFingerprint(e.URL, base, baseURL)
		if before != nil && slices.Equal(before, RuleSourceFingerprint(e.URL, current, baseURL)) {
			out[p] = true
		}
	}
	return out
}

// MihomoRuleEntries reads HTTP providers from the final main document.
func MihomoRuleEntries(doc *yamlx.Node) []RuleEntry {
	out := []RuleEntry{}
	providers := get(doc, "rule-providers")
	for _, name := range keys(providers) {
		p := get(providers, name)
		u, path := get(p, "url"), get(p, "path")
		if text(get(p, "type")) == "http" && u != nil && path != nil && u.ShortTag() == "!!str" && path.ShortTag() == "!!str" {
			out = append(out, RuleEntry{Name: name, URL: text(u), LocalPath: text(path)})
		}
	}
	return out
}
