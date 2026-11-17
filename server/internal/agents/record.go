package agents

import "configflow/internal/jsonx"

// Record is the legacy registry record, opaque to new consumers until N8-4e.
type Record = jsonx.Object

// Result is the legacy registry or Agent answer, opaque to new consumers.
type Result = jsonx.Object
