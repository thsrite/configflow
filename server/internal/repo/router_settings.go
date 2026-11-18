package repo

import (
	"configflow/internal/ctxx"
	"configflow/internal/model"
	"context"
	"encoding/json"
)

// SaveRouterSettingsJSON commits a router PUT or DELETE under its caller's job
// lock. It shares the transaction and ownership guard with SaveRouterSettings;
// expected retains the original stored presence used for CAS.
func (r *Repository) SaveRouterSettingsJSON(ctx context.Context, profileID string, expected, replacement json.RawMessage) error {
	ctx = ctxx.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	old := viewOfJSON(expected)
	raw, present := old.get("id")
	id, valid := jsonString(raw)
	if !present || jsonKind(raw) == '{' || jsonKind(raw) == '[' {
		return validationError(msgRouterIDInvalid)
	}
	key := routerKey{profileID, id}

	return r.transact(ctx, writeOptions{lockedRouters: map[routerKey]bool{key: true}}, func(work context.Context, tx *transaction) error {
		if err := work.Err(); err != nil {
			return err
		}
		p, err := profileFor(tx.document, profileID)
		if err != nil {
			return err
		}
		records, err := routerRecordsIn(tx.document)
		if err != nil {
			return err
		}
		current, found := records.values[key]
		if !valid || !found || !sameStoredObject(current, old) {
			return inUse(msgRouterSettingsChanged)
		}

		var next model.Router
		if replacement != nil {
			v := viewOfJSON(replacement)
			raw, _ := v.get("id")
			other, ok := jsonString(raw)
			if !ok || other != id {
				return validationError(msgRouterIDImmutable)
			}
			if err := json.Unmarshal(replacement, &next); err != nil {
				return err
			}
		}
		items := make([]model.Router, 0, len(p.Routers.Items))
		for _, item := range p.Routers.Items {
			if item.ID != id {
				items = append(items, item)
			} else if replacement != nil {
				items = append(items, next)
			}
		}
		p.Routers.Items = items
		p.Revision++
		p.UpdatedAt = nowISO()
		return work.Err()
	})
}
func sameStoredObject(a, b memberView) bool {
	left, e := a.fields.MarshalJSON()
	if e != nil {
		return false
	}
	right, e := b.fields.MarshalJSON()
	return e == nil && sameStored(left, right)
}
