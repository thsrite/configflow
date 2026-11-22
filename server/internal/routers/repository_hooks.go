package routers

import (
	"configflow/internal/model"
	"context"
	"encoding/json"
)

// jobLockHook tries only the existing L1 router publication lease under L3.
func jobLockHook(ctx context.Context, profileID, routerID string, router model.Router) (func(), bool) {
	connection := &Router{RouterSettings: RouterSettings{Type: routerStoredText(&router, "type"), Endpoint: routerStoredText(&router, "endpoint")}}
	_, release, err := RouterJobLock(ctx, profileID, routerID, connection)
	return release, err == nil
}

func routerStoredText(router *model.Router, key string) string {
	raw, _, err := model.EncodeMember(router, key)
	if err != nil {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return ""
	}
	return text
}
