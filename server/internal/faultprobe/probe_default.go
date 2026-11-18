//go:build !configflow_difftest

package faultprobe

import "context"

// Point is an inlineable, allocation-free no-op in production.
func Point(context.Context, Phase) {}

// Active returns no test phases in production.
func Active() []Phase { return nil }
