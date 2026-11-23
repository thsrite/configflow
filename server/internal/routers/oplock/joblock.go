// Package oplock provides the L1 router publication job lock and L2 per-device
// operation locks.
package oplock

import (
	"configflow/internal/routers/device"
	"context"
	"strings"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/routers/spec"
)

// One L1 lock table serves repository commit guards and every publication owner.
// The key is device identity, not a profile-local router ID: aliases of the same
// connection must not publish concurrently.
var routerJobs = lockx.NewKeyed[string](lockx.LevelJob, "router-job")

func routerJobKey(profileID, routerID string, router *spec.Router) string {
	if router != nil {
		return router.Type + "\n" + strings.TrimRight(router.Endpoint, "/")
	}
	return profileID + "\n" + routerID
}

// RouterJobLock tries the publication lock without waiting. Existing records
// share a key by device type and endpoint; a missing record uses profileID and
// routerID, allowing callers to take the lock before lookup/validation.
// It ignores client disconnects but retains Runtime shutdown cancellation.
// Pass the returned context into RouterOperationLock and repository writes,
// and release exactly once. A busy device returns device.KindConflict; this lock is
// not reentrant, including calls that already hold the same device's job lock.
func RouterJobLock(ctx context.Context, profileID, routerID string, router *spec.Router) (context.Context, func(), error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}

	held, release, ok := routerJobs.TryLock(ctx, routerJobKey(profileID, routerID, router))
	if !ok {
		if err := ctx.Err(); err != nil {
			return ctx, nil, err
		}
		return ctx, nil, device.KindError(device.KindConflict, device.MessageEditBusy)
	}
	if err := held.Err(); err != nil {
		release()
		return held, nil, err
	}

	return held, release, nil
}
