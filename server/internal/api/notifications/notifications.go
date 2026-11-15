// Package notifications holds the routes of backend/routes/notifications.py
// (blueprint "notifications", prefix /api/notifications).
//
// POST /read works on its own. GET (the list) needs every notification
// source (internal/notifications.Complete: the task list, Agents) and every
// task-row slot (tasks.Complete: the list reads all task rows); Register adds
// it only then, so call Register after the other packages' wiring.
package notifications

import (
	"errors"
	"os"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/tasks"
)

// Register adds the routes available now.
func Register(r *httpx.Router) {
	if ListComplete() {
		r.GET("/api/notifications", "notifications.list_notifications", auth.Require(listNotifications))
	}
	r.POST("/api/notifications/read", "notifications.read_notifications", auth.Require(readNotifications))
}

// ListComplete reports whether GET /api/notifications can register: every
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
	items, err := notifications.Snapshot(c.Context(), "")
	if err != nil {
		if isOSOrValueError(err) {
			return httpx.Message(503, false, "通知暂时无法读取，请稍后重试。"), nil
		}
		return nil, err
	}
	return httpx.JSON(jsonx.Obj("success", true, "items", items, "checked_at", notifications.Now())).NoStore(), nil
}

func readNotifications(c *httpx.Ctx) (*httpx.Response, error) {
	body := c.JSONObject()
	versions, ok := body.Value("versions").(*jsonx.Object)
	valid := ok && versions != nil && versions.Len() <= 2000
	if valid {
		for key, value := range versions.All() {
			text, isString := value.(string)
			if len([]rune(key)) > 1024 || !isString || len([]rune(text)) > 100 {
				valid = false
				break
			}
		}
	}
	if !valid {
		return httpx.Message(400, false, "请指定要标为已读的通知及其版本。"), nil
	}
	repository, err := repo.GetRepository(c.Context())
	if err != nil {
		return nil, err
	}
	if err := notifications.MarkRead(repository.DataDir(), versions); err != nil {
		return nil, err
	}
	return httpx.JSON(jsonx.Obj("success", true)), nil
}
