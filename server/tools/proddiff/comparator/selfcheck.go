package main

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

// Self-check failures; a blind comparator cannot pass.
var (
	errSelfCheckSample = errors.New("self-check requires actual successful Go outputs")
	errSelfCheckFailed = errors.New("differential mutation self-check failed")
)

// selfCheck mutates real Go outputs and requires the production comparator
// to report each exact change. It returns the passed check names in order.
func (c *comparator) selfCheck(real reply, samples map[string]reply) ([]string, error) {
	for _, required := range []string{"yaml", "text", "headers", "http-json"} {
		if _, ok := samples[required]; !ok {
			return nil, fmt.Errorf("%w: %s", errSelfCheckSample, required)
		}
	}
	var names []string
	results := map[string]bool{}
	record := func(name string, ok bool) {
		if _, seen := results[name]; !seen {
			names = append(names, name)
		}
		results[name] = ok
	}
	compare := func(left, right reply) []diff {
		found, err := c.responseDiffs(left, right, "/self-check", nil, nil, nil)
		if err != nil {
			return []diff{{"self-check", "error"}}
		}
		return found
	}
	detects := func(name string, left, right reply, expected diff) {
		found := false
		for _, d := range compare(left, right) {
			if d == expected {
				found = true
			}
		}
		record(name, found)
	}
	agrees := func(name string, left, right reply) { record(name, len(compare(left, right)) == 0) }

	// HTTP status and headers of a real Go reply.
	agrees("control-real-reply", real, real)
	bumped := real
	bumped.Status++
	detects("status", real, bumped, diff{"status", "value"})
	retyped := real
	retyped.ContentType = "text/plain"
	detects("content-type", real, retyped, diff{"Content-Type", "value"})
	detects("transport-both", transportFailure, transportFailure, diff{"status", "transport-both"})
	sample := samples["headers"]
	var headerNames []string
	for name := range sample.Headers {
		if !ignoredHeaders[name] {
			headerNames = append(headerNames, name)
		}
	}
	if len(headerNames) == 0 {
		return nil, fmt.Errorf("%w: headers", errSelfCheckSample)
	}
	sort.Strings(headerNames)
	name := headerNames[0]
	changed := withHeaders(sample, func(h map[string]string) { h[name] += "-self-check" })
	detects("header-value", sample, changed, diff{"header." + c.keys.safeHeader(name), "value"})
	dropped := withHeaders(sample, func(h map[string]string) { delete(h, name) })
	detects("header-missing", sample, dropped, diff{"header." + c.keys.safeHeader(name), "missing-in-go"})

	// Deep JSON traversal: synthetic seven-level control document.
	good := bodyReply(200, "application/json", []byte(`{"data":{"profiles":[{"config":{"dns":{"upstreams":[{"port":53}]}}}]}}`))
	port := "body.data.profiles[0].config.dns.upstreams[0].port"
	replace := func(r reply, old, new string) reply {
		r.Body = bytes.ReplaceAll(r.Body, []byte(old), []byte(new))
		return r
	}
	detects("json-leaf", good, replace(good, "53", "54"), diff{port, "value"})
	detects("json-int-float", good, replace(good, "53", "53.0"), diff{port, "int-float"})
	detects("json-int-string", good, replace(good, "53", `"53"`), diff{port, "type"})
	detects("json-duplicate-key", good, replace(good, `{"port":53}`, `{"port":53,"port":53}`), diff{"body", "parse-go"})
	rules := bodyReply(200, "application/json", []byte(`{"rules":["DOMAIN,a.example,DIRECT","MATCH,PROXY"]}`))
	detects("json-list-order", rules, bodyReply(200, rules.ContentType, []byte(`{"rules":["MATCH,PROXY","DOMAIN,a.example,DIRECT"]}`)),
		diff{"body.rules[0]", "value"})

	// Real JSON reply: re-encoding is no difference; each structural mutation is reported at its path.
	jsonSample := bodyReply(samples["http-json"].Status, samples["http-json"].ContentType, samples["http-json"].Body)
	original, err := loadJSONLoose(jsonSample.Body)
	if err != nil {
		return nil, fmt.Errorf("self-check JSON sample: %w", err)
	}
	encode := func(v any) reply {
		return bodyReply(200, jsonSample.ContentType, []byte(dumpJSON(v, dumpOptions{indent: 1, sortKeys: true})))
	}
	agrees("control-json-reencoded", jsonSample, encode(original))
	nodes := walkValues(original)
	var leaves, objects, lists []node
	for _, n := range nodes {
		switch v := n.value.(type) {
		case *Map:
			if v.Len() > 0 {
				objects = append(objects, n)
			}
		case []any:
			if len(v) >= 2 && !pyEqual(v[0], v[1]) {
				lists = append(lists, n)
			}
		default:
			if len(n.keys) > 0 {
				leaves = append(leaves, n)
			}
		}
	}
	if len(leaves) == 0 || len(objects) == 0 {
		return nil, errors.New("real JSON self-check sample has no leaf or object")
	}
	leaf := leaves[len(leaves)-1]
	mutated := deepCopy(original)
	setAt(mutated, leaf.keys, bump(leaf.value))
	detects("json-real-leaf", jsonSample, encode(mutated), diff{c.shown(original, leaf.keys), kind(leaf.value, bump(leaf.value))})
	object := objects[len(objects)-1]
	objectMap := object.value.(*Map)
	first := objectMap.keys[0]
	mutated = deepCopy(original)
	locate(mutated, object.keys).(*Map).Delete(first)
	detects("json-missing-key", jsonSample, encode(mutated), diff{c.shown(original, append(append([]any{}, object.keys...), first)), "missing-in-go"})
	mutated = deepCopy(original)
	locate(mutated, object.keys).(*Map).Set("self_check_extra", "")
	detects("json-extra-key", jsonSample, encode(mutated), diff{c.shown(original, object.keys) + "." + c.keys.safeKey("self_check_extra"), "extra-in-go"})
	mutated = deepCopy(original)
	firstValue, _ := objectMap.Get(first)
	var replacement any
	if firstValue == nil {
		replacement = ""
	}
	locate(mutated, object.keys).(*Map).Set(first, replacement)
	detects("json-null-vs-value", jsonSample, encode(mutated), diff{c.shown(original, append(append([]any{}, object.keys...), first)), "type"})
	if len(lists) > 0 {
		list := lists[0]
		mutated = deepCopy(original)
		moved := locate(mutated, list.keys).([]any)
		moved[0], moved[1] = moved[1], moved[0]
		found := compare(jsonSample, encode(mutated))
		prefix := c.shown(original, list.keys) + "["
		ok := len(found) > 0
		for _, d := range found {
			if !strings.HasPrefix(d.Path, prefix) {
				ok = false
			}
		}
		record("json-real-list-order", ok)
	}

	// Real YAML: a re-dump is no difference, a leaf change is reported at its path, duplicates fail.
	yamlSample := bodyReply(samples["yaml"].Status, samples["yaml"].ContentType, samples["yaml"].Body)
	loaded, err := loadYAML(yamlSample.Body)
	loadedMap, isMap := loaded.(*Map)
	switch {
	case err != nil:
		// An unreadable sample cannot be a formatting-only control.
		record("control-yaml-redumped", false)
		record("yaml-leaf", false)
		record("yaml-duplicate-key", false)
	case !isMap || loadedMap.Len() == 0:
		return nil, errors.New("YAML self-check needs a top-level mapping")
	default:
		// Reformat only the body: changing response metadata would turn the
		// formatting control into an unrelated mutation.
		yamlReply := func(v any, sortKeys bool) reply {
			out, err := dumpYAML(v, sortKeys)
			if err != nil {
				out = []byte("self-check: unencodable YAML\n")
			}
			return bodyReply(yamlSample.Status, yamlSample.ContentType, out)
		}
		agrees("control-yaml-redumped", yamlSample, yamlReply(loaded, true))
		var yamlLeaves []node
		for _, n := range walkValues(loaded) {
			switch n.value.(type) {
			case *Map, []any, nil:
				continue
			}
			if len(n.keys) > 0 {
				yamlLeaves = append(yamlLeaves, n)
			}
		}
		if len(yamlLeaves) == 0 {
			return nil, errors.New("no YAML leaf available for self-check")
		}
		yamlLeaf := yamlLeaves[0]
		changedYAML := deepCopy(loaded)
		setAt(changedYAML, yamlLeaf.keys, bump(yamlLeaf.value))
		detects("yaml-leaf", yamlSample, yamlReply(changedYAML, false), diff{c.shown(loaded, yamlLeaf.keys), kind(yamlLeaf.value, bump(yamlLeaf.value))})
		single := newMap()
		single.Set(loadedMap.keys[0], nil)
		extra, err := dumpYAML(single, false)
		if err != nil {
			return nil, fmt.Errorf("self-check YAML duplicate: %w", err)
		}
		duplicate := append(append(bytes.TrimRight(append([]byte{}, yamlSample.Body...), "\n"), '\n'), extra...)
		detects("yaml-duplicate-key", yamlSample, bodyReply(yamlSample.Status, yamlSample.ContentType, duplicate), diff{"body", "parse-go"})
	}

	// Real text: one changed line, one inserted line and CRLF line endings are all reported.
	textSample := bodyReply(samples["text"].Status, samples["text"].ContentType, samples["text"].Body)
	lines := splitLines(string(textSample.Body))
	if len(lines) == 0 {
		return nil, errors.New("no text line available for self-check")
	}
	middle := len(lines) / 2
	edited := append([]string{}, lines...)
	edited[middle] += " # self-check-mutated"
	detects("text-line", textSample, bodyReply(200, "text/plain", []byte(strings.Join(edited, "\n")+"\n")), diff{fmt.Sprintf("body[%d]", middle), "value"})
	inserted := append(append(append([]string{}, lines[:middle]...), "# self-check-inserted"), lines[middle:]...)
	detects("text-insert", textSample, bodyReply(200, "text/plain", []byte(strings.Join(inserted, "\n")+"\n")), diff{"body", "length"})
	detects("text-line-endings", textSample, bodyReply(200, "text/plain", []byte(strings.Join(lines, "\r\n")+"\r\n")), diff{"body", "line-endings"})

	var failed []string
	for _, n := range names {
		if !results[n] {
			failed = append(failed, n)
		}
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		return nil, fmt.Errorf("%w: %s", errSelfCheckFailed, strings.Join(failed, ","))
	}
	return names, nil
}

