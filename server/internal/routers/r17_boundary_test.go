package routers

import (
	"bytes"
	"configflow/internal/routers/device"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"configflow/internal/routers/routerfake"
)

type r17Output struct {
	Preview *FixedIPOperation `json:"preview"`
	Binding *FixedIPBinding   `json:"binding"`
	Error   *struct {
		Class   string `json:"class"`
		Message string `json:"message"`
	} `json:"error"`
}

func r17Reference(t *testing.T, url string, input any) r17Output {
	t.Helper()
	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()
	cmd := exec.CommandContext(t.Context(), filepath.Join(home, "cf-test/.venv/bin/python"), filepath.Join(wd, "testdata/r17_fixed_reference.py"))
	cmd.Dir = filepath.Clean(filepath.Join(wd, "../../.."))
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "R17_ENDPOINT="+url)
	cmd.Stdin = bytes.NewReader(device.JSON(input))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		t.Fatalf("Python vendor %v %s", err, stderr.String())
	}
	var result r17Output
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err, string(data))
	}
	return result
}

var r17Marker = regexp.MustCompile(`^(cf-client:.*):[0-9a-f]{32}$`)
var r17Tag = regexp.MustCompile(`^cfip_[0-9a-f]{10}$`)

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
func TestR17RealPythonVendorBoundaries(t *testing.T) {
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
				var d FixedIPDriver
				var initial any
				var reset func()
				var state func() any
				var url string
				if vendor == "routeros" {
					seed := r17ROSState(mode == "convert")
					if mode == "scope" {
						seed.Tables[rosDHCPServers][0]["disabled"] = "true"
					}
					if mode == "ambiguous" {
						seed.Tables[rosDHCPServers] = append(seed.Tables[rosDHCPServers], routerfake.Row{".id": "*9", "name": "other", "interface": "bridge", "disabled": "false"})
					}
					if mode == "conflict" {
						seed.Tables[fixedROSLeases] = []routerfake.Row{{".id": "*E0", "address": ip, "mac-address": "02:00:00:00:00:99", "server": "dhcp-example", "comment": "manual", "disabled": "false"}}
					}
					if mode == "rejected" {
						seed.Faults = []routerfake.Fault{{Method: "PUT", Path: fixedROSLeases, Count: 1, Status: 403}}
					}
					if mode == "lost_reply" {
						seed.Faults = []routerfake.Fault{{Method: "PUT", Path: fixedROSLeases, Count: 1, AfterCommit: true, LoseReply: true}}
					}
					f := routerfake.New(nil)
					srv := httptest.NewServer(f)
					defer srv.Close()
					url = srv.URL
					initial = seed
					reset = func() { f.Reset(seed) }
					state = func() any { return f.Snapshot().Tables }
					d = NewRouterOS(url, "synthetic-user", "synthetic-pass", false)
				} else {
					seed := r17IKState()
					if mode == "scope" {
						seed.Tables["dhcp_server"][0]["enabled"] = "no"
					}
					if mode == "ambiguous" {
						copy := routerfake.Row{}
						for k, v := range seed.Tables["dhcp_server"][0] {
							copy[k] = v
						}
						copy["id"] = 2
						copy["interface"] = "lan2"
						seed.Tables["dhcp_server"] = append(seed.Tables["dhcp_server"], copy)
					}
					if mode == "conflict" {
						seed.Tables["dhcp_static"] = []routerfake.Row{{"id": 5, "enabled": "yes", "tagname": "manual", "interface": "lan1", "mac": "02:00:00:00:00:99", "gateway": "192.0.2.1", "dns1": "", "dns2": "", "ip_addr": ip, "comment": "manual"}}
					}
					if mode == "rejected" {
						code := 1003
						seed.Faults = []routerfake.IkuaiFault{{Func: "dhcp_static", Action: "add", Count: 1, Code: &code}}
					}
					if mode == "lost_reply" {
						seed.Faults = []routerfake.IkuaiFault{{Func: "dhcp_static", Action: "add", Count: 1, LoseReply: true}}
					}
					f := routerfake.NewIkuai(seed)
					srv := httptest.NewServer(f)
					defer srv.Close()
					url = srv.URL
					initial = seed
					reset = func() { f.Reset(seed) }
					state = func() any { return f.Snapshot().Tables }
					d = NewIkuai(url, routerfake.IkuaiUser, routerfake.IkuaiPassword, false)
				}
				defer d.Close()
				expected := r17Reference(t, url, map[string]any{"vendor": vendor, "state": initial, "ip": ip, "mac": mac, "write": write})
				pythonState := state()
				reset()
				op, err := d.PreviewFixedIP(t.Context(), ip, mac, "Synthetic")
				if !reflect.DeepEqual(r17Comparable(t, op), r17Comparable(t, expected.Preview)) {
					t.Fatalf("preview differs Go=%s Python=%s", device.JSON(op), device.JSON(expected.Preview))
				}
				var binding *FixedIPBinding
				if err == nil && write {
					binding, err = d.ApplyFixedIP(t.Context(), op)
					if err == nil {
						deletion, e := d.PreviewDeleteFixedIP(t.Context(), binding)
						err = e
						if err == nil {
							var removed, restored *FixedIPBinding
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
				if expected.Error == nil {
					if err != nil {
						t.Fatal(err)
					}
				} else {
					if err == nil {
						t.Fatalf("Python rejected %s", expected.Error.Message)
					}
					if mode != "lost_reply" && err.Error() != expected.Error.Message {
						t.Errorf("error Go=%s Python=%s", err, expected.Error.Message)
					}
				}
				if binding != nil {
					binding.Undo = nil
				}
				if !reflect.DeepEqual(r17Comparable(t, binding), r17Comparable(t, expected.Binding)) {
					t.Fatal("binding differs")
				}
				if !reflect.DeepEqual(r17Comparable(t, state()), r17Comparable(t, pythonState)) {
					t.Fatalf("device tables differ Go=%s Python=%s", device.JSON(state()), device.JSON(pythonState))
				}
			})
		}
	}
}
