package nodes

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsonresp"
	"configflow/internal/repo"
)

// record preserves fields unknown to this route when editing shared resources.
type record map[string]json.RawMessage

func (r record) text(key string) string {
	var value string
	_ = json.Unmarshal(r[key], &value)
	return value
}
func (r record) object(key string) record {
	var value record
	_ = json.Unmarshal(r[key], &value)
	return value
}
func (r record) list(key string) []record {
	var value []record
	_ = json.Unmarshal(r[key], &value)
	return value
}
func (r record) strings(key string) []string {
	var value []string
	_ = json.Unmarshal(r[key], &value)
	return value
}
func (r record) put(key string, value any) { r[key], _ = json.Marshal(value) }
func nativeJSON(status int, value any) *httpx.Response {
	data, err := json.Marshal(value)
	if err != nil {
		return httpx.Message(500, false, err.Error())
	}
	return httpx.RawJSON(status, data)
}
func readObject(c *httpx.Ctx, silent bool) (record, error) {
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
	var object record
	if json.Unmarshal(raw, &object) != nil {
		return nil, nil
	}
	return object, nil
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
	body, err := readObject(c, false)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return httpx.Message(400, false, msgNodeObject), nil
	}
	if _, exists := body["id"]; !exists {
		var bytes [4]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return nil, fmt.Errorf("create node ID: %w", err)
		}
		body.put("id", "node_"+hex.EncodeToString(bytes[:]))
	}
	if _, exists := body["dialer_ref"]; exists {
		return httpx.Message(400, false, msgSharedDialer), nil
	}
	if err := validateNodeLineFields(body); err != nil {
		return httpx.Message(400, false, err.Error()), nil
	}
	err = mutateNodes(ctx, func(list []record) ([]record, error) { return append(list, body), nil })
	return nativeJSON(200, struct {
		Success bool   `json:"success"`
		Data    record `json:"data"`
	}{true, body}), err
}
func handleNode(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	deleting := c.Request.Method == "DELETE"
	var body record
	if !deleting {
		var err error
		body, err = readObject(c, false)
		if err != nil {
			return nil, err
		}
		if body == nil {
			return httpx.Message(400, false, msgNodeNonNull), nil
		}
		if _, exists := body["dialer_ref"]; exists {
			return httpx.Message(400, false, msgSharedDialer), nil
		}
		if err := validateNodeLineFields(body); err != nil {
			return httpx.Message(400, false, err.Error()), nil
		}
	}
	id := c.Param("node_id")
	err := mutateNodes(ctx, func(list []record) ([]record, error) {
		for i, node := range list {
			if node.text("id") != id {
				continue
			}
			if deleting {
				return append(list[:i], list[i+1:]...), nil
			}
			if raw, ok := body["id"]; ok && string(raw) != "null" && body.text("id") != id {
				return nil, repo.NewValidationError(msgImmutableID)
			}
			if raw, ok := body["id"]; ok && string(raw) == "null" {
				return nil, repo.NewValidationError(msgImmutableID)
			}
			body.put("id", id)
			list[i] = body
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
		Success bool   `json:"success"`
		Data    record `json:"data"`
	}{true, body}), nil
}
func parseLines(c *httpx.Ctx) (*httpx.Response, error) {
	body, err := readObject(c, true)
	if err != nil {
		return nil, err
	}
	var lines []string
	if body == nil || json.Unmarshal(body["lines"], &lines) != nil || string(body["lines"]) == "null" {
		return httpx.Message(400, false, msgLines), nil
	}
	// null elements are accepted by encoding/json into strings; reject them explicitly.
	var raw []json.RawMessage
	_ = json.Unmarshal(body["lines"], &raw)
	for _, item := range raw {
		if string(item) == "null" {
			return httpx.Message(400, false, msgLines), nil
		}
	}
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
		if line = strings.TrimSpace(line); line != "" {
			results[i].Proxy = parseLine(ctx, line)
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
	body, err := readObject(c, false)
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
	body, err := readObject(c, true)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return httpx.Message(400, false, msgNodeObject), nil
	}
	// A non-list or non-string member becomes an empty selection, which the
	// planner rejects with msgSelectNodes. The repository checks confirmation
	// first, so /delete keeps Python's order of the two 400 messages.
	var ids []string
	if !strings.HasPrefix(strings.TrimSpace(string(body["ids"])), "[") || json.Unmarshal(body["ids"], &ids) != nil {
		ids = nil
	}
	response, err := reviewedDeletion(c.Context(), ids, body.text("confirmation"), strings.HasSuffix(c.Path(), "/delete-preview"))
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
