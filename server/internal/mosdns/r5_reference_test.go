package mosdns

import (
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

// projectR5Record changes only declared input members using the shared model's
// decoded values. It never reads generated Go output. Missing optional values
// become missing inputs, allowing the real Python function's consumer defaults.
func projectR5Record(original *jsonx.Object, native reflect.Value) *jsonx.Object {
	out := original.DeepCopy()
	typ := native.Type()
	for i := 0; i < native.NumField(); i++ {
		member := typ.Field(i)
		if !member.IsExported() {
			continue
		}
		key := member.Tag.Get("json")
		if key == "" || key == "-" {
			continue
		}
		old, present := original.Get(key)
		if !present {
			continue
		}
		value := native.Field(i)
		if value.Kind() == reflect.Pointer && value.IsNil() {
			out.Delete(key)
			continue
		}
		out.Set(key, projectR5Value(old, value))
	}
	return out
}
func projectR5Value(original any, value reflect.Value) any {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return projectR5Value(original, value.Elem())
	}
	if value.Type() == reflect.TypeFor[model.TextLines]() {
		v := value.Interface().(model.TextLines)
		if v.Text != nil {
			return *v.Text
		}
		return stringsValue(v.Lines)
	}
	switch value.Kind() {
	case reflect.Struct:
		object, _ := original.(*jsonx.Object)
		if object == nil {
			object = jsonx.NewObject()
		}
		return projectR5Record(object, value)
	case reflect.Slice:
		old, _ := original.([]any)
		out := []any{}
		for i := 0; i < value.Len(); i++ {
			var previous any
			if i < len(old) {
				previous = old[i]
			}
			out = append(out, projectR5Value(previous, value.Index(i)))
		}
		return out
	case reflect.String:
		return value.String()
	case reflect.Bool:
		return value.Bool()
	case reflect.Int, reflect.Int64:
		return jsonx.Int(value.Int())
	}
	return original
}
func stringsValue(values []string) []any {
	out := []any{}
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
func r5Arguments(fn string, args []any) ([]any, bool) {
	projected := jsonx.DeepCopy(args).([]any)
	projectConfig := func(index int) {
		original, ok := args[index].(*jsonx.Object)
		if !ok {
			return
		}
		native, err := legacyConfig(original)
		if err != nil {
			return
		}
		out := original.DeepCopy()
		for _, key := range []string{"mosdns", "rule_configs"} {
			if old, ok := original.Get(key); ok {
				var value reflect.Value
				if key == "mosdns" {
					value = reflect.ValueOf(native.Profile.MosDNS)
				} else {
					value = reflect.ValueOf(native.Profile.RuleConfigs)
				}
				out.Set(key, projectR5Value(old, value))
			}
		}
		if raw, present := original.Get("system_config"); present {
			system, ok := raw.(*jsonx.Object)
			if !ok {
				system = jsonx.NewObject()
			}
			sys := projectR5Record(system, reflect.ValueOf(native.SystemConfig))
			if _, ok := system.Value("rule_proxy_token").(string); !ok {
				if system.Has("rule_proxy_token") {
					sys.Set("rule_proxy_token", "")
				}
			}
			out.Set("system_config", sys)
		}
		if _, present := original.Get("profile_id"); present {
			out.Set("profile_id", url.PathEscape(native.ProfileID))
		}
		for _, key := range []string{"rules", "rule_sets"} {
			if old, ok := original.Get(key); ok {
				rules, sets, err := SplitRules(native)
				if err == nil {
					value := rules
					if key == "rule_sets" {
						value = sets
					}
					out.Set(key, projectR5Value(old, reflect.ValueOf(value)))
				}
			}
		}
		if fn == "downloads" || fn == "generate" {
			for _, key := range []string{"rule_configs", "rule_sets"} {
				if entries, ok := out.Value(key).([]any); ok {
					for _, entry := range entries {
						if item, ok := entry.(*jsonx.Object); ok && (key == "rule_sets" || item.Value("itemType") == "ruleset") && !item.Has("name") {
							item.Set("name", "")
						}
					}
				}
			}
		}

		if fn != "split" {
			for _, key := range []string{"rule_configs", "rules", "rule_sets"} {
				if entries, ok := out.Value(key).([]any); ok {
					for _, entry := range entries {
						if item, ok := entry.(*jsonx.Object); ok {
							for _, field := range []string{"id", "name", "library_rule_id", "rule_type", "value"} {
								if !item.Has(field) {
									item.Set(field, "")
								}
							}
							if !item.Has("itemType") {
								kind := ""
								if key == "rules" {
									kind = "rule"
								}
								item.Set("itemType", kind)
							}
						}
					}
				}
			}
		}

		projected[index] = out
	}
	switch fn {
	case "generate", "downloads", "custom_files", "split", "proxy_url", "ruleset_files":
		if len(args) > 0 {
			projectConfig(0)
		}
	case "custom_matches":
		data, err := jsonx.Compact(jsonx.Obj("custom_matches", args[0]))
		if err == nil {
			var settings model.MosDNSSettings
			if json.Unmarshal(data, &settings) == nil {
				projected[0] = projectR5Value(args[0], reflect.ValueOf(settings.CustomMatches))
			}
		}
	}
	if fn == "ruleset_files" && len(args) > 2 {
		data, err := jsonx.Compact(args[2])
		if err == nil {
			var item model.RuleConfig
			if json.Unmarshal(data, &item) == nil {
				projected[2] = projectR5Value(args[2], reflect.ValueOf(item))
				if object, ok := projected[2].(*jsonx.Object); ok && !object.Has("name") {
					object.Set("name", item.Name)
				}
			}
		}
	}
	if fn == "proxy_url" {
		for _, i := range []int{3, 4} {
			if i < len(args) {
				if _, ok := args[i].(string); !ok {
					projected[i] = nil
				}
			}
		}
	}
	a, _ := jsonx.Compact(args)
	b, _ := jsonx.Compact(projected)
	return projected, string(a) != string(b)
}
func nativeR5Outcome(t *testing.T, oracle *goldenOracle, row *jsonx.Object, got any, actualErr error) bool {
	t.Helper()
	fn := row.GetString("fn")
	if fn == "upstreams" && errors.Is(actualErr, ErrUpstreamType) {
		if _, ok := row.GetList("args")[0].(string); !ok {
			return true
		}
	}
	args, changed := r5Arguments(fn, row.GetList("args"))
	if !changed {
		return false
	}
	if fn == "generate" {
		config, _ := args[0].(*jsonx.Object)
		if config != nil {
			if settings, ok := config.Value("mosdns").(*jsonx.Object); ok {
				for _, key := range []string{"custom_config", "local_dns", "remote_dns", "fallback_dns"} {
					if source, ok := settings.Value(key).(string); ok {
						if key != "custom_config" {
							root, err := yamlx.Parse([]byte(source))
							if err != nil || !nativeDNSContainer(root) {
								continue
							}
						}
						if text, err := yamlxtest.ReferenceText(source); err == nil {
							settings.Set(key, text)
						}
					}
				}
			}
		}
	}
	answer := oracle.Call(t, jsonx.Obj("fn", fn, "args", args))
	if expected := answer.GetList("err"); expected != nil {
		return actualErr != nil && len(expected) == 2 && errClass(actualErr) == expected[0]
	}
	if actualErr != nil {
		return false
	}
	want := answer.Value("out")
	if fn == "generate" {
		a, aok := got.(string)
		b, bok := want.(string)
		return aok && bok && yamlxtest.SemanticEqual(a, b)
	}
	return jsonx.Equal(got, want)
}
func TestR5ReferenceProjectionStaysNarrow(t *testing.T) {
	args := []any{jsonx.Obj("mosdns", jsonx.Obj("cache_size", true, "cache_enabled", "false"), "future", jsonx.Obj("keep", 7))}
	projected, changed := r5Arguments("generate", args)
	if !changed {
		t.Fatal("missing R5 projection")
	}
	config := projected[0].(*jsonx.Object)
	if !jsonx.Equal(config.Value("future"), args[0].(*jsonx.Object).Value("future")) {
		t.Fatal("unowned field changed")
	}
	if config.GetObject("mosdns").Has("cache_size") || config.GetObject("mosdns").Has("cache_enabled") {
		t.Fatal("invalid optional value reached reference")
	}
	normal := []any{jsonx.Obj("mosdns", jsonx.Obj("cache_size", jsonx.Int(128)))}
	if _, changed = r5Arguments("generate", normal); changed {
		t.Fatal("normal input changed")
	}
}