func withHeaders(r reply, edit func(map[string]string)) reply {
	headers := map[string]string{}
	for k, v := range r.Headers {
		headers[k] = v
	}
	edit(headers)
	r.Headers = headers
	return r
}

// node is one value of a depth-first walk with its key path.
type node struct {
	keys  []any // mapping keys and int list indexes
	value any
}

func walkValues(v any) []node {
	var out []node
	var visit func(keys []any, v any)
	visit = func(keys []any, v any) {
		out = append(out, node{keys, v})
		switch x := v.(type) {
		case *Map:
			for i, k := range x.keys {
				visit(append(append([]any{}, keys...), k), x.vals[i])
			}
		case []any:
			for i, item := range x {
				visit(append(append([]any{}, keys...), i), item)
			}
		}
	}
	visit(nil, v)
	return out
}

func locate(v any, keys []any) any {
	for _, k := range keys {
		switch x := v.(type) {
		case *Map:
			v, _ = x.Get(k)
		case []any:
			v = x[k.(int)]
		}
	}
	return v
}

func setAt(root any, keys []any, value any) {
	parent := locate(root, keys[:len(keys)-1])
	last := keys[len(keys)-1]
	switch x := parent.(type) {
	case *Map:
		x.Set(last, value)
	case []any:
		x[last.(int)] = value
	}
}

// shown builds the schema-safe path the comparator prints for keys.
func (c *comparator) shown(doc any, keys []any) string {
	path := "body"
	for _, k := range keys {
		if list, ok := doc.([]any); ok {
			i := k.(int)
			path += fmt.Sprintf("[%d]", i)
			doc = list[i]
			continue
		}
		path += "." + c.keys.safeKey(k)
		doc, _ = doc.(*Map).Get(k)
	}
	return path
}

func kind(old, new any) string {
	if pyType(old) == pyType(new) {
		return "value"
	}
	return "type"
}

func bump(v any) any {
	switch x := v.(type) {
	case bool:
		return !x
	case int64:
		if x == 1<<63-1 {
			return new(big.Int).Add(big.NewInt(x), big.NewInt(1))
		}
		return x + 1
	case *big.Int:
		return new(big.Int).Add(x, big.NewInt(1))
	case float64:
		return x + 1
	case nil:
		return int64(0)
	}
	return pyStr(v) + "-self-check"
}
