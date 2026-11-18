package health

import (
	"math"

	"configflow/internal/jsonx"
)

const (
	classIdentityWrongType     = "identity_wrong_type"
	classNumericAsText         = "numeric_as_text"
	classIntegralOrRangeNumber = "integral_or_range_number"
	classStoreWriteValidation  = "store_write_validation"
	classMalformedShapeR5      = "malformed_shape_r5"
)

func isInt64Literal(v any) bool {
	n, ok := v.(jsonx.Number)
	if !ok || !n.IsInt() {
		return false
	}
	_, fits := n.Int64()
	return fits
}

func isIntegralInt64(v any) (int64, bool) {
	n, ok := v.(jsonx.Number)
	if !ok {
		return 0, false
	}
	if n.IsInt() {
		return n.Int64()
	}
	f := n.Float64()
	if math.IsNaN(f) || math.IsInf(f, 0) || math.Trunc(f) != f {
		return 0, false
	}
	if f < -9223372036854775808.0 || f >= 9223372036854775808.0 {
		return 0, false
	}
	return int64(f), true
}

func classifyRegionResultsInput(items []any, checkYouTubeNumericGL bool) string {
	for _, raw := range items {
		obj, ok := raw.(*jsonx.Object)
		if !ok || obj == nil {
			return classMalformedShapeR5
		}
		id, hasID := obj.Get("id")
		if !hasID || id == nil {
			return classIdentityWrongType
		}
		switch id.(type) {
		case string, jsonx.Number:
		default:
			return classIdentityWrongType
		}
		if status, ok := obj.Get("status"); ok && status != nil {
			n, isNum := status.(jsonx.Number)
			if !isNum {
				return classMalformedShapeR5
			}
			if !isInt64Literal(n) {
				return classIntegralOrRangeNumber
			}
		}
		if finalURL, ok := obj.Get("final_url"); ok && finalURL != nil {
			if _, ok := finalURL.(string); !ok {
				return classMalformedShapeR5
			}
		}
		if redirects, ok := obj.Get("redirects"); ok {
			list, isList := redirects.([]any)
			if !isList {
				return classMalformedShapeR5
			}
			for _, elem := range list {
				switch elem.(type) {
				case string:
				default:
					return classMalformedShapeR5
				}
			}
		}
		if markers, ok := obj.Get("markers"); ok {
			mObj, isObj := markers.(*jsonx.Object)
			if !isObj || mObj == nil {
				return classMalformedShapeR5
			}
			for k, mv := range mObj.All() {
				switch mv.(type) {
				case nil, string:
				case jsonx.Number:
					if checkYouTubeNumericGL && id == "youtube-premium" && k == "gl" {
						return classNumericAsText
					}
				default:
					return classMalformedShapeR5
				}
			}
		}
		if errVal, ok := obj.Get("error"); ok && errVal != nil {
			if _, ok := errVal.(string); !ok {
				return classMalformedShapeR5
			}
		}
	}
	return ""
}

func classifyPathStatsInput(v any) string {
	if v == nil {
		return ""
	}
	obj, ok := v.(*jsonx.Object)
	if !ok || obj == nil {
		return classMalformedShapeR5
	}
	if totalVal, hasTotal := obj.Get("total"); hasTotal {
		n, isNum := totalVal.(jsonx.Number)
		if !isNum {
			return classMalformedShapeR5
		}
		if !isInt64Literal(n) {
			return classIntegralOrRangeNumber
		}
		if t, _ := n.Int64(); t != 0 && !obj.Has("ok") {
			return classMalformedShapeR5
		}
	}
	if okVal, hasOK := obj.Get("ok"); hasOK {
		n, isNum := okVal.(jsonx.Number)
		if !isNum {
			return classMalformedShapeR5
		}
		if !isInt64Literal(n) {
			return classIntegralOrRangeNumber
		}
	}
	if errs, hasErrs := obj.Get("errors"); hasErrs {
		eObj, isObj := errs.(*jsonx.Object)
		if !isObj || eObj == nil {
			return classMalformedShapeR5
		}
	}
	if delays, hasDelays := obj.Get("delays"); hasDelays && delays != nil {
		list, isList := delays.([]any)
		if !isList {
			return classMalformedShapeR5
		}
		for _, item := range list {
			n, isNum := item.(jsonx.Number)
			if !isNum {
				return classMalformedShapeR5
			}
			f := n.Float64()
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return classIntegralOrRangeNumber
			}
		}
	}
	return ""
}

