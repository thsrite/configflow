package oplock

import (
	"configflow/internal/routers/device"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"errors"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/routers/spec"
)

func operationKeys(routers []*spec.Router) [][2]string {
	keys := make([][2]string, 0, len(routers))
	for _, r := range routers {
		if r != nil {
			keys = append(keys, [2]string{r.Type, strings.TrimRight(r.Endpoint, "/")})
		}
	}
	slices.SortFunc(keys, func(a, b [2]string) int {
		if c := strings.Compare(a[0], b[0]); c != 0 {
			return c
		}
		return strings.Compare(a[1], b[1])
	})
	return slices.Compact(keys)
}

// RouterOperationLocks acquires the shared route-editor device locks as one
// sorted group. Cloned connections have one lock. Only this API may acquire
// sibling device locks: the returned context records the device level so any
// subsequent external same-level acquisition remains an order violation.
func RouterOperationLocks(ctx context.Context, routers []*spec.Router, write bool) (context.Context, func(), error) {
	limit := time.Duration(0)
	if write {
		ctx = lifecycle.Detach(ctx)
		limit = 10 * time.Minute
	}
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	parent := ctx
	var releases []func()
	var once sync.Once
	release := func() {
		once.Do(func() {
			for i := len(releases) - 1; i >= 0; i-- {
				releases[i]()
			}
		})
	}
	for _, key := range operationKeys(routers) {
		// All members check the original ancestors, never a sibling. Sorted keys
		// provide the internal deadlock order; no checker exemption escapes here.
		held, unlock, err := routeLocks.Lock(parent, key, limit)
		if err != nil {
			release()
			if write && parent.Err() == nil {
				return parent, nil, device.KindError(device.KindConflict, device.MessageEditBusy)
			}
			return parent, nil, err
		}
		releases = append(releases, unlock)
		ctx = held
		if err := parent.Err(); err != nil {
			release()
			return parent, nil, err
		}
	}
	return ctx, release, nil
}

// L2: shared by publish, manual edits, previews and fixed-IP operations.
var routeLocks = lockx.NewKeyed[[2]string](lockx.LevelDevice, "router-operation")

// RouterOperationLock serializes read-modify-write operations on a connection.
// Read callers retain cancellation; write callers detach from client disconnects
// and wait at most ten minutes, bounded by Runtime shutdown cancellation.
// Do not acquire recursively: pass the returned context down under the same lock.
func RouterOperationLock(ctx context.Context, router spec.Router, write bool) (context.Context, func(), error) {
	limit := time.Duration(0)
	if write {
		ctx = lifecycle.Detach(ctx)
		if err := ctx.Err(); err != nil {
			return ctx, nil, err
		}
		limit = 10 * time.Minute
	}
	key := [2]string{router.Type, strings.TrimRight(router.Endpoint, "/")}
	held, release, err := routeLocks.Lock(ctx, key, limit)
	if err != nil && write && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return ctx, nil, device.KindError(device.KindConflict, device.MessageEditBusy)
	}
	return held, release, err
}

// OperationLockHeld reports whether the L2 operation lock for router is currently held.
func OperationLockHeld(router spec.Router) bool {
	return routeLocks.Held([2]string{router.Type, strings.TrimRight(router.Endpoint, "/")})
}
