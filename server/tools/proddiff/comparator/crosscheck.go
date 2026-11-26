package main

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// The crosscheck corpus holds reply pairs exactly as the comparator saw them
// (private: bodies are base64 of real responses). `run` writes it with
// PRODDIFF_CORPUS=1; `crosscheck` replays it through responseDiffs so the
// Go and the Python comparator can be compared entry by entry.

var errCorpus = errors.New("invalid corpus entry")

// corpusWriter appends reply pairs to report/corpus.jsonl.
type corpusWriter struct {
	mu   sync.Mutex
	file *os.File
}

func newCorpusWriter(report string) (*corpusWriter, error) {
	f, err := os.OpenFile(filepath.Join(report, "corpus.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open corpus: %w", err)
	}
	return &corpusWriter{file: f}, nil
}

func (w *corpusWriter) add(route string, left, right reply) error {
	if w == nil {
		return nil
	}
	entry := newMap()
	entry.Set("route", route)
	entry.Set("left", corpusReply(left))
	entry.Set("right", corpusReply(right))
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := w.file.WriteString(dumpJSON(entry, dumpOptions{indent: -1, ensureASCII: true}) + "\n")
	return err
}

func (w *corpusWriter) close() {
	if w != nil {
		w.file.Close()
	}
}

func corpusReply(r reply) []any {
	var headers any
	if r.HasHeaders {
		m := newMap()
		for _, name := range sortedStrings(r.Headers) {
			m.Set(name, r.Headers[name])
		}
		headers = m
	}
	return []any{int64(r.Status), r.ContentType, base64.StdEncoding.EncodeToString(r.Body), headers}
}

func replyFromCorpus(v any) (reply, error) {
	list, ok := v.([]any)
	if !ok || len(list) != 4 {
		return reply{}, errCorpus
	}
	status, ok1 := list[0].(int64)
	contentType, ok2 := list[1].(string)
	encoded, ok3 := list[2].(string)
	if !ok1 || !ok2 || !ok3 {
		return reply{}, errCorpus
	}
	body, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return reply{}, errCorpus
	}
	if list[3] == nil {
		return bodyReply(int(status), contentType, body), nil
	}
	headerMap, ok := list[3].(*Map)
	if !ok {
		return reply{}, errCorpus
	}
	headers := map[string]string{}
	for i, k := range headerMap.keys {
		value, ok := headerMap.vals[i].(string)
		if !ok {
			return reply{}, errCorpus
		}
		headers[pyStr(k)] = value
	}
	return httpReply(int(status), contentType, body, headers), nil
}

// crosscheck replays a corpus with the real normalization rules and one
// shared identity pairing, writing one sorted difference list per entry.
func crosscheck(root string, cmp *comparator, corpusPath, outPath string) error {
	rows, err := readRows(filepath.Join(root, "server/parity/routes.tsv"))
	if err != nil {
		return err
	}
	routes := map[string]bool{}
	for _, row := range rows {
		if len(row) >= 2 {
			routes[row[1]] = true
		}
	}
	rules, err := loadRules(filepath.Join(root, "server/parity/proddiff-normalize.json"), routes, "go")
	if err != nil {
		return err
	}
	in, err := os.Open(corpusPath)
	if err != nil {
		return fmt.Errorf("open corpus: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open output: %w", err)
	}
	defer out.Close()
	writer := bufio.NewWriter(out)
	identities := newAgentIdentities()
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1<<20), 1<<30)
	for scanner.Scan() {
		value, err := parseJSON(scanner.Text(), true)
		if err != nil {
			return errCorpus
		}
		route, _ := getOr(value, "route", nil).(string)
		left, err := replyFromCorpus(getOr(value, "left", nil))
		if err != nil {
			return err
		}
		right, err := replyFromCorpus(getOr(value, "right", nil))
		if err != nil {
			return err
		}
		var result any
		found, err := cmp.responseDiffs(left, right, route, rules, identities, nil)
		if err != nil {
			result = "error"
		} else {
			pairs := make([][2]string, len(found))
			for i, d := range found {
				pairs[i] = [2]string{d.Path, d.Kind}
			}
			sort.Slice(pairs, func(i, j int) bool {
				if pairs[i][0] != pairs[j][0] {
					return pairs[i][0] < pairs[j][0]
				}
				return pairs[i][1] < pairs[j][1]
			})
			list := make([]any, len(pairs))
			for i, p := range pairs {
				list[i] = []any{p[0], p[1]}
			}
			result = list
		}
		if _, err := writer.WriteString(dumpJSON(result, dumpOptions{indent: -1, ensureASCII: true}) + "\n"); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read corpus: %w", err)
	}
	return writer.Flush()
}
