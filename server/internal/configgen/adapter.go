package configgen

import (
	"context"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

// Render adapts legacy snapshot callers (currently internal/api/config/live.go
// on the parallel W5 branch) to RenderConfig.
func Render(ctx context.Context, config *jsonx.Object, engine Engine, baseURL string) (Output, error) {
	data, err := jsonx.Compact(config)
	if err != nil {
		return Output{}, err
	}
	native, err := model.DecodeConfig(data)
	if err != nil {
		return Output{}, err
	}
	return RenderConfig(ctx, native, engine, baseURL)
}
