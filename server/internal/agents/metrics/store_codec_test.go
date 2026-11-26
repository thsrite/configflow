package metrics

import (
	"configflow/internal/agents/trafficstore"
	"encoding/json"
)

// nativeStoreReport transcodes only the frozen dynamic fixture input.
func nativeStoreReport(value any) (*trafficstore.Report, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return trafficstore.DecodeReport(raw)
}
