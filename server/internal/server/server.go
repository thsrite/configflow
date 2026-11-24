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
	"configflow/internal/httpx"
	"configflow/internal/mcp"
	"configflow/internal/repo"
)

// apiHandlers holds the explicitly constructed handler of every api package.
type apiHandlers struct {
	mcp    *mcp.Handler
	agents *apiagents.Handler
	w10b   w10bHandlers
	w10c   *w10cHandlers
}

// registerRoutes mounts every api package once, in Flask-blueprint order
// (tasks and the notification list last).
func registerRoutes(r *httpx.Router, h apiHandlers) {
	w10b, w10c := h.w10b, h.w10c
	routePackages := []func(*httpx.Router){
		h.mcp.Register,
		w10c.auth.Register,
		w10c.clientdevices.Register,
		w10c.nodes.Register,
		w10c.subscriptions.Register,
		w10c.aggregations.Register,
		w10b.rules.Register,
		w10b.rulelibrary.Register,
		w10c.proxygroups.Register,
		h.agents.Register,
		w10b.routers.Register,
		w10c.ipv6.Register,
		w10c.linuxgateway.Register,
		w10b.config.Register,
		w10b.generate.Register,
		w10b.profiles.Register,
		w10c.gatewayonboarding.Register,
		w10c.settings.Register,
		w10c.setup.Register,
		w10c.logs.Register,
		w10c.stats.Register,
		w10b.mosdns.Register,
		w10b.oxidns.Register,
		w10c.domaindiscovery.Register,
		w10c.tasks.Register,
		w10c.notifications.Register,
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
