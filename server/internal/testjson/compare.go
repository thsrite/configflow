package testjson

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

// CompareOptions controls structural number comparison.
type CompareOptions struct {
	// DistinguishNumberLiterals distinguishes integer literals (1) from decimal
	// or exponent literals (1.0, 1e0). Values within each class compare numerically.
	DistinguishNumberLiterals bool
}

// Equal reports whether two JSON-marshalable values have equal structure.
// Object order is ignored, array order matters, and numbers compare exactly by
// value. Invalid values are unequal, even to themselves. The optional options
// distinguish integer and decimal/exponent literals when requested.
func Equal(want, got any, options ...CompareOptions) bool {
	return Diff(want, got, options...) == ""
}

// Diff returns the first structural difference with a deterministic JSON path,
// or an empty string when the values are equal. Encoding/parse errors are differences.
func Diff(want, got any, options ...CompareOptions) string {
	w, err := normalize(want)
	if err != nil {
		return fmt.Sprintf("$: invalid expected JSON: %v", err)
	}
	g, err := normalize(got)
	if err != nil {
		return fmt.Sprintf("$: invalid actual JSON: %v", err)
	}
	var opt CompareOptions
	if len(options) > 0 {
		opt = options[0]
	}
	return difference("$", w, g, opt)
}

func difference(path string, want, got any, opt CompareOptions) string {
	switch w := want.(type) {
	case *Object:
		if g, ok := got.(*Object); ok {
			keys := make(map[string]bool, len(w.fields)+len(g.fields))
			for _, f := range w.fields {
				keys[f.Key] = true
			}
			for _, f := range g.fields {
				keys[f.Key] = true
			}
			ordered := make([]string, 0, len(keys))
			for k := range keys {
				ordered = append(ordered, k)
			}
			sort.Strings(ordered)
			for _, key := range ordered {
				wi, wok := w.index[key]
				gi, gok := g.index[key]
				next := path + "[" + summary(key) + "]"
				if !wok {
					return next + ": unexpected member"
				}
				if !gok {
					return next + ": missing member"
				}
				if diff := difference(next, w.fields[wi].Value, g.fields[gi].Value, opt); diff != "" {
					return diff
				}
			}
			return ""
		}
	case []any:
		if g, ok := got.([]any); ok {
			if len(w) != len(g) {
				return fmt.Sprintf("%s: array length: want %d, got %d", path, len(w), len(g))
			}
			for i := range w {
				if diff := difference(fmt.Sprintf("%s[%d]", path, i), w[i], g[i], opt); diff != "" {
					return diff
				}
			}
			return ""
		}
	case json.Number:
		if g, ok := got.(json.Number); ok {
			sameClass := !opt.DistinguishNumberLiterals || strings.ContainsAny(string(w), ".eE") == strings.ContainsAny(string(g), ".eE")
			if sameClass && canonicalNumber(w) == canonicalNumber(g) {
				return ""
			}
		}
	case nil:
		if got == nil {
			return ""
		}
	case bool:
		if g, ok := got.(bool); ok && w == g {
			return ""
		}
	case string:
		if g, ok := got.(string); ok && w == g {
			return ""
		}
	}
	return fmt.Sprintf("%s: want %s, got %s", path, summary(want), summary(got))
}

// canonicalNumber compares decimal significands and exponents without expanding
// powers of ten. Huge exponents therefore require only input-proportional space.
// Inputs have already been validated by encoding/json.
func canonicalNumber(n json.Number) string {
	s := string(n)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	exponent := new(big.Int)
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exponent.SetString(s[i+1:], 10)
		s = s[:i]
	}
	if i := strings.IndexByte(s, '.'); i >= 0 {
		exponent.Sub(exponent, big.NewInt(int64(len(s)-i-1)))
		s = s[:i] + s[i+1:]
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return "0"
	}
	digits := strings.TrimRight(s, "0")
	exponent.Add(exponent, big.NewInt(int64(len(s)-len(digits))))
	return sign + digits + "e" + exponent.String()
}

func summary(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<%T>", v)
	}
	if len(b) > 160 {
		return string(b[:160]) + "..."
	}
	return string(b)
}

// WithoutEmptyMembers returns a decoded JSON value (from encoding/json into
// any) without object members holding "", null, {} or [], at any depth. Stored
// plain records leave such optional members out (O18 T1c) while Python wrote
// them on some paths only, so stored-state comparisons treat them as absent.
func WithoutEmptyMembers(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			item = WithoutEmptyMembers(item)
			if item == nil || item == "" {
				continue
			}
			if object, ok := item.(map[string]any); ok && len(object) == 0 {
				continue
			}
			if list, ok := item.([]any); ok && len(list) == 0 {
				continue
			}
			out[key] = item
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = WithoutEmptyMembers(item)
		}
		return out
	}
	return value
}
