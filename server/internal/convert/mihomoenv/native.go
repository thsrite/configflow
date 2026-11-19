package mihomoenv

import (
	"context"
	"encoding/json"

	"configflow/internal/convert"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

type liveNativeConverter struct{}

func (liveNativeConverter) ConvertProxy(ctx context.Context, text string) (*yamlx.Node, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	proxy, err := substore.NativeConverter().ConvertProxy(ctx, text)
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	// Single-node conversion is an optional lookup in this environment. The
	// legacy callback returned no node on conversion failure; keep that caller
	// policy while the typed client retains errors for its other consumers.
	if err != nil {
		return nil, nil
	}
	return proxy, nil
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
