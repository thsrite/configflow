package tasks_test

import (
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
)

func TestIntervalBindingRetainsLiteralValidation(t *testing.T) {
	srv := setup(t)
	for _, value := range []string{"60.0", "6e1", "true", "false", `"60"`, "9223372036854775808", "null", "[]", "{}"} {
		srv.Put("/api/tasks/interval", httpxtest.Body([]byte(`{"key":"subscription:s","interval_seconds":`+value+`}`), "application/json")).WantStatus(400).
			WantJSON(jsonx.Obj("success", false, "message", "更新间隔必须为 0（关闭自动更新）或 60 ~ 604800 秒的整数"))
	}
	for _, body := range []string{`{"key":"subscription:s","interval_seconds":60,"extra":0}`, `{"Key":"subscription:s","interval_seconds":60}`, `{"key":"subscription:s","Interval_seconds":60}`, "", `{"key":`, "null", "[]", `{"key":"subscription:s","interval_seconds":NaN}`} {
		srv.Put("/api/tasks/interval", httpxtest.Body([]byte(body), "application/json")).WantStatus(400).
			WantJSON(jsonx.Obj("success", false, "message", "请指定任务和更新间隔"))
	}
	for _, value := range []string{"60", "0", "604800"} {
		srv.Put("/api/tasks/interval", httpxtest.Body([]byte(`{"key":"subscription:s","interval_seconds":`+value+`}`), "application/json")).WantStatus(200)
	}
	srv.Put("/api/tasks/interval", httpxtest.Body([]byte(`{"key":"wrong","key":"subscription:s","interval_seconds":59,"interval_seconds":60}`), "application/json")).WantStatus(200)
}
