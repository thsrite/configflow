package convert_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

type recordedAgentCase struct {
	ArchiveSHA256 string   `json:"archive_sha256"`
	Index         int      `json:"index"`
	RowSHA256     string   `json:"row_sha256"`
	Error         []string `json:"error"`
	Mutation      string   `json:"mutation"`
	Reason        string   `json:"reason"`
}

func recordedAgentReference(t *testing.T) recordedAgentCase {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_recorded_agent_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref recordedAgentCase
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	if ref.Index != 1144 || len(ref.Error) != 2 || ref.Error[0] != "EasyTierSettingsError" || ref.Mutation != "unchanged from original record" || ref.Reason == "" {
		t.Fatal("invalid exact Agent guard reference")
	}
	archive, err := os.ReadFile("testdata/mihomo_recorded.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(archive)) != ref.ArchiveSHA256 {
		t.Fatal("Agent recorded source changed")
	}
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 65536), 1<<26)
	for i := 0; scanner.Scan(); i++ {
		if i == ref.Index {
			if fmt.Sprintf("%x", sha256.Sum256(scanner.Bytes())) != ref.RowSHA256 {
				t.Fatal("Agent guard reference row changed")
			}
			return ref
		}
	}
	t.Fatal("Agent reference row missing", scanner.Err())
	return ref
}
