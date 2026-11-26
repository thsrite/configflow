package convert

import (
	"fmt"
	"net/netip"
	"strings"

	"configflow/internal/model"
	"configflow/internal/weburl"
)

// appendProfileQuery composes panel-generated paths without re-escaping their
// product path names. Query pairs retain order, duplicates and blank values.
func appendProfileQuery(address string, extra [][2]string) (string, error) {
	rest, fragment, hasFragment := strings.Cut(address, "#")
	base, query, _ := strings.Cut(rest, "?")
	if _, after, ok := strings.Cut(base, "//"); ok {
		authority, _, _ := strings.Cut(after, "/")
		if strings.Contains(authority, "[") != strings.Contains(authority, "]") {
			return "", fmt.Errorf("%s", msgMihomoURL)
		}
		if at := strings.LastIndexByte(authority, '@'); at >= 0 {
			authority = authority[at+1:]
		}
		if start := strings.IndexByte(authority, '['); start >= 0 {
			literal, _, _ := strings.Cut(authority[start+1:], "]")
			parsed, err := weburl.Parse("http://[" + literal + "]")
			if err != nil {
				return "", fmt.Errorf("%s", msgMihomoURL)
			}
			addr, err := netip.ParseAddr(parsed.Hostname())
			if err != nil || !addr.Is6() {
				return "", fmt.Errorf("%s", msgMihomoURL)
			}
		}
	}
	pairs := [][2]string{}
	for _, item := range strings.Split(query, "&") {
		if item == "" {
			continue
		}
		key, value, _ := strings.Cut(item, "=")
		pairs = append(pairs, [2]string{weburl.UnquotePlus(key), weburl.UnquotePlus(value)})
	}
	pairs = append(pairs, extra...)
	result := base
	if len(pairs) > 0 {
		encoded := make([]string, len(pairs))
		for i, pair := range pairs {
			encoded[i] = weburl.Quote(pair[0]) + "=" + weburl.Quote(pair[1])
		}
		result += "?" + strings.Join(encoded, "&")
	}
	if hasFragment && fragment != "" {
		result += "#" + fragment
	}
	return result, nil
}
func ruleDownloadURL(config *model.Config, rule model.RuleConfig, baseURL string) (string, string, error) {
	address := stringValue(rule.URL)
	original := address
	if rule.LibraryRuleID != "" {
		for _, library := range config.RuleLibrary {
			if library.ID != rule.LibraryRuleID {
				continue
			}
			if library.URL != nil {
				original = *library.URL
			}
			if library.Name != "" {
				address = profilePath(config, "/rules/local/"+library.Name)
				format, err := RuleFormat(rule, config, original)
				if err != nil {
					return "", "", err
				}
				if format != "mrs" {
					address, err = appendProfileQuery(address, [][2]string{{"target", "mihomo"}})
					if err != nil {
						return "", "", err
					}
				}
			}
			break
		}
	}
	if strings.HasPrefix(address, "/") && baseURL != "" {
		address = baseURL + address
	} else if strings.HasPrefix(address, "http") {
		value, err := GitHubURL(address, config)
		if err != nil {
			return "", "", err
		}
		address = value
	}
	return address, original, nil
}
