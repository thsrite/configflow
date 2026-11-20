package convert_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/model"
)

type generationByteCase struct {
	Source string `json:"source"`
	Row    int    `json:"row"`
	SHA256 string `json:"sha256"`
}

type generationByteFixture struct {
	Baseline string               `json:"baseline"`
	Sources  map[string]string    `json:"sources"`
	Cases    []generationByteCase `json:"cases"`
}

func generationDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func generateByteCase(t *testing.T, entry *convert.JObj) (string, error) {
	t.Helper()
	args := entry.GetObject("args")
	profile := args.GetObject("profile").DeepCopy()
	switch entry.GetString("fn") {
	case "generate":
		cfg, err := convert.DecodeConfigJ(profile)
		if err != nil {
			return "", err
		}
		version, err := coreVersionRaw(args.Value("core_version"))
		if err != nil {
			return "", err
		}
		restore := convert.SetMihomoEnvironment(corpusEnv(entry.GetObject("caches"), entry.GetObject("downloads")))
		defer restore()
		return convert.MihomoConfig(context.Background(), cfg, convert.MihomoOptions{
			BaseURL: args.GetString("base_url"), SyncLANHosts: args.GetBool("sync_lan_hosts"), SkipProviderPreflight: true,
			RegionProbe: args.GetBool("region_probe"), Report: &convert.GenerationReport{}, CoreVersion: version, ForAgent: args.GetBool("for_agent"),
		})
	case "surge":
		cfg, err := decodeClientConfig(profile)
		if err != nil {
			return "", err
		}
		return convert.SurgeConfig(context.Background(), cfg, args.GetString("base_url"))
	case "loon":
		cfg, err := decodeClientConfig(profile)
		if err != nil {
			return "", err
		}
		return convert.LoonConfig(context.Background(), cfg, args.GetString("base_url"))
	}
	return "", fmt.Errorf("not a generated config")
}

