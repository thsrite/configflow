//go:build configflow_proddiff

package server

// InstallProddiffContentObserver attaches the proddiff content-sync observer
// backed by this App's wired agents handler.
func (a *App) InstallProddiffContentObserver() {
	if a == nil || a.HTTP == nil || a.api.agents == nil {
		return
	}
	a.api.agents.InstallProddiffContentObserver(a.HTTP)
}
