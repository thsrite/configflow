package tasks

// TODO(N8-9): the refresh owner still exposes its dynamic interval validator.
import (
	"configflow/internal/jsonx"
	"configflow/internal/refresh"
)

func validateInterval(value *int64) (int64, error) {
	if value == nil {
		return refresh.ValidateRefreshInterval(nil, msgIntervalLabel)
	}
	return refresh.ValidateRefreshInterval(jsonx.Int(*value), msgIntervalLabel)
}
