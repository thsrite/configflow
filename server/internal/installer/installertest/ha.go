package installertest

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	"configflow/internal/yamlx"
)

//go:embed l1-ha-deltas.json
var haJSON []byte

type haDeltas struct {
	Module       string `json:"module"`
	Port         string `json:"haPortCheck"`
	Ready        string `json:"haNewReadiness"`
	Migration    string `json:"migration"`
	Verification string `json:"verification"`
}

// HAStartupModule returns the exact approved L1 D5 startup checks.
func HAStartupModule() (string, error) {
	var d haDeltas
	err := json.Unmarshal(haJSON, &d)
	return d.Module, err
}

// HAVerificationModule returns the exact approved L1 D6 tool-selection script.
func HAVerificationModule() (string, error) {
	var d haDeltas
	err := json.Unmarshal(haJSON, &d)
	return d.Verification, err
}

func shellWord(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// GuideBaseline reverses fixed approved HA script additions only. Compose and
// every other field remain strict; the image and container name come from that
// same answer's unmodified Compose document, rather than a loose regexp.
func GuideBaseline(data []byte) ([]byte, error) {
	var answer map[string]json.RawMessage
	if err := json.Unmarshal(data, &answer); err != nil {
		return nil, err
	}
	var d haDeltas
	if err := json.Unmarshal(haJSON, &d); err != nil {
		return nil, err
	}
	var verification string
	if json.Unmarshal(answer["verification_command"], &verification) == nil {
		line, _, _ := strings.Cut(verification, "\n")
		if vip, ok := strings.CutPrefix(line, "cf_dns_vip="); ok {
			ip, err := netip.ParseAddr(vip)
			if err == nil && ip.Is4() && verification == strings.ReplaceAll(d.Verification, "{{VIP}}", vip) {
				answer["verification_command"], _ = json.Marshal("nslookup example.com " + vip + "\n")
				return json.Marshal(answer)
			}
		}
		return data, nil
	}
	var command string
	if json.Unmarshal(answer["install_command"], &command) != nil {
		return data, nil
	}
	if !strings.Contains(command, d.Module) {
		return data, nil
	}
	var compose string
	var document struct {
		Services map[string]struct {
			Image string `yaml:"image"`
			Name  string `yaml:"container_name"`
		} `yaml:"services"`
	}
	if err := json.Unmarshal(answer["compose"], &compose); err != nil {
		return nil, err
	}
	if err := yamlx.Unmarshal([]byte(compose), &document); err != nil {
		return nil, err
	}
	main, ok := document.Services["agent"]
	if !ok || main.Image == "" || main.Name == "" {
		return nil, fmt.Errorf("approved HA delta requires agent image/name")
	}
	original := command
	command = strings.ReplaceAll(command, d.Module, "")
	command = strings.ReplaceAll(command, strings.ReplaceAll(d.Port, "{{IMAGE}}", shellWord(main.Image)), "")
	command = strings.ReplaceAll(command, strings.ReplaceAll(d.Ready, "{{NAME}}", shellWord(main.Name)), "")
	command = strings.ReplaceAll(command, d.Migration, "")
	answer["install_command"], _ = json.Marshal(command)
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(answer["command_blocks"], &blocks); err != nil {
		return nil, err
	}
	for _, block := range blocks {
		var id, text string
		_ = json.Unmarshal(block["id"], &id)
		if id != "install" {
			continue
		}
		if json.Unmarshal(block["command"], &text) != nil || text != original {
			return nil, fmt.Errorf("HA install command block differs from install_command")
		}
		block["command"], _ = json.Marshal(command)
	}
	answer["command_blocks"], _ = json.Marshal(blocks)
	return json.Marshal(answer)
}
