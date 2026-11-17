// Package server wires the application together: every route package, the
// repository hooks owned by the HTTP layer, and the notification sources.
// cmd/configflow and the differential tests build the application here.
//
// Adding a route package: append its Register to routePackages (in the
// position of its blueprint in backend/routes/__init__.py; tasks and the
// notification list stay last because they only register once every
// provider is wired). Adding a background loop or a repository hook: do it
// from the package's own Register/Wire function with lifecycle.Loop or
// repo.RegisterHooks; list that function in wirePackages.
package server

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/agents/metrics"
	"configflow/internal/agents/monitor"
	"configflow/internal/agents/upgrades"
	apiagents "configflow/internal/api/agents"
	apiauth "configflow/internal/api/auth"
	apidiscovery "configflow/internal/api/domaindiscovery"
	apilogs "configflow/internal/api/logs"
	apimosdns "configflow/internal/api/mosdns"
	apinodes "configflow/internal/api/nodes"
	apinotifications "configflow/internal/api/notifications"
	apisettings "configflow/internal/api/settings"
	apistats "configflow/internal/api/stats"
	apisubscriptions "configflow/internal/api/subscriptions"
	apitasks "configflow/internal/api/tasks"
	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/deploy"
	"configflow/internal/discovery"
	"configflow/internal/gateway"
	"configflow/internal/health"
	"configflow/internal/httpx"
	"configflow/internal/ippresence"
	"configflow/internal/notifications"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/substore"
	"configflow/internal/tasks"
	"configflow/internal/utilx"
	"configflow/internal/webdav"
)

// routePackages are the Register functions of the ported blueprints.
var routePackages = []func(*httpx.Router){
	apiauth.Register,
	apinodes.Register,
	apisubscriptions.Register,
	apiagents.Register,
	apisettings.Register,
	apilogs.Register,
	apistats.Register,
	apimosdns.Register,
	apidiscovery.Register,
}

// lateRoutePackages register after everything else is wired.
var lateRoutePackages = []func(*httpx.Router){
	apitasks.Register,
	apinotifications.Register,
}

// wirePackages run before the routes are registered (hooks, loops,
// providers).
var wirePackages = []func(){
	deploy.Wire,
	wireCore,
	webdav.Wire,
	apinodes.Wire,
	agents.Wire,
	metrics.Wire,
	metrics.WireTraffic,
	monitor.Wire,
	upgrades.Wire,
	substore.Wire,
	refresh.Wire,
	health.Wire,
	discovery.Wire,
	mihomoenv.Wire,
	ippresence.Wire, // routers R02: convert.MihomoHooks.IsFullTun, oxidns classical_ip_lines
	routers.Wire,    // routers R01: repo.Hooks.RedactRouters
	gateway.Wire,    // agents A19: convert.MihomoHooks Linux gateway
}

func wireCore() {
	notifications.SetSources(notifications.Sources{ListTasks: tasks.AvailableTasks})
	repo.RegisterHooks(repo.Hooks{DefaultServerDomain: defaultServerDomain})
	// Desensitized export of EasyTier settings (repo cannot import convert).
	repo.RegisterHooks(repo.Hooks{RedactEasytierSettings: convert.RedactEasyTierSettings})
}

// defaultServerDomain is what load_config stores outside a container:
// http://<local ip>:<backend port>.
func defaultServerDomain() string {
	address, err := BackendAddressFromEnv()
	port := 5001
	if err == nil {
		port = address.Port
	}
	return "http://" + utilx.GetLocalIP() + ":" + strconv.Itoa(port)
}

// NewApp builds the full application: wiring, every route package, the
// authentication gate.
func NewApp() *httpx.App {
	for _, wire := range wirePackages {
		wire()
	}
	app := httpx.New()
	for _, register := range routePackages {
		register(app.Router)
	}
	for _, register := range lateRoutePackages {
		register(app.Router)
	}
	auth.Install(app)
	return app
}

// BackendAddress is http_server.backend_address().
type BackendAddress struct {
	Host string
	Port int
}

// BackendAddressFromEnv validates CONFIGFLOW_BACKEND_PORT (default 5001) and
// CONFIGFLOW_BACKEND_HOST (default 0.0.0.0) like http_server.backend_address.
func BackendAddressFromEnv() (BackendAddress, error) {
	raw, ok := os.LookupEnv("CONFIGFLOW_BACKEND_PORT")
	if !ok {
		raw = "5001"
	}
	raw = strings.TrimSpace(raw)
	valid := raw != "" && len(raw) <= 5
	for i := 0; i < len(raw) && valid; i++ {
		valid = raw[i] >= '0' && raw[i] <= '9'
	}
	port, _ := strconv.Atoi(raw)
	if !valid || port < 1 || port > 65535 {
		return BackendAddress{}, errors.New("CONFIGFLOW_BACKEND_PORT must be an integer between 1 and 65535")
	}
	host, ok := os.LookupEnv("CONFIGFLOW_BACKEND_HOST")
	if !ok {
		host = "0.0.0.0"
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return BackendAddress{}, errors.New("CONFIGFLOW_BACKEND_HOST must not be empty")
	}
	return BackendAddress{Host: host, Port: port}, nil
}
