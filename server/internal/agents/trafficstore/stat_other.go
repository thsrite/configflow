//go:build !linux

package trafficstore

import (
	"io"
	"io/fs"
	"os"

	"configflow/internal/jsonstore"
)

// signatureOf: without a portable ctime accessor the change time is
// approximated by the modification time (the server runs on Linux).
func signatureOf(info fs.FileInfo) jsonstore.Signature {
	sig := jsonstore.Signature{Size: info.Size(), MtimeNs: info.ModTime().UnixNano()}
	sig.CtimeNs = sig.MtimeNs
	return sig
}

func copyFile(dst, src *os.File) error {
	_, err := io.Copy(dst, src)
	return err
}
