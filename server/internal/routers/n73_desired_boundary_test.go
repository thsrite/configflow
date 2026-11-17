package routers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"configflow/internal/routers"
	"configflow/internal/routers/routerfake"
	"configflow/internal/yamlx"
)

func TestN73DesiredPolicyStopsBeforeAnyDeviceRequest(t *testing.T) {
	var chain strings.Builder
	chain.WriteString("a0: &a0 [x]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "a%d: &a%d [*a%d,*a%d]\n", i, i, i-1, i-1)
	}
	tests := []struct {
		name, source string
		reason       error
	}{
		{"duplicate", "\ndns: {fake-ip-range: 198.19.0.0/16}\ndns: {}\n", yamlx.ErrDuplicateKey},
		{"alias budget", chain.String(), yamlx.ErrAliasBudget},
		{"recursive alias", "extra: &e [*e]\n", yamlx.ErrAliasRecursion},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := routerfake.New(map[string][]routerfake.Row{"/ip/route": {}})
			server := httptest.NewServer(fake)
			defer server.Close()
			// This loader deliberately reaches the loopback device. The desired YAML
			// guard must stop before any caller-supplied work can make that request.
			reads := 0
			loader := func(routers.RuleSet, *routers.LibraryRule) (string, error) {
				reads++
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/rest/ip/route", nil)
				if err != nil {
					return "", err
				}
				response, err := server.Client().Do(request)
				if err != nil {
					return "", err
				}
				defer response.Body.Close()
				_, _ = io.Copy(io.Discard, response.Body)
				if response.StatusCode != 200 {
					return "", fmt.Errorf("fake router status %d", response.StatusCode)
				}
				return "IP-CIDR,198.51.100.0/24", nil
			}
			profile := func(source string) json.RawMessage {
				raw, err := json.Marshal(map[string]any{"id": "synthetic-profile", "name": "Synthetic", "mihomo": map[string]any{"custom_config": source}, "rule_configs": []any{map[string]any{"id": "selected", "itemType": "ruleset", "name": "Synthetic rule", "behavior": "classical", "policy": "PROXY"}}})
				if err != nil {
					t.Fatal(err)
				}
				return raw
			}
			plan, err := routers.ComputeDesired(context.Background(), profile(test.source), "192.0.2.1", []string{"selected"}, loader)
			var validation *routers.ValidationError
			if plan != nil || !errors.As(err, &validation) || !errors.Is(err, test.reason) {
				t.Fatalf("policy generated plan: %#v %v", plan, err)
			}
			if !strings.Contains(err.Error(), "Synthetic（synthetic-profile）") || !strings.Contains(err.Error(), "Mihomo 基础 YAML") {
				t.Fatalf("configuration context lost: %v", err)
			}
			if test.reason == yamlx.ErrDuplicateKey && (!strings.Contains(err.Error(), "dns") || !strings.Contains(err.Error(), "第 3 行")) {
				t.Fatalf("original source key/line lost: %v", err)
			}
			if reads != 0 || len(fake.Calls()) != 0 {
				t.Fatalf("invalid YAML contacted device: loader=%d calls=%v", reads, fake.Calls())
			}
			// A real syntax error retains the old default and proves the fake/loader
			// observation is live rather than a disconnected zero-request assertion.
			control, controlErr := routers.ComputeDesired(context.Background(), profile("["), "192.0.2.1", []string{"selected"}, loader)
			if controlErr != nil || control == nil || control.FakeIPRange != "198.18.0.0/16" || reads != 1 || len(fake.Calls()) != 1 {
				t.Fatalf("syntax fallback control: %#v %v reads=%d calls=%v", control, controlErr, reads, fake.Calls())
			}
		})
	}
}
