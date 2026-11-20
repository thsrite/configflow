package tasks

import (
	"encoding/json"

	"configflow/internal/refresh"
)

func validateInterval(value *int64) (int64, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	return refresh.ParseInterval(raw, msgIntervalLabel)
}
