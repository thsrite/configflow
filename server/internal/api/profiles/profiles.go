// Package profiles exposes profile management and client import endpoints.
// Storage identity, validation and atomic commits remain owned by repo; agent
// binding changes are delegated to the agent manager.
package profiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/auth"
	"configflow/internal/configexport"
	"configflow/internal/httpx"
	"configflow/internal/importer"
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

// Deps supplies the collaborators for the profiles HTTP Handler.
type Deps struct {
	Repo   *repo.Repository
	Agents *agents.Manager
}

// Handler serves profile CRUD, import/export, and Agent binding HTTP routes.
type Handler struct {
	repo   *repo.Repository
	agents *agents.Manager
}

// New constructs a profiles Handler with explicit dependencies. Nil fields fall
// back to transitional process defaults until W11.
func New(deps Deps) *Handler {
	return &Handler{repo: deps.Repo, agents: deps.Agents}
}

func (h *Handler) repository(ctx context.Context) (*repo.Repository, error) {
	if h != nil && h.repo != nil {
		return h.repo, nil
	}
	return repo.GetRepository(ctx)
}

func (h *Handler) manager(ctx context.Context) (*agents.Manager, error) {
	if h != nil && h.agents != nil {
		return h.agents, nil
	}
	return agents.Get(ctx)
}

// Register installs profile CRUD, imports, exports and binding endpoints.
// Generation aliases are installed by the generate package.
func Register(r *httpx.Router) { New(Deps{}).Register(r) }

// Register mounts profile CRUD, import/export, and binding routes on r.
func (h *Handler) Register(r *httpx.Router) {
	r.Handle([]string{"GET", "POST"}, "/api/profiles", "profiles.handle_profiles", auth.Require(h.handleProfiles))
	r.Handle([]string{"GET", "PUT", "DELETE"}, "/api/profiles/<profile_id>", "profiles.handle_profile", auth.Require(h.handleProfile))
	r.POST("/api/profiles/<profile_id>/default", "profiles.set_default_profile", auth.Require(h.setDefaultProfile))
	r.POST("/api/profiles/<profile_id>/clone", "profiles.clone_profile", auth.Require(h.cloneProfile))
	r.GET("/api/profiles/<profile_id>/agents", "profiles.profile_agent_bindings", auth.Require(h.profileAgentBindings))
	r.POST("/api/profiles/<profile_id>/agents/rebind", "profiles.rebind_profile_agents", auth.Require(h.rebindProfileAgents))
	r.GET("/api/profiles/<profile_id>/export", "profiles.export_profile", auth.Require(h.exportProfile))
	r.POST("/api/profiles/<profile_id>/import", "profiles.import_profile", auth.Require(h.importProfile))
	r.POST("/api/profiles/import-client", "profiles.import_client_profile", auth.Require(h.importClient))
}

func (h *Handler) handleProfiles(c *httpx.Ctx) (*httpx.Response, error) {
	repository, err := h.repository(c.Context())
	if err != nil {
		return nil, err
	}
	if c.Method() != "POST" {
		out, err := repository.ListProfileRecords(c.Context())
		return publicReply(out, err, 200)
	}
	body, err := objectBody(c, true)
	if err != nil {
		return nil, err
	}
	input, err := creation(body, false)
	if err != nil {
		return nil, err
	}
	cloneFrom, err := optionalID(body["clone_from"])
	if err != nil {
		return nil, err
	}
	out, err := h.mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (any, error) {
		return r.CreateProfileRecord(ctx, input, cloneFrom)
	})
	return reply(out, err, 201)
}

