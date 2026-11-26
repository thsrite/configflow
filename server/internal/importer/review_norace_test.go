//go:build !race

package importer_test

import "time"

// Wall time includes scheduling delays from other packages. The child enforces
// the smaller CPU budget independently with RLIMIT_CPU.
const importProbeTimeout = 30 * time.Second
const importProbeCPU = 3 * time.Second
const importProbeRSSKiB = 256 << 10
