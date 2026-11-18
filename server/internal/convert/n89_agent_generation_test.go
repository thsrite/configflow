package convert_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"configflow/internal/convert"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"

	"configflow/internal/jsonx"
)

func nativeAgentGenerationCases(t *testing.T, archive string) *jsonx.Object {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_agent_generation_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	fixture := parsed.(*jsonx.Object)
	source, err := os.ReadFile("testdata/" + archive + ".jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(source)
	if hex.EncodeToString(sum[:]) != fixture.GetObject("sources").GetString(archive) {
		t.Fatal("Agent generation source drift")
	}
	manifest, err := os.ReadFile("testdata/n88_generation_bytes.json")
	if err != nil {
		t.Fatal(err)
	}
	sum = sha256.Sum256(manifest)
	if hex.EncodeToString(sum[:]) != fixture.GetString("byte_manifest_sha256") {
		t.Fatal("byte baseline changed")
	}
	reader, err := gzip.NewReader(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 65536), 1<<24)
	cases := fixture.GetObject("cases")
	for index := 0; scanner.Scan(); index++ {
		if entry := cases.GetObject(archive + "/" + strconv.Itoa(index)); entry != nil {
			sum := sha256.Sum256(scanner.Bytes())
			if hex.EncodeToString(sum[:]) != entry.GetString("input_sha256") || entry.GetString("reason") == "" {
				t.Fatal("Agent generation row drift", index)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return cases
}
func applyNativeAgentGeneration(row mihomoRow, expected *jsonx.Object) mihomoRow {
	if expected == nil {
		return row
	}
	out := expected.GetObject("expected")
	entry := row.entry.DeepCopy()
	for _, field := range []string{"out", "err", "report", "mutated", "document"} {
		entry.Delete(field)
		if out.Has(field) {
			entry.Set(field, out.Value(field))
		}
	}
	row.entry = entry
	failure := out.GetList("err")
	row.errClass, row.errMsg = failure[0].(string), failure[1].(string)
	return row
}

func TestN89AgentGenerationReferences(t *testing.T) {
	readers := loadMihomoReaderOracle(t)
	patches := loadInputPatches(t)
	checked := 0
	for _, archive := range []string{"mihomo_corpus", "review_mihomo"} {
		cases := nativeAgentGenerationCases(t, archive)
		rows := loadJSONLines(t, "testdata/"+archive+".jsonl.gz")
		for index, entry := range rows {
			expected := cases.GetObject(fmt.Sprintf("%s/%d", archive, index))
			if expected == nil {
				continue
			}
			row := applyNativeAgentGeneration(readers.row(t, archive+".jsonl.gz", index, rowOf(entry)), expected)
			if archive == "mihomo_corpus" {
				if paths := patches[index]; len(paths) > 0 {
					row = keepOriginalInput(t, row, paths)
				}
			}
			args := row.entry.GetObject("args")
			profile := args.GetObject("profile").DeepCopy()
			report := jsonx.NewObject()
			restore := convert.SetMihomoEnv(corpusEnv(row.entry.GetObject("caches"), row.entry.GetObject("downloads")))
			out, err := convert.GenerateMihomoConfig(context.Background(), profile, convert.GenerateOptions{BaseURL: args.GetString("base_url"), SyncLANHosts: args.GetBool("sync_lan_hosts"), SkipProviderPreflight: true, RegionProbe: args.GetBool("region_probe"), Report: report, MihomoCoreVersion: args.Value("core_version"), ForAgent: args.GetBool("for_agent")})
			restore()
			var invalid *convert.EasyTierSettingsError
			if !errors.As(err, &invalid) || err.Error() != row.errMsg || out != "" || !sameValue(row.entry.Value("report"), report) || !sameValue(row.entry.Value("mutated"), profile) {
				t.Fatalf("Agent pipeline %s/%d mismatch: err=%v report=%s", archive, index, err, encode(report))
			}
			checked++
		}
	}
	if checked != 13 {
		t.Fatalf("only %d Agent generation references consumed", checked)
	}
}
