package nodes

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/repo"
)

// member is one request member; see jsondecode.Member.
type member[T any] = jsondecode.Member[T]

func nativeJSON(status int, value any) *httpx.Response {
	data, err := json.Marshal(value)
	if err != nil {
		return httpx.Message(500, false, err.Error())
	}
	return httpx.RawJSON(status, data)
}

// requestObject returns the sent JSON object, or nil for any other body.
func requestObject(c *httpx.Ctx, silent bool) (json.RawMessage, error) {
	var raw json.RawMessage
	if silent {
		// Keep the route's forgiving syntax behavior, but Bind still prevents
		// non-finite tokens from reaching any mutation or probe owner.
		if !c.BindSilent(&raw) {
			if !c.IsJSON() {
				return nil, nil
			}
			data, err := c.Data()
			if err != nil {
				return nil, err
			}
			if _, nonfinite := jsonresp.NormalizeNonFinite(data); nonfinite {
				return nil, httpx.BadRequest("")
			}
			return nil, nil
		}
	} else if err := c.Bind(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil, nil
	}
	return raw, nil
}

// decodeObject decodes an object body into a request DTO; nil for no object.
func decodeObject[T any](c *httpx.Ctx, silent bool) (*T, error) {
	raw, err := requestObject(c, silent)
	if err != nil || raw == nil {
		return nil, err
	}
	out := new(T)
	if err := jsondecode.Unmarshal(raw, out); err != nil {
		return nil, err
	}
	return out, nil
}

// nodeRequest is a node as sent: the stored record (flat protocol settings
// included) and the members checked as sent before it is stored.
type nodeRequest struct {
	node model.Node
	ID   member[string]
	// Identity members spliced into single-line client configurations.
	Name, Type, Server, Port member[string]
	ProxyString              member[string]
	DialerRef                member[json.RawMessage]
}

func decodeNode(c *httpx.Ctx) (*nodeRequest, error) {
	raw, err := requestObject(c, false)
	if err != nil || raw == nil {
		return nil, err
	}
	var checks struct {
		ID          member[string]          `json:"id"`
		Name        member[string]          `json:"name"`
		Type        member[string]          `json:"type"`
		Server      member[string]          `json:"server"`
		Port        member[string]          `json:"port"`
		ProxyString member[string]          `json:"proxy_string"`
		DialerRef   member[json.RawMessage] `json:"dialer_ref"`
	}
	if err := jsondecode.Unmarshal(raw, &checks); err != nil {
		return nil, err
	}
	out := &nodeRequest{ID: checks.ID, Name: checks.Name, Type: checks.Type, Server: checks.Server, Port: checks.Port, ProxyString: checks.ProxyString, DialerRef: checks.DialerRef}
	// The stored record's own decoding: a member of another type is dropped.
	if err := json.Unmarshal(raw, &out.node); err != nil {
		return nil, err
	}
	return out, nil
}

