//go:build linux

package trafficstore

import (
	"io"
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/unix"

	"configflow/internal/jsonstore"
)

// signatureOf is _file_signature(os.stat(path)): st_dev, st_ino, st_size,
// st_mtime_ns, st_ctime_ns (the same tuple jsonstore returns for a
// verified write).
func signatureOf(info fs.FileInfo) jsonstore.Signature {
	st := info.Sys().(*syscall.Stat_t)
	return jsonstore.Signature{
		Dev: uint64(st.Dev), Ino: uint64(st.Ino), Size: st.Size,
		MtimeNs: st.Mtim.Nano(),
		CtimeNs: st.Ctim.Nano(),
	}
}

func copyFile(dst, src *os.File) error {
	if err := unix.IoctlFileClone(int(dst.Fd()), int(src.Fd())); err == nil {
		return nil
	}
	// Filesystems and sandbox policies can reject the ioctl even when an
	// ordinary copy is permitted. Both descriptors still start at offset zero.
	_, err := io.Copy(dst, src)
	return err
}
