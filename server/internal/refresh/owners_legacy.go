package refresh

// Existing substore/subs owners still expose JSON records on this baseline.
// These adapters translate one owned record at a time; they do not implement
// their cache declarations, node parsing, filters, hosts or dialer schemas.
import (
	"context"
	"encoding/json"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/substore"
)

func decodeLegacy(value, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(data, target)
}
func legacyObject(value any) *jsonx.Object {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	parsed, _ := jsonx.Parse(data)
	object, _ := parsed.(*jsonx.Object)
	return object
}

// Forget adapts the legacy health delete entry point.
func Forget(ctx context.Context, id string) error { return ForgetHealth(ctx, id) }

func libraryRecord(value *jsonx.Object) model.RuleLibraryItem {
	var result model.RuleLibraryItem
	_ = decodeLegacy(value, &result)
	return result
}

// SourceFetch is the current baseline's subscription callback shape.
type SourceFetch func(context.Context, string, string) (*substore.SubscriptionResult, error)
