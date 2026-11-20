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
		if err := writeGlobal(c.Context(), raw, false); err != nil {
			return globalFailure(err)
		}
		return httpx.JSON(struct {
			Success bool `json:"success"`
		}{true}), nil
	}))
	r.POST("/api/config/reset", "config.reset_config", auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		if err := writeGlobal(c.Context(), nil, true); err != nil {
			return globalFailure(err)
		}
		return httpx.Message(200, true, msgReset), nil
	}))
}

func writeGlobal(ctx context.Context, data json.RawMessage, reset bool) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if reset {
		_, err = repository.ResetAllJSON(ctx)
	} else {
		_, err = repository.ImportAllJSON(ctx, data)
	}
	if err == nil {
		repo.ResetConfigContext(ctx)
	}
	return err
}

func globalFailure(err error) (*httpx.Response, error) {
	if errors.Is(err, repo.ErrInUse) {
		return httpx.Message(409, false, err.Error()), nil
	}
	return nil, err
}
