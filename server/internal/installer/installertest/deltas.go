// Package installertest provides approved installer source-delta normalization
// for comparisons against historical Python output at test boundaries.
package installertest

import (
	_ "embed"
	"encoding/json"
	"fmt"
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
	Module   string  `json:"module"`
	Deltas   []delta `json:"deltas"`
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
	return baseline(file, text, false)
}

// PythonBaseline reverses the approved D3 additions in fully rendered scripts.
// All other bytes remain available for strict Python-reference comparisons.
func PythonBaseline(text string) (string, error) {
	return baseline("", text, true)
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
