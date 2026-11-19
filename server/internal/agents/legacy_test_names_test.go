package agents

import "time"

// The frozen reference keeps its historical helper name. Production runtime
// ownership uses the single repository resolver, with no private path walker.
func pyResolve(path string) string { return runtimeDirectory(path) }

// Frozen time oracles keep their helper names, without production emulation.
func pyTimestamp(moment time.Time) float64               { return localEpochSeconds(moment) }
func pyTimestampOf(moment time.Time, naive bool) float64 { return timestampSeconds(moment, naive) }
func pyTimeFromNanos(nanos int64) float64                { return epochSecondsFromNanos(nanos) }
