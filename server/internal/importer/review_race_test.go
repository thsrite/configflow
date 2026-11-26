//go:build race

package importer_test

import "time"

// Race instrumentation adds shadow memory and checks every parser access. Full
// suite parallelism may delay the child without consuming its CPU budget.
const importProbeTimeout = 5 * time.Minute
const importProbeCPU = 30 * time.Second
const importProbeRSSKiB = 768 << 10
