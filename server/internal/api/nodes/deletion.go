package nodes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"configflow/internal/httpx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

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

// planDeletion removes the selected nodes from candidate (a private copy of
// the document) and the references editors can drop: aggregation members and
// group candidates. Chains, rule policies and dialers that use a node block
// the deletion. The preview's confirmation digests what is removed.
func planDeletion(candidate *model.Document, nodeIDs []string, profileNodes map[string][]string, dialers map[string]string) (*repo.NodeDeletionPreview, error) {
	if len(nodeIDs) == 0 {
		return nil, repo.NewValidationError(msgSelectNodes)
	}
	ids := map[string]bool{}
	for _, id := range nodeIDs {
		if strings.TrimSpace(id) == "" {
			return nil, repo.NewValidationError(msgSelectNodes)
		}
		ids[id] = true
	}
	shared := &candidate.Shared
	var selected []json.RawMessage
	kept := []model.Node{}
	names := map[string]bool{}
	preview := &repo.NodeDeletionPreview{Nodes: []repo.DeletionNode{}, Usages: []repo.DeletionUsage{}, Groups: []repo.DeletionGroup{}, Blockers: []string{}, EmptySources: []string{}}
	for _, node := range shared.Nodes {
		if !ids[node.ID] {
			kept = append(kept, node)
			continue
		}
		data, err := model.Marshal(node)
		if err != nil {
			return nil, err
		}
		selected = append(selected, data)
		if !builtin(node.Name) {
			names[node.Name] = true
		}
		preview.Nodes = append(preview.Nodes, repo.DeletionNode{ID: node.ID, Name: node.Name})
	}
	if len(selected) != len(ids) {
		return nil, &httpx.HTTPError{Code: 404, Description: msgNodesGone}
	}
	originals := []json.RawMessage{}
	for i := range shared.Aggregations {
		agg := &shared.Aggregations[i]
		if !intersects(agg.Nodes, ids) {
			continue
		}
		original, err := model.Marshal(agg)
		if err != nil {
			return nil, err
		}
		originals = append(originals, original)
		preview.Usages = append(preview.Usages, repo.DeletionUsage{Kind: "aggregation", ID: agg.ID, Name: agg.Name})
		agg.Nodes = removeIDs(agg.Nodes, ids)
		if len(agg.Nodes) == 0 && len(agg.Subscriptions) == 0 {
			preview.EmptySources = append(preview.EmptySources, fmt.Sprintf(msgAggregationLabel, agg.Name))
		}
	}
	for _, profile := range candidate.Profiles {
		profileID := profile.Key()
		if intersects(profileNodes[profileID], ids) {
			preview.Usages = append(preview.Usages, repo.DeletionUsage{Kind: "profile", ID: profileID, Name: profile.Name})
		}
		for i := range profile.ProxyGroups {
			group := &profile.ProxyGroups[i]
			label := fmt.Sprintf(msgGroupLabel, profile.Name, group.Name)
			if group.Type == "chain" {
				if chain := group.Chain; chain != nil && (usesNode(chain.Entry, ids) || usesNode(chain.Exit, ids)) {
					preview.Blockers = append(preview.Blockers, fmt.Sprintf(msgChainBlocker, label))
				}
				continue
			}
			original, err := model.Marshal(group)
			if err != nil {
				return nil, err
			}
			changed := dropIDs(group.ManualNodes, ids)
			if model.TextValue(group.Source) == "node" {
				changed = dropIDs(group.Proxies, ids) || changed
			}
			if group.ProxiesOrder != nil {
				order := []model.ResourceRef{}
				for _, ref := range *group.ProxiesOrder {
					if !usesNode(ref, ids) {
						order = append(order, ref)
					}
				}
				changed = changed || len(order) != len(*group.ProxiesOrder)
				group.ProxiesOrder = &order
			}
			if !changed {
				continue
			}
			entry, err := json.Marshal(struct {
				ProfileID string          `json:"profile_id"`
				Group     json.RawMessage `json:"group"`
			}{profileID, original})
			if err != nil {
				return nil, err
			}
			originals = append(originals, entry)
			preview.Groups = append(preview.Groups, repo.DeletionGroup{ID: group.ID, Name: group.Name, ProfileID: profileID, ProfileName: profile.Name})
			if !hasCandidates(group) {
				preview.EmptySources = append(preview.EmptySources, label)
			}
		}
		for _, rule := range profile.RuleConfigs {
			policy := model.TextValue(rule.Policy)
			if names[policy] {
				name := rule.Name
				if name == "" {
					name = rule.ID
				}
				preview.Blockers = append(preview.Blockers, fmt.Sprintf(msgRuleBlocker, profile.Name, name, policy))
			}
		}
	}
	for _, node := range shared.Nodes {
		if !ids[node.ID] && names[dialers[node.ID]] {
			preview.Blockers = append(preview.Blockers, fmt.Sprintf(msgDialerBlocker, node.Name))
		}
	}
	// The confirmation is an existing storage/API protocol: sorted UTF-8 JSON,
	// separators comma-space and colon-space, hashed before confirmation is added.
	signature := struct {
		Nodes     []json.RawMessage         `json:"nodes"`
		Originals []json.RawMessage         `json:"originals"`
		Preview   *repo.NodeDeletionPreview `json:"preview"`
	}{selected, originals, preview}
	digest, err := confirmationDigest(signature)
	if err != nil {
		return nil, err
	}
	preview.Confirmation = digest
	shared.Nodes = kept
	return preview, nil
}

func usesNode(ref model.ResourceRef, ids map[string]bool) bool {
	return ref.Type == "node" && ids[ref.ID]
}

// dropIDs removes ids from a present candidate list and reports a change.
func dropIDs(list *[]string, ids map[string]bool) bool {
	if list == nil {
		return false
	}
	kept := removeIDs(*list, ids)
	changed := len(kept) != len(*list)
	*list = kept
	return changed
}

// hasCandidates reports a group with any candidate source left.
func hasCandidates(group *model.ProxyGroup) bool {
	for _, list := range []*[]string{group.ManualNodes, group.Proxies, group.Subscriptions, group.Aggregations, group.IncludeGroups} {
		if list != nil && len(*list) > 0 {
			return true
		}
	}
	return model.TextValue(group.FollowGroup) != ""
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
