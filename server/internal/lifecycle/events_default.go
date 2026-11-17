//go:build !configflow_difftest

package lifecycle

import "time"

func emitEvent(time.Time, string, string) {}
