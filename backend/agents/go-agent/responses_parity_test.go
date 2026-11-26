package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agent/routes"
)

func marshalJSONLineMain(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		t.Fatalf("encode %T: %v", v, err)
	}
	return buf.Bytes()
}

// TestMainRouteResponsesByteIdenticalToLegacyMaps verifies that all package main
// HTTP routes (/api/agent-address, /api/domain-probe, /api/region-check,
// /api/region-check/targets, /api/mihomo-panel/*) produce byte-for-byte
// identical JSON to the pre-change map[string]any payloads.
func TestMainRouteResponsesByteIdenticalToLegacyMaps(t *testing.T) {
	tests := []struct {
		name   string
		typed  any
		legacy any
	}{
		{
			name: "GET /api/agent-address success",
			typed: agentAddressGetResponse{
				Addresses:  []string{"192.0.2.10"},
				Configured: "192.0.2.10",
				Protocol:   1,
				Reported:   "192.0.2.10",
				Success:    true,
				Writable:   true,
			},
			legacy: map[string]any{
				"success":    true,
				"protocol":   1,
				"configured": "192.0.2.10",
				"reported":   "192.0.2.10",
				"addresses":  []string{"192.0.2.10"},
				"writable":   true,
			},
		},
		{
			name: "POST /api/agent-address invalid host",
			typed: agentAddressInvalidResponse{
				Addresses: []string{"192.0.2.10"},
				Message:   "这台设备没有地址 192.0.2.99，只能选择它现有的局域网地址",
				Success:   false,
			},
			legacy: map[string]any{
				"success":   false,
				"message":   "这台设备没有地址 192.0.2.99，只能选择它现有的局域网地址",
				"addresses": []string{"192.0.2.10"},
			},
		},
		{
			name:   "POST /api/agent-address success",
			typed:  agentAddressPostResponse{Host: "192.0.2.10", Success: true},
			legacy: map[string]any{"success": true, "host": "192.0.2.10"},
		},
		{
			name:   "POST /api/domain-probe success",
			typed:  domainProbeResponse{Results: []probeTargetResult{}, Success: true},
			legacy: map[string]any{"success": true, "results": []probeTargetResult{}},
		},
		{
			name:   "GET /api/region-check/targets success",
			typed:  regionTargetsResponse{Success: true, Targets: []regionTarget{{Name: "Proxy"}}},
			legacy: map[string]any{"success": true, "targets": []regionTarget{{Name: "Proxy"}}},
		},
		{
			name:   "POST /api/region-check success",
			typed:  regionCheckResponse{Results: []regionResult{}, Success: true, Target: "Proxy"},
			legacy: map[string]any{"success": true, "target": "Proxy", "results": []regionResult{}},
		},
		{
			name:   "mihomo panel error",
			typed:  routes.CodedErrorResponse{Code: "NOT_MIHOMO", Message: "requires mihomo", Success: false},
			legacy: map[string]any{"success": false, "code": "NOT_MIHOMO", "message": "requires mihomo"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := marshalJSONLineMain(t, tc.typed)
			want := marshalJSONLineMain(t, tc.legacy)
			if !bytes.Equal(got, want) {
				t.Fatalf("JSON bytes differ:\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

func TestMainHandlersErrorParity(t *testing.T) {
	cfg := &Config{ServiceType: "oxidns", Token: "tok"}

	cases := []struct {
		name       string
		handler    http.HandlerFunc
		method     string
		path       string
		body       string
		wantStatus int
		wantLegacy any
	}{
		{
			name:       "POST /api/domain-probe non-mihomo",
			handler:    domainProbeHandler(cfg),
			method:     http.MethodPost,
			path:       "/api/domain-probe",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "message": "domain probe requires a mihomo agent"},
		},
		{
			name:       "GET /api/region-check/targets non-mihomo",
			handler:    regionTargetsHandler(cfg),
			method:     http.MethodGet,
			path:       "/api/region-check/targets",
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "message": "region check requires a mihomo agent"},
		},
		{
			name:       "POST /api/region-check non-mihomo",
			handler:    regionCheckHandler(cfg),
			method:     http.MethodPost,
			path:       "/api/region-check",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "message": "region check requires a mihomo agent"},
		},
		{
			name:       "POST /api/agent-address invalid json",
			handler:    agentAddressHandler(cfg),
			method:     http.MethodPost,
			path:       "/api/agent-address",
			body:       `{bad`,
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "message": "请求内容无效"},
		},
		{
			name:       "GET /api/mihomo-panel/flow non-mihomo",
			handler:    mihomoPanelHandler(cfg),
			method:     http.MethodGet,
			path:       "/api/mihomo-panel/flow",
			wantStatus: http.StatusBadRequest,
			wantLegacy: map[string]any{"success": false, "code": "WRONG_SERVICE", "message": "mihomo panel requires a mihomo agent"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			want := marshalJSONLineMain(t, tc.wantLegacy)
			if !bytes.Equal(rec.Body.Bytes(), want) {
				t.Fatalf("body mismatch:\n got: %s\nwant: %s", rec.Body.Bytes(), want)
			}
		})
	}
}