func (h *Handler) handleProfile(c *httpx.Ctx) (*httpx.Response, error) {
	if c.Method() != "PUT" && c.Method() != "DELETE" {
		r, err := h.repository(c.Context())
		if err != nil {
			return nil, err
		}
		out, err := r.ProfileMetadataRecord(c.Context(), c.Param("profile_id"))
		return reply(out, err, 200)
	}
	body, err := objectBody(c, true)
	if err != nil {
		return nil, err
	}
	if c.Method() == "PUT" {
		out, err := h.mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (any, error) {
			input, err := metadata(body)
			if err != nil {
				return nil, err
			}
			return r.UpdateProfileMetadataRecord(ctx, c.Param("profile_id"), input)
		})
		return reply(out, err, 200)
	}
	target, _ := c.Arg("new_default")
	if target == "" {
		target = textField(body["new_default"])
	}
	_, err = h.mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (any, error) {
		return nil, r.DeleteProfile(ctx, c.Param("profile_id"), target)
	})
	if err != nil {
		return nil, err
	}
	return httpx.Empty(204), nil
}

func (h *Handler) setDefaultProfile(c *httpx.Ctx) (*httpx.Response, error) {
	_, err := h.mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (any, error) {
		_, err := r.SetDefaultProfile(ctx, c.Param("profile_id"))
		return nil, err
	})
	return reply(struct {
		Success bool   `json:"success"`
		Default string `json:"default_profile_id"`
	}{true, c.Param("profile_id")}, err, 200)
}

func (h *Handler) cloneProfile(c *httpx.Ctx) (*httpx.Response, error) {
	body, err := objectBody(c, true)
	if err != nil {
		return nil, err
	}
	out, err := h.mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (any, error) {
		input, err := creation(body, false)
		if err != nil {
			return nil, err
		}
		return r.CloneProfileRecord(ctx, c.Param("profile_id"), input)
	})
	return reply(out, err, 201)
}

func (h *Handler) profileAgentBindings(c *httpx.Ctx) (*httpx.Response, error) {
	r, err := h.repository(c.Context())
	if err != nil {
		return nil, err
	}
	out, err := r.ProfileAgentBindingsRecord(c.Context(), c.Param("profile_id"))
	return publicReply(out, err, 200)
}

func (h *Handler) rebindProfileAgents(c *httpx.Ctx) (*httpx.Response, error) {
	body, err := objectBody(c, false)
	if err != nil {
		return nil, err
	}
	out, err := h.mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (any, error) {
		manager, err := h.manager(ctx)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		target, err := optionalID(body["target_profile_id"])
		if err != nil {
			return nil, err
		}
		// A non-list or non-string agent_ids is passed as no IDs: RebindProfiles
		// validates source, target and "same profile" first, as Python does.
		var ids []string
		if len(body["agent_ids"]) == 0 || body["agent_ids"][0] != '[' || json.Unmarshal(body["agent_ids"], &ids) != nil {
			ids = nil
		}
		return manager.RebindProfiles(ctx, c.Param("profile_id"), target, ids)
	})
	if err == nil {
		out = struct {
			Success bool `json:"success"`
			*agents.ProfileRebinding
		}{true, out.(*agents.ProfileRebinding)}
	}
	return publicReply(out, err, 200)
}

func (h *Handler) exportProfile(c *httpx.Ctx) (*httpx.Response, error) {
	r, err := h.repository(c.Context())
	if err != nil {
		return nil, err
	}
	var out json.RawMessage
	err = r.WithReadSnapshot(c.Context(), func(ctx context.Context) error {
		out, err = r.ExportProfileJSON(ctx, c.Param("profile_id"))
		if err != nil {
			return err
		}
		out, err = configexport.Sanitize(ctx, out)
		return err
	})
	if err != nil {
		return nil, err
	}
	return httpx.JSON(out).WithHeader("Content-Disposition", "attachment; filename="+c.Param("profile_id")+".json"), nil
}

func (h *Handler) importProfile(c *httpx.Ctx) (*httpx.Response, error) {
	body, err := objectBody(c, false)
	if err != nil {
		return nil, err
	}
	_, err = h.mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (any, error) {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		return r.ImportProfileJSON(ctx, c.Param("profile_id"), raw)
	})
	return reply(struct {
		Success bool   `json:"success"`
		Profile string `json:"profile_id"`
	}{true, c.Param("profile_id")}, err, 200)
}

