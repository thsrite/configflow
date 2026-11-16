package nodes

// This file is the only bridge to the repository's legacy document model.
import (
	"context"
	"encoding/json"
	"fmt"

	"configflow/internal/convert"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/substore"
)

func decodeStored(value any, target any) error {
	data, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
func stored(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}
func storedObject(value any) (*jsonx.Object, error) {
	v, err := stored(value)
	if err != nil {
		return nil, err
	}
	o, ok := v.(*jsonx.Object)
	if !ok {
		return nil, fmt.Errorf("expected object")
	}
	return o, nil
}
func sharedNodes(ctx context.Context) ([]record, error) {
	shared, err := repo.GetSharedConfig(ctx)
	if err != nil {
		return nil, err
	}
	var result []record
	err = decodeStored(shared.Value("nodes"), &result)
	return result, err
}
func mutateNodes(ctx context.Context, mutate func([]record) ([]record, error)) error {
	_, err := repo.UpdateSharedConfigTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		var nodes []record
		if err := decodeStored(shared.Value("nodes"), &nodes); err != nil {
			return nil, err
		}
		nodes, err := mutate(nodes)
		if err != nil {
			return nil, err
		}
		v, err := stored(nodes)
		if err != nil {
			return nil, err
		}
		shared.Set("nodes", v)
		return nil, nil
	})
	return err
}
func reorderNodesStored(ctx context.Context, body record) ([]string, []string, error) {
	var order, missing []string
	err := mutateNodes(ctx, func(nodes []record) ([]record, error) {
		v, err := stored(nodes)
		if err != nil {
			return nil, err
		}
		b, err := storedObject(body)
		if err != nil {
			return nil, err
		}
		list, absent := rules.ResolveNewOrder(v.([]any), b, "nodes")
		missing = absent
		if len(missing) > 0 {
			return nil, &missingOrderError{missing}
		}
		var result []record
		if err := decodeStored(list, &result); err != nil {
			return nil, err
		}
		order = make([]string, len(result))
		for i, item := range result {
			order[i] = item.text("id")
		}
		return result, nil
	})
	return order, missing, err
}
func parseLine(ctx context.Context, line string) json.RawMessage {
	proxy := substore.Default().Client.ConvertProxyString(ctx, line)
	if proxy == nil {
		return nil
	}
	data, _ := jsonx.Compact(proxy)
	return data
}
func reviewedDeletion(ctx context.Context, body record, preview bool) (*httpx.Response, error) {
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	ids, err := stored(body["ids"])
	if err != nil {
		return nil, err
	}
	if preview {
		result, err := repository.PreviewNodeDeletion(ctx, ids)
		return httpx.JSON(result), err
	}
	result, err := repository.DeleteNodesWithReferences(ctx, ids, body.text("confirmation"))
	if err != nil {
		return nil, err
	}
	repo.ResetConfigContext(ctx)
	idsOut := []string{}
	for _, item := range result.GetList("nodes") {
		idsOut = append(idsOut, item.(*jsonx.Object).GetString("id"))
	}
	return nativeJSON(200, struct {
		Success bool     `json:"success"`
		IDs     []string `json:"deleted_ids"`
	}{true, idsOut}), nil
}

// Wire installs the node deletion planner. It does no I/O inside repo's lock.
func Wire() {
	repo.RegisterHooks(repo.Hooks{PlanNodeDeletion: func(document *jsonx.Object, ids any) (*jsonx.Object, *jsonx.Object, error) {
		var doc record
		if err := decodeStored(document, &doc); err != nil {
			return nil, nil, err
		}
		var nodeIDs []string
		if err := decodeStored(ids, &nodeIDs); err != nil {
			return nil, nil, repo.NewValidationError(msgSelectNodes)
		}
		usages := map[string][]string{}
		profiles := doc.object("profiles")
		r := new(repo.Repository)
		for profileID := range profiles {
			view, err := r.CompatConfigFrom(document, profileID)
			if err != nil {
				return nil, nil, err
			}
			for _, node := range view.GetList("nodes") {
				usages[profileID] = append(usages[profileID], node.(*jsonx.Object).GetString("id"))
			}
		}
		dialers := map[string]string{}
		for _, value := range document.GetObject("shared").GetList("nodes") {
			n := value.(*jsonx.Object)
			target, err := convert.RawDialer(n)
			if err != nil {
				return nil, nil, err
			}
			dialers[n.GetString("id")], _ = target.(string)
		}
		plan, candidate, err := planDeletion(doc, nodeIDs, usages, dialers)
		if err != nil {
			return nil, nil, err
		}
		p, err := storedObject(plan)
		if err != nil {
			return nil, nil, err
		}
		d, err := storedObject(candidate)
		return p, d, err
	}})
}
