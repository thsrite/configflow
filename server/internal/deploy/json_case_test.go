package deploy

import (
	"encoding/json"
	"testing"
)

func TestAuditManifestFileMetadataExactNames(t *testing.T) {
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, tc := range []struct {
		name, raw string
		reject    bool
	}{
		{"required-case-aliases", `{"PATH":"config.yaml","SIZE":0,"SHA256":"` + digest + `","ROLE":"config"}`, true},
		{"mixed-case-path", `{"path":"config.yaml","PATH":"shadow.yaml","size":0,"sha256":"` + digest + `","role":"config"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got File
			err := json.Unmarshal([]byte(tc.raw), &got)
			if tc.reject {
				if err == nil {
					t.Fatal("case aliases proved required manifest metadata")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Path != "config.yaml" {
				t.Fatalf("file path overwritten: %q", got.Path)
			}
		})
	}
}
