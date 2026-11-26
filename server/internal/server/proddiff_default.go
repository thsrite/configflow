//go:build !configflow_proddiff

package server

// InstallProddiffContentObserver is a no-op outside configflow_proddiff builds.
func (a *App) InstallProddiffContentObserver() {}
