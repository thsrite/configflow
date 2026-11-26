//go:build !linux

package jsonstore

import (
	"io/fs"
	"syscall"
)

// signatureOf: on platforms without a portable ctime accessor the change time
// is approximated by the modification time (the server runs on Linux).
func signatureOf(info fs.FileInfo) Signature {
	sig := Signature{Size: info.Size(), MtimeNs: info.ModTime().UnixNano()}
	sig.CtimeNs = sig.MtimeNs
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		sig.Dev, sig.Ino = uint64(st.Dev), uint64(st.Ino)
	}
	return sig
}
