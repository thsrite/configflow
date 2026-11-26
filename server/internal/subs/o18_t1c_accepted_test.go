package subs

import (
	"bytes"
	"encoding/json"

	"configflow/internal/model"
	"configflow/internal/testjson"
)

// o18T1cStoredResources is the part of a Python reference input that Go reads
// as stored plain records (O18 T1c).
type o18T1cStoredResources struct {
	Nodes         []model.Node         `json:"nodes"`
	ProxyGroups   []model.ProxyGroup   `json:"proxy_groups"`
	Subscriptions []model.Subscription `json:"subscriptions"`
	Aggregations  []model.Aggregation  `json:"subscription_aggregations"`
}

// o18T1cDirtyInput reports a Python reference input whose stored records
// carry dirty data: a member of another JSON type (a number as an id, a list
// that is not a list of strings or objects …) or a member the record does not
// declare. Since O18 T1c such stored data reads leniently (zero value or
// dropped, with a load warning) instead of failing with Python's shape
// message, so these rows are an accepted difference ("O18 T1c 接受"); rows
// without such data still compare exactly.
func o18T1cDirtyInput(raw json.RawMessage) bool {
	var probe map[string]json.RawMessage
	if json.Unmarshal(raw, &probe) == nil && probe["id"] != nil && probe["nodes"] == nil && probe["proxy_groups"] == nil {
		// A single stored node (raw_dialer and friends).
		var node model.Node
		issues, err := model.DecodeStored(raw, &node, "")
		return err == nil && len(issues) > 0
	}
	var stored o18T1cStoredResources
	issues, err := model.DecodeStored(raw, &stored, "")
	if err != nil {
		return false
	}
	for _, issue := range issues {
		// Other top-level members of the input are not part of these records.
		for _, list := range []string{"nodes", "proxy_groups", "subscriptions", "subscription_aggregations"} {
			if model.IssueUnder([]model.FieldIssue{issue}, list) {
				return true
			}
		}
	}
	return false
}

// o18T1cChainMissingEndpoint reports an input with a stored chain group whose
// chain object lacks entry or exit, refused by Python with "代理链 chain 必须
// 包含 entry 和 exit". Go reads the missing endpoint as the zero reference and
// refuses it with the same message (subs.chainEndpoints), so this only
// matches if the message regresses again; kept as the documented class.
func o18T1cChainMissingEndpoint(raw json.RawMessage, message string) bool {
	if message != "代理链 chain 必须包含 entry 和 exit" {
		return false
	}
	var input struct {
		ProxyGroups []struct {
			Chain map[string]json.RawMessage `json:"chain"`
		} `json:"proxy_groups"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return false
	}
	for _, group := range input.ProxyGroups {
		if group.Chain == nil {
			continue
		}
		if _, ok := group.Chain["entry"]; !ok {
			return true
		}
		if _, ok := group.Chain["exit"]; !ok {
			return true
		}
	}
	return false
}

// o18T1cSameParsed reports parsed nodes equal to Python's once the empty
// optional members Go leaves out are dropped: a node with "server": "" or
// "port": 0 omits them since O18 T1c (Node members are omitzero; Python
// always wrote them). Any other difference still fails.
func o18T1cSameParsed(want json.RawMessage, got any) bool {
	data, err := json.Marshal(got)
	if err != nil {
		return false
	}
	decode := func(raw []byte) any {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var value any
		if dec.Decode(&value) != nil {
			return nil
		}
		return value
	}
	wantValue, gotValue := decode(want), decode(data)
	dropEmptyServerPort(wantValue, gotValue)
	return testjson.Equal(wantValue, gotValue)
}

func dropEmptyServerPort(want, got any) {
	switch w := want.(type) {
	case []any:
		g, _ := got.([]any)
		for i := range w {
			if i < len(g) {
				dropEmptyServerPort(w[i], g[i])
			}
		}
	case map[string]any:
		g, _ := got.(map[string]any)
		if _, ok := g["server"]; !ok && w["server"] == "" {
			delete(w, "server")
		}
		if _, ok := g["port"]; !ok && w["port"] == json.Number("0") {
			delete(w, "port")
		}
		for key, item := range w {
			dropEmptyServerPort(item, g[key])
		}
	}
}
