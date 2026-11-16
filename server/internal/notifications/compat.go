// routers_of comes from the leaf internal/routerdoc. event_message, MESSAGE_LIMIT and
// failure_text come from the shared leaf package internal/agents/diag.

package notifications

import (
	"configflow/internal/agents/diag"
	"configflow/internal/jsonx"
	"configflow/internal/routerdoc"
)

// MessageLimit is operation_events.MESSAGE_LIMIT (UTF-8 bytes).
const MessageLimit = diag.MessageLimit

// EventMessage is operation_events.event_message(value, secrets): download
// addresses hidden, then failure_text with the 1024-byte limit. Non-string
// values give "".
func EventMessage(value any, secrets ...any) string { return diag.EventMessage(value, secrets) }

// RoutersOf is routers.store.routers_of(config).
func RoutersOf(config *jsonx.Object) []*jsonx.Object { return routerdoc.RoutersOf(config) }
