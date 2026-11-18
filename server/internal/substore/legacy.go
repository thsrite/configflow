package substore

// Compatibility boundaries only. Native owners never call these adapters.
import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func decodeLegacy[T any](value any) (T, error) {
	var result T
	data, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(data, &result)
	}
	return result, err
}
func encodeLegacy(value any) (*jsonx.Object, error) {
	data, err := model.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoded, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	object, _ := decoded.(*jsonx.Object)
	return object, nil
}

// SaveSubscriptionNodes transcodes the old cache boundary once per batch.
func SaveSubscriptionNodes(ctx context.Context, id string, nodes any, metadata *jsonx.Object) (*jsonx.Object, error) {
	record, err := decodeLegacy[*Cache](jsonx.Obj("subscription_id", id, "updated_at", "", "count", jsonx.Int(0), "nodes", nodes, "metadata", metadata))
	if err != nil {
		return nil, err
	}
	var raw struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	data, err := json.Marshal(jsonx.Obj("nodes", nodes))
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(data, &raw) == nil {
		count := model.Int(len(raw.Nodes))
		record.Count = &count
	}
	result, err := saveCache(ctx, id, record)
	if err != nil {
		return nil, err
	}
	return encodeLegacy(result)
}

// LoadSubscriptionCache transcodes one typed cache for unported consumers.
func LoadSubscriptionCache(ctx context.Context, id string) (*jsonx.Object, error) {
	result, err := LoadCache(ctx, id)
	if err != nil || result == nil {
		return nil, err
	}
	return encodeLegacy(result)
}

func iterate(value any) ([]any, error) {
	switch v := value.(type) {
	case []any:
		return v, nil
	case *jsonx.Object:
		keys := make([]any, 0, v.Len())
		for key := range v.All() {
			keys = append(keys, key)
		}
		return keys, nil
	case string:
		items := []any{}
		for _, r := range v {
			items = append(items, string(r))
		}
		return items, nil
	}
	return nil, pyErr("TypeError", fmt.Sprintf("'%s' object is not iterable", typeName(value)))
}
func pyMakedirs(name string) error {
	head, tail := filepath.Split(name)
	if tail == "" {
		head, tail = filepath.Split(strings.TrimRight(head, "/"))
	}
	head = strings.TrimRight(head, "/")
	if head == "" && strings.HasPrefix(name, "/") {
		head = "/"
	}
	if head != "" && tail != "" {
		if _, err := os.Stat(head); err != nil {
			if err := pyMakedirs(head); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}
			if tail == "." {
				return nil
			}
		}
	}
	if err := os.Mkdir(name, 0o777); err != nil {
		if info, serr := os.Stat(name); serr == nil && info.IsDir() {
			return nil
		}
		return err
	}
	return nil
}
