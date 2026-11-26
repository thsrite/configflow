package installer

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"io"
)

func zlibDeflate(data []byte) []byte {
	var output bytes.Buffer
	writer, _ := flate.NewWriter(&output, flate.DefaultCompression)
	_, _ = writer.Write(data)
	_ = writer.Close()
	return output.Bytes()
}
func nativeDeflatePayload(data, want []byte) bool {
	reader := flate.NewReader(bytes.NewReader(data))
	defer reader.Close()
	decoded, err := io.ReadAll(reader)
	return err == nil && bytes.Equal(decoded, want)
}
func nativeArchivePayload(data []byte, want []zipEntry) bool {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(reader.File) != len(want) {
		return false
	}
	for i, file := range reader.File {
		if file.Name != want[i].name || file.ExternalAttrs>>16 != want[i].permissions|0100000 {
			return false
		}
		input, err := file.Open()
		if err != nil {
			return false
		}
		body, err := io.ReadAll(input)
		_ = input.Close()
		if err != nil || !bytes.Equal(body, want[i].data) {
			return false
		}
	}
	return true
}
