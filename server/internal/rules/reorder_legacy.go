package rules

import "configflow/internal/jsonx"

// Collections are reordered by id (backend/utils/reorder.py). The old
// endpoints accepted the full object array and wrote back display data; ids
// let the server reorder stored data instead.

// ReorderByIDs moves the items named by ids, in that order, to one end of the
// collection ("top" or "bottom") and keeps the rest in their relative order.
// Unknown ids are returned in missing and the collection comes back unchanged.
func ReorderByIDs(items []any, ids []string, position string) (ordered []any, missing []string) {
	return Reorder(items, ids, position, func(item any) string {
		if object, ok := item.(*jsonx.Object); ok {
			return object.GetString("id")
		}
		return ""
	})
}

// ResolveNewOrder reorders by the ids in the request body ("ids", or the ids
// of a legacy object array under objectKey, which only supplies the order and
// never overwrites resource definitions). The second result lists error
// messages or unknown ids; non-empty means the collection is unchanged.
func ResolveNewOrder(current []any, body *jsonx.Object, objectKey string) ([]any, []string) {
	idsValue := body.Value("ids")
	if idsValue == nil {
		list, isList := body.Value(objectKey).([]any)
		ids := make([]any, len(list))
		for i, item := range list {
			object, ok := item.(*jsonx.Object)
			if !ok {
				isList = false
				break
			}
			ids[i] = object.Value("id")
		}
		if !isList {
			return current, []string{objectKey + " 必须是对象数组"}
		}
		idsValue = ids
	}
	values, ok := idsValue.([]any)
	if !ok {
		return current, []string{"ids 必须是数组"}
	}
	ids := make([]string, len(values))
	seen := map[string]bool{}
	for i, value := range values {
		id, isText := value.(string)
		if !isText || seen[id] {
			return current, []string{"id 必须是互不重复的字符串"}
		}
		ids[i], seen[id] = id, true
	}
	position, _ := body.GetOr("position", "top").(string)
	return ReorderByIDs(current, ids, position)
}
