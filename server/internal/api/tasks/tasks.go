// Package tasks exposes task status and ready task commands. Missing owners are
// reported explicitly; production never installs empty providers as placeholders.
package tasks

import (
	"errors"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/repo"
	"configflow/internal/tasks"
)

// Handler serves the /api/tasks routes backed by a tasks.Registry and an
// optional explicit Repository.
type Handler struct {
	Tasks *tasks.Registry
	Repo  *repo.Repository
}

// NewHandler constructs a Handler for reg (nil falls back to tasks.Default).
func NewHandler(reg *tasks.Registry) *Handler {
	return &Handler{Tasks: reg}
}

// NewHandlerWithRepo constructs a Handler with an explicit tasks.Registry and
// Repository (nil fields fall back to package/context defaults until W11).
func NewHandlerWithRepo(reg *tasks.Registry, r *repo.Repository) *Handler {
	return &Handler{Tasks: reg, Repo: r}
}

func (h *Handler) registry() *tasks.Registry {
	if h != nil && h.Tasks != nil {
		return h.Tasks
	}
	return tasks.Default()
}

// Register adds the /api/tasks routes to r.
func (h *Handler) Register(r *httpx.Router) {
	r.GET("/api/tasks", "tasks.get_tasks", auth.Require(h.getTasks))
	r.POST("/api/tasks/run", "tasks.run_task", auth.Require(h.runTask))
	r.PUT("/api/tasks/interval", "tasks.set_interval", auth.Require(h.setInterval))
}

// Register always adds the routes; handlers report capability readiness.
func Register(r *httpx.Router) {
	RegisterAlways(r)
}

// RegisterAlways adds the routes regardless (tests with fake slots).
func RegisterAlways(r *httpx.Router) {
	NewHandler(nil).Register(r)
}

func (h *Handler) getTasks(c *httpx.Ctx) (*httpx.Response, error) {
	reg := h.registry()
	if missing := reg.Missing(); len(missing) > 0 {
		return unavailable(missing), nil
	}
	list, err := reg.List(c.Context())
	if err != nil {
		return nil, err
	}
	return httpx.JSON(struct {
		Success bool          `json:"success"`
		Tasks   []*tasks.Task `json:"tasks"`
	}{true, list}).NoStore(), nil
}

func message(status int, text string) *httpx.Response {
	return httpx.Message(status, false, text)
}

func (h *Handler) runTask(c *httpx.Ctx) (*httpx.Response, error) {
	var body runRequest
	if !c.BindSilent(&body) || body.Key == "" {
		return message(400, msgSpecifyTask), nil
	}
	err := h.registry().RunNow(c.Context(), body.Key)
	var notReady *tasks.Unavailable
	if errors.As(err, &notReady) {
		return unavailable(notReady.Slots), nil
	}
	if errors.Is(err, tasks.ErrUnknownTask) {
		return message(400, err.Error()), nil
	}

	var busy *tasks.Busy
	var notFound *tasks.NotFound
	var invalid *tasks.Invalid
	var conflict *tasks.Conflict
	switch {
	case err == nil:
		return httpx.Message(202, true, msgStarted), nil
	case errors.As(err, &conflict):
		return message(409, conflict.Message), nil
	case errors.As(err, &busy):
		return message(409, busy.Message), nil
	case errors.As(err, &notFound):
		return message(404, notFound.Message), nil
	case errors.As(err, &invalid):
		return message(409, invalid.Message), nil
	}
	return nil, err
}

func (h *Handler) setInterval(c *httpx.Ctx) (*httpx.Response, error) {
	var body intervalRequest
	if !c.BindSilent(&body) || !body.valid {
		return message(400, msgSpecifyTaskInterval), nil
	}
	row, err := updateIntervalOn(c.Context(), h.registry(), h.Repo, body)
	var notReady *tasks.Unavailable
	var invalid *tasks.Invalid
	var missing *tasks.NotFound
	switch {
	case errors.As(err, &notReady):
		return unavailable(notReady.Slots), nil
	case errors.As(err, &invalid):
		return message(400, invalid.Message), nil
	case errors.As(err, &missing):
		return message(404, missing.Message), nil
	case err != nil:
		return nil, err
	}
	return httpx.JSON(struct {
		Success bool        `json:"success"`
		Task    *tasks.Task `json:"task"`
	}{true, row}), nil
}

func unavailable(slots []string) *httpx.Response {
	return httpx.JSONStatus(503, struct {
		Success      bool     `json:"success"`
		Message      string   `json:"message"`
		MissingSlots []string `json:"missing_slots"`
	}{false, (&tasks.Unavailable{Slots: slots}).Error(), slots}).NoStore()
}
