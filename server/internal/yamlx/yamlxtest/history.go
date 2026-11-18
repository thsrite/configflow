package yamlxtest

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"configflow/internal/yamlx"
)

// Historical comparisons were recorded while both Go loaders still existed.
// Callers need only a compact fingerprint of their own inputs. The complete
// archive is embedded by history_test.go and replayed once in this package.
const (
	frozenArchiveSHA = "2289da8b3711546f68a7a4c0f440c05d005e6ecc60598a3fb259b64321992222"
	frozenRevision   = "7d58ac61"
	frozenCount      = 21343
)

//go:generate go run ./testdata/frozenindex

//go:embed testdata/n73_index.json.gz
var frozenIndexBytes []byte

//go:embed testdata/reader_history.json.gz
var readerHistoryBytes []byte

type frozenSummary struct {
	Category    string `json:"category"`
	NativeSHA   string `json:"native_sha256"`
	NativeError string `json:"native_error"`
}

type frozenIndex struct {
	ArchiveSHA string                   `json:"archive_sha256"`
	Revision   string                   `json:"revision"`
	Records    map[string]frozenSummary `json:"records"`
}

func readArchive(data []byte, dst any) error {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer reader.Close()
	return json.NewDecoder(reader).Decode(dst)
}

var frozenSummaries = sync.OnceValues(func() (map[string]frozenSummary, error) {
	if fmt.Sprintf("%x", sha256.Sum256(frozenIndexBytes)) != "47e7732d7e632eacae626e6a99ecc2e136a5873c8d2297a68c0c8e5f9c45134b" {
		return nil, fmt.Errorf("historical YAML comparison index changed")
	}
	var index frozenIndex
	if err := readArchive(frozenIndexBytes, &index); err != nil {
		return nil, err
	}
	if index.ArchiveSHA != frozenArchiveSHA || index.Revision != frozenRevision || len(index.Records) != frozenCount {
		return nil, fmt.Errorf("historical YAML comparison index metadata changed")
	}
	return index.Records, nil
})

func auditSHA(nodes []ParseNode) (string, error) {
	data, err := json.Marshal(nodes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

// Classify verifies today's Parse against a frozen actual old/new comparison.
// It decodes only the compact index, then parses and audits this caller's source;
// history_test.go independently verifies the index against every full record.
// Unrecorded sources fail; no new input silently acquires a historical exemption.
func Classify(source string) (string, error) {
	records, err := frozenSummaries()
	if err != nil {
		return "", err
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(source)))
	record, ok := records[id]
	if !ok || record.Category == "" {
		return "", fmt.Errorf("missing or invalid historical YAML input %s", id)
	}
	node, err := yamlx.Parse([]byte(source))
	if err != nil {
		if err.Error() != record.NativeError {
			return "", fmt.Errorf("native YAML refusal changed for %s: %w", id, err)
		}
	} else {
		digest, err := auditSHA(ParseAudit(node))
		if err != nil {
			return "", err
		}
		if record.NativeError != "" || digest != record.NativeSHA {
			return "", fmt.Errorf("native YAML physical graph changed for %s", id)
		}
	}
	return record.Category, nil
}

// ReaderHistory preserves the original mode-specific old error and categories.
type ReaderHistory struct {
	Mode         string   `json:"mode"`
	SourceSHA256 string   `json:"source_sha256"`
	OldError     string   `json:"old_error"`
	Classes      []string `json:"classes"`
}

type readerArchive struct {
	Records  map[string]ReaderHistory `json:"records"`
	Fixtures map[string]string        `json:"fixtures"`
}

var readerHistories = sync.OnceValues(func() (readerArchive, error) {
	if fmt.Sprintf("%x", sha256.Sum256(readerHistoryBytes)) != "a0cd96234d53f4348b020832a85d118e53fc19d7d2a50d459117ca9d82a81505" {
		return readerArchive{}, fmt.Errorf("historical mode archive changed")
	}
	var archive readerArchive
	err := readArchive(readerHistoryBytes, &archive)
	return archive, err
})

// HistoricalReader looks up a mode-specific frozen comparison, never loading
// user YAML with a second parser. New sources require a real reference capture.
func HistoricalReader(mode, source string) (ReaderHistory, error) {
	archive, err := readerHistories()
	if err != nil {
		return ReaderHistory{}, err
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(mode+"\x00"+source)))
	row, ok := archive.Records[id]
	if !ok || row.Mode != mode || row.SourceSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(source))) {
		return ReaderHistory{}, fmt.Errorf("missing historical reader input %s", id)
	}
	row.Classes = append([]string(nil), row.Classes...)
	return row, nil
}

func historicalFixture(data io.Reader) error {
	archive, err := readerHistories()
	if err != nil {
		return err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, data); err != nil {
		return err
	}
	if _, ok := archive.Fixtures[fmt.Sprintf("%x", hash.Sum(nil))]; !ok {
		return fmt.Errorf("unrecorded or changed reader fixture")
	}
	return nil
}
