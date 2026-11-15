//go:build linux

package trafficstore

import (
	"io/fs"
	"syscall"

	"configflow/internal/jsonstore"
)

// signatureOf is _file_signature(os.stat(path)): st_dev, st_ino, st_size,
// st_mtime_ns, st_ctime_ns (the same tuple jsonstore returns for a
// verified write).
func signatureOf(info fs.FileInfo) jsonstore.Signature {
	st := info.Sys().(*syscall.Stat_t)
	return jsonstore.Signature{
		Dev: uint64(st.Dev), Ino: uint64(st.Ino), Size: st.Size,
		MtimeNs: st.Mtim.Sec*1e9 + st.Mtim.Nsec,
		CtimeNs: st.Ctim.Sec*1e9 + st.Ctim.Nsec,
	}
}
