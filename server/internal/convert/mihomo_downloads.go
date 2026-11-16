package convert

import (
	"context"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
)

// Download is one entry of get_mihomo_provider_downloads /
// get_mihomo_ruleset_downloads: {'name', 'url', 'local_path'}.
type Download struct {
	Name      string
	URL       any
	LocalPath any
}

// Object is the Python dict of the entry.
func (d Download) Object() *jsonx.Object {
	return jsonx.Obj("name", d.Name, "url", d.URL, "local_path", d.LocalPath)
}

// GetMihomoProviderDownloads is get_mihomo_provider_downloads(config,
// base_url, main_config=main): every proxy provider of the generated
// configuration that has a URL. main nil generates it (preflight off).
func GetMihomoProviderDownloads(ctx context.Context, config *jsonx.Object, baseURL string, main *jsonx.Object) (out []Download, err error) {
	if main == nil {
		main, err = GenerateMihomoDocument(ctx, config, GenerateOptions{BaseURL: baseURL, SkipProviderPreflight: true})
		if err != nil {
			return nil, err
		}
	}
	defer catch(&err)
	return providerDownloads(main, "proxy-providers", false), nil
}

func providerDownloads(main *jsonx.Object, field string, httpOnly bool) []Download {
	out := []Download{}
	providers := dictGet(main, field, jsonx.NewObject())
	obj, ok := providers.(*jsonx.Object)
	if !ok {
		raise(attributeError(providers, "items"))
	}
	for name, provider := range obj.All() {
		if httpOnly && !jsonx.Equal(dictGet(provider, "type", nil), "http") {
			continue
		}
		if !jsonx.Truthy(dictGet(provider, "url", nil)) {
			continue
		}
		out = append(out, Download{Name: name, URL: dictItem(provider, "url"), LocalPath: dictItem(provider, "path")})
	}
	return out
}

// GetMihomoRulesetDownloads is get_mihomo_ruleset_downloads(config, base_url):
// the rule-set files a Mihomo device downloads. With the Linux gateway on, the
// generated rule-providers are used (their stable URLs).
func GetMihomoRulesetDownloads(ctx context.Context, config *jsonx.Object, baseURL string) (out []Download, err error) {
	enabled, err := gatewayEnabled(config)
	if err != nil {
		return nil, err
	}
	if enabled {
		main, err := GenerateMihomoDocument(ctx, config, GenerateOptions{BaseURL: baseURL, SkipProviderPreflight: true})
		if err != nil {
			return nil, err
		}
		defer catch(&err)
		return providerDownloads(main, "rule-providers", true), nil
	}
	defer catch(&err)
	out = []Download{}
	serverDomain := pystr.Strip(strMethod(dictGet(dictGet(config, "system_config", jsonx.NewObject()), "server_domain", ""), "strip"))
	effectiveBaseURL := serverDomain
	if effectiveBaseURL == "" {
		effectiveBaseURL = baseURL
	}
	_, ruleSets := splitRulesAndRulesets(config)
	ruleLibrary := dictGet(config, "rule_library", []any{})
	for _, ruleSet := range iterate(ruleSets) {
		if !jsonx.Truthy(dictGet(ruleSet, "enabled", true)) {
			continue
		}
		url, originalURL := mihomoRulesetURL(config, ruleSet, ruleLibrary, effectiveBaseURL)
		ruleFormat := mustValue(RulesetFormat(ruleSet.(*jsonx.Object), config, originalURL))
		extension := ruleFormat
		if ruleFormat == "text" {
			extension = "list"
		}
		name := dictItem(ruleSet, "name")
		out = append(out, Download{Name: providerKey(name), URL: url, LocalPath: "./ruleset/" + fstr(name) + "." + extension})
	}
	return out, nil
}
