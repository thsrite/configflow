package routers

import (
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"configflow/internal/routers/routerfake"
)

// TestO17NullFieldsStayRefused: a null that a reader refused before rows
// became text (O17 1.8) is still refused by every list read, including the
// write path (payload) that takes over or restores an object.
func TestO17NullFieldsStayRefused(t *testing.T) {
	f := routerfake.New(map[string][]routerfake.Row{
		ros.PathIPv6ND: {
			{".id": "*1", "interface": "bridge", "disabled": nil, "dynamic": "false", "invalid": "false"},
			{".id": "*2", "interface": "bridge", "disabled": "false", "dynamic": nil, "invalid": "false"},
			{".id": "*3", "interface": "bridge", "disabled": "false", "dynamic": "false", "invalid": nil},
			{".id": "*4", "interface": "bridge", "disabled": "false", "dynamic": "false", "invalid": "false"},
		},
		ros.PathDHCPNetworks: {
			{".id": "*5", "address": "192.0.2.0/24", "dns-none": nil},
			{".id": "*6", "address": "198.51.100.0/24", "dns-none": "false"},
		},
		"/interface/list": {{".id": "*7", "name": "LAN", "include": nil}},
	})
	server := httptest.NewServer(f)
	defer server.Close()
	d := NewRouterOS(server.URL, "synthetic-user", "synthetic-password", false)
	defer d.Close()
	ctx := context.Background()

	nd, err := d.List(ctx, ros.PathIPv6ND)
	if err != nil {
		t.Fatal(err)
	}
	for i, key := range []string{"disabled", "dynamic", "invalid"} {
		if ros.Payload(ros.PathIPv6ND, nd[i]) != nil {
			t.Errorf("ND row with null %s was writable", key)
		}
		if got, want := ros.PayloadError(ros.PathIPv6ND, nd[i]).Error(), fmt.Sprintf(ros.MessageIPv6DNSFlag, key); !strings.Contains(got, want) {
			t.Errorf("ND null %s: %q, want %q", key, got, want)
		}
	}
	if ros.Payload(ros.PathIPv6ND, nd[3]) == nil {
		t.Error("plain ND row refused")
	}

	networks, err := d.List(ctx, ros.PathDHCPNetworks)
	if err != nil {
		t.Fatal(err)
	}
	if ros.Payload(ros.PathDHCPNetworks, networks[0]) != nil {
		t.Error("DHCP network with null dns-none was writable")
	}
	if ros.Payload(ros.PathDHCPNetworks, networks[1]) == nil {
		t.Error("plain DHCP network refused")
	}

	lists, err := d.List(ctx, "/interface/list")
	if err != nil {
		t.Fatal(err)
	}
	s := &PolicySnapshot{InterfaceLists: lists, InterfaceMembers: []Row{}}
	if got := ipv6.PolicyInterface(s, new("ether1"), "LAN", map[string]bool{}); got != PolicyUnknown {
		t.Errorf("interface list with null include: %v, want unknown", got)
	}
}

// TestO17StructuredSelectorUnknown: a firewall selector that is a JSON array
// or object (held as its JSON text) is unknown, as before rows were text.
func TestO17StructuredSelectorUnknown(t *testing.T) {
	s := &PolicySnapshot{AddressLists: []Row{}, InterfaceLists: []Row{}, InterfaceMembers: []Row{}}
	packet := PolicyPacket{Protocol: new("tcp"), InInterface: new("ether1"), SrcAddress: new("192.0.2.1"),
		DstPort: new(443)}
	for _, raw := range []string{
		`{"chain":"forward","protocol":["udp"]}`,
		`{"chain":"forward","in-interface":{"name":"ether2"}}`,
		`{"chain":"forward","src-address":["198.51.100.0/24"]}`,
		`{"chain":"forward","dst-port":[80]}`,
	} {
		if got := ipv6.MatchPolicyRule(s, decodedRow(t, raw), packet); got != PolicyUnknown {
			t.Errorf("%s: %v, want unknown", raw, got)
		}
	}
	if got := ipv6.MatchPolicyRule(s, decodedRow(t, `{"chain":"forward","protocol":"udp"}`), packet); got != PolicyNoMatch {
		t.Errorf("text selector: %v, want no match", got)
	}
}
