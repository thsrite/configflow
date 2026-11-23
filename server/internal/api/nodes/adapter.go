package nodes

// Typed repository mutations and the reviewed deletion planner are wired here.
import (
	"context"
	"encoding/json"
	"slices"

	"configflow/internal/convert"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

func sharedNodes(ctx context.Context) ([]model.Node, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := r.Shared(ctx)
	if err != nil {
		return nil, err
	}
	return shared.Nodes, nil
}
func mutateNodes(ctx context.Context, mutate func([]model.Node) ([]model.Node, error)) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	return r.UpdateShared(ctx, func(ctx context.Context, shared *model.Shared) error {
		nodes, err := mutate(slices.Clone(shared.Nodes))
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		shared.Nodes = nodes
		return nil
	})
}
func reorderNodesStored(ctx context.Context, body json.RawMessage) ([]string, []string, error) {
	request, err := rules.DecodeOrder(body, "nodes")
	if err != nil {
		return nil, nil, httpx.BadRequest(err.Error())
	}
	var order, missing []string
	err = mutateNodes(ctx, func(nodes []model.Node) ([]model.Node, error) {
		list, absent := rules.Reorder(nodes, request.IDs, request.Position, func(node model.Node) string { return node.ID })
		missing = absent
		if len(missing) > 0 {
			return nil, &missingOrderError{missing}
		}
		order = make([]string, len(list))
		for i, item := range list {
			order[i] = item.ID
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
		// Plan on a copy: request snapshots stay immutable, even for previews.
		data, err := model.Marshal(document)
		if err != nil {
			return nil, nil, err
		}
		candidate, err := model.DecodeDocument(data)
		if err != nil {
			return nil, nil, err
		}
		preview, err := planDeletion(candidate, ids, usages, dialers)
		if err != nil {
			return nil, nil, err
		}
		return preview, candidate, nil
	}}})
}
