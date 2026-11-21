package repo

import (
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
			message = e.Member + " 参数必须是对象"
		case "routers":
			message = "routers 参数必须是包含 items 列表的对象"
		case "proxy_groups":
			if raw == nil {
				_, err := itemsOf(raw, "策略组")
				message = shapeMessage(err)
			} else {
				message = shapeMessage(validateResourceArray(raw, "proxy_groups"))
			}
		case "rule_configs":
			_, err := itemsOf(raw, "规则配置")
			message = shapeMessage(err)
		case "device_notes":
			if _, err := devicenotes.Parse(raw); err != nil {
				message = err.Error()
			}
		case "client_fixed_ips":
			message = routerdoc.ValidateBindings(raw)
		case "name":
			if !nonBlank(raw) {
				message = "配置名称不能为空"
			}
		case "id":
			message = msgProfileIDMismatch
		}
	case "shared":
		for _, field := range SharedFields {
			if e.Member == field {
				_, err := itemsOf(raw, field)
				message = shapeMessage(err)
			}
		}
	case "system":
		switch e.Member {
		case "system_config", "backup":
			message = "系统设置和备份设置必须是对象"
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
				return validationError("system 节只接受全局设置、备份与 Agent")
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
					return validationError("独立配置不能保存共享资源副本")
				}
			}
			if view.has("resource_refs") || view.has("node_dialers") {
				return validationError("独立配置不接受资源白名单或节点拨号覆盖字段")
			}
			if err := check("profile", view); err != nil {
				return err
			}
		}
	}
	return nil
}
