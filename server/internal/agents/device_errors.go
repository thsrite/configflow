package agents

// ErrInvalidAgentReply rejects malformed successful Agent response objects,
// including duplicate members. The raw upstream body is never a public reason.
var ErrInvalidAgentReply = &DeviceResponseError{Code: "INVALID_RESPONSE", Message: msgAgentReplyShape}
