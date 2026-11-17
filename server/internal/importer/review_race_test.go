//go:build race

package importer_test

import "time"

// Race instrumentation adds shadow memory and checks every parser access.
const importProbeTimeout = 30 * time.Second
const importProbeRSSKiB = 768 << 10
