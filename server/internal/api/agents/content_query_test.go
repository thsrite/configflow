package agents

import "testing"

func FuzzContentPageQuery(f *testing.F) {
	f.Add("version", "rules/example.list", "added", "0", "50", "中文")
	f.Add("", "", "", "-1", "２０１", "")
	f.Fuzz(func(t *testing.T, version, path, change, offset, limit, q string) {
		result, err := contentPageQuery(version, path, change, offset, limit, q)
		if err == nil && (result.Offset < 0 || result.Offset > 10000000 || result.Limit < 1 || result.Limit > 200) {
			t.Fatal("accepted invalid page bounds")
		}
	})
}
