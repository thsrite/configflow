package oxidns

import (
	"encoding/json"
	"reflect"

	"configflow/internal/model"
)

// projectReferenceMetadata uses only the shared record decoder, before Python
// business logic runs. It never consults a generated Go result or catalog.
func projectReferenceMetadata(fn string, args []any) bool {
	index := -1
	switch fn {
	case "generate", "downloads", "custom_files", "unavailable_rulesets":
		index = 0
	case "read_common_settings", "prepare_editor":
		index = 1
	case "apply_common_settings", "read_routing", "apply_routing", "resource_downloads":
		index = 2
	}
	if index < 0 || index >= len(args) {
		return false
	}
	config, ok := args[index].(map[string]any)
	if !ok {
		return false
	}
	changed := false
	for _, key := range []string{"rule_configs", "rule_library"} {
		entries, ok := config[key].([]any)
		if !ok {
			continue
		}
		for _, value := range entries {
			original, ok := value.(map[string]any)
			if !ok {
				continue
			}
			data, err := json.Marshal(original)
			if err != nil {
				continue
			}
			var decoded reflect.Value
			if key == "rule_configs" {
				var record model.RuleConfig
				if json.Unmarshal(data, &record) != nil {
					continue
				}
				decoded = reflect.ValueOf(record)
			} else {
				var record model.RuleLibraryItem
				if json.Unmarshal(data, &record) != nil {
					continue
				}
				decoded = reflect.ValueOf(record)
			}
			typ := decoded.Type()
			for i := 0; i < decoded.NumField(); i++ {
				field := typ.Field(i)
				name := field.Tag.Get("json")
				previous, present := original[name]
				if !field.IsExported() || name == "" || name == "-" || !present {
					continue
				}
				member := decoded.Field(i)
				if member.Kind() == reflect.Pointer {
					if member.IsNil() {
						delete(original, name)
						changed = true
						continue
					}
					member = member.Elem()
				}
				var consumed any
				switch member.Kind() {
				case reflect.String:
					consumed = member.String()
				case reflect.Bool:
					consumed = member.Bool()
				default:
					continue
				}
				before, _ := json.Marshal(previous)
				after, _ := json.Marshal(consumed)
				if string(before) != string(after) {
					original[name] = consumed
					changed = true
				}
			}
		}
	}
	return changed
}
