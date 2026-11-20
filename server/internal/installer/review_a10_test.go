package installer

// A10 adversarial review: the zlib raw deflate and zipfile ports compared
// live with CPython 3.11 (the cf-test virtual environment on the test
// server) on inputs the porter's 142-input corpus does not reach: sizes up
// to 1.2 MB (window slides, blocks whose start slid out of the window),
// period/distance edges, TOO_FAR length-3 matches, compressible prefixes
// followed by incompressible data, and Compose-bundle-like text with long
// data directories. A10_DEFLATE_FUZZ=N raises the number of random inputs;
// A10_ZLIB_LIBS (see testdata/review_a10_deflate.py) also checks other
// zlib builds.

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/testgolden"
)

// reviewDeflateInputs returns deterministic adversarial deflate inputs.
func reviewDeflateInputs(random int) [][]byte {
	rng := rand.New(rand.NewSource(20261116))
	bytesOf := func(n int, alphabet []byte) []byte {
		out := make([]byte, n)
		for i := range out {
			if alphabet == nil {
				out[i] = byte(rng.Intn(256))
			} else {
				out[i] = alphabet[rng.Intn(len(alphabet))]
			}
		}
		return out
	}
	periodic := func(n, period int) []byte {
		unit := bytesOf(period, nil)
		out := make([]byte, n)
		for i := range out {
			out[i] = unit[i%period]
		}
		return out
	}
	var inputs [][]byte
	add := func(b []byte) { inputs = append(inputs, b) }
	add(nil)
	for _, n := range []int{1, 2, 3, 4, 257, 258, 259, 260, 261, 262, 263, 16383, 16384, 16385, 32506, 32507, 32768, 32769, 65274, 65275, 65536, 65537, 98304, 131072, 262144} {
		add(bytes.Repeat([]byte{'x'}, n))
		add(bytesOf(n, nil))
		add(bytesOf(n, []byte("ab")))
	}
	add(bytes.Repeat([]byte{0}, 1<<20+12345)) // long runs: blocks span window slides
	add(bytesOf(1<<20, nil))                  // incompressible: stored blocks
	for _, period := range []int{1, 2, 3, 7, 255, 256, 257, 258, 259, 4095, 4096, 4097, 32505, 32506, 32507, 32767, 32768, 32769, 40000} {
		add(periodic(200000, period))
	}
	// Length-3 matches nearer and farther than TOO_FAR (4096).
	for _, gap := range []int{4093, 4094, 4095, 4096, 4097, 4098} {
		var b []byte
		for i := 0; i < 60; i++ {
			b = append(b, 'Q', 'Z', 'J')
			b = append(b, bytesOf(gap, []byte("0123456789"))...)
		}
		add(b)
	}
	// Compressible prefix (block start slides out) followed by random data.
	for _, prefix := range []int{70000, 300000, 1000000} {
		add(append(bytes.Repeat([]byte("cf_state="), prefix/9), bytesOf(40000, nil)...))
		add(append(bytesOf(40000, nil), bytes.Repeat([]byte{'A'}, prefix)...))
	}
	// Bundle-like text: prepare.sh blocks with a long data directory.
	for _, size := range []int{10, 300, 2000, 5000} {
		dir := "/srv/" + strings.Repeat("数据$`'\"", size/8+1)[:size]
		var b strings.Builder
		for i := 0; i < 40; i++ {
			b.WriteString(composePrepareBlocks[i%5])
			b.WriteString("cf_data=" + shlexQuote(dir) + "\n")
		}
		b.WriteString(composeBootstrap)
		add([]byte(b.String()))
	}
	// Seeded random compositions of literals, copies and runs.
	for i := 0; i < random; i++ {
		var b []byte
		limit := []int{300, 5000, 70000, 400000}[rng.Intn(4)]
		for len(b) < limit {
			switch r := rng.Float64(); {
			case r < 0.25:
				b = append(b, bytesOf(1+rng.Intn(300), nil)...)
			case r < 0.5 && len(b) > 0:
				start := rng.Intn(len(b))
				end := start + 3 + rng.Intn(600)
				if rng.Intn(4) == 0 && len(b) > 40000 { // distant copy near the window edge
					start = len(b) - 32768 + rng.Intn(400) - 200
					end = start + 3 + rng.Intn(300)
				}
				if start < 0 {
					start = 0
				}
				if end > len(b) {
					end = len(b)
				}
				b = append(b, b[start:end]...)
			case r < 0.65:
				b = append(b, bytes.Repeat([]byte{byte(rng.Intn(256))}, 1+rng.Intn(2000))...)
			case r < 0.8:
				b = append(b, bytesOf(1+rng.Intn(800), []byte("abcdefgh"))...)
			default:
				b = append(b, []byte(strings.Repeat("持久目录 ", 1+rng.Intn(20)))...)
			}
		}
		if rng.Intn(5) == 0 {
			b = bytes.Repeat(b, 2+rng.Intn(6))
		}
		add(b)
	}
	return inputs
}

