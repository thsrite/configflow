package lockx

import (
	"context"
	"sync"
	"sync/atomic"

	"configflow/internal/logx"
)

// Level orders the server's tracked locks. Blocking acquisitions must increase
// the level; a try-only acquisition can be made while holding any level.
type Level int

const (
	// LevelJob is L1, the try-only operation/409 lock level.
	LevelJob Level = 10
	// LevelSetupSession is the first L2 suborder: setup session → device/profile
	// operation → repository. Reverse blocking nesting is a violation.
	LevelSetupSession Level = LevelDevice - 1
	// LevelDevice is L2, blocking device/session read-modify-write locks.
	LevelDevice Level = 20
	// LevelDeviceControl is the second L2 suborder: Sub-Store work leases
	// precede control operations; reversing that nesting is a violation.
	LevelDeviceControl Level = LevelDevice + 1
	// LevelRepository is L3, reserved for repo's configuration transaction lock.
	LevelRepository Level = 30
)

type heldKey struct{}

type acquisition struct {
	level  Level
	name   string
	parent *acquisition
	active atomic.Bool
}

type orderPair struct{ held, waiting string }

var orderChecks struct {
	count  atomic.Int64
	mu     sync.Mutex
	logged map[orderPair]bool
}

var logger = logx.Get("backend.lockx")

// Violations reports the number of blocking acquisition attempts that found
// an active lock at the same or a higher level in their context. Attempts still
// proceed; each offending name pair is logged only once for the process.
func Violations() int64 { return orderChecks.count.Load() }

func checkOrder(ctx context.Context, level Level, name string) {
	parent, _ := ctx.Value(heldKey{}).(*acquisition)
	violated := false
	for h := parent; h != nil; h = h.parent {
		if !h.active.Load() || h.level < level {
			continue
		}
		violated = true
		pair := orderPair{held: h.name, waiting: name}
		orderChecks.mu.Lock()
		if orderChecks.logged == nil {
			orderChecks.logged = make(map[orderPair]bool)
		}
		logged := orderChecks.logged[pair]
		orderChecks.logged[pair] = true
		orderChecks.mu.Unlock()
		if !logged {
			logger.Errorf(msgOrderViolation, h.name, h.level, name, level)
		}
	}
	if violated {
		orderChecks.count.Add(1)
	}
}

func hold(ctx context.Context, level Level, name string, unlock func()) (context.Context, func()) {
	parent, _ := ctx.Value(heldKey{}).(*acquisition)
	h := &acquisition{level: level, name: name, parent: parent}
	h.active.Store(true)
	var once sync.Once
	release := func() { once.Do(func() { h.active.Store(false); unlock() }) }
	return context.WithValue(ctx, heldKey{}, h), release
}
