package check

import (
	"configflow/internal/model"
	"fmt"
)

// ValidateGroupHealth validates the health-check fields of a stored proxy group.
func ValidateGroupHealth(group *model.ProxyGroup) error {
	if group == nil {
		return invalidInput(msgHealthMustBeObject)
	}
	raw, err := model.Marshal(group)
	if err != nil {
		return fmt.Errorf("marshal proxy group health: %w", err)
	}
	return validateStoredHealth(raw, healthValues{URL: group.URL, Interval: group.Interval, Timeout: group.Timeout, Lazy: group.Lazy, ExpectedStatus: group.ExpectedStatus, Tolerance: group.Tolerance}, false)
}