type reviewZipEntry struct {
	Name string `json:"name"`
	File string `json:"file"`
	Perm uint32 `json:"perm"`
}

func TestReviewDeflateAndZipMatchPython(t *testing.T) {
	t.Parallel()
	random := 120
	if n, err := strconv.Atoi(os.Getenv("A10_DEFLATE_FUZZ")); err == nil {
		random = n
	}
	inputs := reviewDeflateInputs(random)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "deflate"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i, data := range inputs {
		if err := os.WriteFile(filepath.Join(dir, "deflate", fmt.Sprintf("%06d.bin", i)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Archives: the bundle's five names plus names zipfile flags as UTF-8,
	// every permission pattern, empty/large/incompressible members.
	rng := rand.New(rand.NewSource(7))
	names := []string{"compose.yaml", "prepare.sh", "bootstrap.py", "enrollment.json", "README.txt", "数据/说明.txt", "emoji-😀.sh", "a b", "dir/", "x"}
	var cases [][]reviewZipEntry
	var entries [][]zipEntry
	for c := 0; c < 60; c++ {
		var spec []reviewZipEntry
		var goEntries []zipEntry
		for e := 0; e < 1+rng.Intn(6); e++ {
			idx := rng.Intn(len(inputs))
			if c == 0 {
				idx = 0
			}
			name := names[rng.Intn(len(names))]
			if c < 5 {
				name = names[e%5]
			}
			perm := uint32(rng.Intn(0o10000))
			spec = append(spec, reviewZipEntry{name, fmt.Sprintf("deflate/%06d.bin", idx), perm})
			goEntries = append(goEntries, zipEntry{name, inputs[idx], perm})
		}
		cases = append(cases, spec)
		entries = append(entries, goEntries)
	}
	manifest, _ := json.Marshal(cases)
	if err := os.WriteFile(filepath.Join(dir, "zips.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	// The temporary directory is transport only; hash the exact input members
	// and manifest so an enlarged/adapted corpus cannot reuse another answer.
	hash := sha256.New()
	for _, input := range inputs {
		fmt.Fprintf(hash, "%d:", len(input))
		hash.Write(input)
	}
	hash.Write(manifest)
	out := testgolden.New(t, testgolden.Spec{
		Sources: testgolden.Sources(t, "backend", "server/internal/installer/testdata/review_a10_deflate.py"),
		Input:   hash.Sum(nil),
		Run: func() ([]byte, error) {
			cmd := exec.Command(testgolden.Python(), "testdata/review_a10_deflate.py", dir)
			return cmd.Output()
		},
	}).Output(t)

	deflates, zips, alts := 0, 0, 0
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		var row struct {
			Kind   string            `json:"kind"`
			ID     int               `json:"id"`
			SHA256 string            `json:"sha256"`
			Size   int               `json:"size"`
			Alt    map[string]string `json:"alt"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		switch row.Kind {
		case "deflate":
			deflates++
			got := zlibDeflate(inputs[row.ID])
			if !nativeDeflatePayload(got, inputs[row.ID]) {
				t.Errorf("deflate input %d (%d bytes): Go %d bytes, Python %d bytes", row.ID, len(inputs[row.ID]), len(got), row.Size)
			}
			for version, sum := range row.Alt {
				alts++
				if sum != row.SHA256 {
					t.Errorf("deflate input %d: zlib %s differs from the reference zlib", row.ID, version)
				}
			}
		case "zip":
			zips++
			got, err := composeArchive(entries[row.ID])
			// A directory with a nonempty payload is an internal zipfile
			// artifact, never one of the five installation package members.
			// archive/zip refuses it; retain its frozen Python answer.
			nonemptyDirectory := false
			for _, entry := range entries[row.ID] {
				if strings.HasSuffix(entry.name, "/") && len(entry.data) != 0 {
					nonemptyDirectory = true
				}
			}
			if nonemptyDirectory {
				if err == nil {
					t.Error("nonempty directory archive member was accepted")
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			if !nativeArchivePayload(got, entries[row.ID]) {
				t.Errorf("zip case %d: Go %d bytes, Python %d bytes", row.ID, len(got), row.Size)
			}
		}
	}
	if deflates != len(inputs) || zips != len(cases) {
		t.Fatalf("python answered %d/%d deflate and %d/%d zip cases", deflates, len(inputs), zips, len(cases))
	}
	total := 0
	for _, b := range inputs {
		total += len(b)
	}
	t.Logf("deflate: %d inputs (%d bytes) equal; zip: %d archives equal; other zlib builds: %d comparisons", deflates, total, zips, alts)
}

// TestComposeReviewCorpusMatchesPython replays generate_guide for the three
// Compose methods with adversarial values (testdata/gen_review_a10.py).
func TestComposeReviewCorpusMatchesPython(t *testing.T) {
	t.Parallel()
	replayRecordFile(t, "testdata/docker_ha_compose_review.jsonl.gz")
}

// The Go-rendered prepare.sh treats shell metacharacters, quotes and
// Unicode line breaks in the data directory and identity path as literal
// path text (run with bash and the fake docker, temp directories only).
func TestReviewBundlePrepareKeepsMetacharactersLiteral(t *testing.T) {
	for _, inData := range []bool{false, true} {
		dir := t.TempDir()
		odd := " $(touch PWNED1) `touch PWNED2` 'q' \"d\" ;|&<>*?!#~ \\  \u0085é ${HOME}"
		identity := filepath.Join(dir, "id"+odd)
		body := composeParams(dir, "data_dir", filepath.Join(dir, "data"+odd), "ha_host_id_path", identity, "create_host_identity", true)
		p := composePreparation(t, dir, body, nil, inData)
		for round := 0; round < 2; round++ {
			if r := p.prepare(t); r.code != 0 {
				t.Fatalf("in_data=%v round %d: %+v", inData, round, r)
			}
		}
		data := mapStr(body, "data_dir")
		for _, name := range []string{"agent", "oxidns", "ha", ".ha-compose/setup", ".ha-compose/bootstrap.py", ".ha-compose/enrollment.json"} {
			if _, err := os.Stat(filepath.Join(data, name)); err != nil {
				t.Fatalf("in_data=%v: %s: %v", inData, name, err)
			}
		}
		if content, err := os.ReadFile(identity); err != nil || len(strings.TrimSpace(string(content))) != 32 {
			t.Fatalf("identity %q %v", content, err)
		}
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err == nil && strings.HasPrefix(info.Name(), "PWNED") {
				t.Errorf("command substitution executed: %s", path)
			}
			return nil
		})
		if matches, _ := filepath.Glob(filepath.Join(p.files, "..", "PWNED*")); len(matches) > 0 {
			t.Errorf("command substitution executed: %v", matches)
		}
	}
}
