package installer

import (
	"archive/zip"
	"bytes"
	"fmt"
	"time"
)

type zipEntry struct {
	name        string
	data        []byte
	permissions uint32
}

// composeArchive uses the standard ZIP writer. Member bytes, order and Unix
// permissions are the installation contract; compression streams are not.
func composeArchive(entries []zipEntry) ([]byte, error) {
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate, CreatorVersion: 3<<8 | 20, ReaderVersion: 20, ExternalAttrs: (entry.permissions | 0100000) << 16, Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)}
		member, err := writer.CreateHeader(header)
		if err != nil {
			return nil, fmt.Errorf("create Compose archive member: %w", err)
		}
		if _, err := member.Write(entry.data); err != nil {
			return nil, fmt.Errorf("write Compose archive member: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close Compose archive: %w", err)
	}
	return output.Bytes(), nil
}
