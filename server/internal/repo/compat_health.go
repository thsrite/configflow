// validate_health is C16's check.ValidateHealth (leaf package, no cycle).
// validate_refresh_interval stays a copy: internal/refresh imports repo
// (TestValidateRefreshIntervalMatchesTheRepoCopyGolden keeps them equal).

package repo

import (
	"errors"
	"strconv"

	"configflow/internal/health/check"
	"configflow/internal/jsonx"
)

func valueError(message string) error { return helperError(ClassValueError, message) }

// pyInt is `type(v) is int`: a Number holding an integer, never a bool.
func pyInt(v any) (jsonx.Number, bool) {
	number, ok := v.(jsonx.Number)
	return number, ok && number.IsInt()
}

// validateHealth is health_check.validate_health (C16's port,
// check.ValidateHealth), its ValueError as repo's helper ValueError.
func validateHealth(values any, provider bool) error {
	err := check.ValidateHealth(values, provider)
	var invalid *check.ValueError
	if errors.As(err, &invalid) {
		return valueError(invalid.Message)
	}
	return err
}

// validateRefreshInterval is validate_refresh_interval with its default label.
func validateRefreshInterval(value any) error {
	number, ok := pyInt(value)
	if ok {
		if n, fits := number.Int64(); fits && (n == 0 || (60 <= n && n <= maxRefreshInterval)) {
			return nil
		}
	}
	return valueError("订阅源更新间隔必须为 0（关闭自动更新）或 60 ~ " + strconv.Itoa(maxRefreshInterval) + " 秒的整数")
}
