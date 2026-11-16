package oxidns

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// TestC25Recorded replays every top-level call of the C25 functions
// (oxidns_common_routing, dns_watchdog) the whole Python test suite makes
// (testdata/c25_record.py). The C26 collaborators (prepare_editor,
// prepare_rule_update, finish_rule_updates) answer with what Python's
// answered during the call (Go must ask the same questions in the same
// order) and rule_ip_presence.classical_ip_lines (routers hook) with
// Python's answer for the same arguments.
func TestC25Recorded(t *testing.T) {
	replayC25(t, "testdata/c25_recorded.jsonl.gz", func(entry *jsonx.Object) string {
		return strings.TrimPrefix(entry.GetString("test"), "backend/")
	})
}

// TestC25Corpus replays the random corpus of testdata/gen_c25.py.
func TestC25Corpus(t *testing.T) {
	replayC25(t, "testdata/c25_corpus.jsonl.gz", nil)
}

// restoreAliases makes the objects at each pair's second path the object at
// its first path again (YAML aliases inside the argument list).
func restoreAliases(root []any, pairs []any) {
	at := func(path []any) any {
		var cur any = root
		for _, part := range path {
			switch p := part.(type) {
			case string:
				cur = cur.(*jsonx.Object).Value(p)
			case jsonx.Number:
				i, _ := p.Int64()
				cur = cur.([]any)[i]
			}
		}
		return cur
	}
	for _, pair := range pairs {
		paths := pair.([]any)
		from, to := paths[0].([]any), paths[1].([]any)
		value := at(from)
		parent := at(to[:len(to)-1])
		switch key := to[len(to)-1].(type) {
		case string:
			parent.(*jsonx.Object).Set(key, value)
		case jsonx.Number:
			i, _ := key.Int64()
			parent.([]any)[i] = value
		}
	}
}

// installC25Collaborators answers the C26 collaborators from q and
// classical_ip_lines from the row's table.
func installC25Collaborators(q *collabQueue, ipl []any) func() {
	saved := []any{prepareEditor, prepareRuleUpdate, finishRuleUpdates}
	prepareEditor = func(settings, config any, baseURL string) *jsonx.Object {
		return copyValue(q.next("prepare_editor", settings, config, baseURL).GetObject("out")).(*jsonx.Object)
	}
	prepareRuleUpdate = func(ctx *commonContext, changes *jsonx.Object, config any, baseURL string) {
		applyAfter(ctx, q.next("prepare_rule_update", ctx.doc, ctx.settings, changes, config, baseURL).GetObject("after"))
	}
	finishRuleUpdates = func(ctx *commonContext, changes *jsonx.Object, config any, baseURL string) {
		applyAfter(ctx, q.next("finish_rule_updates", ctx.doc, ctx.settings, changes, config, baseURL).GetObject("after"))
	}
	answers := map[string]*jsonx.Object{}
	for _, element := range ipl {
		entry := element.(*jsonx.Object)
		answers[entry.GetString("h")] = entry
	}
	restoreHook := SetClassicalIPLines(func(_ context.Context, item, config any) (any, error) {
		h := hashArgs(item, config)
		entry, ok := answers[h]
		if !ok {
			return nil, &collabMismatch{"classical_ip_lines asked with arguments Python did not use (" + h + ")"}
		}
		if failure := entry.GetList("err"); failure != nil {
			return nil, pyErr(failure[0].(string), failure[1].(string))
		}
		return entry.Value("out"), nil
	})
	return func() {
		restoreHook()
		prepareEditor = saved[0].(func(any, any, string) *jsonx.Object)
		prepareRuleUpdate = saved[1].(func(*commonContext, *jsonx.Object, any, string))
		finishRuleUpdates = saved[2].(func(*commonContext, *jsonx.Object, any, string))
	}
}

