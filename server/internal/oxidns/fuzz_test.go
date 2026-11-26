package oxidns

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"
)

// FuzzZoneValidation uses real-Python boundary inputs as the seed corpus; every
// mutated record must return a validation result without panicking or hanging.
func FuzzZoneValidation(f *testing.F) {
	file, err := os.Open(testdataDir + "review_s.jsonl.gz")
	if err != nil {
		f.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		f.Fatal(err)
	}
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		var r row
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			f.Fatal(err)
		}
		if r.Fn != "zone_records_error" {
			continue
		}
		var text string
		var single bool
		if err := json.Unmarshal(r.Args[0], &text); err != nil {
			f.Fatal(err)
		}
		if err := json.Unmarshal(r.Args[1], &single); err != nil {
			f.Fatal(err)
		}
		f.Add(text, single)
	}
	if err := scanner.Err(); err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, text string, single bool) {
		if len(text) > 64<<10 {
			t.Skip()
		}
		zoneRecordsError(S(text), single)
	})
}

func FuzzNativeYAMLRoundTrip(f *testing.F) {
	for _, text := range []string{"plugins: []", "a: !!int \"0x1F\"", "defaults: &d {value: 1}\ncopy: {<<: *d}", "plugins: [] # comment\ntext: |\n  第一行\n  第二行\n"} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 4096 {
			t.Skip()
		}
		doc, err := LoadNativeYAML(text, NativeLabel)
		if err != nil {
			return
		}
		encoded, err := dump(doc)
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) > MaxNativeBytes {
			return
		} // alias expansion can exceed the input limit
		actual, err := LoadNativeYAML(encoded, NativeLabel)
		if err != nil || !same(doc, actual) {
			t.Fatalf("YAML round trip changed values: %v", err)
		}
	})
}
