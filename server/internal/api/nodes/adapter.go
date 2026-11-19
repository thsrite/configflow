package nodes

// Typed repository mutations and the reviewed deletion planner are wired here.
import (
	"context"
	"encoding/json"
	"fmt"

	"configflow/internal/convert"
	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

func decodeStored(value any, target any) error {
	data, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(data, target)
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

func position(body record) string {
	if _, present := body["position"]; !present {
		return "top"
	}
	return body.text("position")
}
