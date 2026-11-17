// Package model defines the stored configuration as Go records. It preserves
// unknown and unchanged JSON members, including their bytes and order. It does
// no I/O: callers own decoded records and report FieldIssues at their boundary.
package model
