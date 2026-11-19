package nodes

import (
	"bytes"
	"configflow/internal/jsondecode"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"configflow/internal/httpx"
	"configflow/internal/repo"
)

type deletionNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type deletionUsage struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}
type deletionGroup struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name"`
}
type deletionPreview struct {
	Nodes        []deletionNode  `json:"nodes"`
	Usages       []deletionUsage `json:"usages"`
	Groups       []deletionGroup `json:"groups"`
	Blockers     []string        `json:"blockers"`
	EmptySources []string        `json:"empty_sources"`
	Confirmation string          `json:"confirmation,omitempty"`
}

func removeIDs(values []string, ids map[string]bool) []string {
	out := []string{}
	for _, value := range values {
		if !ids[value] {
			out = append(out, value)
		}
	}
	return out
}
func intersects(values []string, ids map[string]bool) bool {
	for _, value := range values {
		if ids[value] {
			return true
		}
	}
	return false
}
func builtin(name string) bool {
	return name == "DIRECT" || name == "REJECT"
}

func planDeletion(document record, nodeIDs []string, profileNodes map[string][]string, dialers map[string]string) (*deletionPreview, record, error) {
	if len(nodeIDs) == 0 {
		return nil, nil, repo.NewValidationError(msgSelectNodes)
	}
	ids := map[string]bool{}
	for _, id := range nodeIDs {
		if strings.TrimSpace(id) == "" {
			return nil, nil, repo.NewValidationError(msgSelectNodes)
		}
		ids[id] = true
	}
	// Decode a separate candidate: request snapshots remain immutable even for previews.
	data, err := json.Marshal(document)
	if err != nil {
		return nil, nil, err
	}
	var candidate record
	if err := json.Unmarshal(data, &candidate); err != nil {
		return nil, nil, err
	}
	shared := candidate.object("shared")
	nodes := shared.list("nodes")
	selected, kept := []record{}, []record{}
	names := map[string]bool{}
	preview := &deletionPreview{Nodes: []deletionNode{}, Usages: []deletionUsage{}, Groups: []deletionGroup{}, Blockers: []string{}, EmptySources: []string{}}
	for _, node := range nodes {
		if !ids[node.text("id")] {
			kept = append(kept, node)
			continue
		}
		selected = append(selected, node)
		name := node.text("name")
		if !builtin(name) {
			names[name] = true
		}
		preview.Nodes = append(preview.Nodes, deletionNode{node.text("id"), name})
	}
	if len(selected) != len(ids) {
		return nil, nil, &httpx.HTTPError{Code: 404, Description: msgNodesGone}
	}
	originals := []json.RawMessage{}
	aggregations := shared.list("subscription_aggregations")
	for _, agg := range aggregations {
		if !intersects(agg.strings("nodes"), ids) {
			continue
		}
		original, _ := json.Marshal(agg)
		originals = append(originals, original)
		preview.Usages = append(preview.Usages, deletionUsage{"aggregation", agg.text("id"), agg.text("name")})
		agg.put("nodes", removeIDs(agg.strings("nodes"), ids))
		if len(agg.strings("nodes")) == 0 && len(agg.strings("subscriptions")) == 0 {
			preview.EmptySources = append(preview.EmptySources, fmt.Sprintf(msgAggregationLabel, agg.text("name")))
		}
	}
	shared.put("subscription_aggregations", aggregations)
	profiles := candidate.object("profiles")
	// Repository documents retain profile insertion order. The adapter supplies that
	// order separately through the original raw JSON (not Go map iteration).
	profileIDs := objectKeys(document["profiles"])
	for _, profileID := range profileIDs {
		profile := profiles.object(profileID)
		if intersects(profileNodes[profileID], ids) {
			preview.Usages = append(preview.Usages, deletionUsage{"profile", profileID, profile.text("name")})
		}
		groups := profile.list("proxy_groups")
		for _, group := range groups {
			label := fmt.Sprintf(msgGroupLabel, profile.text("name"), group.text("name"))
			if group.text("type") == "chain" {
				chain := group.object("chain")
				for _, raw := range chain {
					var ref struct {
						Type string `json:"type"`
						ID   string `json:"id"`
					}
					_ = jsondecode.Unmarshal(raw, &ref)
					if ref.Type == "node" && ids[ref.ID] {
						preview.Blockers = append(preview.Blockers, fmt.Sprintf(msgChainBlocker, label))
						break
					}
				}
				continue
			}
			before, _ := json.Marshal(group)
			for _, field := range []string{"manual_nodes", "proxies"} {
				if _, exists := group[field]; exists && (field == "manual_nodes" || group.text("source") == "node") {
					group.put(field, removeIDs(group.strings(field), ids))
				}
			}
			if _, exists := group["proxies_order"]; exists {
				order := []record{}
				for _, ref := range group.list("proxies_order") {
					if ref.text("type") != "node" || !ids[ref.text("id")] {
						order = append(order, ref)
					}
				}
				group.put("proxies_order", order)
			}
			after, _ := json.Marshal(group)
			var originalValue, updatedValue any
			_ = json.Unmarshal(before, &originalValue)
			_ = json.Unmarshal(after, &updatedValue)
			if reflect.DeepEqual(originalValue, updatedValue) {
				continue
			}
			var original record
			_ = json.Unmarshal(before, &original)
			entry, _ := json.Marshal(struct {
				ProfileID string `json:"profile_id"`
				Group     record `json:"group"`
			}{profileID, original})
			originals = append(originals, entry)
			preview.Groups = append(preview.Groups, deletionGroup{group.text("id"), group.text("name"), profileID, profile.text("name")})
			empty := true
			for _, key := range []string{"manual_nodes", "proxies", "subscriptions", "aggregations", "include_groups", "follow_group"} {
				if nonempty(group[key]) {
					empty = false
					break
				}
			}
			if empty {
				preview.EmptySources = append(preview.EmptySources, label)
			}
		}
		for _, rule := range profile.list("rule_configs") {
			if names[rule.text("policy")] {
				name := rule.text("name")
				if name == "" {
					name = rule.text("id")
				}
				preview.Blockers = append(preview.Blockers, fmt.Sprintf(msgRuleBlocker, profile.text("name"), name, rule.text("policy")))
			}
		}
		profile.put("proxy_groups", groups)
		profiles.put(profileID, profile)
	}
	for _, node := range nodes {
		if !ids[node.text("id")] && names[dialers[node.text("id")]] {
			preview.Blockers = append(preview.Blockers, fmt.Sprintf(msgDialerBlocker, node.text("name")))
		}
	}
	// The confirmation is an existing storage/API protocol: sorted UTF-8 JSON,
	// separators comma-space and colon-space, hashed before confirmation is added.
	signature := struct {
		Nodes     []record          `json:"nodes"`
		Originals []json.RawMessage `json:"originals"`
		Preview   *deletionPreview  `json:"preview"`
	}{selected, originals, preview}
	digest, err := confirmationDigest(signature)
	if err != nil {
		return nil, nil, err
	}
	preview.Confirmation = digest
	shared.put("nodes", kept)
	candidate.put("shared", shared)
	candidate.put("profiles", profiles)
	return preview, candidate, nil
}
func nonempty(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null" && string(raw) != "[]" && string(raw) != "{}" && string(raw) != `""` && string(raw) != "false" && string(raw) != "0"
}
func objectKeys(raw json.RawMessage) []string {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	_, err := decoder.Token()
	if err != nil {
		return nil
	}
	var keys []string
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			break
		}
		keys = append(keys, key.(string))
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			break
		}
	}
	return keys
}
func confirmationDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return "", err
	}
	var output bytes.Buffer
	if err := writeCanonical(&output, decoded); err != nil {
		return "", err
	}
	digest := sha256.Sum256(output.Bytes())
	return hex.EncodeToString(digest[:]), nil
}
func writeCanonical(out *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteString(", ")
			}
			if err := writeCanonical(out, key); err != nil {
				return err
			}
			out.WriteString(": ")
			if err := writeCanonical(out, v[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteString(", ")
			}
			if err := writeCanonical(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	default:
		var b bytes.Buffer
		encoder := json.NewEncoder(&b)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(value); err != nil {
			return err
		}
		text := bytes.TrimSuffix(b.Bytes(), []byte("\n"))
		text = bytes.ReplaceAll(text, []byte(`\u2028`), []byte("\u2028"))
		text = bytes.ReplaceAll(text, []byte(`\u2029`), []byte("\u2029"))
		out.Write(text)
	}
	return nil
}
