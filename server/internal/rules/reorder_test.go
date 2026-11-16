package rules

import (
	"testing"

	"configflow/internal/jsonx"
)

// reorder.py has no direct Python tests; backend/test_aggregation_reorder.py
// drives it through /api/aggregations/reorder (left for package #16). These
// tests pin the same observable behaviour at function level.

func idsOf(t *testing.T, items []any) string {
	t.Helper()
	ids := make([]any, len(items))
	for i, item := range items {
		ids[i] = item.(*jsonx.Object).Value("id")
	}
	return compact(t, ids)
}

func collection() []any {
	return seq(obj("id", "a", "name", "A"), obj("id", "b", "name", "B"), obj("id", "c", "name", "C"), "stray")
}

func TestReorderByIDsMovesSelectedItemsToTheTopInGivenOrder(t *testing.T) {
	ordered, missing := ReorderByIDs(collection(), []string{"c", "a"}, "top")
	if len(missing) != 0 || compact(t, jsonx.ListOf([]any{ordered[0].(*jsonx.Object).Value("id"), ordered[1].(*jsonx.Object).Value("id"), ordered[2].(*jsonx.Object).Value("id")})) != `["c","a","b"]` || ordered[3] != "stray" {
		t.Fatalf("ordered %s missing %v", compact(t, ordered), missing)
	}
}

func TestReorderByIDsBottomKeepsTheRestInRelativeOrder(t *testing.T) {
	ordered, _ := ReorderByIDs(collection(), []string{"a"}, "bottom")
	if got := compact(t, ordered); got != `[{"id":"b","name":"B"},{"id":"c","name":"C"},"stray",{"id":"a","name":"A"}]` {
		t.Fatalf("ordered %s", got)
	}
}

func TestReorderByIDsRejectsUnknownIDsAndLeavesTheCollectionAlone(t *testing.T) {
	items := collection()
	ordered, missing := ReorderByIDs(items, []string{"a", "agg_missing"}, "top")
	if len(missing) != 1 || missing[0] != "agg_missing" || &ordered[0] != &items[0] {
		t.Fatalf("missing %v", missing)
	}
}

func TestResolveNewOrderLegacyObjectArrayOnlySuppliesTheOrder(t *testing.T) {
	body := obj("aggregations", seq(obj("id", "b", "name", "Forged"), obj("id", "a")))
	ordered, problems := ResolveNewOrder(collection(), body, "aggregations")
	if len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}
	if got := ordered[0].(*jsonx.Object).GetString("name"); got != "B" {
		t.Fatalf("definition overwritten: %q", got)
	}
	if idsOf(t, ordered[:2]) != `["b","a"]` {
		t.Fatalf("order %s", idsOf(t, ordered[:2]))
	}
}

func TestResolveNewOrderValidation(t *testing.T) {
	for body, want := range map[*jsonx.Object]string{
		obj("aggregations", "x"):      "aggregations 必须是对象数组",
		obj("aggregations", seq("x")): "aggregations 必须是对象数组",
		obj():                         "aggregations 必须是对象数组",
		obj("ids", "a"):               "ids 必须是数组",
		obj("ids", seq("a", "a")):     "id 必须是互不重复的字符串",
		obj("ids", seq(jsonx.Int(1))): "id 必须是互不重复的字符串",
	} {
		_, problems := ResolveNewOrder(collection(), body, "aggregations")
		if len(problems) != 1 || problems[0] != want {
			t.Errorf("%s: problems %v, want %q", jsonx.Repr(body), problems, want)
		}
	}
	_, problems := ResolveNewOrder(collection(), obj("ids", seq("zzz")), "aggregations")
	if len(problems) != 1 || problems[0] != "zzz" {
		t.Errorf("unknown id: %v", problems)
	}
	if ordered, _ := ResolveNewOrder(collection(), obj("ids", seq("c"), "position", "bottom"), "aggregations"); idsOf(t, ordered[len(ordered)-1:]) != `["c"]` {
		t.Errorf("position bottom ignored: %s", compact(t, ordered))
	}
}
