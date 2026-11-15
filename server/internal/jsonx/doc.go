// Package jsonx holds JSON documents as dynamic, insertion-ordered values and
// encodes them byte for byte like CPython 3.11's json module.
//
// # Value model
//
// A value is one of:
//
//	Python      Go
//	dict        *Object   (insertion-ordered, string keys)
//	list        []any
//	str         string
//	int, float  Number    (canonical Python text: "1" is an int, "1.0" a float)
//	bool        bool
//	None        nil
//
// Parse and DeepCopy only ever produce these types. For convenience the
// encoders, Equal, Truthy, the As* helpers and the typed getters also accept
// Go's built-in integer and float types where a Number is expected, and
// Object.Set converts them to Number on the way in, so `o.Set("port", 8080)`
// and `[]any{1, 2}` work. Anything else (maps, typed slices, structs) is not a
// jsonx value: the encoders return an *UnsupportedTypeError for it, like
// json.dumps raises TypeError.
//
// # Numbers
//
// Python's json.loads turns `1.50` into the float 1.5 and json.dumps writes
// it back as `1.5`; `1E2` comes back as `100.0`, `-0` as `0`, `1e999` as
// `Infinity`. A Go read-modify-write must produce the same bytes as a Python
// one, so Number does not keep the literal found in the input: Parse
// normalises every number to the text Python would write (int.__repr__ or
// float.__repr__). Integers keep every digit, whatever their size.
//
// # Encoders
//
// Compact, Indent2, Sorted, Default and Response are the json.dumps argument
// sets the application uses; Encode takes the options one by one. All of
// them are verified against CPython 3.11 and Flask 3.0 by the corpus in
// testdata (see testdata/gen.py).
//
// # Deviations from CPython
//
//   - Strings are UTF-8. A lone surrogate escape (`"\ud800"`), which Python
//     accepts and keeps as a lone surrogate code point, is decoded as U+FFFD.
//     Bytes that are not valid UTF-8 inside a Go string are encoded as U+FFFD
//     (one per invalid byte); Parse rejects input that is not valid UTF-8.
//   - Parse takes the text form only (json.loads(str)): no byte order mark,
//     no UTF-16/32 detection.
//   - Nesting deeper than MaxDepth is an error instead of a RecursionError
//     at whatever depth the Python stack happens to allow.
//   - Equal treats every NaN inside a list or object as the same object
//     (true for values that came from json.loads, where NaN is a singleton;
//     Python code that creates its own float('nan') objects differs).
package jsonx
