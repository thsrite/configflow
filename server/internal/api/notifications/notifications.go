// Package notifications holds the routes of backend/routes/notifications.py
// (blueprint "notifications", prefix /api/notifications).
//
// Routes always register. GET reports incomplete source wiring explicitly;
// HEAD projects receipts without committing an observation.
package notifications

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/tasks"
)

// Handler serves the /api/notifications routes backed by explicit services.
type Handler struct {
	Tasks         *tasks.Registry
	Notifications *notifications.Service
	Repo          *repo.Repository
}

// NewHandler constructs a Handler; nil fields fall back to package defaults.
func NewHandler(reg *tasks.Registry, svc *notifications.Service, r *repo.Repository) *Handler {
	return &Handler{Tasks: reg, Notifications: svc, Repo: r}
}

func (h *Handler) taskRegistry() *tasks.Registry {
	if h != nil && h.Tasks != nil {
		return h.Tasks
	}
	return tasks.Default()
}

func (h *Handler) notifService() *notifications.Service {
	if h != nil && h.Notifications != nil {
		return h.Notifications
	}
	return notifications.Default()
}

// Register mounts the /api/notifications routes on r.
func (h *Handler) Register(r *httpx.Router) {
	r.GET("/api/notifications", "notifications.list_notifications", auth.Require(h.listNotifications))
	r.POST("/api/notifications/read", "notifications.read_notifications", auth.Require(h.readNotifications))
}

// Register adds both routes regardless of source readiness.
func Register(r *httpx.Router) {
	RegisterAlways(r)
}

// ListComplete reports whether GET /api/notifications can observe every
// notification source and every task-row slot is provided.
func ListComplete() bool { return notifications.Complete() && tasks.Complete() }

// RegisterAlways adds both routes regardless of the sources (tests).
func RegisterAlways(r *httpx.Router) {
	NewHandler(nil, nil, nil).Register(r)
}

// isOSOrValueError is `except (OSError, ValueError)`: file system errors,
// a store that does not decode, and task rows that cannot be read.
func isOSOrValueError(err error) bool {
	var pathErr *os.PathError
	var linkErr *os.LinkError
	var syntaxErr *json.SyntaxError
	return errors.As(err, &pathErr) || errors.As(err, &linkErr) || errors.As(err, &syntaxErr) ||
		errors.Is(err, notifications.ErrInvalidStore) || errors.Is(err, tasks.ErrIncomplete) || errors.Is(err, tasks.ErrInvalidHistory) ||
		errors.Is(err, repo.ErrRejected) || errors.Is(err, notifications.ErrSourceUnavailable)
}

func (h *Handler) listNotifications(c *httpx.Ctx) (*httpx.Response, error) {
	reg := h.taskRegistry()
	svc := h.notifService()
	missing := append(reg.Missing(), svc.MissingSources()...)
	if len(missing) > 0 {
		return httpx.JSONStatus(503, struct {
			Success bool     `json:"success"`
			Message string   `json:"message"`
			Missing []string `json:"missing_slots"`
		}{false, fmt.Sprintf(msgNotReady, strings.Join(missing, "、")), missing}).NoStore(), nil
	}
	var items []*notifications.Item
	var err error
	if c.Request.Method == http.MethodHead {
		items, err = svc.PreviewItems(c.Context())
	} else {
		items, err = svc.SnapshotItems(c.Context(), "")
	}
	if err != nil {
		if isOSOrValueError(err) {
			return httpx.Message(503, false, msgReadFailed), nil
		}
		return nil, err
	}
	return httpx.JSON(struct {
		Success   bool                  `json:"success"`
		Items     []*notifications.Item `json:"items"`
		CheckedAt string                `json:"checked_at"`
	}{true, items, notifications.Now()}).NoStore(), nil
}

func (h *Handler) readNotifications(c *httpx.Ctx) (*httpx.Response, error) {
	var body readRequest
	if !c.BindSilent(&body) || !body.valid() {
		return httpx.Message(400, false, msgSpecifyVersions), nil
	}
	repository := h.Repo
	if repository == nil {
		var err error
		repository, err = repo.GetRepository(c.Context())
		if err != nil {
			return nil, err
		}
	}
	if err := notifications.Dismiss(c.Context(), repository.DataDir(), body.Versions); err != nil {
		return nil, err
	}
	return httpx.JSON(struct {
		Success bool `json:"success"`
	}{true}), nil
}
