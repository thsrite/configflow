// Package faultprobe supplies build-tagged fault observation for the black-box
// fault harness. Production Point calls are empty; it owns no production work.
package faultprobe

import "time"

// Phase identifies an explicitly registered fault boundary.
type Phase string

const (
	// StoreBeforeCommit is after transaction hooks, immediately before cancellation/COMMIT.
	StoreBeforeCommit Phase = "store-before-commit"
	// MetricsFlush is immediately before history files are flushed.
	MetricsFlush Phase = "metrics-flush"
	// GenerateBuild is after the manual-generation owner takes over its context.
	GenerateBuild Phase = "generate-build"
	// ProviderRender is after a provider worker acquires its render pool slot.
	ProviderRender Phase = "provider-render"
	// MaxWait bounds every test barrier, even if the harness disappears.
	MaxWait = 30 * time.Second
	// ControlEnv selects the private control directory in a difftest binary only.
	ControlEnv = "CONFIGFLOW_FAULT_DIR"
	// TokenHeader authenticates the loopback-only diagnostic endpoint.
	TokenHeader = "X-ConfigFlow-Fault-Token"
)

// Catalog returns the registered symbolic names for harness discovery.
func Catalog() map[string]Phase {
	return map[string]Phase{"store": StoreBeforeCommit, "metrics": MetricsFlush, "generate": GenerateBuild, "provider": ProviderRender}
}

func registered(phase Phase) bool {
	return phase == StoreBeforeCommit || phase == MetricsFlush || phase == GenerateBuild || phase == ProviderRender
}
