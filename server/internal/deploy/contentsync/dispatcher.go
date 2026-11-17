package contentsync

import "sync"

// Dispatcher broadcasts cache invalidation to live Manager-owned Checkers.
// Its zero value is usable. Unregistration waits for any current broadcast.
type Dispatcher struct {
	mu       sync.Mutex
	next     uint64
	checkers map[uint64]*Checker
}

// Register retains a Checker until the returned idempotent function unregisters it.
func (d *Dispatcher) Register(c *Checker) func() {
	d.mu.Lock()
	if d.checkers == nil {
		d.checkers = map[uint64]*Checker{}
	}
	d.next++
	id := d.next
	d.checkers[id] = c
	d.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { d.mu.Lock(); delete(d.checkers, id); d.mu.Unlock() }) }
}

// Invalidate broadcasts to every registered Checker, with no preparation or I/O.
func (d *Dispatcher) Invalidate() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.checkers {
		c.Invalidate()
	}
}

// Count reports the number of live registrations, for resource accounting.
func (d *Dispatcher) Count() int { d.mu.Lock(); defer d.mu.Unlock(); return len(d.checkers) }
