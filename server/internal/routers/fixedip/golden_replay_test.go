package fixedip_test

import (
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"testing"

	"configflow/internal/routers/device"
	"configflow/internal/routers/fixedip"
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/routerfake"
	"configflow/internal/testgolden"
)

var (
	r17Marker = regexp.MustCompile(`^(cf-client:.*):[0-9a-f]{32}$`)
	r17Tag    = regexp.MustCompile(`^cfip_[0-9a-f]{10}$`)
)

func goldenMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func r17Comparable(t *testing.T, value any) any {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(device.JSON(value), &decoded); err != nil {
		t.Fatal(err)
	}
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for key, item := range x {
				if text, ok := item.(string); ok {
					if key == "comment" && r17Marker.MatchString(text) {
						item = r17Marker.ReplaceAllString(text, "${1}:<operation-id>")
					}
					if key == "tagname" && r17Tag.MatchString(text) {
						item = "cfip_<operation-id>"
					}
				}
				x[key] = walk(item)
			}
		case []any:
			for i, item := range x {
				x[i] = walk(item)
			}
		}
		return v
	}
	return walk(decoded)
}

func r17ROSState(dynamic bool) routerfake.State {
	rows := []routerfake.Row{}
	if dynamic {
		rows = append(rows, routerfake.Row{".id": "*10", "address": "192.0.2.21", "mac-address": "02:00:00:00:00:21", "server": "dhcp-example", "dynamic": "true", "disabled": "false", "comment": "", "client-id": "synthetic-client"})
	}
	return routerfake.State{Tables: map[string][]routerfake.Row{
		ros.PathDHCPServers:  {{".id": "*1", "name": "dhcp-example", "interface": "bridge", "disabled": "false"}},
		ros.PathDHCPNetworks: {{".id": "*2", "address": "192.0.2.0/24", "gateway": "192.0.2.1"}},
		"/ip/address":        {{".id": "*3", "address": "192.0.2.1/24", "interface": "bridge"}},
		ros.PathDHCPLeases:   rows,
		"/ip/arp":            {{".id": "*11", "address": "192.0.2.21", "mac-address": "02:00:00:00:00:21", "interface": "bridge"}},
	}}
}

func r17IKState() routerfake.IkuaiState {
	return routerfake.IkuaiState{Tables: map[string][]routerfake.Row{
		"dhcp_server":   {{"id": 1, "enabled": "yes", "status": 1, "interface": "lan1", "check_relay_only": 0, "addr_pool": "192.0.2.10-192.0.2.200", "netmask": "255.255.255.0", "gateway": "192.0.2.1"}},
		"dhcp_static":   {},
		"dhcp_lease":    {{"ip_addr": "192.0.2.21", "mac": "02:00:00:00:00:21", "interface": "lan1"}},
		"monitor_lanip": {},
	}}
}

