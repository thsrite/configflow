// Package installertest provides approved installer source-delta normalization
// for comparisons against historical Python output at test boundaries.
package installertest

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed l1-rollback-deltas.json
var rollbackJSON []byte

// LegacyGeneratorFingerprint identifies the original Python installer revision.
const LegacyGeneratorFingerprint = "4c01744e968c8fa791638b5943d5a61c"

type delta struct {
	File   string `json:"file"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type deltaSet struct {
	Module string  `json:"module"`
	Deltas []delta `json:"deltas"`
	// Wrapper holds rendered-script-only changes outside the templates.
	Wrapper  []delta `json:"rendered"`
	rendered []delta
}

var load = sync.OnceValues(func() (deltaSet, error) {
	var set deltaSet
	if err := json.Unmarshal(rollbackJSON, &set); err != nil {
		return set, fmt.Errorf("read approved installer deltas: %w", err)
	}
	for _, d := range set.Deltas {
		d.After = strings.ReplaceAll(d.After, "{agent_install_rollback}", set.Module)
		set.rendered = append(set.rendered, d)
	}
	set.rendered = append(set.rendered, set.Wrapper...)
	sort.SliceStable(set.Deltas, func(i, j int) bool { return len(set.Deltas[i].After) > len(set.Deltas[j].After) })
	sort.SliceStable(set.rendered, func(i, j int) bool { return len(set.rendered[i].After) > len(set.rendered[j].After) })
	return set, nil
})

// RollbackModule is the exact approved D3 helper, used to detect source drift.
func RollbackModule() (string, error) {
	set, err := load()
	return set.Module, err
}

// TemplateBaseline reverses only the approved changes to one shell template.
func TemplateBaseline(file, text string) (string, error) {
	return baseline(file, BootstrapBaseline(text), false)
}

// PythonBaseline reverses the approved D3 additions in fully rendered scripts.
// All other bytes remain available for strict Python-reference comparisons.
func PythonBaseline(text string) (string, error) {
	return baseline("", BootstrapBaseline(text), true)
}

func baseline(file, text string, rendered bool) (string, error) {
	set, err := load()
	if err != nil {
		return "", err
	}
	changes := set.Deltas
	if rendered {
		if !strings.Contains(text, set.Module) {
			return text, nil
		}
		changes = set.rendered
	}
	// The single-script addition includes the whole shared helper; remove it
	// before smaller additions that may also occur inside that helper.
	for _, d := range changes {
		if file != "" && d.File != file {
			continue
		}
		text = strings.ReplaceAll(text, d.After, d.Before)
	}
	return text, nil
}

var bootstrapJSON = regexp.MustCompile(`,
  "registration_token": "[A-Za-z0-9_-]{43}"
`)
var bootstrapCompose = regexp.MustCompile(`(?m)^      - AGENT_(?:MIHOMO|MOSDNS|OXIDNS)_REGISTRATION_TOKEN=[A-Za-z0-9_-]{43}
`)
var bootstrapRun = regexp.MustCompile(`(?m)^  -e AGENT_(?:MIHOMO|MOSDNS|OXIDNS)_REGISTRATION_TOKEN=[A-Za-z0-9_-]{43} ` + regexp.QuoteMeta("\\") + "\n")
var bootstrapHARun = regexp.MustCompile(`(?m)^  -e 'AGENT_OXIDNS_REGISTRATION_TOKEN=[A-Za-z0-9_-]{43}' ` + regexp.QuoteMeta("\\") + "\n")

// BootstrapBaseline removes only the approved O9-H2 credential fields of their
// exact rendered shapes; malformed proofs and every other script byte remain.
func BootstrapBaseline(text string) string {
	text = bootstrapJSON.ReplaceAllString(text, "\n")
	text = bootstrapCompose.ReplaceAllString(text, "")
	text = bootstrapRun.ReplaceAllString(text, "")
	text = bootstrapHARun.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "token|installation_token|registration_token|registration_key|secret|password", "token|installation_token|secret|password")
	text = strings.ReplaceAll(text, "token|installation_token|registration_token|registration_key|password", "token|installation_token|password")
	return text
}
