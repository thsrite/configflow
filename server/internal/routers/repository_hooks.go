package routers

import (
	"configflow/internal/model"
	"configflow/internal/routers/oplock"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
)

// JobLockRecord tries only the existing L1 router publication lease under L3.
// Wired into repo.Options.RouterJobLock by server assembly.
func JobLockRecord(ctx context.Context, profileID, routerID string, router model.Router) (func(), bool) {
	connection := &spec.Router{RouterSettings: spec.RouterSettings{Type: routerStoredText(&router, "type"), Endpoint: routerStoredText(&router, "endpoint")}}
	_, release, err := oplock.RouterJobLock(ctx, profileID, routerID, connection)
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
