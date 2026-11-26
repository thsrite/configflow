//go:build linux

package jsonstore

import (
	"io/fs"
	"syscall"
)

func signatureOf(info fs.FileInfo) Signature {
	st := info.Sys().(*syscall.Stat_t)
	return Signature{
		Dev: uint64(st.Dev), Ino: uint64(st.Ino), Size: st.Size,
		MtimeNs: st.Mtim.Nano(),
		CtimeNs: st.Ctim.Nano(),
	}
}
