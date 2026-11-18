// Command frozenindex builds the compact caller lookup from the unchanged full
// historical archive. Run with go generate from internal/yamlx/yamlxtest.
package main

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"configflow/internal/yamlx/yamlxtest"
)

type summary struct {
	Category    string `json:"category"`
	NativeSHA   string `json:"native_sha256"`
	NativeError string `json:"native_error"`
}

type index struct {
	ArchiveSHA string             `json:"archive_sha256"`
	Revision   string             `json:"revision"`
	Records    map[string]summary `json:"records"`
}

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	file, err := os.Open("testdata/n73_frozen.json.gz")
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	compressed := io.TeeReader(file, hash)
	reader, err := gzip.NewReader(compressed)
	if err != nil {
		return err
	}
	defer reader.Close()
	decoder := json.NewDecoder(reader)
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return fmt.Errorf("invalid archive header: %v %v", token, err)
	}
	result := index{Revision: "7d58ac61", Records: map[string]summary{}}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		id, ok := token.(string)
		if !ok {
			return fmt.Errorf("invalid archive key")
		}
		if _, exists := result.Records[id]; exists {
			return fmt.Errorf("duplicate archive key %s", id)
		}
		var row struct {
			Source      string                `json:"source"`
			Category    string                `json:"category"`
			Native      []yamlxtest.ParseNode `json:"native"`
			NativeError string                `json:"native_error"`
			Unknown     bool                  `json:"unknown"`
			Revision    string                `json:"revision"`
		}
		if err := decoder.Decode(&row); err != nil {
			return err
		}
		if fmt.Sprintf("%x", sha256.Sum256([]byte(row.Source))) != id || row.Unknown || row.Category == "" || row.Revision != result.Revision {
			return fmt.Errorf("invalid archive metadata %s", id)
		}
		data, err := json.Marshal(row.Native)
		if err != nil {
			return err
		}
		result.Records[id] = summary{Category: row.Category, NativeSHA: fmt.Sprintf("%x", sha256.Sum256(data)), NativeError: row.NativeError}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return fmt.Errorf("invalid archive trailer: %v %v", token, err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("archive trailing data: %v", err)
	}
	if _, err := io.Copy(io.Discard, compressed); err != nil {
		return err
	}
	result.ArchiveSHA = fmt.Sprintf("%x", hash.Sum(nil))
	if result.ArchiveSHA != "2289da8b3711546f68a7a4c0f440c05d005e6ecc60598a3fb259b64321992222" || len(result.Records) != 21343 {
		return fmt.Errorf("archive checksum or count changed")
	}
	output, err := os.Create("testdata/n73_index.json.gz")
	if err != nil {
		return err
	}
	writer := gzip.NewWriter(output)
	if err := json.NewEncoder(writer).Encode(result); err != nil {
		writer.Close()
		output.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}
