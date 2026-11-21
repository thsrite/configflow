package repo

import (
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
			message = shapeMessage(validateResourceArray(raw, "proxy_groups"))
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
		case "schema_version":
			message = msgSchemaMismatch
		}
	}
	if message == "" {
		message = fmt.Sprintf(msgMemberType, e.Member)
	}
	return message
}
