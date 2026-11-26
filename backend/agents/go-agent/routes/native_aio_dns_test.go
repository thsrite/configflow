package routes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNativeAIODNSMergesHostExclusions(t *testing.T) {
	stubAgentIPv4(t, func() (string, error) { return "192.168.0.33", nil })
	for _, test := range []struct {
		name, exclusions string
		want             []string
	}{
		{"missing", "", []string{"127.0.0.0/8", "192.168.0.33/32"}},
		{"null", "null", []string{"127.0.0.0/8", "192.168.0.33/32"}},
		{"preserve", "[10.0.0.0/8, '2001:db8::/32']", []string{"10.0.0.0/8", "2001:db8::/32", "127.0.0.0/8", "192.168.0.33/32"}},
		{"already-present", "[127.0.0.0/8, 192.168.0.33/32]", []string{"127.0.0.0/8", "192.168.0.33/32"}},
		{"broader-network", "[127.0.0.0/8, 192.168.0.0/24]", []string{"127.0.0.0/8", "192.168.0.0/24"}},
		{"partial-loopback", "[127.0.0.1/32, 192.168.0.34/32]", []string{"127.0.0.1/32", "192.168.0.34/32", "127.0.0.0/8", "192.168.0.33/32"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := "mode: rule\ndns: {enable: true, listen: '0.0.0.0:1053'}\ntun:\n  enable: true\n  auto-redirect: true\n  dns-hijack: [any:1053]\n"
			if test.exclusions != "" {
				input += "  route-exclude-address: " + test.exclusions + "\n"
			}
			cfg := &Config{NativeAIO: true, AgentIP: " 192.168.0.33 "}
			got, err := adaptNativeAIODNS(cfg, input)
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				TUN struct {
					Excluded []string `yaml:"route-exclude-address"`
				} `yaml:"tun"`
			}
			if err := yaml.Unmarshal([]byte(got), &doc); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(doc.TUN.Excluded, test.want) {
				t.Fatalf("exclusions = %v, want %v", doc.TUN.Excluded, test.want)
			}
			again, err := adaptNativeAIODNS(cfg, got)
			if err != nil || again != got {
				t.Fatalf("host exclusion adaptation is not idempotent: %v\n%s", err, again)
			}
		})
	}
}

func TestNativeAIODNSUsesAutomaticAddress(t *testing.T) {
	stubAgentIPv4(t, func() (string, error) { return "192.168.0.33", nil })
	cfg := &Config{NativeAIO: true}
	got, err := adaptNativeAIODNS(cfg, "tun: {enable: true, auto-redirect: true, route-exclude-address: [10.0.0.0/8]}\n")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatal(err)
	}
	want := []any{"10.0.0.0/8", "127.0.0.0/8", "192.168.0.33/32"}
	if !reflect.DeepEqual(doc["tun"].(map[string]any)["route-exclude-address"], want) || cfg.AgentIP != "" {
		t.Fatalf("automatic address was not merged or was persisted: %s; agent_ip=%q", got, cfg.AgentIP)
	}
}

func TestNativeAIODNSCoversAllAutomaticAddressesWithoutWideningRoutes(t *testing.T) {
	stubAgentIPv4s(t, func() ([]string, error) { return []string{"10.0.0.2", "192.168.8.2", "192.168.9.2"}, nil })
	got, err := adaptNativeAIODNS(&Config{NativeAIO: true}, "tun: {enable: true, auto-redirect: true, route-exclude-address: [10.0.0.0/8]}\n")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatal(err)
	}
	want := []any{"10.0.0.0/8", "127.0.0.0/8", "192.168.8.2/32", "192.168.9.2/32"}
	if !reflect.DeepEqual(doc["tun"].(map[string]any)["route-exclude-address"], want) {
		t.Fatalf("automatic protection missed an address or widened its prefix: %s", got)
	}
}

