package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"configflow/internal/lifecycle"
)

// Unavailable identifies known task capabilities whose owners are not wired.
type Unavailable struct{ Slots []string }

func (e *Unavailable) Error() string {
	return fmt.Sprintf(msgUnavailable, strings.Join(e.Slots, "、"))
}

// ErrUnknownTask rejects keys with no supported run operation.
var ErrUnknownTask = errors.New(msgCannotRun)

// HasRows reports whether a real provider is installed for a slot.
func HasRows(slot string) bool { mu.Lock(); defer mu.Unlock(); return rows[slot] != nil }

// SlotFor maps public run categories to their owning row providers.
func SlotFor(category string) (string, bool) {
	switch category {
	case SlotSubscriptions, SlotRuleSources, SlotRouters, SlotDomains, SlotBackup:
		return category, true
	case "dns_follow":
		return SlotAgents, true
	case MetricsTaskKey:
		return SlotMetrics, true
	default:
		return "", false
	}
}

// RunNow owns task admission; missing known capabilities never become empty
// production providers, and a stopped generation cannot start a new job.
func RunNow(ctx context.Context, key string) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	category, resource, _ := strings.Cut(key, ":")
	if key == MetricsTaskKey {
		category = key
	}
	slot, known := SlotFor(category)
	if !known || (category == MetricsTaskKey && key != MetricsTaskKey) {
		return ErrUnknownTask
	}
	run := Runner(category)
	if !HasRows(slot) || run == nil {
		return &Unavailable{Slots: []string{slot}}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return run(ctx, key, resource)
}
