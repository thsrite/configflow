package repo

import (
	"configflow/internal/convert"
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/devicenotes"
	"configflow/internal/model"
	"configflow/internal/routerdoc"
)

// Input that does not fit a plain record member (model.MemberTypeError) is
// rejected where it is decoded (O18 T1a, coordinator ruling Q2-A): records no
// longer keep wrong-typed values for validateDocument to find. The message is
// the one validateDocument gives for that member's shape, so the status and
// the Chinese text stay those of Python; a member Python did not validate gets
// msgMemberType.

// inputError turns a member type mismatch or a missing document section into
// a validation error; other errors are returned unchanged.
func inputError(err error) error {
	var typed *model.MemberTypeError
	if errors.As(err, &typed) {
		return &Error{Kind: ErrValidation, Message: memberTypeMessage(typed), Cause: err}
	}
	var missing *model.MissingSectionError
	if errors.As(err, &missing) {
		return &Error{Kind: ErrValidation, Message: msgSectionMissing + missing.Section, Cause: err}
	}
	return err
}

// InputError is inputError for input decoded outside the repository (API
// adapters decoding a request into a record): a member type mismatch becomes
// a validation error with Python's message.
func InputError(err error) error { return inputError(err) }

// shapeMessage returns the validation message of err, or "" if err is nil.
func shapeMessage(err error) string {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Message
	}
	return ""
}

func memberTypeMessage(e *model.MemberTypeError) string {
	raw := e.Value
	message := ""
	switch e.Record {
	case "profile":
		switch e.Member {
		case "mihomo", "surge", "mosdns", "loon", "domain_discovery", "oxidns":
			if jsonKind(raw) != '{' {
				message = fmt.Sprintf(msgMemberObject, e.Member)
			} else if e.Member == "mosdns" {
				for _, key := range []string{"direct_rulesets", "proxy_rulesets", "direct_rules", "proxy_rules"} {
					if _, err := memberIDs(viewOfJSON(raw), key, key); err != nil {
						message = shapeMessage(err)
						break
					}
				}
			}
		case "routers":
			if items, present := viewOfJSON(raw).get("items"); jsonKind(raw) != '{' || present && jsonKind(items) != '[' {
				message = msgRoutersShape
			}
		case "proxy_groups":
			if raw == nil {
				_, err := itemsOf(raw, labelProxyGroups)
				message = shapeMessage(err)
			} else {
				message = groupShapeMessage(raw)
			}
		case "rule_configs":
			message = ruleShapeMessage(raw)
		case "device_notes":
			if _, err := devicenotes.Parse(raw); err != nil {
				message = err.Error()
			}
		case "client_fixed_ips":
			message = routerdoc.ValidateBindings(raw)
		case "name":
			if !nonBlank(raw) {
				message = msgProfileNameEmpty
			}
		case "id":
			message = msgProfileIDMismatch
		}
	case "shared":
		for _, field := range SharedFields {
			if e.Member == field {
				message = sharedShapeMessage(field, raw)
			}
		}
	case "system":
		switch e.Member {
		case "system_config", "backup":
			if jsonKind(raw) != '{' {
				message = msgSystemSectionsObject
			} else if kind := viewOfJSON(raw).kind("default_profile_id"); e.Member == "system_config" && kind != 0 && kind != 'n' && kind != '"' {
				message = msgDefaultProfileIDText
			}
		case "agents":
			_, err := itemsOf(raw, "Agent")
			message = shapeMessage(err)
		}
	case "document":
		switch e.Member {
		case "system", "shared", "profiles":
			message = msgSectionMissing + e.Member
		case "profile":
			message = msgProfileIDMismatch
		case "schema_version":
			message = msgSchemaMismatch
		}
	}
	if e.Member == "_revision" && e.Record != "document" {
		message = msgRevisionInvalid
	}
	if message == "" {
		message = fmt.Sprintf(msgMemberType, e.Member)
	}
	return message
}

// requiredInputMembers are the section members Python's document validation
// refused to miss. Typed records cannot tell a missing member from its zero
// value, so whole-document input (imports) is checked before decoding.
var requiredInputMembers = map[string][]string{
	"system":  {"system_config", "backup", "agents", "_revision"},
	"shared":  append(append([]string{}, SharedFields...), "_revision"),
	"profile": {"name", "proxy_groups", "rule_configs", "mihomo", "surge", "mosdns", "_revision"},
}

// checkDocumentInput refuses whole-document input that misses a required
// section member or has a system member the system section does not accept,
// with the message Python's validation gave.
func checkDocumentInput(raw json.RawMessage) error {
	root := viewOfJSON(raw)
	check := func(record string, section memberView) error {
		for _, member := range requiredInputMembers[record] {
			if !section.has(member) {
				return inputError(&model.MemberTypeError{Record: record, Member: member, Err: model.ErrType})
			}
		}
		return nil
	}
	if raw, present := root.get("system"); present && jsonKind(raw) == '{' {
		system := viewOfJSON(raw)
		if err := check("system", system); err != nil {
			return err
		}
		for _, key := range system.fields.Keys() {
			if !systemFields[key] {
				return validationError(msgSystemMembers)
			}
		}
		// Agent records decode leniently; their identities are checked on
		// the input as Python's validation did (an ID must be text).
		if agents, present := system.get("agents"); present {
			if _, err := itemsOf(agents, "Agent"); err != nil {
				return err
			}
		}
	}
	if raw, present := root.get("shared"); present && jsonKind(raw) == '{' {
		if err := check("shared", viewOfJSON(raw)); err != nil {
			return err
		}
	}
	if raw, present := root.get("profiles"); present && jsonKind(raw) == '{' {
		for _, profile := range viewOfJSON(raw).fields.All() {
			if jsonKind(profile) != '{' {
				continue
			}
			view := viewOfJSON(profile)
			for _, key := range SharedFields {
				if view.has(key) {
					return validationError(msgProfileSharedCopies)
				}
			}
			if view.has("resource_refs") || view.has("node_dialers") {
				return validationError(msgProfileImportOverrides)
			}
			if err := check("profile", view); err != nil {
				return err
			}
		}
	}
	return nil
}

