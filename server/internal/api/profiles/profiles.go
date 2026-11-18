// Package profiles exposes profile management and client import endpoints.
// Storage identity, validation and atomic commits remain owned by repo; agent
// binding changes are delegated to the agent manager.
package profiles

import (
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
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

// Register installs profile CRUD, imports, exports and binding endpoints.
// Generation aliases are installed by the generate package.
func Register(r *httpx.Router) {
	r.Handle([]string{"GET", "POST"}, "/api/profiles", "profiles.handle_profiles", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		repository, err := repo.GetRepository(c.Context())
		if err != nil {
			return nil, err
		}
		if c.Method() != "POST" {
			out, err := repository.ListProfiles(c.Context())
			return reply(publicBindings(out), err, 200)
		}
		body, err := objectBody(c, true)
		if err != nil {
			return nil, err
		}
		cloneFrom, textSource := body.Value("clone_from").(string)
		if !textSource && jsonx.Truthy(body.Value("clone_from")) {
			return nil, repo.ValidateProfileID("")
		}
		out, err := mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (*jsonx.Object, error) {
			return r.CreateProfile(ctx, body, cloneFrom)
		})
		return reply(out, err, 201)
	}))
	r.Handle([]string{"GET", "PUT", "DELETE"}, "/api/profiles/<profile_id>", "profiles.handle_profile", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		if c.Method() != "PUT" && c.Method() != "DELETE" {
			r, err := repo.GetRepository(c.Context())
			if err != nil {
				return nil, err
			}
			out, err := r.ProfileMetadata(c.Context(), c.Param("profile_id"))
			return reply(out, err, 200)
		}
		body, err := objectBody(c, true)
		if err != nil {
			return nil, err
		}
		if c.Method() == "PUT" {
			out, err := mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (*jsonx.Object, error) {
				return r.UpdateProfileMetadata(ctx, c.Param("profile_id"), body)
			})
			return reply(out, err, 200)
		}
		target, _ := c.Arg("new_default")
		if target == "" {
			target = body.GetString("new_default")
		}
		_, err = mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (*jsonx.Object, error) {
			return nil, r.DeleteProfile(ctx, c.Param("profile_id"), target)
		})
		if err != nil {
			return nil, err
		}
		return httpx.Empty(204), nil
	}))
	r.POST("/api/profiles/<profile_id>/default", "profiles.set_default_profile", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		_, err := mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (*jsonx.Object, error) {
			_, err := r.SetDefaultProfile(ctx, c.Param("profile_id"))
			return nil, err
		})
		return reply(struct {
			Success bool   `json:"success"`
			Default string `json:"default_profile_id"`
		}{true, c.Param("profile_id")}, err, 200)
	}))
	r.POST("/api/profiles/<profile_id>/clone", "profiles.clone_profile", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, err := objectBody(c, true)
		if err != nil {
			return nil, err
		}
		out, err := mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (*jsonx.Object, error) {
			return r.CloneProfile(ctx, c.Param("profile_id"), body)
		})
		return reply(out, err, 201)
	}))
	r.GET("/api/profiles/<profile_id>/agents", "profiles.profile_agent_bindings", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		r, err := repo.GetRepository(c.Context())
		if err != nil {
			return nil, err
		}
		out, err := r.GetProfileAgentBindings(c.Context(), c.Param("profile_id"))
		return reply(publicBindings(out), err, 200)
	}))
	r.POST("/api/profiles/<profile_id>/agents/rebind", "profiles.rebind_profile_agents", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, err := objectBody(c, false)
		if err != nil {
			return nil, err
		}
		out, err := mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (*jsonx.Object, error) {
			manager, err := agents.Get(ctx)
			if err != nil {
				return nil, err
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return manager.RebindProfileAgents(ctx, c.Param("profile_id"), body.Value("target_profile_id"), body.Value("agent_ids"))
		})
		if err == nil {
			out.Set("success", true)
		}
		return reply(publicBindings(out), err, 200)
	}))
	r.GET("/api/profiles/<profile_id>/export", "profiles.export_profile", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		r, err := repo.GetRepository(c.Context())
		if err != nil {
			return nil, err
		}
		var out *jsonx.Object
		err = r.WithReadSnapshot(c.Context(), func(ctx context.Context) error {
			out, err = r.ExportProfile(ctx, c.Param("profile_id"))
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
	}))
	r.POST("/api/profiles/<profile_id>/import", "profiles.import_profile", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		body, err := objectBody(c, false)
		if err != nil {
			return nil, err
		}
		_, err = mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (*jsonx.Object, error) {
			return r.ImportProfile(ctx, c.Param("profile_id"), body)
		})
		return reply(struct {
			Success bool   `json:"success"`
			Profile string `json:"profile_id"`
		}{true, c.Param("profile_id")}, err, 200)
	}))
	r.POST("/api/profiles/import-client", "profiles.import_client_profile", auth.Require(importClient))
}

