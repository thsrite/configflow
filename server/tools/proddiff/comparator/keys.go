package main

import (
	"io/fs"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// goLiteral matches string literals and json/yaml tags that look like schema identifiers.
var goLiteral = regexp.MustCompile(`"([A-Za-z_][A-Za-z_0-9-]*)"|\b(?:json|yaml):"([A-Za-z_][A-Za-z_0-9-]*)`)

// goSourceKeys collects string literals and json/yaml tags of non-test Go
// sources under server/ (testdata excluded).
func goSourceKeys(root string) (map[string]bool, error) {
	keys := map[string]bool{}
	base := filepath.Join(root, "server")
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		relative, _ := filepath.Rel(base, path)
		for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
			if part == "testdata" {
				return nil
			}
		}
		body, err := os.ReadFile(path)
		if err != nil || !utf8.Valid(body) {
			return nil
		}
		for _, m := range goLiteral.FindAllStringSubmatch(string(body), -1) {
			if m[1] != "" {
				keys[m[1]] = true
			} else {
				keys[m[2]] = true
			}
		}
		return nil
	})
	return keys, err
}

// fixedHeaders are standard response headers that are always public.
var fixedHeaders = []string{"ETag", "Last-Modified", "Accept-Ranges", "X-Content-Type-Options",
	"Access-Control-Allow-Origin", "Access-Control-Allow-Headers", "Access-Control-Allow-Methods", "Vary"}

// loadKeys builds the public key set: a key is public only if it is a literal
// schema identifier in the Go sources, plus a few fixed names.
func loadKeys(root string) (*keySet, error) {
	keys, err := goSourceKeys(root)
	if err != nil {
		return nil, err
	}
	for _, k := range []string{"<key>", "<file>", "status", "Content-Type", "body", "content", "config", "config.yaml"} {
		keys[k] = true
	}
	headers := map[string]string{}
	// Several spellings may share one lower-case name; prefer the canonical
	// MIME spelling, then the smallest, so reports are deterministic.
	for k := range keys {
		lower := strings.ToLower(k)
		current, ok := headers[lower]
		canonical := textproto.CanonicalMIMEHeaderKey(lower)
		switch {
		case !ok:
			headers[lower] = k
		case current == canonical:
		case k == canonical || k < current:
			headers[lower] = k
		}
	}
	for _, name := range fixedHeaders {
		headers[strings.ToLower(name)] = name
	}
	return &keySet{keys: keys, headers: headers}, nil
}
