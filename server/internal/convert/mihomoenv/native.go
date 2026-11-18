package mihomoenv

import (
	"context"
	"encoding/json"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

type liveNativeConverter struct{}

func (liveNativeConverter) ConvertProxy(ctx context.Context, text string) (*yamlx.Node, error) {
	return substore.NativeConverter().ConvertProxy(ctx, text)
}

// NativeEnv connects migrated subscription owners directly. The remaining
// provider/refresh collaborations retain their owners' codecs until migration.
func NativeEnv() convert.MihomoEnvironment {
	env := convert.MihomoEnvironmentFromLegacy(Env())
	env.Converter = liveNativeConverter{}
	env.LoadSubscriptionCache = func(ctx context.Context, id string) (json.RawMessage, error) {
		cache, err := substore.LoadCache(ctx, id)
		if err != nil || cache == nil {
			return nil, err
		}
		return model.Marshal(cache)
	}
	return env
}