// Binding summaries can include credentials inside a legacy host or label.
// Use the shared public-metadata policy without changing stored connections.
func publicBindings(value any) any {
	return redact.SanitizeCredentials(value, nil)
}

func reply(out any, err error, status int) (*httpx.Response, error) {
	if err != nil {
		var conflict *agents.DeploymentConflict
		if errors.As(err, &conflict) {
			return httpx.JSONStatus(409, redact.SanitizeCredentials(jsonx.Obj("success", false, "message", err.Error()), nil)), nil
		}
		return nil, err
	}
	return httpx.JSONStatus(status, out), nil
}

// objectBody is a legacy repo adapter. BindSilent retains the Python silent
// fallback for absent/malformed bodies; schema validation stays in repo.
func objectBody(c *httpx.Ctx, fallback bool) (*jsonx.Object, error) {
	var raw json.RawMessage
	valid := c.BindSilent(&raw)
	if !valid || string(raw) == "null" {
		if fallback {
			return jsonx.NewObject(), nil
		}
		return nil, &httpx.HTTPError{Code: 400, Description: msgObject}
	}
	if fallback {
		switch string(raw) {
		case `false`, `""`, `[]`:
			return jsonx.NewObject(), nil
		}
		var number json.Number
		if len(raw) > 0 && raw[0] != '"' && json.Unmarshal(raw, &number) == nil {
			if value, err := strconv.ParseFloat(string(number), 64); err == nil && value == 0 {
				return jsonx.NewObject(), nil
			}
		}
	}
	var out *jsonx.Object
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		// Metadata update/clone validation is owned by repo. Unlike the
		// collection and DELETE adapters, Python passes these values through
		// and the owner rejects their shape with its normal validation error.
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
func mutate(ctx context.Context, commit func(context.Context, *repo.Repository) (*jsonx.Object, error)) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return commit(ctx, r)
}

func importClient(c *httpx.Ctx) (*httpx.Response, error) {
	body, err := objectBody(c, false)
	if err != nil {
		return nil, err
	}
	name, ok := body.Value("name").(string)
	if !ok || strings.TrimSpace(name) == "" {
		return httpx.Message(400, false, msgName), nil
	}
	var ids []string
	rawIDs := body.Value("default_subscription_ids")
	if jsonx.Truthy(rawIDs) {
		list, ok := jsonx.AsList(rawIDs)
		if !ok {
			return httpx.Message(400, false, msgSubscriptions), nil
		}
		for _, value := range list {
			id, ok := value.(string)
			if !ok {
				continue // Non-string values cannot name a stored subscription.
			}
			ids = append(ids, id)
		}
	}
	plan, err := importer.Parse(body.GetString("client_type"), body.GetString("content"))
	if err != nil {
		return httpx.Message(400, false, err.Error()), nil
	}
	build := func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		raw, err := json.Marshal(shared)
		if err != nil {
			return nil, err
		}
		var resources importer.Shared
		if err = json.Unmarshal(raw, &resources); err != nil {
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
		var out jsonx.Object
		if err = json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
	var out *jsonx.Object
	if jsonx.Truthy(body.Value("dry_run")) {
		r, err := repo.GetRepository(c.Context())
		if err != nil {
			return nil, err
		}
		shared, err := r.GetShared(c.Context())
		if err != nil {
			return nil, err
		}
		out, err = build(c.Context(), shared)
		if err != nil {
			return nil, err
		}
		return httpx.JSON(jsonx.Obj("success", true, "summary", out.Value("summary"), "warnings", out.Value("warnings"))), nil
	}
	out, err = mutate(c.Context(), func(ctx context.Context, r *repo.Repository) (*jsonx.Object, error) {
		return r.ImportClientProfile(ctx, jsonx.Obj("id", body.Value("id"), "name", name, "description", body.Value("description")), build)
	})
	if err != nil {
		return nil, err
	}
	return httpx.JSONStatus(201, jsonx.Obj("success", true, "profile", out.Value("profile"), "summary", out.Value("summary"), "warnings", out.Value("warnings"))), nil
}
