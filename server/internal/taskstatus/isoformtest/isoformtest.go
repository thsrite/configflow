// Package isoformtest lets reference tests recorded against Python's
// datetime.fromisoformat skip inputs outside the timestamp grammar ConfigFlow
// stores (week dates, basic format, space separators, hour-only times and
// sub-second offsets are CPython-only spellings that are no longer chased).
// It is used by tests only.
package isoformtest

import "regexp"

var stored = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}(T[0-9]{2}:[0-9]{2}:[0-9]{2}([.,][0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2}(:[0-9]{2})?)?)?$`)

// Stored reports whether text is spelled in the stored timestamp grammar
// that taskstatus.ParseISO reads.
func Stored(text string) bool { return stored.MatchString(text) }
