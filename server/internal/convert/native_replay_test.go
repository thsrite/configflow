package convert

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

type nativeProxyReplayCase struct{ ID, Category, Native string }

func nativeProxyReplay(t *testing.T, suite string, row corpusRow, result, mutated any, err error) bool {
	t.Helper()
	source := ""
	if row.fn == "parse_structured_proxy_string" && len(row.args) > 0 {
		source, _ = row.args[0].(string)
	}
	var find func(any)
	find = func(v any) {
		switch v := v.(type) {
		case *JObj:
			if text, ok := v.Value("proxy_string").(string); ok {
				source = text
			}
			for _, child := range v.All() {
				find(child)
			}
		case []any:
			for _, child := range v {
				find(child)
			}
		}
	}
	find(row.args)
	if source == "" {
		return false
	}
	conv := pyConv
	if suite == "review" {
		conv = reviewConv
	}
	oldEqual := err == nil && row.errClass == "" && compactText(t, conv(result)) == compactText(t, row.out)
	if row.errClass != "" && err != nil {
		oldEqual = goClass(err) == row.errClass && (!chineseText(row.errMsg) || err.Error() == row.errMsg)
	}
	if row.hasMut && compactText(t, conv(mutated)) != compactText(t, row.mutated) {
		oldEqual = false
	}
	if suite == "review" && err == nil && compactText(t, conv(result)) == compactText(t, stripKeyMarks(row.out)) {
		oldEqual = true
	}
	if oldEqual {
		return false
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(suite+"/"+row.fn+"/"+compactText(t, row.args))))
	message, class := "", ""
	if err != nil {
		message = err.Error()
		class = goClass(err)
	}
	native := compactText(t, []any{conv(result), conv(mutated), class, message})
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(native)))
	data, e := os.ReadFile("testdata/n73_native_differences.json")
	if e != nil {
		return false
	}
	var cases []nativeProxyReplayCase
	if e := json.Unmarshal(data, &cases); e != nil {
		t.Fatal(e)
	}
	for _, c := range cases {
		if c.ID == id {
			if c.Native != digest {
				t.Errorf("native proxy replay changed: %s/%s %s args=%s native=%s", suite, row.fn, id, compactText(t, row.args), native)
				return false
			}
			return true
		}
	}
	return false
}
