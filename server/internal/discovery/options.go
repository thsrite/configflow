package discovery

import (
	"encoding/json"

	"configflow/internal/model"
)

// JSONOptionEnabled evaluates a stored JSON option flag with default false.
func JSONOptionEnabled(raw json.RawMessage) bool {
	return model.JSONSwitchEnabled(raw, false)
}
