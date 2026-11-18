// Temporary N8-9 / N8-8d collaborator codecs live only here.
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
			return refresh.ValidateRefreshInterval(value, msgProviderInterval)
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

// NativeEnv registers typed callbacks around the sole implementations in owners.
func NativeEnv() convert.MihomoEnvironment { return convert.MihomoEnvironmentFromLegacy(Env()) }
