package upgrades

import (
	"configflow/internal/model"
	"context"
)

func cacheLen() int {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	return cacheOrder.Len()
}

// SetArtifactsForTest replaces the native artifact catalog.
func SetArtifactsForTest(fn func(context.Context) (model.OrderedMap[model.UpgradeArtifact], error)) func() {
	prev := artifactsFunc
	artifactsFunc = fn
	return func() { artifactsFunc = prev }
}
