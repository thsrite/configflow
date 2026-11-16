package oxidns

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// TestC24Recorded replays every top-level read_common_settings /
// apply_common_settings call the whole Python test suite makes
// (testdata/c24_record.py). The functions of later modules (C25, C26) the
// call reaches answer with what Python's answered during the recorded call;
// the replay checks that Go asks them the same questions in the same order.
func TestC24Recorded(t *testing.T) {
	replayC24(t, "testdata/c24_recorded.jsonl.gz", func(entry *jsonx.Object) string {
		return strings.TrimPrefix(entry.GetString("test"), "backend/")
	})
}

// TestC24Corpus replays the random corpus of testdata/gen_c24.py.
func TestC24Corpus(t *testing.T) {
	replayC24(t, "testdata/c24_corpus.jsonl.gz", nil)
}

// collabMismatch reports that Go called the collaborators differently from
// Python (another function, other arguments, more or fewer calls).
type collabMismatch struct{ message string }

func (e *collabMismatch) Error() string { return "collaborator mismatch: " + e.message }

type collabQueue struct {
	entries []any
	pos     int
}

func hashArgs(args ...any) string {
	data, err := jsonx.Encode(plain(args), jsonx.Options{Compact: true})
	if err != nil {
		return "<" + err.Error() + ">"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}

// next returns the recorded answer of the next collaborator call.
func (q *collabQueue) next(fn string, args ...any) *jsonx.Object {
	if q.pos >= len(q.entries) {
		raise(&collabMismatch{"Go calls " + fn + " beyond the recorded calls"})
	}
	entry := q.entries[q.pos].(*jsonx.Object)
	q.pos++
	if got := entry.GetString("fn"); got != fn {
		raise(&collabMismatch{fmt.Sprintf("call %d: Go calls %s, Python called %s", q.pos, fn, got)})
	}
	if want, got := entry.GetString("h"), hashArgs(args...); want != got {
		detail := compact(args)
		if recorded, ok := entry.Get("a"); ok {
			detail = firstDifference(compact(recorded), detail)
		}
		raise(&collabMismatch{fmt.Sprintf("call %d (%s): arguments differ: %.4000s", q.pos, fn, detail)})
	}
	if failure := entry.GetList("err"); failure != nil {
		class, message := failure[0].(string), failure[1].(string)
		switch class {
		case ClassSettings, ClassReference, ClassValidation:
			e := &Error{Class: class, Message: message}
			if references, ok := entry.Get("references"); ok {
				e.References = asList(references)
			}
			if issues, ok := entry.Get("issues"); ok {
				e.Issues, e.HasIssues = asList(issues), true
			}
			raise(e)
		}
		raise(pyErr(class, message))
	}
	return entry
}

// replaceContents makes dst hold exactly the keys and values of src.
func replaceContents(dst, src *jsonx.Object) {
	for _, key := range dst.Keys() {
		dst.Delete(key)
	}
	for key, value := range src.All() {
		dst.Set(key, value)
	}
}

// restoreDocument makes document the recorded document, with its shared
// (aliased) objects shared again.
func restoreDocument(document, after *jsonx.Object) {
	replaceContents(document, copyValue(after.GetObject("doc")).(*jsonx.Object))
	at := func(path []any) any {
		var cur any = document
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
	for _, pair := range after.GetList("aliases") {
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

// applyAfter restores the context state an in-place collaborator left.
func applyAfter(ctx *commonContext, after *jsonx.Object) {
	restoreDocument(ctx.doc, after)
	replaceContents(ctx.settings, copyValue(after.GetObject("settings")).(*jsonx.Object))
	ctx.byTag = map[string]*jsonx.Object{}
	plugins := asList(ctx.doc.Value("plugins"))
	for _, pair := range after.GetList("by_tag") {
		row := pair.([]any)
		tag := row[0].(string)
		if n, ok := row[1].(jsonx.Number); ok {
			index, _ := n.Int64()
			ctx.byTag[tag] = plugins[index].(*jsonx.Object)
		} else {
			ctx.byTag[tag] = copyValue(row[2]).(*jsonx.Object)
		}
	}
	if after.GetBool("parked_live") {
		ctx.parked = ctx.settings.Value("_common_disabled").(*jsonx.Object)
	} else {
		ctx.parked = copyValue(after.GetObject("parked")).(*jsonx.Object)
	}
}

// installCollaborators answers the C25/C26 collaborators from q and returns
// the function that restores the defaults.
func installCollaborators(q *collabQueue) func() {
	saved := []any{prepareEditor, readRouting, ownedChains, applyRouting, resourceDownloads, refreshRuleHelpers, prepareRuleUpdate, finishRuleUpdates}
	prepareEditor = func(settings, config any, baseURL string) *jsonx.Object {
		return copyValue(q.next("prepare_editor", settings, config, baseURL).GetObject("out")).(*jsonx.Object)
	}
	readRouting = func(document, settings *jsonx.Object, config any) *jsonx.Object {
		return copyValue(q.next("read_routing", document, settings, config).GetObject("out")).(*jsonx.Object)
	}
	ownedChains = func(document *jsonx.Object) *jsonx.Object {
		return copyValue(q.next("owned_chains", document).GetObject("out")).(*jsonx.Object)
	}
	applyRouting = func(document, settings *jsonx.Object, config any, changes *jsonx.Object, baseURL string) (*jsonx.Object, *jsonx.Object) {
		out := copyValue(q.next("apply_routing", document, settings, config, changes, baseURL).GetList("out")).([]any)
		return out[0].(*jsonx.Object), out[1].(*jsonx.Object)
	}
	resourceDownloads = func(document, settings *jsonx.Object, config any, baseURL string) []any {
		return copyValue(q.next("resource_downloads", document, settings, config, baseURL).GetList("out")).([]any)
	}
	refreshRuleHelpers = func(document *jsonx.Object) {
		entry := q.next("refresh_rule_helpers", document)
		restoreDocument(document, entry.GetObject("after"))
	}
	prepareRuleUpdate = func(ctx *commonContext, changes *jsonx.Object, config any, baseURL string) {
		applyAfter(ctx, q.next("prepare_rule_update", ctx.doc, ctx.settings, changes, config, baseURL).GetObject("after"))
	}
	finishRuleUpdates = func(ctx *commonContext, changes *jsonx.Object, config any, baseURL string) {
		applyAfter(ctx, q.next("finish_rule_updates", ctx.doc, ctx.settings, changes, config, baseURL).GetObject("after"))
	}
	return func() {
		prepareEditor = saved[0].(func(any, any, string) *jsonx.Object)
		readRouting = saved[1].(func(*jsonx.Object, *jsonx.Object, any) *jsonx.Object)
		ownedChains = saved[2].(func(*jsonx.Object) *jsonx.Object)
		applyRouting = saved[3].(func(*jsonx.Object, *jsonx.Object, any, *jsonx.Object, string) (*jsonx.Object, *jsonx.Object))
		resourceDownloads = saved[4].(func(*jsonx.Object, *jsonx.Object, any, string) []any)
		refreshRuleHelpers = saved[5].(func(*jsonx.Object))
		prepareRuleUpdate = saved[6].(func(*commonContext, *jsonx.Object, any, string))
		finishRuleUpdates = saved[7].(func(*commonContext, *jsonx.Object, any, string))
	}
}

// callRowC24 runs one C24 row with its recorded collaborators.
func callRowC24(entry *jsonx.Object) (out any, err error, ok bool) {
	args := entry.GetList("args")
	q := &collabQueue{entries: entry.GetList("collab")}
	restore := installCollaborators(q)
	defer restore()
	switch entry.GetString("fn") {
	case "read_common_settings":
		base, isText := args[2].(string)
		if !isText {
			return nil, nil, false
		}
		out, err = ReadCommonSettings(args[0], args[1], base)
	case "apply_common_settings":
		base, isText := args[3].(string)
		if !isText {
			return nil, nil, false
		}
		out, err = ApplyCommonSettings(args[0], args[1], args[2], base)
	default:
		return nil, nil, false
	}
	if _, mismatch := err.(*collabMismatch); !mismatch && q.pos < len(q.entries) {
		next := q.entries[q.pos].(*jsonx.Object).GetString("fn")
		err = &collabMismatch{fmt.Sprintf("Go made %d of %d collaborator calls (next: %s); Go result %.300s / %v",
			q.pos, len(q.entries), next, compact(out), err)}
	}
	return out, err, true
}

func replayC24(t *testing.T, path string, label func(*jsonx.Object) string) {
	rows := loadRows(t, path)
	failures, unsupported, notExpressible := 0, 0, 0
	counts := map[string]int{}
	same := map[string]int{}
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
		out, err, ok := callRowC24(entry)
		msg := ""
		switch {
		case !ok:
			notExpressible++
			continue
		case err != nil && IsPending(err):
			pendingBy[err.Error()]++
			continue
		default:
			if mismatch, isMismatch := err.(*collabMismatch); isMismatch {
				msg = mismatch.Error()
			} else {
				msg = compareResult(entry, out, err)
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

// compareResult compares a Go result with the row's recorded result ("" =
// identical).
func compareResult(entry *jsonx.Object, out any, err error) string {
	if want := entry.GetList("err"); want != nil {
		if err == nil {
			return fmt.Sprintf("want %v, got output %.600s", compact(want), compact(out))
		}
		class := errClass(err)
		if class != want[0] || err.Error() != want[1] {
			return fmt.Sprintf("want %s, got [%s %q]", compact(want), class, err.Error())
		}
		if references, present := entry.Get("references"); present {
			e, _ := AsError(err)
			if e == nil || compact(e.References) != compact(references) {
				return "references differ"
			}
		}
		return ""
	}
	if err != nil {
		return fmt.Sprintf("want output, got [%s %q]", errClass(err), err.Error())
	}
	want, _ := entry.Get("out")
	if got, expected := compact(out), compact(want); got != expected {
		return fmt.Sprintf("output differs:\n got %s\nwant %s", got, expected)
	}
	return ""
}

// firstDifference shows where two encodings start to differ.
func firstDifference(want, got string) string {
	i := 0
	for i < len(want) && i < len(got) && want[i] == got[i] {
		i++
	}
	start := i - 200
	if start < 0 {
		start = 0
	}
	end := func(s string) string {
		if i+300 < len(s) {
			return s[start : i+300]
		}
		return s[start:]
	}
	return "\n python ..." + end(want) + "\n go     ..." + end(got)
}
