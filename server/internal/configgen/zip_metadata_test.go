package configgen

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/mosdns"
	"configflow/internal/oxidns"
)

func TestZIPExternalAttributesMatchPython(t *testing.T) {
	type entry struct {
		Name       string `json:"name"`
		Attributes uint32 `json:"attributes"`
		System     uint16 `json:"system"`
		Method     uint16 `json:"method"`
	}
	home, _ := os.UserHomeDir()
	cmd := exec.Command(filepath.Join(home, "cf-test/.venv/bin/python"), "-c", `import io,json,zipfile
buffer=io.BytesIO()
with zipfile.ZipFile(buffer,'w',zipfile.ZIP_DEFLATED) as z:
 for name in ('config.yaml','rules/synthetic.txt'): z.writestr(name,'synthetic: content\n')
with zipfile.ZipFile(buffer) as z:
 print(json.dumps([{'name':f.filename,'attributes':f.external_attr,'system':f.create_system,'method':f.compress_type} for f in z.infolist()]))`)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python ZIP metadata: %v %s", err, raw)
	}
	var want []entry
	if err = json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	builders := []struct {
		name  string
		build func() ([]byte, error)
	}{
		{"mosdns", func() ([]byte, error) {
			return mosdns.BuildMosDNSZip(context.Background(), jsonx.Obj(), "synthetic: content\n", nil, []mosdns.File{{Path: "rules/synthetic.txt", Content: "synthetic: content\n"}}, mosdns.ArchiveOptions{SkipReferenceValidation: true})
		}},
		{"oxidns", func() ([]byte, error) {
			return oxidns.BuildZIP(context.Background(), jsonx.Obj(), "synthetic: content\n", nil, []oxidns.File{{Path: "rules/synthetic.txt", Content: []byte("synthetic: content\n")}}, "")
		}},
	}
	for _, builder := range builders {
		t.Run(builder.name, func(t *testing.T) {
			data, err := builder.build()
			if err != nil {
				t.Fatal(err)
			}

			reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			got := []entry{}
			for _, file := range reader.File {
				got = append(got, entry{file.Name, file.ExternalAttrs, file.CreatorVersion >> 8, file.Method})
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("ZIP metadata differs: got=%+v want=%+v", got, want)
			}
		})
	}

}
