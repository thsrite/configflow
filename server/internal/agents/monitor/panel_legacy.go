package monitor

import (
	"configflow/internal/jsonx"
	"context"
	"encoding/json"
)

// PanelCall is the compatibility codec for API/agents and rulesync.
func PanelCall(ctx context.Context, m Manager, id, resource, method string, args [][2]string, body any, minimum string) (*jsonx.Object, error) {
	var raw []byte
	var err error
	if body != nil {
		raw, err = jsonx.Compact(body)
		if err != nil {
			return nil, err
		}
	}
	result, err := CallPanel(ctx, registryFor(m), id, resource, method, args, raw, minimum)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(encoded)
	if err != nil {
		return nil, panelFail("INVALID_RESPONSE", 0, "")
	}
	return value.(*jsonx.Object), nil
}

// CheckCoreUpdate is the compatibility codec for the core version operation.
func CheckCoreUpdate(ctx context.Context, m Manager, id string, force bool) (*jsonx.Object, error) {
	args := [][2]string{}
	if force {
		args = append(args, [2]string{"force", "1"})
	}
	return PanelCall(ctx, m, id, "core/update", "GET", args, nil, "")
}
func panelAgent(ctx context.Context, m Manager, id, minimum, feature string) (*jsonx.Object, error) {
	a, err := readPanelAgent(ctx, registryFor(m), id, minimum, feature)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	return value.(*jsonx.Object), nil
}
