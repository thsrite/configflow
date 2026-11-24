// Package config exposes global backup and live configuration HTTP endpoints.
// Repository mutation and generation belong to their native owners.
package config

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
)

// Deps supplies the collaborators for the config HTTP Handler.
type Deps struct {
	Repo *repo.Repository
}

// Handler serves global backup, custom-config, and live-config HTTP routes.
type Handler struct {
	repo *repo.Repository
}

// New constructs a config Handler with explicit dependencies. Nil fields fall
// back to transitional process defaults until W11.
func New(deps Deps) *Handler { return &Handler{repo: deps.Repo} }

func (h *Handler) repository(_ context.Context) (*repo.Repository, error) {
	if h != nil && h.repo != nil {
		return h.repo, nil
	}
	return nil, errors.New("api/config: repository is nil")
}

// Register mounts global backup, custom-config, and live-config routes on r.
func (h *Handler) Register(r *httpx.Router) {
	h.registerCustom(r)
	h.registerLive(r)
	r.GET("/api/config/export", "config.export_config", auth.Require(h.exportConfig))
	r.POST("/api/config/import", "config.import_config", auth.Require(h.importConfig))
	r.POST("/api/config/reset", "config.reset_config", auth.Require(h.resetConfig))
}

func (h *Handler) exportConfig(c *httpx.Ctx) (*httpx.Response, error) {
	repository, err := h.repository(c.Context())
	if err != nil {
		return nil, err
	}
	query, _ := c.Arg("desensitize")
	desensitize := strings.EqualFold(query, "true")
	data, err := repository.ExportAllJSON(c.Context(), desensitize)
	if err != nil {
		return nil, err
	}
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	name := "config.json"
	if desensitize {
		name = "config_desensitized.json"
	}
	return httpx.File(httpx.FileOptions{Data: content, MimeType: "application/json", AsAttachment: true, DownloadName: name})
}

func (h *Handler) importConfig(c *httpx.Ctx) (*httpx.Response, error) {
	var raw json.RawMessage
	if err := c.Bind(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || raw[0] != '{' {
		return httpx.Message(400, false, msgObject), nil
	}
	restored, err := h.writeGlobal(c.Context(), raw, false)
	if err != nil {
		return globalFailure(err)
	}
	if restored {
		// Backups hold configuration only (O17): say that the Agents need one publication.
		return httpx.Message(200, true, msgImportRestored), nil
	}
	return httpx.JSON(struct {
		Success bool `json:"success"`
	}{true}), nil
}

func (h *Handler) resetConfig(c *httpx.Ctx) (*httpx.Response, error) {
	if _, err := h.writeGlobal(c.Context(), nil, true); err != nil {
		return globalFailure(err)
	}
	return httpx.Message(200, true, msgReset), nil
}

// writeGlobal imports or resets all data; restored reports Agents imported
// without their run records (they need one publication, O17).
func (h *Handler) writeGlobal(ctx context.Context, data json.RawMessage, reset bool) (restored bool, err error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	repository, err := h.repository(ctx)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var exported json.RawMessage
	if reset {
		exported, err = repository.ResetAllJSON(ctx)
	} else {
		exported, err = repository.ImportAllJSON(ctx, data)
	}
	if err != nil {
		return false, err
	}
	repo.ResetConfigContext(ctx)
	var imported struct {
		System struct {
			Agents []struct {
				StateRestoredAt *string `json:"state_restored_at"`
			} `json:"agents"`
		} `json:"system"`
		Profiles map[string]struct {
			Routers struct {
				Items []struct {
					StateRestoredAt *string `json:"state_restored_at"`
				} `json:"items"`
			} `json:"routers"`
		} `json:"profiles"`
	}
	if json.Unmarshal(exported, &imported) != nil {
		return false, nil // the import committed; only the extra hint is lost
	}
	for _, agent := range imported.System.Agents {
		if agent.StateRestoredAt != nil {
			return true, nil
		}
	}
	for _, profile := range imported.Profiles {
		for _, router := range profile.Routers.Items {
			if router.StateRestoredAt != nil {
				return true, nil
			}
		}
	}
	return false, nil
}

func globalFailure(err error) (*httpx.Response, error) {
	if errors.Is(err, repo.ErrInUse) {
		return httpx.Message(409, false, err.Error()), nil
	}
	return nil, err
}
