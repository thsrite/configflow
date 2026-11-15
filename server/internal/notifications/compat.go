// TODO(port): replace with package #32 router core
// (backend/routers/store.py::routers_of). event_message, MESSAGE_LIMIT and
// failure_text come from the shared leaf package internal/agents/diag.

package notifications

import (
	"configflow/internal/agents/diag"
	"configflow/internal/jsonx"
)

// MessageLimit is operation_events.MESSAGE_LIMIT (UTF-8 bytes).
const MessageLimit = diag.MessageLimit

// EventMessage is operation_events.event_message(value, secrets): download
// addresses hidden, then failure_text with the 1024-byte limit. Non-string
// values give "".
func EventMessage(value any, secrets ...any) string { return diag.EventMessage(value, secrets) }

// RoutersOf is routers.store.routers_of(config).
func RoutersOf(config *jsonx.Object) []*jsonx.Object {
	routers, ok := config.Value("routers").(*jsonx.Object)
	if !ok || routers == nil {
		return nil
	}
	items, ok := routers.Value("items").([]any)
	if !ok {
		return nil
	}
	var out []*jsonx.Object
	for _, item := range items {
		if object, ok := item.(*jsonx.Object); ok && object != nil {
			out = append(out, object)
		}
	}
	return out
}
