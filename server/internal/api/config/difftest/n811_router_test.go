package difftest

import (
	"testing"

	apirouters "configflow/internal/api/routers"
	"configflow/internal/httpx"
)

func TestN811W5RouterNonfiniteConfirmation(t *testing.T) {
	w := newWorld(t)
	apirouters.Register(w.srv.App.Router)
	create := post("/api/routers", map[string]any{"name": "Synthetic", "type": "routeros", "endpoint": "http://192.0.2.1", "username": "operator", "password": "synthetic-password"})
	create.Headers = map[string]string{"Authorization": w.token, "Content-Type": "application/json"}
	py := w.py.send(t, create)
	got := sendGo(t, w.srv, create)
	compare(t, "Python create fixture", py.Status, 200)
	compare(t, "Go create fixture", got.Status, 200)
	pyID := py.Body.(map[string]any)["router"].(map[string]any)["id"].(string)
	goID := got.Body.(map[string]any)["router"].(map[string]any)["id"].(string)
	if pyID == "" || goID == "" {
		t.Fatal("missing router identity")
	}
	pyBefore, goBefore := py.State, got.State
	input := request{Method: "POST", Path: "/api/routers/" + pyID + "/apply", Body: raw("NaN"), Headers: create.Headers}
	py = w.py.send(t, input)
	input.Path = "/api/routers/" + goID + "/apply"
	got = sendGo(t, w.srv, input)
	compare(t, "Python nonfinite confirmation", py.Status, 500)
	compare(t, "Go native confirmation", got.Status, 400)
	compare(t, "Go native confirmation envelope", got.Body, map[string]any{"success": false, "message": httpx.DescBadRequest})
	compare(t, "Python rejected confirmation storage", py.State, pyBefore)
	compare(t, "Go rejected confirmation storage", got.State, goBefore)
}