// sortCapabilities orders the keys of every "capabilities" object (Python
// builds it from a frozenset, whose order depends on the string hash seed).
func sortCapabilities(v any) any {
	switch x := v.(type) {
	case *jsonx.Object:
		if x == nil {
			return v
		}
		out := jsonx.NewObject()
		for key, value := range x.All() {
			if caps, ok := value.(*jsonx.Object); ok && key == "capabilities" && caps != nil {
				keys := caps.Keys()
				sort.Strings(keys)
				sorted := jsonx.NewObject()
				for _, k := range keys {
					sorted.Set(k, sortCapabilities(caps.Value(k)))
				}
				out.Set(key, sorted)
				continue
			}
			out.Set(key, sortCapabilities(value))
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = sortCapabilities(item)
		}
		return out
	}
	return v
}

func stringsOf(v any) ([]string, bool) {
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := []string{}
	for _, element := range list {
		text, ok := element.(string)
		if !ok {
			return nil, false
		}
		out = append(out, text)
	}
	return out, true
}

// callRowC25 runs one C25 row. ok false: the arguments cannot be expressed.
func callRowC25(entry *jsonx.Object, args []any) (out any, same []any, err error, ok bool) {
	q := &collabQueue{entries: entry.GetList("collab")}
	restore := installC25Collaborators(q, entry.GetList("ipl"))
	defer restore()
	defer func() {
		if _, mismatch := err.(*collabMismatch); !mismatch && q.pos < len(q.entries) {
			err = &collabMismatch{fmt.Sprintf("Go made %d of %d collaborator calls (next: %s); Go result %.300s / %v",
				q.pos, len(q.entries), q.entries[q.pos].(*jsonx.Object).GetString("fn"), compact(out), err)}
		}
	}()
	defer catch(&err)
	ok = true // a raised error is a result too
	objs := func(n int) ([]*jsonx.Object, bool) {
		var out []*jsonx.Object
		for i := 0; i < n; i++ {
			o, ok := obj(args[i])
			if !ok {
				return nil, false
			}
			out = append(out, o)
		}
		return out, true
	}
	text := func(i int) (string, bool) {
		s, ok := args[i].(string)
		return s, ok
	}
	switch entry.GetString("fn") {
	case "read_routing":
		o, ok := objs(2)
		if !ok {
			return nil, nil, nil, false
		}
		out = readRoutingImpl(o[0], o[1], args[2])
	case "migrate_domain_only_routing":
		o, ok1 := objs(3)
		base, ok2 := text(3)
		if !ok1 || !ok2 {
			return nil, nil, nil, false
		}
		doc, values := migrateDomainOnlyRouting(o[0], o[1], o[2], base)
		out = []any{doc, values}
		same = []any{doc == o[0], values == o[1]}
	case "remove_rule":
		o, ok1 := objs(2)
		identity, ok2 := text(2)
		base, ok3 := text(3)
		if !ok1 || !ok2 || !ok3 {
			return nil, nil, nil, false
		}
		result := removeRule(o[0], o[1], identity, base)
		if result != nil {
			out = result
		}
	case "resource_readers":
		o, ok := objs(1)
		if !ok {
			return nil, nil, nil, false
		}
		out = resourceReaders(o[0])
	case "resource_is_read":
		path, ok1 := text(0)
		readers, ok2 := stringsOf(args[1])
		if !ok1 || !ok2 {
			return nil, nil, nil, false
		}
		out = ResourceIsRead(path, readers)
	case "owned_chains":
		o, ok := objs(1)
		if !ok {
			return nil, nil, nil, false
		}
		out = ownedChainsImpl(o[0])
	case "refresh_rule_helpers":
		o, ok := objs(1)
		if !ok {
			return nil, nil, nil, false
		}
		refreshRuleHelpersImpl(o[0])
	case "_prefix_end":
		list, ok := args[0].([]any)
		if !ok {
			return nil, nil, nil, false
		}
		out = jsonx.Int(int64(prefixEnd(list)))
	case "_references":
		tag, ok := text(1)
		if !ok {
			return nil, nil, nil, false
		}
		out = references(args[0], tag)
	case "ensure_local_dns_watchdog":
		out = ensureLocalDNSWatchdog(args[0])
	case "resource_downloads":
		o, ok1 := objs(2)
		base, ok2 := text(3)
		if !ok1 || !ok2 {
			return nil, nil, nil, false
		}
		out = resourceDownloadsImpl(o[0], o[1], args[2], base, nil)
	case "apply_routing":
		o, ok1 := objs(2)
		changes, ok2 := obj(args[3])
		base, ok3 := text(4)
		before, ok4 := objOrNil(args[5])
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return nil, nil, nil, false
		}
		var notices *[]any
		if list, isList := args[6].([]any); isList {
			copied := append([]any{}, list...)
			notices = &copied
			// notices.extend(...) is visible to the caller (in place).
			defer func() { args[6] = *notices }()
		} else if args[6] != nil {
			return nil, nil, nil, false
		}
		doc, values := applyRoutingImpl(o[0], o[1], args[2], changes, base, before, args[5] != nil, notices)
		var noticeOut any
		if notices != nil {
			noticeOut = *notices
		}
		out = []any{doc, values, noticeOut}
	default:
		return nil, nil, nil, false
	}
	return out, same, nil, true
}

