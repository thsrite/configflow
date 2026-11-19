package monitor

import (
	"context"

	"configflow/internal/jsonx"
)

// ValidateState validates the legacy representation through the typed owner.
func ValidateState(value any) bool {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return false
	}
	_, err = DecodeState(raw)
	return err == nil
}

// ValidateObservations validates legacy observation input through the typed owner.
func ValidateObservations(value any) bool {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return false
	}
	_, err = decodeObservations(raw)
	return err == nil
}
func (s *StateStore) read(path string) (*jsonx.Object, error) {
	state, err := s.readState(path)
	if err != nil || state == nil {
		return nil, err
	}
	return metricObject(state), nil
}

// Read is the legacy dashboard-state codec.
func (s *StateStore) Read(ctx context.Context, id string) (*jsonx.Object, error) {
	state, err := s.ReadState(ctx, id)
	if err != nil || state == nil {
		return nil, err
	}
	return metricObject(state), nil
}

// CompareAndSet is the legacy dashboard-state codec.
func (s *StateStore) CompareAndSet(ctx context.Context, id, expected string, replacement *jsonx.Object) (*jsonx.Object, error) {
	var state *DashboardState
	if replacement != nil {
		raw, err := jsonx.Compact(replacement)
		if err != nil {
			return nil, err
		}
		state, err = DecodeState(raw)
		if err != nil {
			return nil, &StateError{"Invalid DNS statistics state"}
		}
	}
	result, err := s.CompareState(ctx, id, expected, state)
	if err != nil || result == nil {
		return nil, err
	}
	return metricObject(result), nil
}

// ExceedsDigitLimit is exceedsDigitLimit for the routes.
func ExceedsDigitLimit(value any) bool { return exceedsDigitLimit(value) }

// exceedsDigitLimit reports an int literal longer than 4300 digits, which
// json.loads refuses with ValueError.
func exceedsDigitLimit(value any) bool {
	switch v := value.(type) {
	case jsonx.Number:
		if v.IsInt() {
			text := v.String()
			if len(text) > 0 && text[0] == '-' {
				text = text[1:]
			}
			return len(text) > maxStrDigits
		}
	case []any:
		for _, item := range v {
			if exceedsDigitLimit(item) {
				return true
			}
		}
	case *jsonx.Object:
		for _, item := range v.Values() {
			if exceedsDigitLimit(item) {
				return true
			}
		}
	}
	return false
}

// maxStrDigits remains only for the unmigrated HTTP request binder.
const maxStrDigits = 4300
