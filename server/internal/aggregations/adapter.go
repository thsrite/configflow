package aggregations

import (
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/convert"
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
)

// Fetch provides one subscription body without publishing a provider artifact.
type Fetch = refresh.FetchSourceFn

// Generate decodes the native JSON entry point retained for export consumers.
// Resource and profile records keep unknown stored members through model codecs.
func Generate(ctx context.Context, rawAggregation, rawConfig json.RawMessage, options Options) (*Result, error) {
	var agg model.Aggregation
	if err := json.Unmarshal(rawAggregation, &agg); err != nil {
		return nil, err
	}
	var profile model.Profile
	if err := json.Unmarshal(rawConfig, &profile); err != nil {
		return nil, err
	}
	// Decode the captured flat API document using each model owner's codec.
	var shared model.Shared
	if err := json.Unmarshal(rawConfig, &shared); err != nil {
		return nil, err
	}
	var system model.System
	if err := json.Unmarshal(rawConfig, &system); err != nil {
		return nil, err
	}
	var identity struct {
		ProfileID string `json:"profile_id"`
	}
	if err := jsondecode.Unmarshal(rawConfig, &identity); err != nil {
		return nil, err
	}
	config := &model.Config{Profile: &profile, ProfileID: identity.ProfileID, SystemConfig: system.SystemConfig, Backup: system.Backup, Subscriptions: shared.Subscriptions, Nodes: shared.Nodes, Aggregations: shared.Aggregations, RuleLibrary: shared.RuleLibrary}
	if !options.Persist {
		var request map[string]json.RawMessage
		if err := json.Unmarshal(rawAggregation, &request); err != nil {
			return nil, err
		}
		var name string
		if value := request["name"]; len(value) == 0 || string(value) == "null" || json.Unmarshal(value, &name) != nil {
			return nil, errors.New("aggregation name must be a string")
		}
		if value, present := request["subscriptions"]; present && string(value) == "null" {
			return nil, errors.New("aggregation subscriptions must be an array")
		}
		if value, present := request["regex_filter"]; present {
			var filter string
			if string(value) == "null" || json.Unmarshal(value, &filter) != nil {
				return nil, errors.New("aggregation regex must be a string")
			}
		}
	}
	return GenerateConfig(ctx, agg, config, options)
}

// GenerateConfig validates a frozen main before fetching sources. Persist uses
// the shared all-or-nothing provider bundle; dry renders never commit.
func GenerateConfig(ctx context.Context, agg model.Aggregation, config *model.Config, options Options) (*Result, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.Format != "" {
		ctx = WithFormat(ctx, options.Format)
	} else {
		options.Format, _ = ctx.Value(formatKey{}).(string)
	}
	if config == nil || config.Profile == nil {
		return nil, errors.New("aggregation config must include a profile")
	}
	if err := convert.ConfigDialers(config); err != nil {
		return nil, convert.DialerFailure(err)
	}
	profileID := config.ProfileID
	var err error
	if options.Persist {
		profileID, err = repo.ResolveProfileID(ctx, profileID)
	} else if profileID == "" {
		profileID, err = repo.GetDefaultProfileID(ctx)
	}
	if err != nil {
		return nil, err
	}
	ctx = convert.EnsureConversions(ctx)
	var main *yamlx.Node
	if len(options.Main) > 0 && string(options.Main) != "null" {
		main, err = yamlx.FromJSON(options.Main)
	} else {
		main, err = convert.MihomoDocument(ctx, config, convert.MihomoOptions{SkipProviderPreflight: true})
	}
	if err != nil {
		return nil, err
	}
	if options.Persist {
		return publish(ctx, agg, config, main, profileID, options)
	}
	if model.IssueUnder(model.Issues(&agg), "name") {
		return nil, errors.New("aggregation name must be a string")
	}
	a := aggregation{resource: typedResource(&agg, agg.ID, agg.Name, agg.Enabled), Subscriptions: agg.Subscriptions, Nodes: agg.Nodes}
	for _, issue := range model.Issues(&agg) {
		if issue.Path == "subscriptions" {
			return nil, errors.New("aggregation subscriptions must be an array")
		}
		if issue.Path == "nodes" {
			return nil, errors.New("aggregation nodes must be an array")
		}
	}
	if agg.RegexFilter != nil {
		a.Regex, err = json.Marshal(*agg.RegexFilter)
		if err != nil {
			return nil, err
		}
	}
	snapshot, err := provider.CaptureDelivery(profileID, main, config.Profile)
	if err != nil {
		return nil, err
	}
	return render(ctx, a, newTypedDelivery(config, main, snapshot), options)
}

func publish(ctx context.Context, agg model.Aggregation, config *model.Config, main *yamlx.Node, profileID string, options Options) (*Result, error) {
	bundle, err := provider.PrepareBundle(ctx, config, main, provider.DeliveryOptions{Requested: &provider.BundleRequest{Kind: "aggregation", Aggregation: &agg}, SubscriptionFetch: options.Fetch})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var found *provider.Artifact
	for _, item := range bundle {
		if item.Name == agg.Name {
			found = item
			break
		}
	}
	if found == nil {
		return nil, errors.New("aggregation absent from validated provider bundle")
	}
	if err := provider.CommitArtifacts(ctx, profileID, bundle); err != nil {
		return nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	path, err := r.ProfilePath(profileID, "providers/"+agg.ID+".yaml")
	if err != nil {
		return nil, err
	}
	result := &Result{Name: found.Name, Content: found.Content, FilePath: &path, SubscriptionNodeCounts: map[string]int{}}
	if len(found.Extra) > 0 {
		if err := json.Unmarshal(found.Extra, result); err != nil {
			return nil, err
		}
		result.FilePath, result.Content = &path, found.Content
	}
	values, err := provider.ParseNodes(found.Content, found.Name, path)
	if err != nil {
		return nil, err
	}
	result.Proxies = make([]Proxy, 0, len(values))
	for _, value := range values {
		proxy, err := graphProxy(value)
		if err != nil {
			return nil, err
		}
		result.Proxies = append(result.Proxies, proxy)
	}
	result.TotalCount = len(result.Proxies)
	return result, nil
}

// Wire registers aggregation rendering for conversions and provider bundles.
func Wire() { provider.SetAggregationRenderer(RenderProvider) }

// LoadCatalog reads an owned shared snapshot for SaveCatalog.
func LoadCatalog(ctx context.Context) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	document, err := repo.SharedConfig(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(document)
}

// SaveCatalog commits an edit against its request-scoped repository baseline.
func SaveCatalog(ctx context.Context, raw json.RawMessage) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	var document model.Shared
	if err := json.Unmarshal(raw, &document); err != nil {
		return err
	}
	return repo.SaveSharedRecord(ctx, &document, nil)
}

// AppendCatalog adds an aggregation in one shared repository transaction.
func AppendCatalog(ctx context.Context, raw json.RawMessage) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	var item model.Aggregation
	if err := json.Unmarshal(raw, &item); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	return r.UpdateShared(ctx, func(ctx context.Context, shared *model.Shared) error {
		shared.Aggregations = append(shared.Aggregations, item)
		return ctx.Err()
	})
}
