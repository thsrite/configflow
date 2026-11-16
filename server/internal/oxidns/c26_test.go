package oxidns

import (
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// TestC26Recorded replays the calls of the C26 entry points (editor,
// device policy, rule-update pipeline) the whole Python test suite makes
// (testdata/c26_record.py). Everything they call is ported, so each row
// runs end to end; outputs, errors (class + Chinese message), in-place
// argument changes and the pipeline context state after the call must match.
func TestC26Recorded(t *testing.T) { replayC26(t, "testdata/c26_recorded.jsonl.gz") }

// TestC26Corpus replays the random corpus of testdata/gen_c26.py (public
// entry points with random device policies, listeners, template inputs,
// resolutions and rule sources).
func TestC26Corpus(t *testing.T) { replayC26(t, "testdata/c26_corpus.jsonl.gz") }

func replayC26(t *testing.T, path string) {
	rows := loadRows(t, path)
	failures, notExpressible := 0, 0
	counts, same := map[string]int{}, map[string]int{}
	for _, entry := range rows {
		fn := entry.GetString("fn")
		counts[fn]++
		if entry.Has("unsupported") {
			continue
		}
		args := copyValue(entry.GetList("args")).([]any)
		restoreAliases(args, entry.GetList("aliases"))
		out, ctx, err, ok := callRowC26(entry, args)
		if !ok {
			notExpressible++
			continue
		}
		msg := compareResult(entry, out, err)
		if msg == "" {
			want := entry.GetList("args")
			if after, changed := entry.Get("args_after"); changed {
				want = after.([]any)
			}
			if got, expected := compact(unmark(args)), compact(want); got != expected {
				msg = "arguments after the call differ:" + firstDifference(expected, got)
			}
		}
		if msg == "" && ctx != nil {
			want := entry.GetObject("ctx")
			if after, changed := entry.Get("ctx_after"); changed {
				want = after.(*jsonx.Object)
			}
			if got, expected := compact(ctxState(ctx)), compact(sortByTag(want)); got != expected {
				msg = "context state differs:" + firstDifference(expected, got)
			}
		}
		if msg == "" {
			same[fn]++
			continue
		}
		failures++
		if failures <= 25 {
			t.Errorf("%s (%s) %.300s: %s", strings.TrimPrefix(entry.GetString("test"), "backend/"), fn,
				compact(entry.GetList("args")), msg)
		}
	}
	t.Logf("identical: %v", same)
	t.Logf("%d rows %v; %d not expressible, %d differ", len(rows), counts, notExpressible, failures)
	if failures > 0 || notExpressible > 0 {
		t.Errorf("%d differ, %d not expressible of %d rows", failures, notExpressible, len(rows))
	}
}

// newRecordedContext rebuilds the _Context the Python call received.
func newRecordedContext(document, settings, state *jsonx.Object) *commonContext {
	ctx := &commonContext{doc: document, settings: settings, attached: state.GetBool("attached"),
		byTag: map[string]*jsonx.Object{}, includedTags: map[string]bool{}}
	plugins := asList(document.Value("plugins"))
	for _, pair := range state.GetList("by_tag") {
		row := pair.([]any)
		if n, ok := row[1].(jsonx.Number); ok {
			index, _ := n.Int64()
			ctx.byTag[row[0].(string)] = plugins[index].(*jsonx.Object)
		} else {
			ctx.byTag[row[0].(string)] = copyValue(row[2]).(*jsonx.Object)
		}
	}
	for _, tag := range state.GetList("included") {
		ctx.includedTags[tag.(string)] = true
	}
	if state.GetBool("parked_live") {
		ctx.parked = settings.Value("_common_disabled").(*jsonx.Object)
	} else {
		ctx.parked = copyValue(state.GetObject("parked")).(*jsonx.Object)
	}
	return ctx
}

// ctxState is c26_record._ctx_state(ctx).
func ctxState(ctx *commonContext) *jsonx.Object {
	plugins := asList(ctx.doc.Value("plugins"))
	byTag := []any{}
	for _, tag := range sortedKeys(tagSet(ctx)) {
		plugin := ctx.byTag[tag]
		index := -1
		for i, element := range plugins {
			if element == any(plugin) {
				index = i
				break
			}
		}
		if index >= 0 {
			byTag = append(byTag, []any{tag, jsonx.Int(int64(index))})
		} else {
			byTag = append(byTag, []any{tag, nil, plugin})
		}
	}
	included := []any{}
	for _, tag := range sortedKeys(ctx.includedTags) {
		included = append(included, tag)
	}
	state := jsonx.Obj("by_tag", byTag, "included", included, "attached", ctx.attached)
	if live, ok := ctx.settings.Value("_common_disabled").(*jsonx.Object); ok && live == ctx.parked {
		state.Set("parked_live", true)
	} else {
		state.Set("parked", ctx.parked)
	}
	return state
}

// sortByTag orders a recorded state's by_tag rows by tag (dict order is
// not part of the behaviour).
func sortByTag(state *jsonx.Object) *jsonx.Object {
	rows := map[string]any{}
	tags := map[string]bool{}
	for _, row := range state.GetList("by_tag") {
		tag := row.([]any)[0].(string)
		rows[tag], tags[tag] = row, true
	}
	sorted := []any{}
	for _, tag := range sortedKeys(tags) {
		sorted = append(sorted, rows[tag])
	}
	out := state.Clone()
	out.Set("by_tag", sorted)
	return out
}

func tagSet(ctx *commonContext) map[string]bool {
	out := map[string]bool{}
	for tag := range ctx.byTag {
		out[tag] = true
	}
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

var missingMark = jsonx.Obj("$missing", true)

func isMissingMark(v any) bool {
	o, ok := v.(*jsonx.Object)
	return ok && o != nil && o.Len() == 1 && o.Value("$missing") == true
}

func unmark(v any) any {
	if v == missingValue {
		return missingMark
	}
	if list, ok := v.([]any); ok {
		out := make([]any, len(list))
		for i, element := range list {
			out[i] = unmark(element)
		}
		return out
	}
	return v
}

// callRowC26 runs one row; ok false: the arguments cannot be expressed.
func callRowC26(entry *jsonx.Object, args []any) (out any, ctx *commonContext, err error, ok bool) {
	defer catch(&err)
	ok = true
	text := func(i int) string {
		s, isText := args[i].(string)
		if !isText {
			ok = false
		}
		return s
	}
	object := func(i int) *jsonx.Object {
		o, isObj := obj(args[i])
		if !isObj {
			ok = false
		}
		return o
	}
	switch entry.GetString("fn") {
	case "prepare_rule_update", "finish_rule_updates", "_stop_pipeline":
		document, settings := object(0), object(1)
		if !ok {
			return
		}
		ctx = newRecordedContext(document, settings, entry.GetObject("ctx"))
		switch entry.GetString("fn") {
		case "prepare_rule_update":
			changes := object(2)
			base := text(4)
			if ok {
				prepareRuleUpdateImpl(ctx, changes, args[3], base)
			}
		case "finish_rule_updates":
			changes := object(2)
			base := text(4)
			if ok {
				finishRuleUpdatesImpl(ctx, changes, args[3], base)
			}
		default:
			stopPipeline(ctx, truthy(args[2]))
		}
	case "reconcile_native_withdrawals":
		before, candidate, original, settings := object(0), object(1), object(2), object(3)
		if ok {
			removed, changed := reconcileNativeWithdrawals(before, candidate, original, settings, args[4])
			out = []any{jsonx.Int(int64(removed)), changed}
		}
	case "rebase_native_rule_sources":
		before, after := object(0), object(1)
		if ok {
			out = rebaseNativeRuleSources(before, after)
		}
	case "prepare_native_rule_updates":
		document, settings, config := object(0), object(1), object(2)
		base := text(3)
		if ok {
			out = prepareNativeRuleUpdates(document, settings, config, base)
		}
	case "revision":
		value, e := Revision(args[0])
		must(e)
		out = value
	case "patch_document":
		document := object(0)
		previous, ok1 := args[1].([]any)
		rows, ok2 := args[2].([]any)
		if ok && ok1 && ok2 {
			out = patchDocument(document, previous, rows, args[3])
		} else {
			ok = false
		}
	case "read_policies":
		out = readPolicies(args[0], args[1])
	case "apply_policies":
		base := text(3)
		if ok {
			out = applyPolicies(args[0], args[1], args[2], base)
		}
	case "entry_context":
		out = entryContext(args[0]).Object()
	case "inspect_entries":
		document := object(0)
		if ok {
			out = inspectEntries(document, routingPlugins(document), args[1])
		}
	case "stored_names":
		out = storedNames(args[0])
	case "template_inputs":
		out = templateInputs(args[0], args[1])
	case "prepare_editor":
		base := text(2)
		if ok {
			out = prepareEditorImpl(args[0], args[1], base)
		}
	case "preview_template":
		base := text(3)
		if ok {
			out = previewTemplate(args[0], args[1], args[2], base, args[4])
		}
	case "_merge":
		for i := 0; i < 3; i++ {
			if isMissingMark(args[i]) {
				args[i] = missingValue
			}
		}
		var path []string
		for _, part := range asList(args[3]) {
			path = append(path, part.(string))
		}
		resolutions := object(4)
		conflicts, isList := args[5].([]any)
		if !ok || !isList {
			ok = false
			return
		}
		out = unmark(mergeValues(args[0], args[1], args[2], path, resolutions, &conflicts))
		args[5] = conflicts
	default:
		return nil, nil, fmt.Errorf("unknown function"), false
	}
	return
}
