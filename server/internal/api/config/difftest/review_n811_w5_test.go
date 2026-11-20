package difftest

import (
	"encoding/json"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
)

// withoutEngineSection stores every profile without one engine member, as an
// import of an older backup may (only mihomo/surge/mosdns are required).
func withoutEngineSection(engine string) func(*httpxtest.Server) {
	return func(s *httpxtest.Server) {
		t := s.T
		t.Helper()
		raw, err := s.Repo.ExportAllJSON(s.Context(), false)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err = json.Unmarshal(raw, &document); err != nil {
			t.Fatal(err)
		}
		for _, profile := range document["profiles"].(map[string]any) {
			delete(profile.(map[string]any), engine)
		}
		if raw, err = json.Marshal(document); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Repo.ImportAllJSON(s.Context(), raw); err != nil {
			t.Fatal(err)
		}
	}
}

// Review M1: a profile without a loon section reads as empty settings and a
// save creates the section (Flask setdefault), instead of a 500.
func TestReviewN811W5MissingEngineSection(t *testing.T) {
	w := newWorld(t, withoutEngineSection("loon"))
	got := w.check(request{Method: "GET", Path: "/api/custom-config/loon"})
	compare(t, "missing loon section reads as empty", got.Body, map[string]any{"config": ""})
	w.check(post("/api/custom-config/loon", map[string]string{"config": "[General]\nskip-proxy = 192.0.2.0/24\n"}))
	w.check(request{Method: "GET", Path: "/api/custom-config/loon"})
}

// Review M2: DELETE reads only an optional new_default. An empty body sent with
// a JSON Content-Type keeps Flask's silent {} fallback and deletes; malformed and
// non-finite bodies follow the W5 ruling (400, nothing deleted) where Flask
// deleted (malformed) or crashed (NaN).
func TestReviewN811W5DeleteProfileBody(t *testing.T) {
	headers := func(w *world) map[string]string {
		return map[string]string{"Authorization": w.token, "Content-Type": "application/json"}
	}
	t.Run("empty JSON", func(t *testing.T) {
		w := newWorld(t)
		w.check(post("/api/profiles", map[string]any{"id": "phone", "name": "Phone"}))
		w.check(post("/api/profiles", map[string]any{"id": "tablet", "name": "Tablet"}))
		for _, id := range []string{"phone", "tablet"} {
			text := map[string]string{"phone": "", "tablet": " \n"}[id]
			got := w.check(request{Method: "DELETE", Path: "/api/profiles/" + id, Body: raw(text), Headers: headers(w)})
			compare(t, "empty JSON body deletes "+id, got.Status, 204)
		}
	})
	for _, input := range []struct {
		name, body string
		python     int
	}{{"malformed", "{", 204}, {"nonfinite", "NaN", 500}} {
		t.Run(input.name, func(t *testing.T) {
			w := newWorld(t)
			w.check(post("/api/profiles", map[string]any{"id": "phone", "name": "Phone"}))
			r := request{Method: "DELETE", Path: "/api/profiles/phone", Body: raw(input.body), Headers: headers(w)}
			before := snapshot(t, w.srv)
			py := w.py.send(t, r)
			got := sendGo(t, w.srv, r)
			compare(t, "Python original DELETE body boundary", py.Status, input.python)
			compare(t, "Go native DELETE body status", got.Status, 400)
			compare(t, "Go native DELETE body envelope", got.Body, map[string]any{"success": false, "message": httpx.DescBadRequest})
			compare(t, "Go rejected DELETE storage", got.State, before)
		})
	}
}

// Review M3: Flask validates source, target and "same profile" before the
// reviewed agent_ids, so a request that is wrong in both reports the profile error.
func TestReviewN811W5RebindValidationOrder(t *testing.T) {
	w := newWorld(t)
	for _, payload := range []map[string]any{
		{"target_profile_id": "office", "agent_ids": "not-an-array"},
		{"target_profile_id": nil, "agent_ids": []any{1}},
		{"target_profile_id": "default", "agent_ids": []any{1}},
	} {
		w.check(post("/api/profiles/office/agents/rebind", payload))
	}
}
