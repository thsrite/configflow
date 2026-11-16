package oxidns

import (
	"math/big"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// TestC23Recorded replays every top-level call of the C23 functions
// (validate_native_config, require_valid_document,
// validate_expanded_document, validate_settings, collect_reference_issues,
// collect_plugin_issues, collect_global_issues, validate_plugin_format) the
// whole Python test suite makes (testdata/c23_record.py).
func TestC23Recorded(t *testing.T) {
	replay(t, "testdata/c23_recorded.jsonl.gz", func(entry *jsonx.Object) string {
		return strings.TrimPrefix(entry.GetString("test"), "backend/")
	})
}

// TestC23Corpus replays the random corpus of testdata/gen_c23.py.
func TestC23Corpus(t *testing.T) {
	replay(t, "testdata/c23_corpus.jsonl.gz", nil)
}

// callRowC23 runs the Go counterpart of a C23 row (and of the private
// helpers the corpus calls directly).
func callRowC23(fn string, args []any) (out any, err error, ok bool) {
	arg := func(i int) any {
		if i < len(args) {
			return args[i]
		}
		return nil
	}
	text := func(i int) (string, bool) {
		s, ok := arg(i).(string)
		return s, ok
	}
	guard := func(f func() any) (out any, err error) {
		defer catch(&err)
		return f(), nil
	}
	switch fn {
	case "validate_native_config":
		out, err = ValidateNativeConfig(arg(0))
		return out, err, true
	case "require_valid_document":
		out, err = RequireValidDocument(arg(0))
		return out, err, true
	case "validate_expanded_document":
		out, err = ValidateExpandedDocument(arg(0), arg(1))
		return out, err, true
	case "validate_settings":
		base, ok := text(2)
		if !ok {
			return nil, nil, false
		}
		out, err = ValidateSettings(arg(0), arg(1), base)
		return out, err, true
	case "collect_reference_issues":
		catalog, ok := obj(resolveSchemas(arg(1)))
		given, ok2 := arg(2).(bool)
		if !ok || !ok2 {
			return nil, nil, false
		}
		if !given {
			out, err = CollectReferenceIssues(arg(0), catalog, nil, nil)
			return out, err, true
		}
		graph := map[string][]string{}
		var order []string
		issues, err := CollectReferenceIssues(arg(0), catalog, graph, &order)
		if err != nil {
			return nil, err, true
		}
		object := jsonx.NewObject()
		for _, tag := range order {
			object.Set(tag, jsonx.ListOf(graph[tag]))
		}
		return jsonx.Obj("issues", issues, "graph", object), nil, true
	case "collect_plugin_issues":
		out, err = CollectPluginIssues(arg(0), arg(1), arg(2))
		return out, err, true
	case "collect_global_issues":
		out, err = CollectGlobalIssues(arg(0))
		return out, err, true
	case "validate_plugin_format":
		out, err = ValidatePluginFormat(arg(0), arg(1))
		return out, err, true
	case "zone_records_error":
		single, ok := arg(1).(bool)
		if !ok {
			return nil, nil, false
		}
		out, err = guard(func() any { return orNone(zoneRecordsError(arg(0), single)) })
		return out, err, true
	case "adguard_error":
		raw, ok := text(0)
		if !ok {
			return nil, nil, false
		}
		out, err = guard(func() any {
			message, severity := adguardError(raw)
			return []any{orNone(message), severity}
		})
		return out, err, true
	case "cron_error":
		out, err = guard(func() any { return orNone(cronError(arg(0))) })
		return out, err, true
	case "domain_error":
		dynamic, ok := arg(1).(bool)
		if !ok {
			return nil, nil, false
		}
		out, err = guard(func() any { return orNone(domainError(arg(0), dynamic)) })
		return out, err, true
	case "regex_error":
		raw, ok := text(0)
		if !ok {
			return nil, nil, false
		}
		out, err = guard(func() any { return orNone(regexError(raw)) })
		return out, err, true
	case "duration":
		out, err = guard(func() any {
			seconds, ok := durationSeconds(arg(0))
			if !ok {
				return nil
			}
			return durationText(seconds)
		})
		return out, err, true
	case "zoneinfo":
		key, ok := text(0)
		if !ok {
			return nil, nil, false
		}
		return zoneInfoValid(key), nil, true
	case "idna_to_ascii":
		label, ok := text(0)
		if !ok {
			return nil, nil, false
		}
		encoded, valid := idnaToASCII([]rune(label))
		if !valid {
			return nil, nil, true
		}
		return string(encoded), nil, true
	}
	return nil, nil, false
}

func orNone(message string) any {
	if message == "" {
		return nil
	}
	return message
}

// durationText renders _duration's result the way gen_c23.py records it:
// repr(float(seconds)).
func durationText(seconds *big.Float) string {
	f, _ := seconds.Float64()
	return jsonx.Float(f).String()
}
