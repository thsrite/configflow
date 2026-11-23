package setup

import (
	"context"
	"encoding/json"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

// resourceNodes keeps cache/filter/conversion decisions with their existing
// owners. Only serialization to the current compatibility APIs lives here.
func resourceNodes(ctx context.Context, fields map[string]json.RawMessage, record *Record) (int, []model.Node, []model.Node, error) {
	count := 0
	output := [][]model.Node{{}, {}}
	var subscriptions []model.Subscription
	if err := json.Unmarshal(fields["subscriptions"], &subscriptions); err != nil {
		return 0, nil, nil, err
	}
	var manual []model.Node
	if err := json.Unmarshal(fields["nodes"], &manual); err != nil {
		return 0, nil, nil, err
	}
	subsByID := map[string]model.Subscription{}
	for i := range subscriptions {
		source := &subscriptions[i]
		if resourceEnabled(source.Enabled) {
			subsByID[source.ID] = *source
		}
	}
	nodesByID := map[string]model.Node{}
	for i := range manual {
		node := &manual[i]
		if resourceEnabled(node.Enabled) {
			nodesByID[node.ID] = *node
		}
	}
	for index, ids := range [][]string{record.SubscriptionIDs, record.NodeIDs} {
		for _, id := range ids {
			nodes := []model.Node{}
			if index == 0 {
				source, ok := subsByID[id]
				if !ok {
					return 0, nil, nil, failure(msgPrepareSource, 409)
				}
				cache, err := substore.LoadCache(ctx, id)
				if err != nil {
					return 0, nil, nil, err
				}
				if cache != nil {
					// The cache metadata records its source URL (null when
					// the subscription had none); a legacy cache without the
					// member is not checked (Python: .get('url', <current>)).
					if cache.Metadata != nil && !cache.Metadata.URLAbsent {
						url, err := json.Marshal(cache.Metadata.URL)
						if err != nil {
							return 0, nil, nil, err
						}
						expected, _, err := model.EncodeMember(&source, "url")
						if err != nil {
							return 0, nil, nil, err
						}
						if !equalJSONValue(url, expected) {
							return 0, nil, nil, failure(msgChangedSourceURL, 400)
						}
					}
					nodes = subs.FilterNodes(source, cache.Nodes)
				}
			} else {
				node, ok := nodesByID[id]
				if !ok {
					return 0, nil, nil, failure(msgPrepareSource, 409)
				}
				nodes = append(nodes, node)
			}
			for i := range nodes {
				node := &nodes[i]
				if !resourceEnabled(node.Enabled) {
					continue
				}
				usable, err := convert.NodeProxy(ctx, *node, substore.NativeConverter())
				if err != nil {
					return 0, nil, nil, err
				}
				if usable == nil {
					continue
				}
				output[index] = append(output[index], *node)
				count++
			}
		}
	}
	if count == 0 {
		return 0, nil, nil, failure(msgNoNodes, 400)
	}
	return count, output[0], output[1], nil
}

func validateGenerated(ctx context.Context, candidate *model.Config, baseURL string) error {
	profile, err := oxidns.NewTypedProfile(candidate)
	if err != nil {
		return err
	}
	dns, err := oxidns.Generate(profile, baseURL)
	if err != nil {
		return err
	}
	document, err := yamlx.Parse([]byte(dns))
	if err != nil {
		return err
	}
	if _, err = oxidns.RequireValidDocument(document); err != nil {
		return err
	}
	generated, err := convert.MihomoConfig(ctx, candidate, convert.MihomoOptions{BaseURL: baseURL})
	if err != nil {
		return err
	}
	document, err = yamlx.Parse([]byte(generated))
	if err != nil {
		return err
	}
	for _, key := range []string{"proxy-groups", "rules"} {
		node := yamlx.Resolve(yamlx.Get(document, key))
		if node == nil || len(node.Content) == 0 {
			return failure(msgGeneratedStrategy, 400)
		}
	}
	return nil
}
