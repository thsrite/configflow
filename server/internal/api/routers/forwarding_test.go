package routers_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/routers/routerfake"
)

func TestR15ForwardingHeadAndMalformedReportIsolation(t *testing.T) {
	var writes atomic.Int64
	var bad atomic.Bool
	peer := func(malformed bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "" {
				t.Error("missing registered Agent authorization")
			}
			if r.Method == "POST" {
				writes.Add(1)
				raw, _ := io.ReadAll(r.Body)
				if r.URL.Path != "/api/linux-gateway/forwarding" || string(raw) != "{}" {
					t.Errorf("bad write wire %s %s", r.URL.Path, raw)
				}
			} else if r.URL.Path != "/api/linux-gateway/status" {
				t.Errorf("bad status path %s", r.URL.Path)
			}
			filter := `{"state":"ok","sources":[],"remedies":[]}`
			if malformed && bad.Load() {
				filter = `{"state":[],"token":"synthetic-report-token"}`
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"success":true,"supported":true,"managed":{"active":false},"ipv4_forwarding":{"supported":true,"current":"1","persisted":true},"forward_filter":`+filter+`}`)
		}))
	}
	one, two := peer(true), peer(false)
	defer one.Close()
	defer two.Close()
	s := fixture(t)
	login := s.Login()
	_, err := s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		records := []any{}
		for i, url := range []string{one.URL, two.URL} {
			host, portText, _ := net.SplitHostPort(strings.TrimPrefix(url, "http://"))
			port, _ := strconv.Atoi(portText)
			id := []string{"bad", "healthy"}[i]
			records = append(records, jsonx.Obj("id", id, "name", id, "host", host, "port", port, "service_type", "mihomo", "profile_id", "default", "enabled", true, "status", "online", "last_heartbeat", time.Now().UTC().Format(time.RFC3339Nano), "version", "1.8.43-go", "deployment_method", "shell", "token", "synthetic-"+id+"-token"))
		}
		current.Set("agents", records)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Repo.UpdateProfileTransaction(s.Context(), "default", func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		current.Set("routers", jsonx.Obj("items", []any{jsonx.Obj("id", "router-bad", "name", "Bad reference synthetic-healthy-token", "proxy_gateway_agent_id", "bad"), jsonx.Obj("id", "router-healthy", "name", "Healthy", "proxy_gateway_agent_id", "healthy")}))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.Repo.ConfigRevision(s.Context(), "default")
	files := boundaryFiles(t, s.DataDir)
	s.Do("HEAD", "/api/routers/gateway-forwarding", login, httpxtest.JSON(map[string]string{"agent_id": "healthy"})).WantStatus(200)
	if writes.Load() != 0 {
		t.Fatal("HEAD persisted forwarding")
	}
	after, _ := s.Repo.ConfigRevision(s.Context(), "default")
	if before != after {
		t.Fatal("HEAD changed metadata")
	}
	compareBoundary(t, boundaryFiles(t, s.DataDir), files)
	s.Post("/api/routers/gateway-forwarding", login, httpxtest.JSON(map[string]string{"agent_id": "healthy"})).WantStatus(200)
	if writes.Load() != 1 {
		t.Fatal("POST control did not use the real device write path")
	}
	bad.Store(true)
	result := s.Get("/api/routers/gateway-forwarding", login)
	result.WantStatus(200)
	gateways := result.Object().GetList("gateways")
	if len(gateways) != 2 || gateways[0].(*jsonx.Object).GetString("state") != "error" || gateways[1].(*jsonx.Object).GetString("state") != "persisted" {
		t.Fatalf("bad report hid healthy gateway: %s", result.Body)
	}
	if strings.Contains(result.Text(), "synthetic-healthy-token") || strings.Contains(result.Text(), "synthetic-report-token") {
		t.Fatal("forwarding response leaked peer or report credentials")
	}
}

func TestR15FlaskForwardingSuccessAndFailure(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			data, _ := io.ReadAll(r.Body)
			if string(data) != "{}" || r.URL.Path != "/api/linux-gateway/forwarding" {
				t.Errorf("unexpected forwarding request %s %s", r.URL.Path, data)
			}
		} else if r.URL.Path != "/api/linux-gateway/status" {
			t.Errorf("unexpected status path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"supported":true,"managed":{"active":false},"ipv4_forwarding":{"supported":true,"current":"1","persisted":true}}`)
	}))
	defer peer.Close()
	host, portText, _ := net.SplitHostPort(strings.TrimPrefix(peer.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	system := map[string]any{"agents": []any{map[string]any{"id": "gateway", "name": "Gateway", "host": host, "port": port, "service_type": "mihomo", "profile_id": "default", "enabled": true, "status": "online", "last_heartbeat": time.Now().UTC().Format(time.RFC3339Nano), "version": "1.8.43-go", "deployment_method": "shell", "token": "synthetic-forwarding-token"}}}
	fake := routerfake.New(nil)
	seed := deviceState()
	fake.Reset(seed)
	device := httptest.NewServer(fake)
	defer device.Close()
	steps := []boundaryRequest{{Method: "GET", Path: "/api/routers/gateway-forwarding", Status: 200}, {Method: "POST", Path: "/api/routers/gateway-forwarding", Body: map[string]string{"agent_id": "gateway"}, Status: 200}, {Method: "POST", Path: "/api/routers/gateway-forwarding", Body: map[string]string{"agent_id": "unlinked"}, Status: 404}}
	runHTTPBoundary(t, fake, seed, inputConfig(device.URL), system, steps)
}
