// Package tasks exposes task status and ready task commands. Missing owners are
// reported explicitly; production never installs empty providers as placeholders.
package tasks

import (
	"errors"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/tasks"
)

// Register always adds the routes; handlers report capability readiness.
func Register(r *httpx.Router) {
	RegisterAlways(r)
}

// RegisterAlways adds the routes regardless (tests with fake slots).
func RegisterAlways(r *httpx.Router) {
	r.GET("/api/tasks", "tasks.get_tasks", auth.Require(getTasks))
	r.POST("/api/tasks/run", "tasks.run_task", auth.Require(runTask))
	r.PUT("/api/tasks/interval", "tasks.set_interval", auth.Require(setInterval))
}

func getTasks(c *httpx.Ctx) (*httpx.Response, error) {
	if missing := tasks.Missing(); len(missing) > 0 {
		return unavailable(missing), nil
	}
	list, err := tasks.ListTasks(c.Context())
	if err != nil {
		return nil, err
	}
	return httpx.JSON(jsonx.Obj("success", true, "tasks", jsonx.ListOf(list))).NoStore(), nil
}

func message(status int, text string) *httpx.Response {
	return httpx.JSONStatus(status, jsonx.Obj("message", text, "success", false))
}

func runTask(c *httpx.Ctx) (*httpx.Response, error) {
	var body runRequest
	if !c.BindSilent(&body) || body.Key == "" {
		return message(400, msgSpecifyTask), nil
	}
	err := tasks.RunNow(c.Context(), body.Key)
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
		return httpx.JSONStatus(202, jsonx.Obj("message", "已开始运行，结果会显示在列表中", "success", true)), nil
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

func setInterval(c *httpx.Ctx) (*httpx.Response, error) {
	var body intervalRequest
	if !c.BindSilent(&body) || !body.valid {
		return message(400, msgSpecifyTaskInterval), nil
	}
	row, err := updateInterval(c.Context(), body)
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
		Success bool          `json:"success"`
		Task    *jsonx.Object `json:"task"`
	}{true, row}), nil
}

func unavailable(slots []string) *httpx.Response {
	return httpx.JSONStatus(503, struct {
		Success      bool     `json:"success"`
		Message      string   `json:"message"`
		MissingSlots []string `json:"missing_slots"`
	}{false, (&tasks.Unavailable{Slots: slots}).Error(), slots}).NoStore()
}
