// Package yamlx reads and writes schema-free YAML documents on yaml.v3.
// Parse, Decode, Encode and Marshal use Node trees and native typed values;
// FromJSON and ToJSON bridge ordered dynamic JSON documents. The package does
// no I/O and knows no application document schema.
//
// During the N7 migration, the legacy PyYAML APIs below remain alongside the
// native API on the jsonx value model. Their callers migrate in later steps.
//
// # Dump
//
// Dump is a line-by-line port of PyYAML's pure-Python representer, serializer
// and emitter (yaml.dump / yaml.safe_dump with SafeDumper), so the bytes equal
// PyYAML's for every option set the application uses: see DumpOptions and the
// presets SafeDump, SafeDumpUnicode and IndentDump. Anchors and aliases are
// produced for a *jsonx.Object or non-empty []any that occurs twice, as PyYAML
// does for a Python dict or list that is the same object; use
// DumpOptions.NoAliases when the Python code would have copied it. FlowList,
// FlowMap, SharedString and Marked stand in for the Python subclasses and
// dumper hooks of agents/docker_ha_compact.py and agents/push_diff.py.
//
// # Load
//
// Load is yaml.safe_load: PyYAML 6.0.1's pure-Python reader, scanner, parser
// and composer (internal/pyyaml) and a port of its SafeConstructor, so the
// documents accepted, the values built (YAML 1.1 booleans yes/no/on/off,
// octal, binary and hex integers, sexagesimal numbers, timestamps as
// Timestamp, !!binary as []byte, merge keys, "=" keys, duplicate keys with
// Python dict equality) and the error texts with their marks are PyYAML's.
// Errors are *Error with the Python exception class. LoadOptions selects
// NativeLoader's YAML 1.2 style typing (oxidns_native.NativeLoader) and lets
// the caller check keys. LoadCSafeFirst is the "CSafeLoader, else
// safe_load" pattern of two callers.
//
// # What the jsonx model cannot hold
//
// After PyYAML would have returned, the value is converted to the jsonx
// model. Three kinds of Python values have no counterpart and are reported
// instead, after every PyYAML error: a mapping with a non-string key
// (*NonStringKeyError, or *Mapping with LoadOptions.KeepNonStringKeys), a
// collection that contains itself through an alias, and !!set, !!omap,
// !!pairs (*UnrepresentableError). A shared empty list is not recognised as
// shared by Dump (an empty Go slice has no identity), so PyYAML's anchor for
// it is not written. Strings with lone surrogates cannot exist in Go (a \U
// escape naming one becomes U+FFFD). Python's RecursionError depth is a
// fixed limit. Only the pure-Python SafeDumper is reproduced. CSafeDumper
// (libyaml's emitter, used once, to measure a size in agents/push_diff.py)
// writes different text for some inputs; the corpus records how often.
package yamlx