func TestNativeAIODNSPreservesYAMLAliasesAndMergeKeys(t *testing.T) {
	stubAgentIPv4(t, func() (string, error) { return "192.168.0.33", nil })
	for _, test := range []struct{ name, yaml string }{
		{"exclusion-alias", "custom-exclusions: &excluded [10.0.0.0/8]\ntun: {enable: true, auto-redirect: true, route-exclude-address: *excluded}\n"},
		{"exclusion-anchor", "tun: {enable: true, auto-redirect: true, route-exclude-address: &excluded [10.0.0.0/8]}\ncustom-exclusions: *excluded\n"},
		{"tun-alias", "custom-tun: &tun {enable: true, auto-redirect: true, route-exclude-address: [10.0.0.0/8], dns-hijack: [any:53]}\ntun: *tun\n"},
		{"tun-anchor", "tun: &tun {enable: true, auto-redirect: true, route-exclude-address: [10.0.0.0/8], dns-hijack: [any:53]}\ncustom-tun: *tun\n"},
		{"tun-merge", "custom-tun: &tun {enable: true, auto-redirect: true, route-exclude-address: [10.0.0.0/8], dns-hijack: [any:53]}\ntun: {<<: *tun, stack: system}\n"},
		{"top-level-merge", "custom-defaults: &defaults {tun: {enable: true, auto-redirect: true, route-exclude-address: [10.0.0.0/8]}}\n<<: *defaults\n"},
		{"enable-alias", "custom-enabled: &enabled true\ntun: {enable: *enabled, auto-redirect: *enabled, route-exclude-address: [10.0.0.0/8]}\n"},
		{"dns-anchor", "dns: &dns {enable: true, listen: '127.0.0.1:53'}\ncustom-dns: *dns\ntun: {enable: true, auto-redirect: true, route-exclude-address: [10.0.0.0/8]}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := "mode: rule\n" + test.yaml
			var want map[string]any
			if err := yaml.Unmarshal([]byte(input), &want); err != nil {
				t.Fatal(err)
			}
			want["tun"].(map[string]any)["route-exclude-address"] = []any{"10.0.0.0/8", "127.0.0.0/8", "192.168.0.33/32"}
			cfg := testDeploymentConfig(t)
			cfg.NativeAIO, cfg.AgentIP = true, "192.168.0.33"
			got, err := adaptNativeAIODNS(cfg, input)
			if err != nil {
				t.Fatal(err)
			}
			assertDocument := func(content []byte) {
				t.Helper()
				var doc map[string]any
				if err := yaml.Unmarshal(content, &doc); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(doc, want) {
					t.Fatalf("alias adaptation changed unrelated fields or lost inherited values:\n%s\nwant: %#v", content, want)
				}
			}
			assertDocument([]byte(got))
			// Exercise the real staging path and manifest after YAML expansion;
			// DNS listeners, hijack ports and unrelated aliases must stay intact.
			state := submitBundle(t, cfg, "yaml-host-protection", map[string][]byte{"config.yaml": []byte(input)}, false)
			if state.Status != "ready" {
				t.Fatalf("YAML alias/merge publication failed: %+v", state)
			}
			if err := loadEffectiveManifest(cfg, state); err != nil {
				t.Fatalf("adapted bytes do not match the effective manifest: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(deploymentPath(cfg, state.DeploymentID), "stage", "config.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			assertDocument(data)
		})
	}
}

func TestNativeAIODNSOnlyRequiresAddressForEnabledAutoRedirect(t *testing.T) {
	detections := 0
	addressAvailable := false
	stubAgentIPv4(t, func() (string, error) {
		detections++
		if addressAvailable {
			return "192.168.0.33", nil
		}
		return "", errors.New("no LAN interface")
	})
	for _, input := range []string{"mode: rule\n", "tun: {}\n", "tun: {enable: false, auto-redirect: true}\n", "tun: {enable: true, auto-redirect: true}\nx-configflow-linux-gateway: {enabled: true}\n", "marker: &marker {enabled: true}\ntun: {enable: true, auto-redirect: true}\nx-configflow-linux-gateway: *marker\n", "marker: &marker {enabled: true}\ntun: {enable: true, auto-redirect: true}\nx-configflow-linux-gateway: {<<: *marker}\n"} {
		got, err := adaptNativeAIODNS(&Config{NativeAIO: true}, input)
		if err != nil || got != input {
			t.Fatalf("inactive or gateway TUN changed: %v\n%s", err, got)
		}
	}
	if detections != 0 {
		t.Fatal("inactive or gateway TUN triggered native address detection")
	}
	for _, address := range []string{"", "0.0.0.0", "127.0.0.1", "169.254.0.1", "224.0.0.1", "255.255.255.255", "::1", "2001:db8::1", "example.com", "192.168.0.33/24"} {
		if _, err := adaptNativeAIODNS(&Config{NativeAIO: true, AgentIP: address}, "tun: {enable: true, auto-redirect: true}\n"); err == nil || !strings.Contains(err.Error(), "agent_ip") {
			t.Fatalf("missing/invalid host address %q did not give actionable failure: %v", address, err)
		}
	}
	addressAvailable = true
	for _, exclusions := range []string{"192.168.0.33/32", "{}", "[invalid]", "[false]", "[{}]"} {
		if _, err := adaptNativeAIODNS(&Config{NativeAIO: true, AgentIP: "192.168.0.33"}, "tun: {enable: true, auto-redirect: true, route-exclude-address: "+exclusions+"}\n"); err == nil {
			t.Fatalf("invalid exclusion list accepted: %s", exclusions)
		}
	}
	for _, input := range []string{"", "null", "[]", "tun: []", "tun: false", "tun: 53"} {
		if _, err := adaptNativeAIODNS(&Config{NativeAIO: true}, input); err == nil {
			t.Fatalf("invalid config/TUN mapping accepted: %q", input)
		}
	}
	addressAvailable = false
	for _, marker := range []string{"null", "false", "[]", "{enabled: false}", "{enabled: 'true'}"} {
		if _, err := adaptNativeAIODNS(&Config{NativeAIO: true}, "tun: {enable: true, auto-redirect: true}\nx-configflow-linux-gateway: "+marker); err == nil || !strings.Contains(err.Error(), "agent_ip") {
			t.Fatalf("non-gateway marker bypassed native address validation: %s (%v)", marker, err)
		}
	}
}

func TestNativeAIODNSInactiveSwitchesPreserveConfiguration(t *testing.T) {
	stubAgentIPv4(t, func() (string, error) {
		t.Error("inactive auto-redirect must not detect a LAN address")
		return "", errors.New("no LAN interface")
	})
	values := []string{"true", "false", "null", "missing"}
	for _, enable := range values {
		for _, redirect := range values {
			if enable == "true" && redirect == "true" {
				continue
			}
			for _, withExclusions := range []bool{false, true} {
				t.Run(fmt.Sprintf("enable-%s/redirect-%s/exclusions-%t", enable, redirect, withExclusions), func(t *testing.T) {
					cfg := testDeploymentConfig(t)
					cfg.NativeAIO = true
					input := "# Preserve this comment and the exact YAML formatting.\nmode: direct\ndns: {enable: false, listen: '127.0.0.1:5353'}\ntun:\n  auto-route: false\n  auto-detect-interface: false\n"
					if enable != "missing" {
						input += "  enable: " + enable + " # User's TUN switch\n"
					}
					if redirect != "missing" {
						input += "  auto-redirect: " + redirect + " # User's redirect switch\n"
					}
					if withExclusions {
						input += "  route-exclude-address: [10.0.0.0/8, 127.0.0.0/8, 192.168.0.33/32] # Keep existing exclusions\n"
					}
					for _, address := range []string{"", "not-an-ip"} {
						cfg.AgentIP = address
						got, err := adaptNativeAIODNS(cfg, input)
						if err != nil || got != input {
							t.Fatalf("inactive redirect changed bytes or required AgentIP %q: %v\n%s", address, err, got)
						}
					}
					// The rest of staging serializes YAML; its values must still be
					// identical, including omitted fields and preexisting exclusions.
					cfg.AgentIP = ""
					state := submitBundle(t, cfg, "inactive-redirect", map[string][]byte{"config.yaml": []byte(input)}, false)
					if state.Status != "ready" {
						t.Fatalf("inactive redirect publication failed: %+v", state)
					}
					if err := loadEffectiveManifest(cfg, state); err != nil {
						t.Fatalf("staged content does not match its manifest: %v", err)
					}
					data, err := os.ReadFile(filepath.Join(deploymentPath(cfg, state.DeploymentID), "stage", "config.yaml"))
					if err != nil {
						t.Fatal(err)
					}
					var actual, expected map[string]any
					if err := yaml.Unmarshal(data, &actual); err != nil {
						t.Fatal(err)
					}
					if err := yaml.Unmarshal([]byte(input), &expected); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(actual, expected) {
						t.Fatalf("inactive redirect staging changed configuration:\n%s", data)
					}
				})
			}
		}
	}
}

func TestNativeAIODNSDoesNotGateOnOtherTUNOptions(t *testing.T) {
	stubAgentIPv4(t, func() (string, error) { return "192.168.0.33", nil })
	// Core validation owns auto-route's dependency rules. Host protection must
	// only inspect enable/auto-redirect and preserve all other TUN options.
	for _, option := range []string{"auto-route", "auto-detect-interface"} {
		for _, value := range []string{"true", "false", "null", "missing"} {
			t.Run(option+"/"+value, func(t *testing.T) {
				input := "tun:\n  enable: true\n  auto-redirect: true\n"
				if value != "missing" {
					input += "  " + option + ": " + value + "\n"
				}
				got, err := adaptNativeAIODNS(&Config{NativeAIO: true, AgentIP: "192.168.0.33"}, input)
				if err != nil {
					t.Fatal(err)
				}
				var actual, expected map[string]any
				if err := yaml.Unmarshal([]byte(input), &expected); err != nil {
					t.Fatal(err)
				}
				if err := yaml.Unmarshal([]byte(got), &actual); err != nil {
					t.Fatal(err)
				}
				expected["tun"].(map[string]any)["route-exclude-address"] = []any{"127.0.0.0/8", "192.168.0.33/32"}
				if !reflect.DeepEqual(actual, expected) {
					t.Fatalf("host protection gated on or changed %s: %s", option, got)
				}
			})
		}
	}
}

func TestNativeAIODNSPublicationProtectsHostOnEveryPublish(t *testing.T) {
	for _, mode := range []string{"native", "single", "docker-oxidns", "docker-mosdns"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("ENABLE_OXIDNS", fmt.Sprint(mode == "docker-oxidns"))
			t.Setenv("ENABLE_MOSDNS", fmt.Sprint(mode == "docker-mosdns"))
			cfg := testDeploymentConfig(t)
			cfg.NativeAIO = mode == "native"
			currentAddress := ""
			stubAgentIPv4(t, func() (string, error) {
				if mode != "native" {
					t.Error("ordinary publication must not enumerate LAN addresses")
				}
				return currentAddress, nil
			})
			input := "mode: rule\ndns: {enable: false, listen: '0.0.0.0:5353'}\ntun: {enable: true, auto-route: true, auto-redirect: true, dns-hijack: [any:5353, 'tcp://any:7874'], route-exclude-address: [10.0.0.0/8]}\n"
			for i, address := range []string{"192.168.0.33", "192.168.0.44"} {
				currentAddress = address
				cfg.AgentIP = address
				state := submitBundle(t, cfg, fmt.Sprintf("aio-host-%d", i), map[string][]byte{"config.yaml": []byte(input)}, true)
				if state.Status != "succeeded" {
					t.Fatalf("publication failed: %+v", state)
				}
				data, err := os.ReadFile(cfg.ConfigPath)
				if err != nil {
					t.Fatal(err)
				}
				manifestData, err := os.ReadFile(filepath.Join(deploymentRoot(cfg), "active-manifest.json"))
				if err != nil {
					t.Fatal(err)
				}
				var manifest DeploymentManifest
				if err := json.Unmarshal(manifestData, &manifest); err != nil {
					t.Fatal(err)
				}
				digest, size, err := hashFile(cfg.ConfigPath)
				if err != nil || len(manifest.Files) != 1 || manifest.Files[0].SHA256 != digest || manifest.Files[0].Size != size {
					t.Fatalf("adapted bytes do not match the active manifest: %+v (%v)", manifest.Files, err)
				}
				var doc map[string]any
				if err := yaml.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				tun := doc["tun"].(map[string]any)
				want := []any{"10.0.0.0/8"}
				if mode == "native" {
					want = append(want, "127.0.0.0/8", address+"/32")
				}
				if !reflect.DeepEqual(tun["route-exclude-address"], want) || tun["enable"] != true || tun["auto-route"] != true || tun["auto-redirect"] != true {
					t.Fatalf("publication failed to preserve and protect TUN: %s", data)
				}
				dns := doc["dns"].(map[string]any)
				if dns["enable"] != false || dns["listen"] != "0.0.0.0:5353" || !reflect.DeepEqual(tun["dns-hijack"], []any{"any:5353", "tcp://any:7874"}) {
					t.Fatalf("host protection changed user DNS settings: %s", data)
				}
			}
		})
	}
}