func registerCRUD(r *httpx.Router) {
	r.Handle([]string{"GET", "POST"}, "/api/nodes", "nodes.handle_nodes", auth.Require(handleNodes))
	r.Handle([]string{"DELETE", "PUT"}, "/api/nodes/<node_id>", "nodes.handle_node", auth.Require(handleNode))
	r.POST("/api/nodes/parse-lines", "nodes.parse_node_lines", auth.Require(parseLines))
	r.POST("/api/nodes/reorder", "nodes.reorder_nodes", auth.Require(reorderNodes))
	r.POST("/api/nodes/delete", "nodes.delete_nodes_with_references", auth.Require(deleteNodes))
	r.POST("/api/nodes/delete-preview", "nodes.delete_nodes_with_references", auth.Require(deleteNodes))
}
func handleNodes(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	if c.Request.Method == "GET" {
		list, err := sharedNodes(ctx)
		return nativeJSON(200, list), err
	}
	if c.Request.Method != "POST" {
		return nil, errors.New("unsupported method")
	}
	body, err := decodeNode(c)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return httpx.Message(400, false, msgNodeObject), nil
	}
	if !body.ID.Present() {
		var bytes [4]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return nil, fmt.Errorf("create node ID: %w", err)
		}
		body.node.ID = "node_" + hex.EncodeToString(bytes[:])
	}
	if body.DialerRef.Present() {
		return httpx.Message(400, false, msgSharedDialer), nil
	}
	if err := validateNodeLineFields(body); err != nil {
		return httpx.Message(400, false, err.Error()), nil
	}
	err = mutateNodes(ctx, func(list []model.Node) ([]model.Node, error) { return append(list, body.node), nil })
	return nativeJSON(200, struct {
		Success bool       `json:"success"`
		Data    model.Node `json:"data"`
	}{true, body.node}), err
}
func handleNode(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	deleting := c.Request.Method == "DELETE"
	var body *nodeRequest
	if !deleting {
		var err error
		body, err = decodeNode(c)
		if err != nil {
			return nil, err
		}
		if body == nil {
			return httpx.Message(400, false, msgNodeNonNull), nil
		}
		if body.DialerRef.Present() {
			return httpx.Message(400, false, msgSharedDialer), nil
		}
		if err := validateNodeLineFields(body); err != nil {
			return httpx.Message(400, false, err.Error()), nil
		}
	}
	id := c.Param("node_id")
	err := mutateNodes(ctx, func(list []model.Node) ([]model.Node, error) {
		for i, node := range list {
			if node.ID != id {
				continue
			}
			if deleting {
				return slices.Delete(list, i, i+1), nil
			}
			if body.ID.Present() && (body.ID.Null() || body.ID.Value != id) {
				return nil, repo.NewValidationError(msgImmutableID)
			}
			body.node.ID = id
			list[i] = body.node
			return list, nil
		}
		return nil, &httpx.HTTPError{Code: 404, Description: "Node not found"}
	})
	if err != nil {
		var h *httpx.HTTPError
		if errors.As(err, &h) {
			return httpx.Message(h.Code, false, h.Description), nil
		}
		return nil, err
	}
	if deleting {
		return nativeJSON(200, struct {
			Success bool `json:"success"`
		}{true}), nil
	}
	return nativeJSON(200, struct {
		Success bool       `json:"success"`
		Data    model.Node `json:"data"`
	}{true, body.node}), nil
}
func parseLines(c *httpx.Ctx) (*httpx.Response, error) {
	body, err := decodeObject[struct {
		Lines member[[]*string] `json:"lines"`
	}](c, true)
	if err != nil {
		return nil, err
	}
	if body == nil || !body.Lines.OK || slices.Contains(body.Lines.Value, nil) {
		return httpx.Message(400, false, msgLines), nil
	}
	lines := body.Lines.Value
	if len(lines) > 500 {
		return httpx.Message(400, false, msgLinesLimit), nil
	}
	type result struct {
		Proxy json.RawMessage `json:"proxy,omitempty"`
		Error string          `json:"error,omitempty"`
	}
	results := make([]result, len(lines))
	ctx := c.Context()
	for i, line := range lines {
		if text := strings.TrimSpace(*line); text != "" {
			results[i].Proxy = parseLine(ctx, text)
		}
		if len(results[i].Proxy) == 0 {
			results[i].Error = msgUnknownNode
		}
	}
	return nativeJSON(200, struct {
		Success bool     `json:"success"`
		Results []result `json:"results"`
	}{true, results}), nil
}

type missingOrderError struct{ ids []string }

func (e *missingOrderError) Error() string { return fmt.Sprintf(msgMissingNodeIDs, quotedIDs(e.ids)) }
func quotedIDs(ids []string) string {
	var values []string
	for _, id := range ids {
		values = append(values, "'"+strings.ReplaceAll(strings.ReplaceAll(id, `\`, `\\`), "'", `\'`)+"'")
	}
	return "[" + strings.Join(values, ", ") + "]"
}
func reorderNodes(c *httpx.Ctx) (*httpx.Response, error) {
	body, err := requestObject(c, false)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return httpx.Message(400, false, msgBodyObject), nil
	}
	order, _, err := reorderNodesStored(c.Context(), body)
	var bad *httpx.HTTPError
	if errors.As(err, &bad) && bad.Code == 400 {
		return httpx.Message(400, false, bad.Description), nil
	}
	var missing *missingOrderError
	if errors.As(err, &missing) {
		return httpx.Message(404, false, missing.Error()), nil
	}
	if err != nil {
		return nil, err
	}
	return nativeJSON(200, struct {
		Success bool     `json:"success"`
		Order   []string `json:"order"`
	}{true, order}), nil
}
func deleteNodes(c *httpx.Ctx) (*httpx.Response, error) {
	body, err := decodeObject[struct {
		IDs          member[[]string] `json:"ids"`
		Confirmation member[string]   `json:"confirmation"`
	}](c, true)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return httpx.Message(400, false, msgNodeObject), nil
	}
	// A non-list or non-string member becomes an empty selection, which the
	// planner rejects with msgSelectNodes. The repository checks confirmation
	// first, so /delete keeps Python's order of the two 400 messages.
	response, err := reviewedDeletion(c.Context(), body.IDs.Value, body.Confirmation.Value, strings.HasSuffix(c.Path(), "/delete-preview"))
	if errors.Is(err, repo.ErrNodeDeletionChanged) {
		return nativeJSON(409, struct {
			Success bool   `json:"success"`
			Code    string `json:"code"`
			Message string `json:"message"`
		}{false, "node_deletion_changed", err.Error()}), nil
	}
	var h *httpx.HTTPError
	if errors.As(err, &h) {
		return httpx.Message(h.Code, false, h.Description), nil
	}
	return response, err
}
