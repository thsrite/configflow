package tasks

import (
	"errors"
	"fmt"
	"strings"
)

// Unavailable identifies known task capabilities whose owners are not wired.
type Unavailable struct{ Slots []string }

func (e *Unavailable) Error() string {
	return fmt.Sprintf(msgUnavailable, strings.Join(e.Slots, "、"))
}

// ErrUnknownTask rejects keys with no supported run operation.
var ErrUnknownTask = errors.New(msgCannotRun)

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