// Binding summaries can include credentials inside a legacy host or label.
// Use the shared public-metadata policy without changing stored connections.
func publicReply(value any, err error, status int) (*httpx.Response, error) {
	if err != nil {
		return reply(nil, err, status)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return reply(redact.SanitizeCredentials(json.RawMessage(raw), nil), nil, status)
}

func reply(out any, err error, status int) (*httpx.Response, error) {
	if err != nil {
		var conflict *agents.DeploymentConflict
		if errors.As(err, &conflict) {
			return httpx.JSONStatus(409, redact.SanitizeCredentials(map[string]any{"success": false, "message": err.Error()}, nil)), nil
		}
		return nil, err
	}
	return httpx.JSONStatus(status, out), nil
}

// objectBody binds the route's object body and preserves its silent empty fallback.
func objectBody(c *httpx.Ctx, fallback bool) (map[string]json.RawMessage, error) {
	var raw json.RawMessage
	valid := c.BindSilent(&raw)
	// Malformed or non-finite JSON is a 400 (N8-11 W5 ruling). DELETE reads only
	// an optional new_default: an empty body sent with a JSON Content-Type keeps
	// Flask's silent {} fallback, as generic clients send that header everywhere.
	if !valid && fallback && c.IsJSON() && !(c.Method() == "DELETE" && emptyBody(c)) {
		if err := c.Bind(&raw); err != nil {
			return nil, err
		}
	}

	if !valid || string(raw) == "null" {
		if fallback {
			return map[string]json.RawMessage{}, nil
		}
		return nil, &httpx.HTTPError{Code: 400, Description: msgObject}
	}
	if fallback && emptyInput(raw) {
		return map[string]json.RawMessage{}, nil
	}
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil || out == nil {
		if fallback && (c.Method() == "PUT" || c.Endpoint == "profiles.clone_profile") {
			return nil, nil
		}
		status := 400
		if fallback {
			status = 500
		}
		return nil, &httpx.HTTPError{Code: status, Description: msgObject}
	}
	return out, nil
}
func emptyBody(c *httpx.Ctx) bool {
	data, err := c.Data()
	return err == nil && len(bytes.TrimSpace(data)) == 0
}
func emptyInput(raw json.RawMessage) bool {
	switch string(raw) {
	case "", "null", "false", `""`, "[]", "{}":
		return true
	}
	var n json.Number
	if len(raw) > 0 && raw[0] != '"' && json.Unmarshal(raw, &n) == nil {
		v, e := strconv.ParseFloat(string(n), 64)
		return e == nil && v == 0
	}
	return false
}
func textField(raw json.RawMessage) string {
	var text string
	_ = json.Unmarshal(raw, &text)
	return text
}
func optionalID(raw json.RawMessage) (string, error) {
	if emptyInput(raw) {
		return "", nil
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return "", repo.ValidateProfileID("")
	}
	return text, nil
}
func creation(body map[string]json.RawMessage, requireName bool) (repo.ProfileCreation, error) {
	var input repo.ProfileCreation
	if body == nil {
		return input, repo.NewValidationError("Profile metadata must be an object")
	}
	var err error
	input.ID, err = optionalID(body["id"])
	if err != nil {
		return input, err
	}
	if raw := body["name"]; !emptyInput(raw) || requireName {
		if string(raw) == "null" || json.Unmarshal(raw, &input.Name) != nil {
			return input, repo.NewValidationError(msgProfileName)
		}
	}
	if raw := body["description"]; len(raw) > 0 && string(raw) != "null" {
		if json.Unmarshal(raw, &input.Description) != nil {
			return input, repo.NewValidationError(msgMetadataStrings)
		}
	}

	return input, nil
}
func metadata(body map[string]json.RawMessage) (repo.ProfileMetadataUpdate, error) {
	var input repo.ProfileMetadataUpdate
	if body == nil {
		return input, repo.NewValidationError(msgMetadataFields)
	}
	for key := range body {
		if key != "name" && key != "description" {
			return input, repo.NewValidationError(msgMetadataFields)
		}
	}
	for key, raw := range body {
		if key != "name" && key != "description" {
			return input, repo.NewValidationError(msgMetadataFields)
		}
		var text string
		if string(raw) == "null" || json.Unmarshal(raw, &text) != nil {
			return input, repo.NewValidationError(msgMetadataStrings)
		}
		if key == "name" {
			input.Name = &text
		} else {
			input.Description = &text
		}
	}
	return input, nil
}
func (h *Handler) mutate(ctx context.Context, commit func(context.Context, *repo.Repository) (any, error)) (any, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := h.repository(ctx)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return commit(ctx, r)
}
func (h *Handler) importClient(c *httpx.Ctx) (*httpx.Response, error) {
	body, err := objectBody(c, false)
	if err != nil {
		return nil, err
	}
	name := textField(body["name"])
	if strings.TrimSpace(name) == "" {
		return httpx.Message(400, false, msgName), nil
	}
	var ids []string
	if raw := body["default_subscription_ids"]; !emptyInput(raw) {
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil {
			return httpx.Message(400, false, msgSubscriptions), nil
		}
		for _, raw := range values {
			var id string
			if string(raw) != "null" && json.Unmarshal(raw, &id) == nil {
				ids = append(ids, id)
			}
		}
	}
	plan, err := importer.Parse(textField(body["client_type"]), textField(body["content"]))
	if err != nil {
		return httpx.Message(400, false, err.Error()), nil
	}
	build := func(_ context.Context, shared *model.Shared) (*repo.ClientProfileImport, error) {
		raw, err := json.Marshal(shared)
		if err != nil {
			return nil, err
		}
		var resources importer.Shared
		if err = jsondecode.Unmarshal(raw, &resources); err != nil {
			return nil, err
		}
		result, err := importer.Build(plan, resources, importer.BuildOptions{DefaultSubscriptionIDs: ids})
		if err != nil {
			return nil, repo.NewValidationError(err.Error())
		}
		raw, err = json.Marshal(result)
		if err != nil {
			return nil, err
		}
		var wire struct {
			Shared  json.RawMessage `json:"shared"`
			Profile json.RawMessage `json:"profile"`
		}
		if err = json.Unmarshal(raw, &wire); err != nil {
			return nil, repo.NewValidationError(msgImportShape)
		}
		var additions model.Shared
		if len(wire.Shared) > 0 && string(wire.Shared) != "null" {
			if err = model.DecodeInput("shared", wire.Shared, &additions); err != nil {
				return nil, err
			}
		}
		if len(wire.Profile) == 0 || string(wire.Profile) == "null" {
			return nil, repo.NewValidationError(msgImportShape)
		}
		var details model.Fields
		if err = details.UnmarshalJSON(raw); err != nil {
			return nil, err
		}
		return &repo.ClientProfileImport{Shared: additions, Profile: wire.Profile, Details: details}, nil
	}
	if !emptyInput(body["dry_run"]) {
		r, err := h.repository(c.Context())
		if err != nil {
			return nil, err
		}
		shared, err := r.Shared(c.Context())
		if err != nil {
			return nil, err
		}
		out, err := build(c.Context(), shared)
		if err != nil {
			return nil, err
		}
		summary, _ := out.Details.Get("summary")
		warnings, _ := out.Details.Get("warnings")
		return httpx.JSON(map[string]any{"success": true, "summary": summary, "warnings": warnings}), nil
	}
	input, err := creation(body, true)
	if err != nil {
		return nil, err
	}
	out, err := h.mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (any, error) {
		return r.ImportClientProfileRecord(ctx, input, build)
	})
	if err != nil {
		return nil, err
	}
	result := out.(*repo.ClientProfileResult)
	summary, _ := result.Details.Get("summary")
	warnings, _ := result.Details.Get("warnings")
	return httpx.JSONStatus(201, map[string]any{"success": true, "profile": result.Profile, "summary": summary, "warnings": warnings}), nil
}
