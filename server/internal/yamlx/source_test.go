package yamlx

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSourceWideUnicodeColumnsRemainBounded(t *testing.T) {
	if os.Getenv("CONFIGFLOW_CFG03_SOURCE_CHILD") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSourceWideUnicodeColumnsRemainBounded$")
		command.Env = append(os.Environ(), "CONFIGFLOW_CFG03_SOURCE_CHILD=1")
		start := time.Now()
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("physical source indexing exceeded its bound: %v\n%s", err, output)
		}
		t.Logf("20000 Unicode scalar positions checked in %s", time.Since(start))
		return
	}
	data := []byte("payload: [" + strings.Repeat("'中文',", 19999) + "'中文']\n")
	root, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	source := NewSource(data)
	items := Get(root, "payload").Content
	if len(items) != 20000 {
		t.Fatal("source fixture changed")
	}
	for _, node := range items {
		if !source.IsSingleLineScalar(node) {
			t.Fatal("Unicode source column no longer identifies its token")
		}
	}
}
