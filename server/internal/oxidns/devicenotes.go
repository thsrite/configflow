package oxidns

import (
	"configflow/internal/jsonx"
)

// The read part of backend/common/device_notes.py (module C31), ported early
// because read_policies (C26) shows the stored names. C31 adds the writers
// (set_note, policy_names_revision, prepare_policy_notes, save_policy_rows)
// to this file.

// MaxNotes is MAX_NOTES.
const MaxNotes = 2048

const nameMessage = "设备备注最多 80 个字符，不能包含换行或控制字符。"

// NormalizeName is normalize_name(value).
func NormalizeName(value any) (name string, err error) {
	defer catch(&err)
	return normalizeName(value), nil
}

func normalizeName(value any) string {
	text, ok := value.(string)
	if !ok || runeLen(text) > 80 {
		fail(nameMessage)
	}
	for _, r := range text {
		if r < 32 || r == 127 {
			fail(nameMessage)
		}
	}
	return strip(text)
}

// NormalizeNotes is normalize_notes(value): {canonical address: name}.
func NormalizeNotes(value any) (notes *jsonx.Object, err error) {
	defer catch(&err)
	return normalizeNotes(value), nil
}

func normalizeNotes(value any) *jsonx.Object {
	notes, ok := value.(*jsonx.Object)
	if !ok || notes == nil || notes.Len() > MaxNotes {
		fail("设备备注必须为对象，最多 " + itoa(MaxNotes) + " 条。")
	}
	result := jsonx.NewObject()
	for address, name := range notes.All() {
		ip := normalizeClientRange(address)
		if result.Has(ip) {
			fail("设备备注地址重复：" + ip)
		}
		result.Set(ip, normalizeName(name))
	}
	return result
}

// legacyNames is _legacy_names(config).
func legacyNames(config any) *jsonx.Object {
	rows := normalizePolicies(get(get(config, "oxidns", jsonx.NewObject()), "device_policies", []any{}))
	out := jsonx.NewObject()
	for _, row := range rows {
		if name := row.(*jsonx.Object).GetString("name"); name != "" {
			out.Set(row.(*jsonx.Object).GetString("ip"), name)
		}
	}
	return out
}

// StoredNames is stored_names(config): every address with a known name,
// including empty names that hide a legacy one.
func StoredNames(config any) (names *jsonx.Object, err error) {
	defer catch(&err)
	return storedNames(config), nil
}

func storedNames(config any) *jsonx.Object {
	out := legacyNames(config)
	for ip, name := range normalizeNotes(get(config, "device_notes", jsonx.NewObject())).All() {
		out.Set(ip, name)
	}
	return out
}