// classifyReplayInput classifies synthetic corpus inputs whose dynamic Python
// behaviour intentionally changed under N8 typed / R5 / R7 rules. It inspects
// only input arguments, never Go's output or error.
func classifyReplayInput(fn string, positional []any, named *jsonx.Object, mutated *jsonx.Object) string {
	argAt := func(idx int, key string) any {
		if named != nil {
			return named.Value(key)
		}
		if idx < len(positional) {
			return positional[idx]
		}
		return nil
	}

	switch fn {
	case "initialize_provider_health":
		if src, ok := argAt(0, "source").(*jsonx.Object); ok && src != nil && !src.Has("health_check") {
			if url, ok := src.Get("health_check_url"); ok && url != nil {
				switch url.(type) {
				case string:
				case jsonx.Number:
					return classNumericAsText
				default:
					return classMalformedShapeR5
				}
			}
		}
	case "region_of_name":
		name := argAt(0, "name")
		switch name.(type) {
		case nil, string:
		case jsonx.Number:
			return classNumericAsText
		default:
			return classMalformedShapeR5
		}
	case "region_label":
		code := argAt(0, "code")
		switch code.(type) {
		case nil, string:
		case jsonx.Number:
			return classNumericAsText
		default:
			return classMalformedShapeR5
		}
	case "evaluate_openai", "evaluate_claude", "evaluate_netflix":
		if list, ok := argAt(0, "results").([]any); ok {
			return classifyRegionResultsInput(list, false)
		}
		return classMalformedShapeR5
	case "evaluate_youtube", "evaluate_services":
		if list, ok := argAt(0, "results").([]any); ok {
			return classifyRegionResultsInput(list, true)
		}
		return classMalformedShapeR5
	case "exit_info":
		if list, ok := argAt(0, "results").([]any); ok {
			return classifyRegionResultsInput(list, false)
		}
		return classMalformedShapeR5
	case "classify_domain_result":
		res := argAt(0, "result")
		if res == nil {
			return ""
		}
		obj, ok := res.(*jsonx.Object)
		if !ok || obj == nil {
			return classMalformedShapeR5
		}
		if status, has := obj.Get("status"); has && status != nil {
			n, isNum := status.(jsonx.Number)
			if !isNum {
				return classMalformedShapeR5
			}
			if !isInt64Literal(n) {
				return classIntegralOrRangeNumber
			}
		}
		if finalURL, has := obj.Get("final_url"); has && finalURL != nil {
			if _, ok := finalURL.(string); !ok {
				return classMalformedShapeR5
			}
		}
		if errVal, has := obj.Get("error"); has && errVal != nil {
			if _, ok := errVal.(string); !ok {
				return classMalformedShapeR5
			}
		}
	case "summarize_domain":
		per, ok := argAt(0, "per_target").(*jsonx.Object)
		if !ok || per == nil {
			return classMalformedShapeR5
		}
		for _, v := range per.Values() {
			obj, isObj := v.(*jsonx.Object)
			if !isObj || obj == nil || !obj.Has("state") {
				return classMalformedShapeR5
			}
			if _, ok := obj.Value("state").(string); !ok {
				return classMalformedShapeR5
			}
		}
	case "pick_targets":
		list, ok := argAt(0, "targets").([]any)
		if !ok {
			return classMalformedShapeR5
		}
		hasNumericName := false
		hasShapeR5 := false
		for _, item := range list {
			obj, isObj := item.(*jsonx.Object)
			if !isObj || obj == nil {
				return classMalformedShapeR5
			}
			name, hasName := obj.Get("name")
			if !hasName || name == nil {
				return classIdentityWrongType
			}
			switch name.(type) {
			case string:
			case jsonx.Number:
				hasNumericName = true
			default:
				return classIdentityWrongType
			}
			if kind, hasType := obj.Get("type"); hasType {
				if _, isStr := kind.(string); !isStr {
					hasShapeR5 = true
				}
			}
			if alive, hasAlive := obj.Get("alive"); hasAlive {
				if _, isBool := alive.(bool); !isBool {
					hasShapeR5 = true
				}
			}
		}
		if hasNumericName {
			return classNumericAsText
		}
		if hasShapeR5 {
			return classMalformedShapeR5
		}
	case "judge":
		if c := classifyPathStatsInput(argAt(0, "direct")); c != "" {
			return c
		}
		if c := classifyPathStatsInput(argAt(1, "proxy")); c != "" {
			return c
		}
	case "path_healthy":
		if c := classifyPathStatsInput(argAt(0, "stats")); c != "" {
			return c
		}
	case "probe_url":
		port := argAt(1, "port")
		if port != nil {
			if _, ok := isIntegralInt64(port); !ok {
				return classMalformedShapeR5
			}
		}
	case "domain_suggestion":
		list, ok := argAt(0, "hosts").([]any)
		if !ok {
			return classMalformedShapeR5
		}
		for _, item := range list {
			hObj, isObj := item.(*jsonx.Object)
			if !isObj || hObj == nil {
				return classMalformedShapeR5
			}
			probe, hasProbe := hObj.Get("probe")
			if !hasProbe || probe == nil {
				continue
			}
			pObj, isPObj := probe.(*jsonx.Object)
			if !isPObj || pObj == nil {
				return classMalformedShapeR5
			}
			if pObj.Len() == 0 {
				continue
			}
			if jsonx.Truthy(pObj.Value("target")) {
				conf, hasConf := pObj.Get("confidence")
				if !hasConf {
					return classMalformedShapeR5
				}
				n, isNum := conf.(jsonx.Number)
				if !isNum {
					return classMalformedShapeR5
				}
				if !isInt64Literal(n) {
					return classIntegralOrRangeNumber
				}
			}
		}
	case "ProbeStore.save":
		res, ok := argAt(1, "results").(*jsonx.Object)
		if !ok || res == nil {
			return classStoreWriteValidation
		}
		for _, entry := range res.Values() {
			eObj, isObj := entry.(*jsonx.Object)
			if !isObj || eObj == nil {
				return classStoreWriteValidation
			}
			if at, has := eObj.Get("checked_at"); has {
				if _, isStr := at.(string); !isStr {
					return classStoreWriteValidation
				}
			}
		}
	case "RegionStore.update":
		if mutated != nil {
			for _, section := range []string{"domains", "websites"} {
				sObj, ok := mutated.Value(section).(*jsonx.Object)
				if !ok || sObj == nil {
					return classStoreWriteValidation
				}
				for _, entry := range sObj.Values() {
					eObj, isObj := entry.(*jsonx.Object)
					if !isObj || eObj == nil {
						return classStoreWriteValidation
					}
					if at, has := eObj.Get("checked_at"); has {
						if _, isStr := at.(string); !isStr {
							return classStoreWriteValidation
						}
					}
				}
			}
		}
	}
	return ""
}
