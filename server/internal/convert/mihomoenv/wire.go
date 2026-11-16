// Package mihomoenv installs the config-domain collaborators of the Mihomo
// generator (convert.GenerateMihomoConfig) that live in packages importing
// convert: Sub-Store conversion and caches (substore), the provider interval
// check and download caches (refresh), and provider preflight (provider).
// Python imported them lazily inside generate_mihomo_config; Go breaks the
// import cycle here. Wire is listed in server.wirePackages.
package mihomoenv

import (
	"context"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/substore"
)

// liveConverter resolves substore.Converter() at call time, like Python's
// module attribute lookup of sub_store_client.convert_proxy_string.
type liveConverter struct{}

func (liveConverter) ConvertProxyString(ctx context.Context, proxyString string) *jsonx.Object {
	return substore.Converter().ConvertProxyString(ctx, proxyString)
}

// Env is the production environment.
func Env() convert.MihomoEnv {
	return convert.MihomoEnv{
		Converter: liveConverter{},
		ProviderInterval: func(mihomo *jsonx.Object) (int64, error) {
			value, present := mihomo.Get("provider_update_interval")
			if !present {
				value = jsonx.Int(refresh.DefaultProviderInterval)
			}
			return refresh.ValidateRefreshInterval(value, "Mihomo 订阅同步间隔")
		},
		LoadSubscriptionCache: substore.LoadSubscriptionCache,
		DownloadedText: func(ctx context.Context, url string) (string, bool) {
			r, err := repo.GetRepository(ctx)
			if err != nil {
				return "", false
			}
			downloaded, err := r.ReadSharedJSON(ctx, refresh.DownloadCachePath(url))
			if err != nil || downloaded == nil {
				return "", false
			}
			text, ok := downloaded.Value("text").(string)
			return text, ok
		},
		PrepareProviderBundle: func(ctx context.Context, config, main *jsonx.Object) error {
			_, err := provider.PrepareProviderBundle(ctx, config, main, provider.BundleOptions{})
			return err
		},
	}
}

// Wire installs Env.
func Wire() { convert.SetMihomoEnv(Env()) }
