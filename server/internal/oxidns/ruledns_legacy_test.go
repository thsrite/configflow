package oxidns

import (
	"encoding/json"
	"testing"
)

func TestRuleDNSAgentLegacyNames(t *testing.T) {
	for _, raw := range []string{`0`, `42`, `["legacy",{"name":true}]`, `null`} {
		t.Run(raw, func(t *testing.T) {
			p, err := ProfileFromJSON([]byte(`{"profile_id":"default","agents":[{"id":"dns","name":` + raw + `,"enabled":true,"service_type":"oxidns"}]}`))
			if err != nil {
				t.Fatal(err)
			}
			state, err := ReadRuleDNS(p, RuleDNSReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(state.Engine.Agents) != 1 {
				t.Fatal("missing Agent")
			}
			name := state.Engine.Agents[0].Name
			var actual, expected any
			if err = json.Unmarshal(name, &actual); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal([]byte(raw), &expected); err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(actual)
			b, _ := json.Marshal(expected)
			if string(a) != string(b) {
				t.Fatalf("name=%s want=%s", a, b)
			}
			name[0] = 'x'
			next, err := ReadRuleDNS(p, RuleDNSReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid(next.Engine.Agents[0].Name) {
				t.Fatal("response name shares mutable storage")
			}
		})
	}
}
