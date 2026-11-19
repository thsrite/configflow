package refresh

import (
	"encoding/json"
)

// ValidateRefreshInterval is the legacy converter/editor request boundary.
func ValidateRefreshInterval(value any, label string) (int64, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return ParseInterval(nil, label)
	}
	return ParseInterval(raw, label)
}

// ErrorClass returns the stable native logging category.
func (e *IntervalError) ErrorClass() string { return "ValueError" }
