package nodes

// Typed repository mutations and the reviewed deletion planner are wired here.
import (
	"context"
	"encoding/json"

	"configflow/internal/convert"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

func sharedNodes(ctx context.Context) ([]record, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := r.Shared(ctx)
	if err != nil {
		return nil, err
	}
	var result []record
	raw, err := json.Marshal(shared.Nodes)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, &result)
	return result, err
}
func mutateNodes(ctx context.Context, mutate func([]record) ([]record, error)) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	return r.UpdateShared(ctx, func(ctx context.Context, shared *model.Shared) error {
		raw, err := json.Marshal(shared.Nodes)
		if err != nil {
			return err
		}
		var nodes []record
		if err := json.Unmarshal(raw, &nodes); err != nil {
			return err
		}
		nodes, err = mutate(nodes)
		if err != nil {
			return err
		}
		raw, err = json.Marshal(nodes)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return json.Unmarshal(raw, &shared.Nodes)
	})
}
func reorderNodesStored(ctx context.Context, body record) ([]string, []string, error) {
	ids, err := rules.OrderIDs(body, "nodes")
	if err != nil {
		return nil, nil, httpx.BadRequest(err.Error())
	}
	var order, missing []string
	err = mutateNodes(ctx, func(nodes []record) ([]record, error) {
		list, absent := rules.Reorder(nodes, ids, position(body), func(node record) string { return node.text("id") })
		missing = absent
		if len(missing) > 0 {
			return nil, &missingOrderError{missing}
		}
		order = make([]string, len(list))
		for i, item := range list {
			order[i] = item.text("id")
		}
		return list, nil
	})
	return order, missing, err
}
func parseLine(ctx context.Context, line string) json.RawMessage {
	proxy, err := substore.Default().Client.ConvertProxy(ctx, line)
	if err != nil || proxy == nil {
		return nil
	}
	data, _ := yamlx.ToJSON(proxy)
	return data
}
func reviewedDeletion(ctx context.Context, ids []string, confirmation string, preview bool) (*httpx.Response, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	if preview {
		result, err := repository.PreviewNodeDeletionIDs(ctx, ids)
		return httpx.JSON(result), err
	}
	result, err := repository.DeleteNodesWithReferencesIDs(ctx, ids, confirmation)
	if err != nil {
		return nil, err
	}
	repo.ResetConfigContext(ctx)
	idsOut := []string{}
	for _, item := range result.Nodes {
		idsOut = append(idsOut, item.ID)
	}
	return nativeJSON(200, struct {
		Success bool     `json:"success"`
		IDs     []string `json:"deleted_ids"`
	}{true, idsOut}), nil
}

// Wire installs the node deletion planner. It does no I/O inside repo's lock.
func Wire() {
	repo.RegisterHooks(repo.Hooks{RecordHooks: repo.RecordHooks{PlanNodeDeletionRecords: func(document *model.Document, ids []string) (*repo.NodeDeletionPreview, *model.Document, error) {
		data, err := model.Marshal(document)
		if err != nil {
			return nil, nil, err
		}
		var doc record
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, nil, err
		}
		usages := map[string][]string{}
		for _, profile := range document.Profiles {
			view, err := repo.ConfigFrom(document, profile.Key(), nil)
			if err != nil {
				return nil, nil, err
			}
			for _, node := range view.Nodes {
				usages[profile.Key()] = append(usages[profile.Key()], node.ID)
			}
		}
		dialers := map[string]string{}
		for _, node := range document.Shared.Nodes {
			target, err := convert.NodeDialer(node)
			if err != nil {
				return nil, nil, convert.DialerFailure(err)
			}
			target = yamlx.Resolve(target)
			if target != nil && target.ShortTag() == "!!str" {
				dialers[node.ID] = target.Value
			}
		}
		plan, candidate, err := planDeletion(doc, ids, usages, dialers)
		if err != nil {
			return nil, nil, err
		}
		data, err = json.Marshal(plan)
		if err != nil {
			return nil, nil, err
		}
		var preview repo.NodeDeletionPreview
		if err := json.Unmarshal(data, &preview); err != nil {
			return nil, nil, err
		}
		data, err = json.Marshal(candidate)
		if err != nil {
			return nil, nil, err
		}
		docOut, err := model.DecodeDocument(data)
		return &preview, docOut, err
	}}})

}

func position(body record) string {
	if _, present := body["position"]; !present {
		return "top"
	}
	return body.text("position")
}
