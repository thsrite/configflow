// Package model defines stored configuration and fixed publication-receipt
// envelopes as Go records. It preserves unknown and unchanged JSON members,
// including their bytes and order. It performs no I/O, state transitions or
// ownership validation: callers own decoded records and report FieldIssues.
package model