// TestFixedIPGoldenReplay records and replays deterministic Go-native boundary
// outputs for R17 (fixed IP multi-vendor matrix across RouterOS and iKuai).
func TestFixedIPGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	for _, vendor := range []string{"routeros", "ikuai"} {
		for _, mode := range []string{"add", "convert", "conflict", "bad_mac", "scope", "ambiguous", "rejected", "lost_reply"} {
			if vendor == "ikuai" && mode == "convert" {
				continue
			}
			t.Run(vendor+"/"+mode, func(t *testing.T) {
				ip, mac := "192.0.2.21", "02:00:00:00:00:21"
				write := mode == "add" || mode == "convert" || mode == "rejected" || mode == "lost_reply"
				if mode == "bad_mac" {
					mac = "01:00:00:00:00:21"
				}

				var d fixedip.Device
				var tables func() any
				if vendor == "routeros" {
					seed := r17ROSState(mode == "convert")
					switch mode {
					case "scope":
						seed.Tables[ros.PathDHCPServers][0]["disabled"] = "true"
					case "ambiguous":
						seed.Tables[ros.PathDHCPServers] = append(seed.Tables[ros.PathDHCPServers], routerfake.Row{".id": "*9", "name": "other", "interface": "bridge", "disabled": "false"})
					case "conflict":
						seed.Tables[ros.PathDHCPLeases] = []routerfake.Row{{".id": "*E0", "address": ip, "mac-address": "02:00:00:00:00:99", "server": "dhcp-example", "comment": "manual", "disabled": "false"}}
					case "rejected":
						seed.Faults = []routerfake.Fault{{Method: "PUT", Path: ros.PathDHCPLeases, Count: 1, Status: 403}}
					case "lost_reply":
						seed.Faults = []routerfake.Fault{{Method: "PUT", Path: ros.PathDHCPLeases, Count: 1, AfterCommit: true, LoseReply: true}}
					}
					f := routerfake.New(nil)
					f.Reset(seed)
					srv := httptest.NewServer(f)
					defer srv.Close()
					c := ros.NewClient(srv.URL, "synthetic-user", "synthetic-pass", false)
					defer c.Close()
					d = fixedip.NewROS(c)
					tables = func() any { return f.Snapshot().Tables }
				} else {
					seed := r17IKState()
					switch mode {
					case "scope":
						seed.Tables["dhcp_server"][0]["enabled"] = "no"
					case "ambiguous":
						cp := routerfake.Row{}
						for k, v := range seed.Tables["dhcp_server"][0] {
							cp[k] = v
						}
						cp["id"] = 2
						cp["interface"] = "lan2"
						seed.Tables["dhcp_server"] = append(seed.Tables["dhcp_server"], cp)
					case "conflict":
						seed.Tables["dhcp_static"] = []routerfake.Row{{"id": 5, "enabled": "yes", "tagname": "manual", "interface": "lan1", "mac": "02:00:00:00:00:99", "gateway": "192.0.2.1", "dns1": "", "dns2": "", "ip_addr": ip, "comment": "manual"}}
					case "rejected":
						code := 1003
						seed.Faults = []routerfake.IkuaiFault{{Func: "dhcp_static", Action: "add", Count: 1, Code: &code}}
					case "lost_reply":
						seed.Faults = []routerfake.IkuaiFault{{Func: "dhcp_static", Action: "add", Count: 1, LoseReply: true}}
					}
					f := routerfake.NewIkuai(seed)
					srv := httptest.NewServer(f)
					defer srv.Close()
					c := ikuai.NewClient(srv.URL, routerfake.IkuaiUser, routerfake.IkuaiPassword, false)
					defer c.Close()
					d = fixedip.NewIkuai(c)
					tables = func() any { return f.Snapshot().Tables }
				}

				op, err := d.PreviewFixedIP(t.Context(), ip, mac, "Synthetic")
				var binding *fixedip.Binding
				if err == nil && write {
					binding, err = d.ApplyFixedIP(t.Context(), op)
					if err == nil {
						deletion, e := d.PreviewDeleteFixedIP(t.Context(), binding)
						err = e
						if err == nil {
							var removed, restored *fixedip.Binding
							removed, err = d.DeleteFixedIP(t.Context(), deletion)
							if err == nil {
								restored, err = d.RestoreDeletedFixedIP(t.Context(), removed)
							}
							if err == nil {
								restored.Undo = binding.Undo
								err = d.RollbackFixedIP(t.Context(), restored)
							}
						}
					}
				}
				if binding != nil {
					binding.Undo = nil
				}
				errText := ""
				if err != nil {
					errText = err.Error()
				}
				ref.Assert(t, goldenMarshal(t, map[string]string{"module": "R17", "vendor": vendor, "mode": mode}), goldenMarshal(t, map[string]any{
					"preview": r17Comparable(t, op),
					"binding": r17Comparable(t, binding),
					"error":   errText,
					"tables":  r17Comparable(t, tables()),
				}))
			})
		}
	}
}
