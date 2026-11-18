package convert

import (
	"context"
	"fmt"
	"strings"

	"configflow/internal/model"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// ResourceDownload is a typed generated resource, with deployment consumer metadata.
type ResourceDownload struct {
	Name      string
	URL       string
	LocalPath string
	Behavior  string
	Format    string
}

// MihomoProviderDownloads lists proxy providers from an owned generated document.
func MihomoProviderDownloads(ctx context.Context, config *model.Config, baseURL string, main *yamlx.Node) ([]ResourceDownload, error) {
	if main == nil {
		var err error
		main, err = MihomoDocument(ctx, config, MihomoOptions{BaseURL: baseURL, SkipProviderPreflight: true})
		if err != nil {
			return nil, err
		}
	}
	return documentDownloads(main, "proxy-providers", false)
}
func documentDownloads(main *yamlx.Node, key string, httpOnly bool) ([]ResourceDownload, error) {
	if _, err := yamlx.Encode(main); err != nil {
		return nil, err
	}
	out := []ResourceDownload{}
	providers := yamlx.Resolve(yamlx.Get(main, key))
	if providers == nil {
		return out, nil
	}
	if providers.Kind != yaml.MappingNode {
		return nil, fmt.Errorf(msgMihomoDownload, key)
	}
	for name, provider := range yamlx.Pairs(providers) {
		kind, _ := textNode(yamlx.Get(provider, "type"))
		if httpOnly && kind != "http" {
			continue
		}
		address := yamlx.Get(provider, "url")
		if address == nil || nullNode(address) {
			continue
		}
		url, ok := textNode(address)
		if !ok {
			return nil, fmt.Errorf(msgMihomoDownload, name)
		}
		if url == "" {
			continue
		}
		path, ok := textNode(yamlx.Get(provider, "path"))
		if !ok {
			return nil, fmt.Errorf(msgMihomoDownload, name)
		}
		item := ResourceDownload{Name: name, URL: url, LocalPath: path}
		if key == "rule-providers" {
			item.Behavior, _ = textNode(yamlx.Get(provider, "behavior"))
			item.Format = "yaml"
			if format := yamlx.Get(provider, "format"); format != nil {
				var ok bool
				item.Format, ok = textNode(format)
				if !ok {
					return nil, fmt.Errorf(msgMihomoDownload, name)
				}
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// MihomoRulesetDownloads lists the stable consumer resources for a typed profile.
func MihomoRulesetDownloads(ctx context.Context, config *model.Config, baseURL string) ([]ResourceDownload, error) {
	if config == nil || config.Profile == nil {
		return nil, fmt.Errorf("%s", msgMihomoConfig)
	}
	on, err := gatewayActive(config)
	if err != nil {
		return nil, err
	}
	if on {
		main, err := MihomoDocument(ctx, config, MihomoOptions{BaseURL: baseURL, SkipProviderPreflight: true})
		if err != nil {
			return nil, err
		}
		return documentDownloads(main, "rule-providers", true)
	}
	out := []ResourceDownload{}
	effective := strings.TrimSpace(config.SystemConfig.ServerDomain)
	if effective == "" {
		effective = baseURL
	}
	_, sets, err := MihomoRuleLists(config)
	if err != nil {
		return nil, err
	}
	for _, rule := range sets {
		if !enabled(rule.Enabled) {
			continue
		}
		url, original, err := ruleDownloadURL(config, rule, effective)
		if err != nil {
			return nil, err
		}
		format, err := RuleFormat(rule, config, original)
		if err != nil {
			return nil, err
		}
		extension := format
		if extension == "text" {
			extension = "list"
		}
		behavior := "domain"
		if rule.Behavior != nil {
			behavior = *rule.Behavior
		}
		out = append(out, ResourceDownload{Name: rule.Name, URL: url, LocalPath: "./ruleset/" + rule.Name + "." + extension, Behavior: behavior, Format: format})
	}
	return out, nil
}
