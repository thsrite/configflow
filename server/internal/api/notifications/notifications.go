// Package notifications holds the routes of backend/routes/notifications.py
// (blueprint "notifications", prefix /api/notifications).
//
// Routes always register. GET reports incomplete source wiring explicitly;
// HEAD projects receipts without committing an observation.
package notifications

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/tasks"
)

// Register adds both routes regardless of source readiness.
func Register(r *httpx.Router) {
	RegisterAlways(r)
}

// ListComplete reports whether GET /api/notifications can observe every
// notification source and every task-row slot is provided.
func ListComplete() bool { return notifications.Complete() && tasks.Complete() }

// RegisterAlways adds both routes regardless of the sources (tests).
func RegisterAlways(r *httpx.Router) {
	r.GET("/api/notifications", "notifications.list_notifications", auth.Require(listNotifications))
	r.POST("/api/notifications/read", "notifications.read_notifications", auth.Require(readNotifications))
}

// isOSOrValueError is `except (OSError, ValueError)`: file system errors,
// a store that does not decode, and task rows that cannot be read.
func isOSOrValueError(err error) bool {
	var pathErr *os.PathError
	var linkErr *os.LinkError
	var syntaxErr *jsonx.SyntaxError
	return errors.As(err, &pathErr) || errors.As(err, &linkErr) || errors.As(err, &syntaxErr) ||
		errors.Is(err, notifications.ErrInvalidStore) || errors.Is(err, tasks.ErrIncomplete) ||
		repo.IsValueError(err) || errors.Is(err, notifications.ErrSourceUnavailable)
}

func listNotifications(c *httpx.Ctx) (*httpx.Response, error) {
	missing := append(tasks.Missing(), notifications.MissingSources()...)
	if len(missing) > 0 {
		return httpx.JSONStatus(503, struct {
			Success bool     `json:"success"`
			Message string   `json:"message"`
			Missing []string `json:"missing_slots"`
		}{false, fmt.Sprintf(msgNotReady, strings.Join(missing, "、")), missing}).NoStore(), nil
	}
	var items []any
	var err error
	if c.Request.Method == http.MethodHead {
		items, err = notifications.Preview(c.Context())
	} else {
		items, err = notifications.Snapshot(c.Context(), "")
	}
	if err != nil {
		if isOSOrValueError(err) {
			return httpx.Message(503, false, msgReadFailed), nil
		}
		return nil, err
	}
	return httpx.JSON(jsonx.Obj("success", true, "items", items, "checked_at", notifications.Now())).NoStore(), nil
}

func readNotifications(c *httpx.Ctx) (*httpx.Response, error) {
	var body readRequest
	if !c.BindSilent(&body) || !body.valid() {
		return httpx.Message(400, false, msgSpecifyVersions), nil
	}
	repository, err := repo.GetRepository(c.Context())
	if err != nil {
		return nil, err
	}
	versions := jsonx.NewObject()
	for key, version := range body.Versions {
		versions.Set(key, version)
	}
	if err := notifications.MarkReadContext(c.Context(), repository.DataDir(), versions); err != nil {
		return nil, err
	}
	return httpx.JSON(jsonx.Obj("success", true)), nil
}
