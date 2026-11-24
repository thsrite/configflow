package routers_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"configflow/internal/routers/routerfake"
	"configflow/internal/yamlx"
)

func compareBoundary(t *testing.T, left, right any) {
	t.Helper()
	rawA, _ := json.Marshal(left)
	rawB, _ := json.Marshal(right)
	var a, b any
	if json.Unmarshal(rawA, &a) != nil || json.Unmarshal(rawB, &b) != nil {
		t.Fatal("bad comparison JSON")
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("boundary mismatch:\nleft  %s\nright %s", rawA, rawB)
	}
}

func deviceState() routerfake.State {
	return routerfake.State{PolicyWire: true, Tables: map[string][]routerfake.Row{"/routing/table": {{".id": "*0", "name": "main", "fib": ""}}, "/ip/route": {{".id": "*1", "dst-address": "192.0.2.0/24", "gateway": "bridge", "routing-table": "main", "immediate-gw": "bridge", "active": "true", "connect": "true", "dynamic": "true"}, {".id": "*2", "dst-address": "198.18.0.0/16", "gateway": "192.0.2.4", "routing-table": "main", "active": "true", "immediate-gw": "192.0.2.4%bridge", "disabled": "false", "comment": "cf"}}, "/ip/firewall/nat": {{".id": "*3", "chain": "srcnat", "action": "masquerade", "out-interface": "wan", "disabled": "false"}}, "/ip/address": {{".id": "*A0", "address": "192.0.2.1/24", "interface": "bridge", "disabled": "false"}}, "/interface": {{".id": "*I0", "name": "bridge", "type": "bridge", "running": "true"}, {".id": "*I1", "name": "wan", "type": "ether", "running": "true"}}}}
}

func inputConfig(endpoint string) map[string]any {
	router := routerBody(endpoint)
	router["id"] = "router-one"
	router["proxy_gateway_agent_id"] = "gateway"
	router["verify_tls"] = false
	router["dns_agent_id"] = ""
	router["manage_return_routing"] = false
	router["auto_rule_sync"] = false
	router["rule_sync_interval"] = 60
	router["ip_rule_sets"] = []string{}
	router["ros_policy_rule_sets"] = []string{}
	return map[string]any{"routers": map[string]any{"items": []any{router}}, "mihomo": map[string]any{"custom_config": "dns:\n  enhanced-mode: fake-ip\n  fake-ip-range: 198.18.0.0/16\n"}, "device_notes": map[string]string{"192.0.2.21": "Synthetic"}}
}

func boundaryFiles(t *testing.T, dir string) map[string]any {
	t.Helper()
	out := map[string]any{}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		extension := filepath.Ext(path)
		if extension != ".json" && extension != ".yaml" && extension != ".yml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		key, _ := filepath.Rel(dir, path)
		var value any
		if extension == ".json" {
			if err = json.Unmarshal(data, &value); err != nil {
				return err
			}
		} else {
			node, err := yamlx.Parse(data)
			if err != nil {
				return err
			}
			encoded, err := yamlx.ToJSON(node)
			if err != nil {
				return err
			}
			if err = json.Unmarshal(encoded, &value); err != nil {
				return err
			}
		}
		out[key] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
