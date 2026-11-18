package weblistener

// TODO(N8-9/10): remove when the remaining server/settings callers use native APIs.
import (
	"configflow/internal/jsonx"
	"context"
	"encoding/json"
)

func legacyInformation(value *Information) (*jsonx.Object, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result jsonx.Object
	err = json.Unmarshal(data, &result)
	return &result, err
}
func checkPort(value any) (int, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return 0, fail(msgPort, 400)
	}
	var port int
	if json.Unmarshal(data, &port) != nil {
		return 0, fail(msgPort, 400)
	}
	return validPort(port)
}

// Start adapts the old listener startup result.
func (m *Manager) Start(ctx context.Context) (*jsonx.Object, error) {
	value, err := m.Listen(ctx)
	if err != nil {
		return nil, err
	}
	return legacyInformation(value)
}

// Status adapts the old settings response.
func (m *Manager) Status(ctx context.Context) (*jsonx.Object, error) {
	value, err := m.InformationFor(ctx)
	if err != nil {
		return nil, err
	}
	return legacyInformation(value)
}

// Update decodes the old polymorphic port argument.
func (m *Manager) Update(ctx context.Context, value any) (*jsonx.Object, error) {
	port, err := checkPort(value)
	if err != nil {
		return nil, err
	}
	result, err := m.ChangePort(ctx, port)
	if err != nil {
		return nil, err
	}
	return legacyInformation(result)
}

// Unsupported adapts the unsupported deployment status.
func Unsupported() *jsonx.Object {
	value, _ := legacyInformation(UnsupportedInformation())
	return value
}
