package yamlx

import (
	"errors"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/pyyaml"
)

// Schema selects how plain scalars are typed.
type Schema int

const (
	// SchemaPyYAML is yaml.SafeLoader: YAML 1.1 (yes/no/on/off booleans,
	// octal, sexagesimal, timestamps, ~).
	SchemaPyYAML Schema = iota
	// SchemaNative is oxidns_native.NativeLoader: only true/false booleans,
	// 0x/0o/0b integers, no timestamps, no merge keys; every mapping key goes
	// through Key.
	SchemaNative
)

// LoadOptions tune Load.
type LoadOptions struct {
	Schema Schema
	// Key, when set, is called for every mapping key after it is constructed
	// and before its value is, in PyYAML's order (merged keys included for
	// SchemaPyYAML). key is the loaded key (a string, jsonx.Number, bool,
	// nil, []byte, Timestamp) or an Unhashable for a collection; duplicate
	// tells that an equal key (Python dict equality) is already present in
	// the same mapping. A non-nil result aborts the load and is returned
	// unchanged. NativeLoader's construct_mapping is this hook.
	Key func(key any, duplicate bool) error
	// Name is the stream name in error marks; empty means "<unicode string>".
	Name string
	// KeepNonStringKeys returns a mapping that has a key other than a string
	// (`1: a`, `yes: a`, `~: a`) as *Mapping, keys and order as PyYAML builds
	// them. Without it such a document gives *NonStringKeyError.
	KeepNonStringKeys bool
	// KeepUnrepresentable puts an *Unrepresentable in place of each value
	// jsonx cannot hold (a recursive alias at the point it refers back,
	// !!set, non-empty !!omap/!!pairs) instead of failing with
	// *UnrepresentableError, so callers can tell whether such a value reaches
	// their output (Python would keep and later use or drop it).
	KeepUnrepresentable bool
}

// Error is a Python exception raised while loading: Error() is str(exc) and
// Class the exception class. Scanner, parser, composer and constructor
// errors (ReaderError, ScannerError, ParserError, ComposerError,
// ConstructorError) are yaml.YAMLError subclasses with PyYAML's text and
// marks; constructors also raise ValueError (int(), float(), dates),
// KeyError (!!bool), IndexError, TypeError, AttributeError, and deep nesting
// RecursionError.
type Error struct {
	Class string
	Msg   string
}

func (e *Error) Error() string { return e.Msg }

// PythonClass is the Python exception class name.
func (e *Error) PythonClass() string { return e.Class }

// IsYAMLError reports whether Python would raise a yaml.YAMLError (what
// callers catch with `except yaml.YAMLError`).
func (e *Error) IsYAMLError() bool {
	switch e.Class {
	case "ReaderError", "ScannerError", "ParserError", "ComposerError", "ConstructorError":
		return true
	}
	return false
}

// NonStringKeyError is returned when a mapping key is not a string (`{80: a}`,
// `{yes: a}`, `{~: a}`) and LoadOptions.KeepNonStringKeys is off: PyYAML
// builds a dict with int, float, bool, None, bytes or date keys, a
// jsonx.Object has string keys only. It is reported after the whole document
// was constructed, so every PyYAML error comes first.
type NonStringKeyError struct {
	Key  any
	Line int
	Msg  string
}

func (e *NonStringKeyError) Error() string { return e.Msg }

// UnrepresentableError is a document PyYAML loads into a value the jsonx
// model cannot hold: Kind "recursive" (a collection containing itself through
// an alias), "set" (!!set), "omap" or "pairs" (lists of tuples). Like
// NonStringKeyError it comes after every PyYAML error.
type UnrepresentableError struct {
	Kind string
	Msg  string
}

func (e *UnrepresentableError) Error() string { return e.Msg }

// Unrepresentable stands for a value jsonx cannot hold
// (LoadOptions.KeepUnrepresentable); Kind and Msg as in UnrepresentableError.
type Unrepresentable struct {
	Kind string
	Msg  string
}

// Mapping is a mapping with at least one key that is not a string, as PyYAML
// builds it (LoadOptions.KeepNonStringKeys): keys are string, jsonx.Number,
// bool, nil, []byte or Timestamp, in insertion order, equal keys merged as in
// a Python dict (1, 1.0 and true are one key; the first key object stays,
// the last value wins). Values are ordinary loaded values.
type Mapping struct{ Pairs []Pair }

// Pair is one entry of a Mapping.
type Pair struct{ Key, Value any }

// Get is mapping.get(key) for a string key.
func (m *Mapping) Get(key string) (any, bool) {
	for _, p := range m.Pairs {
		if k, ok := p.Key.(string); ok && k == key {
			return p.Value, true
		}
	}
	return nil, false
}

// Len is len(mapping).
func (m *Mapping) Len() int { return len(m.Pairs) }

// Lookup is `value.get(key)` for a loaded mapping of either kind
// (*jsonx.Object or *Mapping); ok is false for other values and missing keys.
func Lookup(value any, key string) (any, bool) {
	switch m := value.(type) {
	case *jsonx.Object:
		return m.Get(key)
	case *Mapping:
		return m.Get(key)
	}
	return nil, false
}

