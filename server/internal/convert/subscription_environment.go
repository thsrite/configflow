package convert

import (
	"context"
	"errors"

	"configflow/internal/health/check"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
)

func subsError(err error) error {
	var reference *subs.DialerReferenceError
	if errors.As(err, &reference) {
		return &DialerReferenceError{Message: reference.Message, cause: err}
	}
	return err
}

func defaultClientCollaborators() ClientCollaborators {
	return ClientCollaborators{
		ParseNodes: subscriptionSources,
		FilterNodes: func(nodes []model.Node, include, exclude *string) ([]model.Node, error) {
			return rules.Filter(nodes, include, exclude, func(node model.Node) string { return node.Name })
		},
	}
}

// DefaultMihomoEnvironment wires all typed config-domain collaborators without
// legacy JSON documents. Production callers (convert/mihomoenv) attach storage,
// Sub-Store, refresh interval, and provider preflight callbacks on top.
func DefaultMihomoEnvironment() MihomoEnvironment {
	env := MihomoEnvironment{Clients: defaultClientCollaborators()}
	bindSubscriptionEnvironment(&env)
	env.RenderMembers = func(group model.ProxyGroup, config *model.Config) (*yamlx.Node, error) {
		return rules.RenderGroupDocument(group, rules.GroupResources(config)), nil
	}
	env.ProviderHealth = func(health *model.ProviderHealthCheck) (*yamlx.Node, error) {
		raw, err := model.Marshal(health)
		if err != nil {
			return nil, err
		}
		if err := check.ValidateHealthJSON(raw, true); err != nil {
			return nil, err
		}
		var fields model.Fields
		if err := fields.Set("health_check", raw); err != nil {
			return nil, err
		}
		resolved, err := check.ResolveProviderHealth(&fields)
		if err != nil {
			return nil, err
		}
		data, err := jsonresp.Encode(resolved)
		if err != nil {
			return nil, err
		}
		return yamlx.FromJSON(data)
	}
	env.FilterGroups = func(config *model.Config, document *yamlx.Node) error {
		return rules.LowerProviderFilters(rules.GroupResources(config), document)
	}
	env.ValidateRules = func(config *model.Config, names []string, client string) error {
		return rules.ValidatePolicies(config.Profile.RuleConfigs, names, client)
	}
	return env
}

// bindSubscriptionEnvironment connects native subscription owners directly. The
// remaining legacy environment adapters belong to the rules/provider owners;
// these callbacks never transcode an entire Config or document per resource.
func bindSubscriptionEnvironment(env *MihomoEnvironment) {
	env.LowerChains = func(config *model.Config) (*model.Config, error) {
		result, err := subs.LowerChains(config)
		return result, subsError(err)
	}
	env.ChainProviders = func(config *model.Config, document *yamlx.Node) error {
		return subsError(subs.ChainProviders(config, document))
	}
	env.MergeHosts = func(ctx context.Context, document *yamlx.Node, config *model.Config, userKeys, mosdnsKeys []string, inline []InlineNode, load CacheReader) (*HostsResult, error) {
		return mergeHostsWithReader(ctx, document, config, userKeys, mosdnsKeys, inline, nil, load)
	}
	env.NodeDNS = func(document, merged *yamlx.Node, version string) (*yamlx.Node, error) {
		report, err := subs.ApplyDNS(document, merged, version)
		if err != nil || report == nil {
			return nil, err
		}
		data, err := jsonresp.Encode(report)
		if err != nil {
			return nil, err
		}
		return yamlx.FromJSON(data)
	}
	env.LogHosts = func(profileID string, document *yamlx.Node) error {
		data, err := yamlx.ToJSON(document)
		if err != nil {
			return err
		}
		var report subs.HostReport
		if err := jsondecode.Unmarshal(data, &report); err != nil {
			return err
		}
		return subs.LogHostReport(profileID, &report)
	}
	env.DialerGroups = func(ctx context.Context, document *yamlx.Node, config *model.Config, deps DialerCollaborators) (*yamlx.Node, error) {
		inputs := subs.DialerInputs{Convert: deps.Convert, DownloadedText: deps.DownloadedText}
		if deps.LoadCacheTyped != nil {
			inputs.LoadCache = deps.LoadCacheTyped
		} else if deps.LoadCache != nil {
			inputs.LoadCache = func(ctx context.Context, id string) (*subs.SubscriptionCache, error) {
				data, err := deps.LoadCache(ctx, id)
				if err != nil {
					return nil, err
				}
				if len(data) == 0 || string(data) == "null" {
					return nil, nil
				}
				return decodedSubscriptionCache(data)
			}
		}
		report, err := subs.AddGroups(ctx, document, config, inputs)
		if err != nil {
			return nil, subsError(err)
		}
		if report == nil {
			return nil, nil
		}
		data, err := jsonresp.Encode(report)
		if err != nil {
			return nil, err
		}
		return yamlx.FromJSON(data)
	}
	env.ValidateListeners = subs.ValidateListenerNode
}

func mergeHostsWithReader(ctx context.Context, document *yamlx.Node, config *model.Config, userKeys, mosdnsKeys []string, inline []InlineNode, loadTyped TypedCacheReader, load CacheReader) (*HostsResult, error) {
	values := make([]subs.HostInline, 0, len(inline))
	for _, item := range inline {
		values = append(values, subs.HostInline{Node: item.Node, Proxy: item.Proxy})
	}
	report, merged, err := subs.MergeHosts(document, config, userKeys, mosdnsKeys, values, func(sub model.Subscription) (*subs.HostIndex, error) {
		var cache *subs.SubscriptionCache
		if loadTyped != nil {
			var err error
			cache, err = loadTyped(ctx, sub.ID)
			if err != nil || cache == nil {
				return nil, err
			}
		} else {
			if load == nil {
				return nil, &HookMissingError{Hook: "load_subscription_cache", Provider: "convert/mihomoenv"}
			}
			data, err := load(ctx, sub.ID)
			if err != nil {
				return nil, err
			}
			if len(data) == 0 || string(data) == "null" {
				return nil, nil
			}
			cache, err = decodedSubscriptionCache(data)
			if err != nil {
				return nil, err
			}
		}
		index, err := subs.NewHostIndex(cache)
		if err != nil {
			return nil, err
		}
		return index.ForSubscription(sub), nil
	})
	if err != nil {
		return nil, err
	}
	data, err := jsonresp.Encode(report)
	if err != nil {
		return nil, err
	}
	graph, err := yamlx.FromJSON(data)
	if err != nil {
		return nil, err
	}
	return &HostsResult{Report: graph, Merged: merged}, nil
}