func TestNativeAIODNSInvalidAddressFailsBeforeActivation(t *testing.T) {
	stubAgentIPv4(t, func() (string, error) { return "", errors.New("no LAN interface") })
	cfg := testDeploymentConfig(t)
	cfg.NativeAIO = true
	old := "mode: direct\n"
	if err := os.WriteFile(cfg.ConfigPath, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	running := filepath.Join(filepath.Dir(cfg.ConfigPath), "running")
	if err := os.WriteFile(running, []byte("still running"), 0600); err != nil {
		t.Fatal(err)
	}
	state := submitBundle(t, cfg, "missing-aio-address", map[string][]byte{"config.yaml": []byte("mode: rule\ntun: {enable: true, auto-redirect: true}\n")}, true)
	if state.Status != "failed" || !strings.Contains(state.Error, "agent_ip") {
		t.Fatalf("unsafe publication was not rejected clearly: %+v", state)
	}
	if data, err := os.ReadFile(cfg.ConfigPath); err != nil || string(data) != old {
		t.Fatalf("failed adaptation changed live config: %v %s", err, data)
	}
	if data, err := os.ReadFile(running); err != nil || string(data) != "still running" {
		t.Fatalf("failed adaptation stopped or restarted the service: %v %s", err, data)
	}
}

func TestNativeAIODNSRejectsNonlocalExplicitAddressBeforeMutatingLive(t *testing.T) {
	for _, activate := range []bool{false, true} {
		t.Run(fmt.Sprintf("activate-%t", activate), func(t *testing.T) {
			stubAgentIPv4(t, func() (string, error) { return "192.168.8.2", nil })
			cfg := testDeploymentConfig(t)
			cfg.NativeAIO, cfg.AgentIP = true, "192.168.8.99"
			root := filepath.Dir(cfg.ConfigPath)
			cfg.StopCommand = "printf 'stop\\n' >> lifecycle-actions; rm -f running"
			cfg.StartCommand = "printf 'start\\n' >> lifecycle-actions; touch running"
			live := []byte("mode: direct\n")
			writeActivationFixture(t, cfg.ConfigPath, live, 0600)
			writeActivationFixture(t, filepath.Join(root, "running"), nil, 0600)
			state := submitBundle(t, cfg, "nonlocal-explicit-ip", map[string][]byte{
				"config.yaml": []byte("mode: rule\ntun: {enable: true, auto-redirect: true}\n"),
			}, activate)
			if state.Status != "failed" || !strings.Contains(state.Error, "不属于当前本机") {
				t.Fatalf("publication trusted a nonlocal Agent IP: %+v", state)
			}
			if data, err := os.ReadFile(cfg.ConfigPath); err != nil || !bytes.Equal(data, live) {
				t.Fatalf("rejected publication changed live bytes: %q, %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(root, "running")); err != nil {
				t.Fatalf("rejected publication stopped service: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "lifecycle-actions")); !os.IsNotExist(err) {
				t.Fatalf("rejected publication ran a lifecycle command: %v", err)
			}
			if cfg.AgentIP != "192.168.8.99" {
				t.Fatalf("verification overwrote configured Agent IP: %q", cfg.AgentIP)
			}
		})
	}
}

func TestNativeAIODNSPublicationCoversAllLANsWithExplicitAddress(t *testing.T) {
	for _, explicit := range []string{"192.168.8.2", "10.0.0.2"} {
		t.Run(explicit, func(t *testing.T) {
			stubAgentIPv4s(t, func() ([]string, error) {
				return []string{"192.168.8.2", "10.0.0.2", "192.168.9.2"}, nil
			})
			cfg := testDeploymentConfig(t)
			cfg.NativeAIO, cfg.AgentIP = true, explicit
			const input = "dns: {enable: false, listen: '127.0.0.2:5353'}\ntun: {enable: true, auto-redirect: true, route-exclude-address: [10.0.0.0/8]}\n"
			state := submitBundle(t, cfg, "explicit-multiple-lans", map[string][]byte{"config.yaml": []byte(input)}, false)
			if state.Status != "ready" {
				t.Fatalf("explicit local address blocked preparation: %+v", state)
			}
			prepared, err := os.ReadFile(filepath.Join(deploymentPath(cfg, state.DeploymentID), "stage", "config.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := yaml.Unmarshal(prepared, &doc); err != nil {
				t.Fatal(err)
			}
			want := []any{"10.0.0.0/8", "127.0.0.0/8", "192.168.8.2/32", "192.168.9.2/32"}
			if !reflect.DeepEqual(doc["tun"].(map[string]any)["route-exclude-address"], want) {
				t.Fatalf("explicit Agent IP hid other LANs or widened exclusions: %s", prepared)
			}
			state = activateReadyCandidate(t, cfg, state.DeploymentID)
			if state.Status != "succeeded" {
				t.Fatalf("complete explicit-IP LAN snapshot failed activation: %+v", state)
			}
			if data, err := os.ReadFile(cfg.ConfigPath); err != nil || !bytes.Equal(data, prepared) {
				t.Fatalf("activation changed immutable candidate: %q, %v", data, err)
			}
			if cfg.AgentIP != explicit {
				t.Fatalf("detected address persisted over explicit setting: %q", cfg.AgentIP)
			}
		})
	}
}
