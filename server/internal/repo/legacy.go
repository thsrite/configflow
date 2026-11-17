package repo

// Legacy adapter (design n7-n8 2.5): the *jsonx.Object API that consumers
// still use until their own N8-8…N8-10 step migrates them. It holds no logic
// of its own: values cross between the typed core and jsonx by model.Marshal →
// jsonx.Parse and jsonx.Compact → the model codec. Deleted in N8-11.

import (
	"context"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

// workingModel returns a typed copy of the working document of the write
// transaction ctx belongs to; ok is false outside a transaction.
func (r *Repository) workingModel(ctx context.Context) (document *model.Document, ok bool, err error) {
	legacy := r.working(ctx)
	if legacy == nil {
		return nil, false, nil
	}
	rows, err := legacyIntentRows(legacy, nil)
	if err != nil {
		return nil, true, err
	}
	document, _, err = decodeModelRows(rows)
	return document, true, err
}

// threeWayMerge is MergeJSON for jsonx values (nil = absent).
func threeWayMerge(current, baseline, proposed any) (any, error) {
	var encoded [3][]byte
	for index, value := range []any{current, baseline, proposed} {
		data, err := jsonx.Compact(value)
		if err != nil {
			return nil, err
		}
		encoded[index] = data
	}
	merged, changed, err := MergeJSON(encoded[0], encoded[1], encoded[2])
	if err != nil {
		return nil, err
	}
	if !changed {
		return jsonx.DeepCopy(current), nil
	}
	return jsonx.Parse(merged)
}
