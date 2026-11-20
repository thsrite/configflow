package routers

import (
	"testing"

	"configflow/internal/jsonx"
)

func TestRedactHookKeepsOtherFieldsAndDropsSecrets(t *testing.T) {
	doc, err := jsonx.Parse([]byte(`{"items":[{"id":"r1","name":"主路由","username":"admin","password":"pw",
		"endpoint":"http://192.168.1.1","last_probe":{"ok":true},"dns_guard_state":{"x":1},"custom":5},
		"skip"],"other":true}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsonx.Compact(doc)
	if err != nil {
		t.Fatal(err)
	}
	got, err := RedactRouters(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"items":[{"custom":5,"endpoint":"[REDACTED]","id":"r1","name":"主路由","username":"[REDACTED]"}],"other":true}`
	if string(got) != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}
