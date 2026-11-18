package routers

// The repository hook signatures still belong to its unmigrated public API.
import (
	"configflow/internal/jsonx"
	"context"
	"encoding/json"
)

// redactHook adapts RedactRouters to repo's jsonx document (until repo's S pass).
func redactHook(routers any) any {
	if document, err := json.Marshal(routers); err == nil {
		if redacted, err := RedactRouters(document); err == nil {
			if value, err := jsonx.Parse(redacted); err == nil {
				return value
			}
		}
	}
	return routers
}

// jobLockHook only tries the routers-owned L1 lock, even when called under L3.
// It never reads the repository; router is the immutable transaction snapshot.
func jobLockHook(ctx context.Context, profileID, routerID string, router *jsonx.Object) (func(), bool) {
	var connection *Router
	if router != nil {
		connection = &Router{RouterSettings: RouterSettings{Type: router.GetString("type"), Endpoint: router.GetString("endpoint")}}
	}
	_, release, err := RouterJobLock(ctx, profileID, routerID, connection)
	return release, err == nil
}
