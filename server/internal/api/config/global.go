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

// Register installs global backup routes.
func Register(r *httpx.Router) {
	registerCustom(r)
	registerLive(r)
	r.GET("/api/config/export", "config.export_config", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		repository, err := repo.GetRepository(c.Context())
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
	}))
	r.POST("/api/config/import", "config.import_config", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		var raw json.RawMessage
		if err := c.Bind(&raw); err != nil {
			return nil, err
		}
		if len(raw) == 0 || raw[0] != '{' {
			return httpx.Message(400, false, msgObject), nil
		}
		restored, err := writeGlobal(c.Context(), raw, false)
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
	}))
	r.POST("/api/config/reset", "config.reset_config", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		if _, err := writeGlobal(c.Context(), nil, true); err != nil {
			return globalFailure(err)
		}
		return httpx.Message(200, true, msgReset), nil
	}))
}

// writeGlobal imports or resets all data; restored reports Agents imported
// without their run records (they need one publication, O17).
func writeGlobal(ctx context.Context, data json.RawMessage, reset bool) (restored bool, err error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	repository, err := repo.GetRepository(ctx)
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
