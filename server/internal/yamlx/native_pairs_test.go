package yamlx

import (
	"errors"
	"testing"
)

func TestAllPairsRetainsKeysAndMergeOrder(t *testing.T) {
	source := "base: &base {1: first, text: old, false: flag}\nmap: &map {<<: *base, text: new, last: final}\nuse: *map\n"
	doc, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	var keys, values []string
	for key, value := range AllPairs(Get(doc, "use")) {
		keys = append(keys, Resolve(key).ShortTag()+":"+Resolve(key).Value)
		values = append(values, Resolve(value).Value)
	}
	wantKeys := []string{"!!int:1", "!!str:text", "!!bool:false", "!!str:last"}
	wantValues := []string{"first", "new", "flag", "final"}
	if len(keys) != len(wantKeys) {
		t.Fatal(keys)
	}
	for i := range keys {
		if keys[i] != wantKeys[i] || values[i] != wantValues[i] {
			t.Fatalf("%v %v", keys, values)
		}
	}
	count := 0
	for range Pairs(Get(doc, "use")) {
		count++
	}
	if count != 2 {
		t.Fatal(count)
	}
	count = 0
	for range AllPairs(Get(doc, "use")) {
		count++
		break
	}
	if count != 1 {
		t.Fatal(count)
	}
	if _, err := Parse([]byte("1: a\n1: b")); !errors.Is(err, ErrDuplicateKey) {
		t.Fatal(err)
	}
}
