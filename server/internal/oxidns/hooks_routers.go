package oxidns

import (
	"context"
	"sync"
)

// Hooks of the routers domain (coordinator decision 3: interfaces, no
// copies). Added by routers R02, which replaced C22's pending stub with
// this hook; registered from internal/ippresence.Wire.

// ClassicalIPLinesFunc is rule_ip_presence.classical_ip_lines(item,
// config): true/false for a classical rule set whose content is known, nil
// (None) otherwise; the error is an exception Python does not catch there.
type ClassicalIPLinesFunc func(ctx context.Context, item, config any) (any, error)

var (
	routerHooksMu   sync.RWMutex
	classicalIPHook ClassicalIPLinesFunc
)

// SetClassicalIPLines registers rule_ip_presence.classical_ip_lines (routers
// R02) and returns a function restoring the previous hook (tests).
func SetClassicalIPLines(fn ClassicalIPLinesFunc) (restore func()) {
	routerHooksMu.Lock()
	previous := classicalIPHook
	classicalIPHook = fn
	routerHooksMu.Unlock()
	return func() {
		routerHooksMu.Lock()
		classicalIPHook = previous
		routerHooksMu.Unlock()
	}
}

// classicalIPLines is rule_ip_presence.classical_ip_lines(item, config).
// Python reads the rules directory through the global repository; the
// callers here have no request context, so the hook gets
// context.Background() (repo.GetRepository falls back to the default
// repository, as Python's get_repository()). Unregistered it stays pending
// (*PendingError), as C22's stub was.
func classicalIPLines(item any, config any) any {
	routerHooksMu.RLock()
	hook := classicalIPHook
	routerHooksMu.RUnlock()
	if hook == nil {
		pending("routers", "rule_ip_presence.classical_ip_lines")
		return nil
	}
	value, err := hook(context.Background(), item, config)
	if err != nil {
		raise(err)
	}
	return value
}
