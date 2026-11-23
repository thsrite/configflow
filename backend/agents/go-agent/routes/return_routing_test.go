package routes

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const sampleTables = "table inet mihomo_return\ntable inet mihomo\n"

const sampleReturnTable = `table inet mihomo_return {
	chain prerouting {
		type filter hook prerouting priority mangle; policy accept;
		iifname "eth0" ip daddr @proxy_dst ct state new ct mark set ct mark | 0x00400000 comment "return-mark"
		iifname "Meta" ct mark & 0x00400000 == 0x00400000 meta mark set meta mark | 0x00400000
	}
}`

const sampleMihomoTable = `table inet mihomo {
	chain prerouting {
		type nat hook prerouting priority dstnat + 1; policy accept;
		meta nfproto ipv4 meta l4proto tcp redirect to :41123 return
	}
}`

const sampleRules = `0:	from all lookup local
104:	from all fwmark 0x400000/0x400000 lookup 104
9000:	from all to 198.18.0.0/30 lookup 2022
32766:	from all lookup main
`

func TestParseHelpers(t *testing.T) {
	tables := parseNftTables(sampleTables)
	if !reflect.DeepEqual(tables, [][2]string{{"inet", "mihomo_return"}, {"inet", "mihomo"}}) {
		t.Fatalf("tables = %v", tables)
	}
	if marks := parseCtMarks(sampleReturnTable); !reflect.DeepEqual(marks, []string{"0x00400000"}) {
		t.Fatalf("marks = %v", marks)
	}
	if marks := parseCtMarks(sampleMihomoTable); len(marks) != 0 {
		t.Fatalf("mihomo table must not count as return table: %v", marks)
	}
	rules := parseFwmarkRules(sampleRules)
	want := []fwmarkRule{{Priority: "104", Mark: "0x400000/0x400000", Table: "104"}}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("rules = %v", rules)
	}
}

func TestCollectReturnRouting(t *testing.T) {
	oldRun, oldIfaces := runCommand, listInterfaces
	defer func() { runCommand, listInterfaces = oldRun, oldIfaces }()
	listInterfaces = func() []string { return []string{"lo", "eth0", "Meta"} }
	runCommand = func(name string, args ...string) (string, error) {
		switch strings.Join(append([]string{name}, args...), " ") {
		case "nft list tables":
			return sampleTables, nil
		case "nft list table inet mihomo_return":
			return sampleReturnTable, nil
		case "nft list table inet mihomo":
			return sampleMihomoTable, nil
		case "ip -N -4 rule show":
			return sampleRules, nil
		case "ip route show table 104":
			return "default via 192.168.1.1 dev eth0\n192.168.1.0/24 via 192.168.1.1 dev eth0\n", nil
		}
		return "", errors.New("unexpected command")
	}
	result := collectReturnRouting(&Config{})
	if !result.NFTAvailable || !result.IPRouteAvailable {
		t.Fatalf("availability flags: %v", result)
	}
	tables := result.ReturnTables
	if len(tables) != 1 || tables[0].Name != "mihomo_return" {
		t.Fatalf("return tables = %v", tables)
	}
	routes := result.TableRoutes
	if len(routes["104"]) != 2 {
		t.Fatalf("table routes = %v", routes)
	}
	if tun := result.TunDevices; !reflect.DeepEqual(tun, []string{"Meta"}) {
		t.Fatalf("tun = %v", tun)
	}
}

func TestCollectReturnRoutingWithoutTools(t *testing.T) {
	oldRun, oldIfaces := runCommand, listInterfaces
	defer func() { runCommand, listInterfaces = oldRun, oldIfaces }()
	listInterfaces = func() []string { return nil }
	runCommand = func(string, ...string) (string, error) { return "", errors.New("not found") }
	result := collectReturnRouting(&Config{})
	if result.NFTAvailable || result.IPRouteAvailable || !result.Success {
		t.Fatalf("missing tools must still report: %v", result)
	}
}

func TestReturnRoutingHandlerRejectsPost(t *testing.T) {
	recorder := httptest.NewRecorder()
	ReturnRoutingHandler(&Config{})(recorder, httptest.NewRequest(http.MethodPost, "/api/return-routing", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d", recorder.Code)
	}
}
