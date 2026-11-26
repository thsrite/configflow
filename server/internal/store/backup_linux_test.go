//go:build linux

package store

import (
	"errors"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestBackupWriteFailureMessage: when writing the backup fails (here: the
// file size limit), Python reports the destination connection's message,
// the plain errstr of the code ("disk I/O error", extended code 778);
// the driver's source-connection text ("not an error") must not leak.
// Reference: backup_to under RLIMIT_FSIZE=50000 in CPython 3.11.
func TestBackupWriteFailureMessage(t *testing.T) {
	directory := t.TempDir()
	target := legacyNew(t, filepath.Join(directory, "big.sqlite3"), seconds(1))
	must(target.Create(testObj("system", testObj("blob", strings.Repeat("x", 200000))), nil))(t)
	signal.Ignore(syscall.SIGXFSZ)
	defer signal.Reset(syscall.SIGXFSZ)
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 50000, Max: limit.Max}); err != nil {
		t.Skip(err)
	}
	err := target.BackupTo(filepath.Join(directory, "out.sqlite3"))
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		t.Fatal(err)
	}
	if kindOf(err) != ErrStorage || err.Error() != "配置数据库访问失败：disk I/O error" || sqliteCode(err) != 778&0xff {
		t.Errorf("BackupTo: %v (%v)", err, kindOf(err))
	}
}

func kindOf(err error) error {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Kind
	}
	return nil
}
