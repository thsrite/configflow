// Package tasks holds the routes of backend/routes/tasks.py (blueprint
// "tasks", prefix /api/tasks). Register adds them only when every row slot
// of internal/tasks is provided (call it after the other packages' wiring);
// until then the routes stay unregistered rather than answer with a partial
// list.
package tasks

import (
	"context"
	"errors"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/tasks"
)

// Register adds the routes when the task rows are complete; it reports
// whether it did.
func Register(r *httpx.Router) {
	if !tasks.Complete() {
		return
	}
	RegisterAlways(r)
}

// RegisterAlways adds the routes regardless (tests with fake slots).
func RegisterAlways(r *httpx.Router) {
	r.GET("/api/tasks", "tasks.get_tasks", auth.Require(getTasks))
	r.POST("/api/tasks/run", "tasks.run_task", auth.Require(runTask))
	r.PUT("/api/tasks/interval", "tasks.set_interval", auth.Require(setInterval))
}

func getTasks(c *httpx.Ctx) (*httpx.Response, error) {
	list, err := tasks.ListTasks(c.Context())
	if err != nil {
		return nil, err
	}
	return httpx.JSON(jsonx.Obj("success", true, "tasks", jsonx.ListOf(list))).NoStore(), nil
}

func bodyKey(c *httpx.Ctx) (string, *jsonx.Object) {
	body := c.JSONSilent()
	object, _ := body.(*jsonx.Object)
	key, ok := object.Value("key").(string)
	if object == nil || !ok || key == "" {
		return "", object
	}
	return key, object
}

func message(status int, text string) *httpx.Response {
	return httpx.JSONStatus(status, jsonx.Obj("message", text, "success", false))
}

func runTask(c *httpx.Ctx) (*httpx.Response, error) {
	key, _ := bodyKey(c)
	if key == "" {
		return message(400, "请指定任务"), nil
	}
	category, resource, _ := strings.Cut(key, ":")
	var run tasks.RunFunc
	switch {
	case category == "subscription" || category == "rule_source" || category == "router_rule_sync" ||
		category == "dns_follow" || category == "domain_discovery" || category == "webdav_backup":
		run = tasks.Runner(category)
	case key == tasks.MetricsTaskKey:
		run = tasks.Runner(key)
	}
	if run == nil {
		return message(400, "这个任务不能在这里立即运行"), nil
	}
	err := run(c.Context(), key, resource)
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

func findRow(ctx context.Context, key string) (any, error) {
	list, err := tasks.ListTasks(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range list {
		if row.Value("key") == key {
			return row, nil
		}
	}
	return nil, nil
}

func setInterval(c *httpx.Ctx) (*httpx.Response, error) {
	var body intervalRequest
	if !c.BindSilent(&body) || !body.valid {
		return message(400, msgSpecifyTaskInterval), nil
	}
	key := body.Key
	if key == tasks.MetricsTaskKey {
		setter := tasks.IntervalSetter(key)
		if setter == nil {
			return nil, errors.New("metrics_flush.set_interval is not wired")
		}
		if err := setter(c.Context(), body.intervalValue()); err != nil {
			var invalid *tasks.Invalid
			if errors.As(err, &invalid) {
				return message(400, invalid.Message), nil
			}
			return nil, err
		}
		row, err := findRow(c.Context(), key)
		if err != nil {
			return nil, err
		}
		return httpx.JSON(jsonx.Obj("success", true, "task", row)), nil
	}
	category, resourceID, _ := strings.Cut(key, ":")
	if category != "subscription" && category != "rule_source" {
		return message(400, msgIntervalUnsupported), nil
	}
	interval, err := refresh.ValidateRefreshInterval(body.intervalValue(), msgIntervalLabel)
	if err != nil {
		return message(400, err.Error()), nil
	}
	field, listName := "update_interval", "rule_library"
	if category == "subscription" {
		field, listName = "interval", "subscriptions"
	}
	_, err = repo.UpdateSharedConfigTransaction(c.Context(), func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		items, _ := shared.GetOr(listName, []any{}).([]any)
		for _, value := range items {
			item, ok := value.(*jsonx.Object)
			if ok && item.Value("id") == resourceID {
				item.Set(field, interval)
				return nil, nil
			}
		}
		return nil, &tasks.NotFound{Message: msgSourceMissing}
	})
	var notFound *tasks.NotFound
	if errors.As(err, &notFound) {
		return message(404, notFound.Message), nil
	}
	if err != nil {
		return nil, err
	}
	row, err := findRow(c.Context(), key)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(jsonx.Obj("success", true, "task", row)), nil
}