// IsMapping reports whether a loaded value is a Python dict.
func IsMapping(value any) bool {
	switch value.(type) {
	case *jsonx.Object, *Mapping:
		return true
	}
	return false
}

// Load is yaml.safe_load(text): one document (an empty stream is nil).
func Load(text string) (any, error) {
	return LoadWith(text, LoadOptions{})
}

// SafeLoad is fast_yaml.safe_load: yaml.safe_load. The libyaml verification
// cache of the Python module only makes repeated loads faster; the result is
// always PyYAML's.
func SafeLoad(text string) (any, error) {
	return LoadWith(text, LoadOptions{})
}

// LoadBytes is yaml.safe_load(bytes): UTF-8, or UTF-16 after a UTF-16 byte
// order mark, with the stream name "<byte string>". A UTF-8 decoding error is
// PyYAML's ReaderError; UTF-16 decoding errors are not reproduced (invalid
// sequences become U+FFFD).
func LoadBytes(data []byte, opts LoadOptions) (any, error) {
	if opts.Name == "" {
		opts.Name = "<byte string>"
	}
	var text string
	switch {
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		text = decodeUTF16(data, false)
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		text = decodeUTF16(data, true)
	default:
		if !utf8.Valid(data) {
			position, reason := utf8Failure(data)
			return nil, &Error{Class: "ReaderError", Msg: fmt.Sprintf("unacceptable character #x%04x: %s\n  in \"%s\", position %d",
				data[position], reason, opts.Name, position)}
		}
		text = string(data)
	}
	return LoadWith(text, opts)
}

func decodeUTF16(data []byte, bigEndian bool) string {
	units := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		if bigEndian {
			units = append(units, uint16(data[i])<<8|uint16(data[i+1]))
		} else {
			units = append(units, uint16(data[i+1])<<8|uint16(data[i]))
		}
	}
	return string(utf16.Decode(units))
}

// utf8Failure is the start and reason of CPython's UnicodeDecodeError.
func utf8Failure(data []byte) (int, string) {
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if r != utf8.RuneError || size > 1 {
			i += size
			continue
		}
		c := data[i]
		switch {
		case c < 0xC2 || c > 0xF4:
			return i, "invalid start byte"
		case i+1 >= len(data):
			return i, "unexpected end of data"
		}
		lo, hi := byte(0x80), byte(0xBF)
		switch c {
		case 0xE0:
			lo = 0xA0
		case 0xED:
			hi = 0x9F
		case 0xF0:
			lo = 0x90
		case 0xF4:
			hi = 0x8F
		}
		need := 1
		if c >= 0xE0 {
			need = 2
		}
		if c >= 0xF0 {
			need = 3
		}
		for j := 1; j <= need; j++ {
			if i+j >= len(data) {
				return i, "unexpected end of data"
			}
			b := data[i+j]
			if b < lo || b > hi {
				return i, "invalid continuation byte"
			}
			lo, hi = 0x80, 0xBF
		}
		i++
	}
	return 0, ""
}

// LoadWith is yaml.load(text, Loader=...) with the given options: PyYAML's
// reader, scanner, parser and composer (internal/pyyaml), then its
// constructor (see construct.go).
func LoadWith(text string, opts LoadOptions) (any, error) {
	if opts.Name == "" {
		opts.Name = "<unicode string>"
	}
	root, err := pyyaml.ComposeWith(text, pyyaml.ComposeOptions{Name: opts.Name, Resolve: resolverFor(opts.Schema)})
	if err != nil {
		return nil, composeError(err)
	}
	return constructRoot(root, opts)
}

func constructRoot(root *pyyaml.Node, opts LoadOptions) (any, error) {
	if root == nil {
		return nil, nil
	}
	c := &constructor{opts: opts, constructed: map[*pyyaml.Node]any{}, recursive: map[*pyyaml.Node]bool{}}
	value, err := c.document(root)
	if err != nil {
		return nil, err
	}
	v := &converter{keep: opts.KeepNonStringKeys, placeholder: opts.KeepUnrepresentable, done: map[any]any{}, active: map[any]bool{}}
	return v.convert(value)
}

func composeError(err error) error {
	var marked *pyyaml.Error
	var value *pyyaml.ValueError
	var recursion *pyyaml.RecursionError
	switch {
	case errors.As(err, &marked):
		return &Error{Class: marked.Class, Msg: marked.Error()}
	case errors.As(err, &value):
		return &Error{Class: "ValueError", Msg: value.Error()}
	case errors.As(err, &recursion):
		return &Error{Class: "RecursionError", Msg: recursion.Error()}
	}
	return &Error{Class: "YAMLError", Msg: err.Error()}
}

func resolverFor(schema Schema) func(string, [2]bool) string {
	resolvers := pyResolvers
	if schema == SchemaNative {
		resolvers = nativeResolvers
	}
	return func(value string, implicit [2]bool) string {
		if !implicit[0] {
			return tagStr
		}
		return detectWith(resolvers, value)
	}
}
