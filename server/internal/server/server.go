// Package server wires the application together: every route package, the
// repository hooks owned by the HTTP layer, and the notification sources.
// cmd/configflow and the differential tests build the application here.
//
// Adding a route package: append its Register to routePackages (in the
// position of its blueprint in backend/routes/__init__.py; tasks and the
// notification list stay last because they only register once every
// provider is wired). Adding a background loop or a cross-package callback:
// construct or register it in New (app.go) on the App's Runtime and service
// instances, in the order wireSubsystems documents.
package server

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"

	apiagents "configflow/internal/api/agents"
	apiaggregations "configflow/internal/api/aggregations"
	apiauth "configflow/internal/api/auth"
	apiclientdevices "configflow/internal/api/clientdevices"
	apiconfig "configflow/internal/api/config"
	apidiscovery "configflow/internal/api/domaindiscovery"
	apigatewayonboarding "configflow/internal/api/gatewayonboarding"
	apigenerate "configflow/internal/api/generate"
	apiipv6 "configflow/internal/api/ipv6"
	apilinuxgateway "configflow/internal/api/linuxgateway"
	apilogs "configflow/internal/api/logs"
	apimosdns "configflow/internal/api/mosdns"
	apinodes "configflow/internal/api/nodes"
	apioxidns "configflow/internal/api/oxidns"
	apiprofiles "configflow/internal/api/profiles"
	apiproxygroups "configflow/internal/api/proxygroups"
	apirouters "configflow/internal/api/routers"
	apirulelibrary "configflow/internal/api/rulelibrary"
	apirules "configflow/internal/api/rules"
	apisettings "configflow/internal/api/settings"
	apisetup "configflow/internal/api/setup"
	apistats "configflow/internal/api/stats"
	apisubscriptions "configflow/internal/api/subscriptions"
	"configflow/internal/httpx"
	"configflow/internal/mcp"
	"configflow/internal/repo"
	"configflow/internal/routers"
	routerrulesync "configflow/internal/routers/rulesync"
)

// registerRoutes mounts every ported blueprint in Flask-blueprint order,
// injecting explicit service instances for handlers already migrated.
func registerRoutes(r *httpx.Router, agHandler *apiagents.Handler, rtr *routers.Service, rr *routerrulesync.RuleSyncService) {
	registerAgents := apiagents.Register
	if agHandler != nil {
		registerAgents = agHandler.Register
	}
	routePackages := []func(*httpx.Router){
		mcp.Register,
		apiauth.Register,
		apiclientdevices.Register,
		apinodes.Register,
		apisubscriptions.Register,
		apiaggregations.Register,
		apirules.Register,
		apirulelibrary.Register,
		apiproxygroups.Register,
		registerAgents,
		apirouters.NewHandler(rtr, rr).Register,
		apiipv6.Register,
		apilinuxgateway.Register,
		apiconfig.Register,
		apigenerate.Register,
		apiprofiles.Register,
		apigatewayonboarding.Register,
		apisettings.Register,
		apisetup.Register,
		apilogs.Register,
		apistats.Register,
		apimosdns.Register,
		apioxidns.Register,
		apidiscovery.Register,
	}
	for _, register := range routePackages {
		register(r)
	}
}

func wireCore() {
	repo.RegisterHooks(repo.Hooks{DefaultServerDomain: defaultServerDomain})
}

// defaultServerDomain is what load_config stores outside a container:
// http://<local ip>:<backend port>.
func defaultServerDomain() string {
	address, err := BackendAddressFromEnv()
	port := 5001
	if err == nil {
		port = address.Port
	}
	return "http://" + localIP(net.Dial) + ":" + strconv.Itoa(port)
}

// NewApp builds the full application with default Options and without the
// process-only startup steps load-config and agent-manager-init (test helper
// during the W1-W11 migration; its registrations are the pre-W1 NewApp's).
func NewApp() *httpx.App {
	app, err := build(context.Background(), Options{}, false)
	if err != nil {
		panic("server.NewApp: " + err.Error())
	}
	return app.HTTP
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
