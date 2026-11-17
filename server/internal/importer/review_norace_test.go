//go:build !race

package importer_test

import "time"

const importProbeTimeout = 3 * time.Second
const importProbeRSSKiB = 256 << 10
