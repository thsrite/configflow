package agents

import "errors"

// ErrAgentNotFound reports that no registered Agent exists for the requested ID.
var ErrAgentNotFound = errors.New(msgAgentNotFound)

// ErrInvalidAgentReply rejects malformed successful Agent response objects,
// including duplicate members. The raw upstream body is never a public reason.
var ErrInvalidAgentReply = &DeviceResponseError{Code: "INVALID_RESPONSE", Message: msgAgentReplyShape}