// Recording is performed only on the unchanged pre-migration baseline named
// in the fixture. Every subsequent tree reads those hashes, never regenerates
// expectations. Python goldens and the existing semantic oracle remain intact.
func TestN88GenerationBytes(t *testing.T) {
	const path = "testdata/n88_generation_bytes.json"
	fixture := generationByteFixture{Baseline: "2277ce047d4abb78ffed436442c19b31152ba85d", Sources: map[string]string{}}
	rows := map[string][]*convert.JObj{}
	for _, name := range []string{"mihomo_corpus.jsonl.gz", "surge_loon_corpus.jsonl.gz"} {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		fixture.Sources[name] = generationDigest(data)
		rows[name] = loadJSONLines(t, "testdata/"+name)
	}
	restore := convert.SetMihomoEnvironment(corpusEnv(nil, nil))
	defer restore()
	if os.Getenv("CONFIGFLOW_RECORD_N88") == "1" {
		for _, name := range []string{"mihomo_corpus.jsonl.gz", "surge_loon_corpus.jsonl.gz"} {
			for index, entry := range rows[name] {
				fn := entry.GetString("fn")
				if fn != "generate" && fn != "surge" && fn != "loon" || entry.Has("err") {
					continue
				}
				profile := entry.GetObject("args").GetObject("profile")
				if convert.CommonReplayIssue(profile) != "" {
					continue
				}
				// Include only well-typed stored records, including nested records.
				data, err := convert.CompactJ(convert.JObjKV("profiles", convert.JObjKV("p", profile), "shared", profile, "system", profile))
				if err != nil {
					continue
				}
				_, issues, err := model.DecodeDocument(data)
				if err != nil || len(issues) != 0 {
					continue
				}
				out, err := generateByteCase(t, entry)
				if err != nil {
					continue
				}
				fixture.Cases = append(fixture.Cases, generationByteCase{name, index, generationDigest([]byte(out))})
			}
		}
		if len(fixture.Cases) < 10 {
			t.Fatalf("only %d normal generation cases", len(fixture.Cases))
		}
		data, err := json.MarshalIndent(fixture, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("recorded %d original Go byte outputs", len(fixture.Cases))
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var expected generationByteFixture
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	if expected.Baseline != fixture.Baseline || len(expected.Cases) < 10 {
		t.Fatal("invalid baseline manifest")
	}
	for source, digest := range fixture.Sources {
		if expected.Sources[source] != digest {
			t.Fatalf("source archive changed: %s", source)
		}
	}
	if err := validateGenerationByteCoverage(expected, rows); err != nil {
		t.Fatal(err)
	}
	providerPaths := providerPathReferences(t)
	numericSpellings := numericSpellingReferences(t)
	usedNumeric := map[int]bool{}
	nativeVersions := nativeAgentGenerationCases(t, "mihomo_corpus")
	usedVersions := map[int]bool{}
	clientExceptions := readClientByteExceptions(t)
	usedExceptions := map[int]bool{}
	counts := map[string]int{}
	for _, sample := range expected.Cases {
		entries, ok := rows[sample.Source]
		if !ok || sample.Row < 0 || sample.Row >= len(entries) {
			t.Fatal("invalid source row")
		}
		entry := entries[sample.Row]
		counts[entry.GetString("fn")]++
		out, err := generateByteCase(t, entry)
		if sample.Source == "mihomo_corpus.jsonl.gz" {
			if exception := nativeVersions.GetObject(fmt.Sprintf("mihomo_corpus/%d", sample.Row)); exception != nil {
				expected := exception.GetObject("expected").GetList("err")
				var invalid *convert.EasyTierSettingsError
				if exception.GetString("old_byte_sha256") != sample.SHA256 || !errors.As(err, &invalid) || err.Error() != expected[1] || out != "" {
					t.Fatalf("invalid machine-version byte exception changed: row %d err=%v", sample.Row, err)
				}
				usedVersions[sample.Row] = true
				continue
			}
		}
		if err != nil {
			t.Errorf("%s/%d: %v", sample.Source, sample.Row, err)
			continue
		}
		if entry.GetString("fn") != "generate" {
			typed, err := typedClientByteCase(t, entry)
			if err != nil || typed != out {
				t.Errorf("%s/%d: typed/client codec bytes differ: %v", sample.Source, sample.Row, err)
			}
		}
		expectedSHA := sample.SHA256
		if sample.Source == "surge_loon_corpus.jsonl.gz" {
			if exception, ok := clientExceptions[sample.Row]; ok {
				want := explicitClientBytes(t, entry, exception)
				if exception.OldSHA != sample.SHA256 {
					t.Fatal("client exception baseline moved")
				}
				expectedSHA = generationDigest([]byte(want))
				usedExceptions[sample.Row] = true
			}
		}
		compared := out
		if sample.Source == "mihomo_corpus.jsonl.gz" {
			compared = providerByteReference(t, providerPaths, sample.Row, out)
			if ref, ok := numericSpellings[fmt.Sprint(sample.Row)]; ok {
				compared = numericByteReference(t, compared, ref, sample.SHA256)
				usedNumeric[sample.Row] = true
			}
		}
		if generationDigest([]byte(compared)) != expectedSHA {
			t.Errorf("%s/%d: generated bytes changed", sample.Source, sample.Row)
		}
	}
	if len(usedNumeric) != 20 {
		t.Fatal("numeric spelling references not fully consumed")
	}
	if len(usedVersions) != 9 {
		t.Fatalf("invalid Agent version exceptions: %d, want 9", len(usedVersions))
	}
	if len(usedExceptions) != len(clientExceptions) {
		t.Fatalf("unused client byte exceptions: %d/%d", len(usedExceptions), len(clientExceptions))
	}
	t.Logf("original Go byte comparisons: %v; explicit client exceptions: %d; explicit invalid Agent versions: %d; unknown: 0", counts, len(usedExceptions), len(usedVersions))
}

// The frozen baseline covers all three engines. A shortened or duplicated
// manifest must fail before it can silently turn a missing engine into a pass.
func validateGenerationByteCoverage(fixture generationByteFixture, rows map[string][]*convert.JObj) error {
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, sample := range fixture.Cases {
		entries, ok := rows[sample.Source]
		if !ok || sample.Row < 0 || sample.Row >= len(entries) {
			return fmt.Errorf("invalid source row: %s/%d", sample.Source, sample.Row)
		}
		key := fmt.Sprintf("%s/%d", sample.Source, sample.Row)
		digest, err := hex.DecodeString(sample.SHA256)
		if seen[key] || err != nil || len(digest) != sha256.Size {
			return fmt.Errorf("invalid or duplicate byte baseline entry: %s", key)
		}
		seen[key] = true
		counts[entries[sample.Row].GetString("fn")]++
	}
	want := map[string]int{"generate": 2353, "loon": 1064, "surge": 1131}
	if len(counts) != len(want) {
		return fmt.Errorf("byte baseline engine coverage changed: %v", counts)
	}
	for name, count := range want {
		if counts[name] != count {
			return fmt.Errorf("byte baseline %s coverage=%d, want %d", name, counts[name], count)
		}
	}
	return nil
}
