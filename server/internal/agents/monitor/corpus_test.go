package monitor

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"

	"configflow/internal/jsonx"
)

type referenceCorpus struct {
	once  sync.Once
	value *jsonx.Object
	err   error
}

var corpusCache = struct {
	sync.Mutex
	files map[string]*referenceCorpus
}{files: make(map[string]*referenceCorpus)}

// referenceFields parses each immutable corpus once, then copies only the
// sections the caller used before. Tests retain independent mutable inputs;
// unrelated sections no longer get decompressed/parsed for every test.
func referenceFields(t *testing.T, path string, fields ...string) *jsonx.Object {
	t.Helper()
	corpusCache.Lock()
	c := corpusCache.files[path]
	if c == nil {
		c = &referenceCorpus{}
		corpusCache.files[path] = c
	}
	corpusCache.Unlock()
	c.once.Do(func() {
		f, err := os.Open(path)
		if err != nil {
			c.err = err
			return
		}
		defer f.Close()
		reader, err := gzip.NewReader(f)
		if err != nil {
			c.err = err
			return
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			c.err = err
			return
		}
		value, err := jsonx.Parse(data)
		if err != nil {
			c.err = err
			return
		}
		var ok bool
		c.value, ok = value.(*jsonx.Object)
		if !ok {
			c.err = fmt.Errorf("%s: reference root must be an object", path)
		}
	})
	if c.err != nil {
		t.Fatal(c.err)
	}
	if len(fields) == 0 {
		t.Fatal("reference fields must be explicit")
	}
	out := jsonx.Obj()
	for _, key := range fields {
		value, ok := c.value.Get(key)
		if !ok {
			t.Fatalf("%s: missing reference section %q", path, key)
		}
		out.Set(key, jsonx.DeepCopy(value))
	}
	return out
}
