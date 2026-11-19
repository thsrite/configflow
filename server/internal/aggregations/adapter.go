package aggregations

// This is the boundary to the repository and existing provider collaborators.
// Collection/rendering code uses native types and does not access legacy values.
import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

// Fetch provides one subscription body without publishing a provider artifact.
type Fetch = refresh.SourceFetch

type delivery struct {
	config, main *jsonx.Object
	profileID    string
}

func legacyObject(raw json.RawMessage) (*jsonx.Object, error) {
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil {
		return nil, errors.New("aggregation document must be an object")
	}
	return object, nil
}
func native(value any, target any) error {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(raw, target)
}
func projection(value *jsonx.Object) (resource, error) {
	raw, err := jsonx.Compact(value)
	return resource{ID: value.GetString("id"), Name: value.GetString("name"), Enabled: jsonx.Truthy(value.GetOr("enabled", true)), raw: raw}, err
}
func selectedIDs(object *jsonx.Object, key string) ([]string, error) {
	value, present := object.Get(key)
	if !present || value == nil && key == "nodes" {
		return []string{}, nil
	}
	if value == nil {
		return nil, fmt.Errorf("aggregation %s must be an array", key)
	}
	var ids []string
	if err := native(value, &ids); err != nil {
		return nil, fmt.Errorf("decode aggregation %s: %w", key, err)
	}
	return ids, nil
}

// Generate validates a frozen main before fetching sources. Persist publishes
// through the shared all-or-nothing provider bundle; dry renders never commit.
func Generate(ctx context.Context, rawAggregation, rawConfig json.RawMessage, options Options) (*Result, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.Format != "" {
		ctx = WithFormat(ctx, options.Format)
	} else {
		options.Format, _ = ctx.Value(formatKey{}).(string)
	}
	agg, err := legacyObject(rawAggregation)
	if err != nil {
		return nil, err
	}
	config, err := legacyObject(rawConfig)
	if err != nil {
		return nil, err
	}
	if err := convert.ValidateDialers(config); err != nil {
		return nil, err
	}
	profileID := config.GetString("profile_id")
	if options.Persist {
		profileID, err = repo.ResolveProfileID(ctx, profileID)
	} else if profileID == "" {
		profileID, err = repo.GetDefaultProfileID(ctx)
	}
	if err != nil {
		return nil, err
	}
	ctx = convert.EnsureNodeConversions(ctx)
	var main *jsonx.Object
	if len(options.Main) > 0 && string(options.Main) != "null" {
		main, err = legacyObject(options.Main)
	} else {
		main, err = convert.GenerateMihomoDocument(ctx, config, convert.GenerateOptions{SkipProviderPreflight: true})
	}
	if err != nil {
		return nil, err
	}
	d := &delivery{config: config, main: main, profileID: profileID}
	if options.Persist {
		return d.publish(ctx, agg, options)
	}
	if _, ok := agg.Value("name").(string); !ok {
		return nil, errors.New("aggregation name must be a string")
	}
	base, err := projection(agg)
	if err != nil {
		return nil, err
	}
	a := aggregation{resource: base}
	a.Subscriptions, err = selectedIDs(agg, "subscriptions")
	if err != nil {
		return nil, err
	}
	a.Nodes, err = selectedIDs(agg, "nodes")
	if err != nil {
		return nil, err
	}
	if agg.Has("regex_filter") {
		a.Regex, err = jsonx.Compact(agg.Value("regex_filter"))
		if err != nil {
			return nil, err
		}
	}
	var profile model.Profile
	if err := native(jsonx.Obj("proxy_groups", config.GetList("proxy_groups")), &profile); err != nil {
		return nil, err
	}
	typed := &model.Config{ProfileID: profileID, Profile: &profile}
	if err := native(config.GetList("subscriptions"), &typed.Subscriptions); err != nil {
		return nil, err
	}
	if err := native(config.GetList("nodes"), &typed.Nodes); err != nil {
		return nil, err
	}
	if err := native(config.GetList("subscription_aggregations"), &typed.Aggregations); err != nil {
		return nil, err
	}
	mainJSON, err := jsonx.Compact(main)
	if err != nil {
		return nil, err
	}
	graph, err := yamlx.FromJSON(mainJSON)
	if err != nil {
		return nil, err
	}
	snapshot, err := provider.CaptureDelivery(profileID, graph, &profile)
	if err != nil {
		return nil, err
	}
	return render(ctx, a, newTypedDelivery(typed, graph, snapshot), options)
}

func defaultFetch(ctx context.Context, id, url string) (*substore.SubscriptionResult, error) {
	return substore.Default().Client.GetSubscriptionProxiesYAML(ctx, id, url, "", substore.FetchOptions{})
}

func (d *delivery) publish(ctx context.Context, agg *jsonx.Object, options Options) (*Result, error) {
	bundle, err := provider.PrepareProviderBundle(ctx, d.config, d.main, provider.BundleOptions{Requested: &provider.Requested{Kind: "aggregation", Resource: agg}, SubscriptionFetch: options.Fetch})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var found *provider.Rendered
	for _, item := range bundle {
		if item.Name == agg.GetString("name") {
			found = item
			break
		}
	}
	if found == nil {
		return nil, errors.New("aggregation absent from validated provider bundle")
	}
	if err := provider.CommitProviderBundle(ctx, d.profileID, bundle); err != nil {
		return nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	path, err := r.ProfilePath(d.profileID, "providers/"+agg.GetString("id")+".yaml")
	if err != nil {
		return nil, err
	}
	result := &Result{Name: found.Name, Content: found.Content, FilePath: &path, SubscriptionNodeCounts: map[string]int{}}
	if found.Extra != nil {
		if err := native(found.Extra, result); err != nil {
			return nil, err
		}
		result.FilePath = &path
		result.Content = found.Content
	}
	values, err := provider.ParseProviderProxies(found.Content, found.Name, path)
	if err != nil {
		return nil, err
	}
	if err := native(values, &result.Proxies); err != nil {
		return nil, err
	}
	result.TotalCount = len(result.Proxies)
	return result, nil
}

// Wire registers aggregation rendering for conversions and provider bundles.
func Wire() { provider.SetAggregationRenderer(RenderProvider) }

// LoadCatalog reads the shared catalog and captures the baseline for SaveCatalog.
func LoadCatalog(ctx context.Context) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	document, err := repo.GetSharedConfig(ctx)
	if err != nil {
		return nil, err
	}
	return jsonx.Compact(document)
}

// SaveCatalog commits a catalog edit using its request-scoped baseline.
func SaveCatalog(ctx context.Context, raw json.RawMessage) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	document, err := legacyObject(raw)
	if err != nil {
		return err
	}
	return repo.SaveSharedConfig(ctx, document, nil)
}

// AppendCatalog adds an aggregation in one shared repository transaction.
func AppendCatalog(ctx context.Context, raw json.RawMessage) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	item, err := legacyObject(raw)
	if err != nil {
		return err
	}
	_, err = repo.UpdateSharedConfigTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("subscription_aggregations", append(shared.GetList("subscription_aggregations"), item))
		return nil, nil
	})
	return err
}
