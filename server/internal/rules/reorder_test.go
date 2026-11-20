package rules

import (
	"encoding/json"
	"testing"
)

// reorder.py has no direct Python tests; backend/test_aggregation_reorder.py
// drives it through /api/aggregations/reorder (left for package #16). These
// tests pin the same observable behaviour at function level.

type reorderItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func itemID(item any) string {
	if r, ok := item.(reorderItem); ok {
		return r.ID
	}
	if m, ok := item.(map[string]any); ok {
		id, _ := m["id"].(string)
		return id
	}
	return ""
}

func idsOf(t *testing.T, items []any) string {
	t.Helper()
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = itemID(item)
	}
	return compact(t, ids)
}

func collection() []any {
	return []any{
		reorderItem{ID: "a", Name: "A"},
		reorderItem{ID: "b", Name: "B"},
		reorderItem{ID: "c", Name: "C"},
		"stray",
	}
}

func parseOrderBody(t *testing.T, raw string) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func resolveOrder(current []any, body map[string]json.RawMessage, objectKey string) ([]any, []string) {
	ids, err := OrderIDs(body, objectKey)
	if err != nil {
		return current, []string{err.Error()}
	}
	position := "top"
	if raw, ok := body["position"]; ok {
		var p string
		if string(raw) != "null" && json.Unmarshal(raw, &p) == nil {
			position = p
		} else {
			position = ""
		}
	}
	return Reorder(current, ids, position, itemID)
}

func TestReorderByIDsMovesSelectedItemsToTheTopInGivenOrder(t *testing.T) {
	ordered, missing := Reorder(collection(), []string{"c", "a"}, "top", itemID)
	if len(missing) != 0 || idsOf(t, ordered[:3]) != `["c","a","b"]` || ordered[3] != "stray" {
		t.Fatalf("ordered %s missing %v", compact(t, ordered), missing)
	}
}

func TestReorderByIDsBottomKeepsTheRestInRelativeOrder(t *testing.T) {
	ordered, _ := Reorder(collection(), []string{"a"}, "bottom", itemID)
	if got := compact(t, ordered); got != `[{"id":"b","name":"B"},{"id":"c","name":"C"},"stray",{"id":"a","name":"A"}]` {
		t.Fatalf("ordered %s", got)
	}
}

func TestReorderByIDsRejectsUnknownIDsAndLeavesTheCollectionAlone(t *testing.T) {
	items := collection()
	ordered, missing := Reorder(items, []string{"a", "agg_missing"}, "top", itemID)
	if len(missing) != 1 || missing[0] != "agg_missing" || &ordered[0] != &items[0] {
		t.Fatalf("missing %v", missing)
	}
}

func TestResolveNewOrderLegacyObjectArrayOnlySuppliesTheOrder(t *testing.T) {
	body := parseOrderBody(t, `{"aggregations":[{"id":"b","name":"Forged"},{"id":"a"}]}`)
	ordered, problems := resolveOrder(collection(), body, "aggregations")
	if len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}
	if got := ordered[0].(reorderItem).Name; got != "B" {
		t.Fatalf("definition overwritten: %q", got)
	}
	if idsOf(t, ordered[:2]) != `["b","a"]` {
		t.Fatalf("order %s", idsOf(t, ordered[:2]))
	}
}

func TestResolveNewOrderValidation(t *testing.T) {
	for raw, want := range map[string]string{
		`{"aggregations":"x"}`:   "aggregations 必须是对象数组",
		`{"aggregations":["x"]}`: "aggregations 必须是对象数组",
		`{}`:                     "aggregations 必须是对象数组",
		`{"ids":"a"}`:            "ids 必须是数组",
		`{"ids":["a","a"]}`:      "id 必须是互不重复的字符串",
		`{"ids":[1]}`:            "id 必须是互不重复的字符串",
	} {
		_, problems := resolveOrder(collection(), parseOrderBody(t, raw), "aggregations")
		if len(problems) != 1 || problems[0] != want {
			t.Errorf("%s: problems %v, want %q", raw, problems, want)
		}
	}
	_, problems := resolveOrder(collection(), parseOrderBody(t, `{"ids":["zzz"]}`), "aggregations")
	if len(problems) != 1 || problems[0] != "zzz" {
		t.Errorf("unknown id: %v", problems)
	}
	if ordered, _ := resolveOrder(collection(), parseOrderBody(t, `{"ids":["c"],"position":"bottom"}`), "aggregations"); idsOf(t, ordered[len(ordered)-1:]) != `["c"]` {
		t.Errorf("position bottom ignored: %s", compact(t, ordered))
	}
}
