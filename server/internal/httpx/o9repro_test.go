//go:build o9repro

package httpx_test

// O9 round 1 reproductions (fail until fixed; excluded from the gate by the
// o9repro build tag): go test -tags o9repro -run TestO9Repro ./internal/httpx

import (
	"bytes"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

// Flask-CORS defaults: any Origin is reflected. With login disabled (the
// default install) any web page a LAN user opens can read and write the
// whole API cross-site (preflight included).
func TestO9ReproCORSReflectsForeignOrigin(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(echoRoutes)})
	res := srv.Do("OPTIONS", "/api/echo", httpxtest.Header("Origin", "https://attacker.example"),
		httpxtest.Header("Access-Control-Request-Method", "POST"), httpxtest.Header("Access-Control-Request-Headers", "content-type"))
	if res.Header.Get("Access-Control-Allow-Origin") == "https://attacker.example" {
		t.Fatalf("foreign origin allowed: %v", res.Header)
	}
}

// The backend listener (CONFIGFLOW_BACKEND_HOST, default 0.0.0.0:5001) serves
// the App without the Edge's 1 MiB / 96 MiB limits; Go reads request bodies
// fully into memory (waitress capped at 1 GiB and spooled to disk).
func TestO9ReproBackendPortHasNoBodyLimit(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true})
	big := bytes.Repeat([]byte(" "), 8<<20)
	big = append(big, []byte(`{"username":"x","password":"y"}`)...)
	res := srv.Post("/api/auth/login", httpxtest.Body(big, "application/json"))
	if res.Status != 413 {
		t.Fatalf("8 MiB login body accepted and parsed: %d", res.Status)
	}
}
