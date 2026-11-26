package routers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"configflow/internal/routers/device"
)

func TestRouterOSApplyDispatchCoversAllFeatureKinds(t *testing.T) {
	// Every one of the 13 feature operation kinds previously installed via
	// RegisterRouterOSHandler must dispatch directly (never ErrNotPorted) and
	// reject mismatched field types with MessageOperationFormat.
	tests := []struct {
		kind    string
		payload string
	}{
		{"ros_policy_replace", `{"kind":"ros_policy_replace","spec":123}`},
		{"ros_policy_restore", `{"kind":"ros_policy_restore","steps":123}`},
		{"nat_list_sync", `{"kind":"nat_list_sync","comment":123}`},
		{"nat_list_restore", `{"kind":"nat_list_restore","steps":123}`},
		{"dns_guard_replace", `{"kind":"dns_guard_replace","spec":123}`},
		{"dns_guard_restore", `{"kind":"dns_guard_restore","changes":123}`},
		{"ipv6_fakeip_replace", `{"kind":"ipv6_fakeip_replace","spec":123}`},
		{"ipv6_fakeip_restore", `{"kind":"ipv6_fakeip_restore","changes":123}`},
		{"ipv6_routes_replace", `{"kind":"ipv6_routes_replace","adds":123}`},
		{"ipv6_routes_restore", `{"kind":"ipv6_routes_restore","undos":123}`},
		{"ipv6_dns_update", `{"kind":"ipv6_dns_update","id":123}`},
		{"ipv6_pd_guard_sync", `{"kind":"ipv6_pd_guard_sync","spec":123}`},
		{"ipv6_pd_guard_restore", `{"kind":"ipv6_pd_guard_restore","changes":123}`},
	}
	driver := NewRouterOSWithHTTP("http://127.0.0.1", nil)
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			_, err := driver.Apply(context.Background(), json.RawMessage(tc.payload))
			if err == nil {
				t.Fatalf("expected error for malformed %s payload", tc.kind)
			}
			if errors.Is(err, device.ErrNotPorted) {
				t.Fatalf("%s returned ErrNotPorted; missing switch case", tc.kind)
			}
			if err.Error() != device.MessageOperationFormat {
				t.Fatalf("%s error = %q, want %q", tc.kind, err.Error(), device.MessageOperationFormat)
			}
		})
	}
	t.Run("unknown_kind", func(t *testing.T) {
		_, err := driver.Apply(context.Background(), json.RawMessage(`{"kind":"totally_unknown_op"}`))
		want := fmt.Sprintf(device.MessageUnsupportedOperation, "totally_unknown_op")
		if err == nil || err.Error() != want {
			t.Fatalf("unknown kind error = %v, want %q", err, want)
		}
	})
}