// Nested records are plain records since O18 T1c: input whose nested member
// has the wrong JSON type fails its strict decode instead of reaching
// validateDocument. The shape checks below are the parts of validateDocument
// that look at one member's JSON, run on the refused input in Python's order,
// so the message stays Python's; "" falls back to msgMemberType.

// groupShapeMessage checks refused proxy_groups input.
func groupShapeMessage(raw json.RawMessage) string {
	if message := shapeMessage(validateResourceArray(raw, "proxy_groups")); message != "" {
		return message
	}
	// dialer_references.validate_shapes runs first (checkProfileShapes).
	var list []json.RawMessage
	_ = json.Unmarshal(raw, &list)
	for _, item := range list {
		var members map[string]json.RawMessage
		_ = json.Unmarshal(item, &members)
		if id, present := members["id"]; present && !nonBlank(id) {
			return msgResourceID
		}
		if !nonBlank(members["name"]) {
			return msgResourceName
		}
		if err := convert.GroupMemberShapes(members); err != nil {
			return err.Error()
		}
	}
	if message := shapeMessage(checkGroupReferenceShapes(raw)); message != "" {
		return message
	}
	groups, err := itemsOf(raw, labelProxyGroups)
	if err != nil {
		return shapeMessage(err)
	}
	for _, id := range groups.ids {
		if name, ok := jsonString(mustGet(groups.byID[id], "name")); !ok || name == "" {
			return msgGroupNameEmpty
		}
	}
	for _, id := range groups.ids {
		group := groups.byID[id]
		groupJSON, err := group.fields.MarshalJSON()
		if err != nil {
			return ""
		}
		if message := shapeMessage(checkHealth(groupJSON, false)); message != "" {
			return message
		}
		label := "策略组“" + displayJSON(mustGet(group, "name")) + "”"
		for _, pair := range groupMemberFields {
			if _, err := memberIDs(group, pair[0], label+pair[0]); err != nil {
				return shapeMessage(err)
			}
		}
		if include, ok := group.get("include_groups"); ok && jsonKind(include) != '[' {
			return label + " 引用了无效的策略组：" + displayJSON(include)
		}
		if order, ok := group.get("proxies_order"); ok {
			if jsonKind(order) != '[' {
				return msgGroupOrderArray
			}
			var entries []json.RawMessage
			_ = json.Unmarshal(order, &entries)
			for _, entry := range entries {
				if jsonKind(entry) != '{' {
					return msgGroupOrderEntry
				}
			}
		}
	}
	return ""
}

// ruleShapeMessage checks refused rule_configs input.
func ruleShapeMessage(raw json.RawMessage) string {
	rules, err := itemsOf(raw, labelRuleConfigs)
	if err != nil {
		return shapeMessage(err)
	}
	for _, id := range rules.ids {
		if itemType, _ := jsonString(mustGet(rules.byID[id], "itemType")); itemType != "rule" && itemType != "ruleset" {
			return msgRuleItemType
		}
	}
	return ""
}

// sharedShapeMessage checks refused input for a shared resource list.
func sharedShapeMessage(field string, raw json.RawMessage) string {
	items, err := itemsOf(raw, field)
	if err != nil {
		return shapeMessage(err)
	}
	for _, id := range items.ids {
		item := items.byID[id]
		if field == kindSubscriptions {
			if interval, ok := item.get("interval"); ok && !refreshIntervalValid(interval) {
				return msgRefreshInterval
			}
		}
		if field == kindAggregations {
			// Python stored any name; the plain record takes text only.
			if name, ok := item.get("name"); ok && jsonKind(name) != '"' && jsonKind(name) != 'n' {
				return msgResourceName
			}
		}
		if field == kindSubscriptions || field == kindAggregations {
			if health, ok := item.get("health_check"); ok {
				if message := shapeMessage(checkHealth(health, true)); message != "" {
					return message
				}
			}
		}
		if field == kindNodes {
			// dialer_references.validate_shapes, then the node rules.
			if name, ok := jsonString(mustGet(item, "name")); !ok || trimText(name) == "" {
				return msgResourceName
			}
			if subscription, ok := item.get("subscription_id"); ok && jsonTruthy(subscription) && jsonKind(subscription) != '"' {
				return msgNodeSubscriptionMissing
			}
			if params, ok := item.get("params"); ok && jsonKind(params) != '{' {
				return msgNodeParamsObject
			}
			if text, ok := item.get("proxy_string"); ok && jsonKind(text) != '"' && jsonKind(text) != 'n' {
				return msgNodeProxyString
			}
			if item.has("dialer_ref") {
				return msgNodeDialerRef
			}
		}
		if field == kindRuleLibrary && (item.has("policy") || item.has("target")) {
			return msgLibraryPolicy
		}
	}
	names := map[string]bool{}
	for _, id := range items.ids {
		item := items.byID[id]
		switch field {
		case kindRuleLibrary:
			name, ok := jsonString(mustGet(item, "name"))
			if !ok || name == "" || names[name] {
				return msgLibraryNames
			}
			names[name] = true
		case kindAggregations:
			label := "聚合 " + displayJSON(mustGet(item, "name"))
			for _, member := range []string{kindSubscriptions, kindNodes} {
				if _, err := memberIDs(item, member, label+" 的 "+member); err != nil {
					return shapeMessage(err)
				}
			}
		}
	}
	return ""
}
