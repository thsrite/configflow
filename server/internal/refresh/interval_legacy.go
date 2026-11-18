package refresh

import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
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

// SubscriptionInterval projects only the stored interval for unported callers.
func SubscriptionInterval(value *jsonx.Object) jsonx.Number {
	var source model.Subscription
	_ = decodeLegacy(value, &source)
	return jsonx.Int(int64(SourceInterval(source)))
}

// RuleInterval projects only the stored update interval for unported callers.
func RuleInterval(value *jsonx.Object) jsonx.Number {
	var source model.RuleLibraryItem
	_ = decodeLegacy(value, &source)
	return jsonx.Int(int64(RemoteInterval(source)))
}
func (e *IntervalError) PythonClass() string { return "ValueError" }
