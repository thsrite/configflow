package ikuai

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"testing"
)

// The group size bound must never be below what a driver sends with
// encoding/json (HTML escaping off): iKuai refuses larger bodies.
func TestGroupBytesBoundsTheRequest(t *testing.T) {
	notes := map[string]string{}
	var values []string
	for i, note := range []string{"公司", "a<b>&c", "x\"y\\z", "\x01ctl", "emoji \U0001F600", "line\nbreak", " pad ", "　wide", "%;$|`'"} {
		value := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, 0, byte(i), 0}), 24).String()
		values = append(values, value, "1.2.3."+string(rune('1'+i)))
		notes[value] = note
	}
	id := int64(9999999999)
	form := GroupPayload("cf_nat_12", "nat", values, &id, notes)
	form.GroupName = ikuaiEncode(form.GroupName, false)
	for i := range form.GroupValue {
		form.GroupValue[i] = GroupEntry{IP: ikuaiEncode(form.GroupValue[i].IP, false), Comment: ikuaiEncode(form.GroupValue[i].Comment, true)}
	}
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]any{"func_name": "route_object", "action": "edit", "param": form}); err != nil {
		t.Fatal(err)
	}
	if got, bound := body.Len()-1, GroupBytes("cf_nat_12", "nat", values, notes); got > bound {
		t.Fatalf("request %d bytes, bound %d", got, bound)
	}
}

func TestIkuaiEncode(t *testing.T) {
	for _, c := range []struct {
		in      string
		comment bool
		want    string
	}{
		{"1.2.3.4/32", false, "1.2.3.4/32"},
		{" a b ", false, "%20a%20b%20"},
		{" a\r\nb\nc\rd ", true, "a%0Ab%0Ac%0Ad"},
		{"a\nb", false, "a%20b"},
		{"50%\"`$&;\\|'", false, "50%25%22%60%24%26%3B%5C%7C%27"},
		{"中　文", true, "中%20文"},
	} {
		if got := ikuaiEncode(c.in, c.comment); got != c.want {
			t.Errorf("ikuaiEncode(%q, %v) = %q, want %q", c.in, c.comment, got, c.want)
		}
	}
}
