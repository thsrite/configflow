package nodes

// Typed repository mutations and the reviewed deletion planner are wired here.
import (
	"context"
	"encoding/json"
	"slices"

	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

func (h *Handler) repo(ctx context.Context) (*repo.Repository, error) {
	if h != nil && h.Repo != nil {
		return h.Repo, nil
	}
	return repo.GetRepository(ctx)
}

func (h *Handler) substore() *substore.Service {
	if h != nil && h.SubStore != nil {
		return h.SubStore
	}
	return substore.NewService(substore.NewEnv())
}

func (h *Handler) sharedNodes(ctx context.Context) ([]model.Node, error) {
	r, err := h.repo(ctx)
	if err != nil {
		return nil, err
	}
	shared, err := r.Shared(ctx)
	if err != nil {
		return nil, err
	}
	return shared.Nodes, nil
}
func (h *Handler) mutateNodes(ctx context.Context, mutate func([]model.Node) ([]model.Node, error)) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := h.repo(ctx)
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
func (h *Handler) reorderNodesStored(ctx context.Context, body json.RawMessage) ([]string, []string, error) {
	request, err := rules.DecodeOrder(body, "nodes")
	if err != nil {
		return nil, nil, httpx.BadRequest(err.Error())
	}
	var order, missing []string
	err = h.mutateNodes(ctx, func(nodes []model.Node) ([]model.Node, error) {
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
func (h *Handler) parseLine(ctx context.Context, line string) json.RawMessage {
	proxy, err := h.substore().Client.ConvertProxy(ctx, line)
	if err != nil || proxy == nil {
		return nil
	}
	data, _ := yamlx.ToJSON(proxy)
	return data
}
func (h *Handler) reviewedDeletion(ctx context.Context, ids []string, confirmation string, preview bool) (*httpx.Response, error) {
	repository, err := h.repo(ctx)
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
