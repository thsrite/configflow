package taskstatus

// TODO(N8-9): remove adapters after all task producers accept native receipts.
import (
	"configflow/internal/jsonx"
	"context"
	"encoding/json"
	"time"
)

// ParseTime adapts the legacy optional timestamp.
func ParseTime(value any) (time.Time, bool) { text, _ := value.(string); return ParseTimestamp(text) }

func legacyReceipt(value *Receipt) (*jsonx.Object, error) {
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

// Load adapts the snapshot for unconverted producers.
func (s *Store) Load() (*jsonx.Object, error) {
	value, err := s.Read()
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result jsonx.Object
	err = json.Unmarshal(data, &result)
	return &result, err
}

// Get adapts one receipt for unconverted producers.
func (s *Store) Get(key string) (*jsonx.Object, error) {
	value, err := s.Read()
	if err != nil {
		return nil, err
	}
	return legacyReceipt(value.Entry(key))
}

// Record adapts a completed result.
func (s *Store) Record(key string, r Result) (*jsonx.Object, error) {
	return s.RecordContext(context.Background(), key, r)
}

// RecordContext preserves the originating work lifetime in the native store.
func (s *Store) RecordContext(ctx context.Context, key string, r Result) (*jsonx.Object, error) {
	value, err := s.SaveResult(ctx, key, r)
	if err != nil {
		return nil, err
	}
	return legacyReceipt(value)
}
