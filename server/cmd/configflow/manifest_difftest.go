//go:build configflow_difftest && !configflow_proddiff

package main

import (
	"crypto/subtle"
	"net"
	"os"
	"runtime"

	"configflow/internal/faultprobe"
	"configflow/internal/httpx"
	"configflow/internal/provider"
)

func writeTestManifest(app *httpx.App, _ *provider.Service, _ func()) error {
	if os.Getenv(faultprobe.ControlEnv) == "" {
		return nil
	}
	token, err := faultprobe.Token()
	if err != nil {
		return err
	}
	app.Router.GET("/__faults/state", "test.fault_state", func(c *httpx.Ctx) (*httpx.Response, error) {
		host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() {
			return httpx.Empty(403), nil
		}
		if subtle.ConstantTimeCompare([]byte(c.Request.Header.Get(faultprobe.TokenHeader)), token) != 1 {
			return httpx.Empty(401), nil
		}
		return httpx.JSON(struct {
			Goroutines int                `json:"goroutines"`
			Phases     []faultprobe.Phase `json:"phases"`
		}{runtime.NumGoroutine(), faultprobe.Active()}).NoStore(), nil
	})
	return nil
}
