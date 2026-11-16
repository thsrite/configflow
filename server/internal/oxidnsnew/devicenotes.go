package oxidns

import "fmt"

// Display names of clients (device_notes); policy rows of older versions
// still carry names, which notes override (an empty note hides one).

// MaxNotes bounds device_notes.
const MaxNotes = 2048

// NormalizeName validates one note.
func NormalizeName(value *Node) (string, error) {
	if !isStr(value) || len([]rune(value.Value)) > 80 || hasControl(value.Value) || containsRune(value.Value, 0x7f) {
		return "", fail("设备备注最多 80 个字符，不能包含换行或控制字符。")
	}
	return trimSpace(value.Value), nil
}

// NormalizeNotes validates device_notes (address → name), in order.
func NormalizeNotes(value *Node) (*Node, error) {
	if !isMap(value) || len(value.Content)/2 > MaxNotes {
		return nil, fail(fmt.Sprintf("设备备注必须为对象，最多 %d 条。", MaxNotes))
	}
	result := M()
	var err error
	pairs(value, func(address string, name *Node) bool {
		var ip, text string
		if ip, err = NormalizeClientRange(S(address)); err != nil {
			return false
		}
		if has(result, ip) {
			err = fail("设备备注地址重复：" + ip)
			return false
		}
		if text, err = NormalizeName(name); err != nil {
			return false
		}
		set(result, ip, S(text))
		return true
	})
	return result, err
}

// StoredNames is every address with a known name (empty names included).
func StoredNames(p *Profile) (map[string]string, error) {
	policies := get(p.Settings(), "device_policies")
	if !has(p.Settings(), "device_policies") {
		policies = L()
	}
	rows, err := NormalizePolicies(policies)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, row := range rows.Content {
		if name := str(get(row, "name")); name != "" {
			names[str(get(row, "ip"))] = name
		}
	}
	stored := p.get("device_notes")
	if !has(p.doc, "device_notes") {
		stored = M()
	}
	notes, err := NormalizeNotes(stored)
	if err != nil {
		return nil, err
	}
	pairs(notes, func(ip string, name *Node) bool { names[ip] = name.Value; return true })
	return names, nil
}
