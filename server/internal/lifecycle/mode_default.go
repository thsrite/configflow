//go:build !configflow_proddiff

package lifecycle

func defaultMode() Mode    { return ModeRun }
func reportReady(*Runtime) {}
