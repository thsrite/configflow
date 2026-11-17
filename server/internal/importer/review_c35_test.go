package importer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"syscall"
	"testing"

	"configflow/internal/importer"
)

func adversarialImport(name string) (string, string) {
	switch name {
	case "deep":
		return "mihomo", "proxies: [{name: N, type: http}]\ncustom: " + strings.Repeat("[", 20000) + "0" + strings.Repeat("]", 20000)
	case "recursive":
		return "mihomo", "proxies: [{name: N, type: http}]\ncustom: &loop [*loop]\n"
	case "wide":
		var content strings.Builder
		content.WriteString("proxies: [{name: N, type: http}]\n")
		for i := 0; i < 20000; i++ {
			fmt.Fprintf(&content, "custom-%d: %d\n", i, i)
		}
		return "mihomo", content.String()
	case "warnings":
		var content strings.Builder
		content.WriteString("[Proxy]\n")
		for i := 0; i < 40000; i++ {
			fmt.Fprintf(&content, "Bad%d=unsupported,example.com,80\n", i)
		}
		content.WriteString("[Rule]\nFINAL,DIRECT\n")
		return "surge", content.String()
	case "large":
		return "surge", "[Proxy]\nN=http,example.com,80\n[General]\n#" + strings.Repeat("x", 32<<20) + "\n"
	case "merge expansion":
		var content strings.Builder
		content.WriteString("proxies: [{name: N, type: http}]\na0: &a0 {key: value}\n")
		for i := 1; i < 22; i++ {
			fmt.Fprintf(&content, "a%d: &a%d {<<: [*a%d, *a%d]}\n", i, i, i-1, i-1)
		}
		return "mihomo", content.String()
	}
	return "mihomo", "invalid: ["
}
func TestAdversarialImporterResourceBounds(t *testing.T) {
	if mode := os.Getenv("C35_IMPORT_CHILD"); mode != "" {
		debug.SetMemoryLimit(96 << 20)
		debug.SetMaxStack(32 << 20)
		client, content := adversarialImport(mode)
		plan, err := importer.Parse(client, content)
		if err == nil {
			_, err = importer.Build(plan, importer.Shared{}, importer.BuildOptions{})
		}
		var usage syscall.Rusage
		_ = syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
		result, _ := json.Marshal(struct {
			Rejected bool
			PeakKiB  int64
		}{err != nil, usage.Maxrss})
		fmt.Println(string(result))
		return
	}
	for _, name := range []string{"deep", "recursive", "wide", "warnings", "large", "merge expansion"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), importProbeTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAdversarialImporterResourceBounds$")
			cmd.Env = append(os.Environ(), "C35_IMPORT_CHILD="+name)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("import process failed (%v), timed_out=%t: %.300s", err, ctx.Err() != nil, out)
			}
			line, _, _ := bytes.Cut(out, []byte("\n"))
			var result struct {
				Rejected bool
				PeakKiB  int64
			}
			if err := json.Unmarshal(line, &result); err != nil {
				t.Fatalf("invalid child report: %.300s", out)
			}
			if result.PeakKiB > importProbeRSSKiB {
				t.Errorf("import used %d KiB RSS", result.PeakKiB)
			}
			t.Logf("rejected=%t peak_RSS_KiB=%d", result.Rejected, result.PeakKiB)
		})
	}
}
