package mihomoenv

import (
	"context"
	"encoding/json"

	"configflow/internal/convert"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

type liveNativeConverter struct {
	sub *substore.Service
}

func (c liveNativeConverter) ConvertProxy(ctx context.Context, text string) (*yamlx.Node, error) {
	var converter convert.NodeConverter
	if c.sub != nil {
		converter = c.sub.Client
	}
	return convert.OptionalNodeConverter(converter).ConvertProxy(lifecycle.Detach(ctx), text)
}

// NativeEnvFor constructs the production typed MihomoEnvironment directly from
// explicit typed service instances without legacy JSON transcoding.
func NativeEnvFor(r *repo.Repository, sub *substore.Service, pv *provider.Service) convert.MihomoEnvironment {
	env := convert.DefaultMihomoEnvironment()
	env.Converter = liveNativeConverter{sub: sub}
	env.ProviderInterval = func(settings model.MihomoSettings) (int64, error) {
		if settings.ProviderUpdateInterval != nil {
			return refresh.CheckInterval(*settings.ProviderUpdateInterval, msgProviderInterval)
		}
		if raw, err := model.Marshal(settings); err == nil {
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) == nil {
				if value, present := fields["provider_update_interval"]; present {
					return refresh.ParseInterval(value, msgProviderInterval)
				}
			}
		}
		return refresh.CheckInterval(refresh.DefaultProviderInterval, msgProviderInterval)
	}
	env.DownloadedText = func(ctx context.Context, url string) (string, bool) {
		if r == nil {
			return "", false
		}
		data, err := r.ReadSharedBytes(ctx, refresh.DownloadCachePath(url))
		if err != nil || len(data) == 0 {
			return "", false
		}
		var receipt struct {
			Text json.RawMessage `json:"text"`
		}
		if json.Unmarshal(data, &receipt) != nil || len(receipt.Text) == 0 {
			return "", false
		}
		var text string
		if json.Unmarshal(receipt.Text, &text) != nil {
			return "", false
		}
		return text, true
	}
	env.Preflight = func(ctx context.Context, config *model.Config, main *yamlx.Node) error {
		if pv != nil {
			_, err := pv.PrepareBundle(ctx, config, main, provider.DeliveryOptions{})
			return err
		}
		_, err := provider.PrepareBundle(ctx, config, main, provider.DeliveryOptions{})
		return err
	}
	env.BindTypedSubscriptionCache(
		func(ctx context.Context, id string) (json.RawMessage, error) {
			return substore.LoadCacheJSON(ctx, r, id)
		},
		func(ctx context.Context, id string) (*subs.SubscriptionCache, error) {
			return substore.LoadSubscriptionCacheShared(ctx, r, id)
		},
	)
	return env
}
