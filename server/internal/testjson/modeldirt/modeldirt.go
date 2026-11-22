// Package modeldirt finds O18 T1c dirty stored data in recorded Python
// inputs shaped like ConfigFlow records (a resolved configuration, a profile,
// shared resources). It only classifies test inputs: corpora use it to mark
// rows whose difference from Python is an accepted O18 T1c difference (plain
// records read a number in a text member as its literal, null and "" as a
// missing member, drop undeclared members and read values of another kind as
// zero). It does not decide anything for the product.
package modeldirt

import (
	"encoding/json"
	"slices"
	"sort"

	"configflow/internal/model"
)

type profileIDShape struct {
	ProfileID string `json:"profile_id"`
}

// Config returns the dirt kinds of a resolved configuration document: the
// profile's members next to the shared lists, system members and profile_id,
// as model.DecodeConfig reads it.
func Config(data []byte) []string {
	var members map[string]json.RawMessage
	if json.Unmarshal(data, &members) != nil {
		return Dirt(data, model.Profile{})
	}
	samples := []any{model.Profile{}, model.Shared{}, model.System{}, profileIDShape{}}
	found := map[string]bool{}
	for key, raw := range members {
		one, _ := json.Marshal(map[string]json.RawMessage{key: raw})
		declared := false
		for _, sample := range samples {
			kinds := Dirt(one, sample)
			if slices.Contains(kinds, DirtUnknown) {
				continue
			}
			declared = true
			for _, k := range kinds {
				found[k] = true
			}
			break
		}
		if !declared {
			found[DirtUnknown] = true
		}
	}
	out := make([]string, 0, len(found))
	for k := range found {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Of returns the dirt kinds of data read as sample's type (Dirt).
func Of(data []byte, sample any) []string { return Dirt(data, sample) }