func replayC25(t *testing.T, path string, label func(*jsonx.Object) string) {
	rows := loadRows(t, path)
	failures, unsupported, notExpressible := 0, 0, 0
	counts, same := map[string]int{}, map[string]int{}
	pendingBy := map[string]int{}
	tests := map[string]bool{}
	for i, entry := range rows {
		fn := entry.GetString("fn")
		counts[fn]++
		tests[entry.GetString("test")] = true
		if entry.Has("unsupported") {
			unsupported++
			continue
		}
		args := copyValue(entry.GetList("args")).([]any)
		restoreAliases(args, entry.GetList("aliases"))
		out, identity, err, ok := callRowC25(entry, args)
		if !ok {
			notExpressible++
			continue
		}
		if err != nil && IsPending(err) {
			pendingBy[err.Error()]++
			continue
		}
		msg := ""
		if mismatch, isMismatch := err.(*collabMismatch); isMismatch {
			msg = mismatch.Error()
		} else {
			msg = compareResult(sortedRow(entry), sortCapabilities(plain(out)), err)
		}
		if msg == "" {
			wantArgs := entry.GetList("args")
			if after, changed := entry.Get("args_after"); changed {
				wantArgs = after.([]any)
			}
			if got, want := compact(args), compact(wantArgs); got != want {
				msg = "arguments after the call differ:" + firstDifference(want, got)
			}
		}
		if msg == "" && entry.Has("same") && err == nil {
			if got, want := compact(identity), compact(entry.Value("same")); got != want {
				msg = fmt.Sprintf("returned identity %s, want %s", got, want)
			}
		}
		if msg == "" {
			same[fn]++
			continue
		}
		failures++
		if failures <= 25 {
			where := fmt.Sprintf("row %d", i)
			if label != nil {
				where = label(entry)
			}
			t.Errorf("%s (%s) %.300s: %s", where, fn, compact(entry.GetList("args")), msg)
		}
	}
	t.Logf("identical: %v", same)
	pendingTotal := 0
	for _, n := range pendingBy {
		pendingTotal += n
	}
	t.Logf("%d rows from %d tests %v; %d unsupported, %d not expressible, %d pending %v, %d differ",
		len(rows), len(tests), counts, unsupported, notExpressible, pendingTotal, pendingBy, failures)
	if failures > 0 || notExpressible > 0 {
		t.Errorf("%d differ, %d not expressible of %d rows", failures, notExpressible, len(rows))
	}
}

// sortedRow is entry with its recorded output's capabilities sorted.
func sortedRow(entry *jsonx.Object) *jsonx.Object {
	out, ok := entry.Get("out")
	if !ok {
		return entry
	}
	copied := entry.Clone()
	copied.Set("out", sortCapabilities(out))
	return copied
}
