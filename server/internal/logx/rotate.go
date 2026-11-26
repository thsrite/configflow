package logx

import (
	"fmt"
	"os"
	"unicode/utf8"
)

// rotatingFile is logging.handlers.RotatingFileHandler(maxBytes, backupCount,
// encoding="utf-8"): append mode, and before a record that would make the file
// reach maxBytes the files are shifted app.log -> app.log.1 -> ... -> app.log.N
// (the oldest is deleted) and a fresh app.log is started. Python measures the
// record in characters, the file in bytes; so does this.
type rotatingFile struct {
	path     string
	maxBytes int64
	backups  int
	file     *os.File
}

func openRotating(path string, maxBytes int64, backups int) (*rotatingFile, error) {
	f := &rotatingFile{path: path, maxBytes: maxBytes, backups: backups}
	if err := f.open(); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *rotatingFile) open() error {
	file, err := os.OpenFile(f.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o666)
	if err != nil {
		return err
	}
	f.file = file
	return nil
}

func (f *rotatingFile) close() error {
	if f.file == nil {
		return nil
	}
	err := f.file.Close()
	f.file = nil
	return err
}

func (f *rotatingFile) shouldRollover(line string) bool {
	if f.maxBytes <= 0 || f.file == nil {
		return false
	}
	info, err := f.file.Stat()
	if err != nil {
		return false
	}
	return info.Size()+int64(utf8.RuneCountInString(line)) >= f.maxBytes
}

func (f *rotatingFile) doRollover() {
	_ = f.close()
	if f.backups > 0 {
		for i := f.backups - 1; i > 0; i-- {
			source, dest := fmt.Sprintf("%s.%d", f.path, i), fmt.Sprintf("%s.%d", f.path, i+1)
			if _, err := os.Stat(source); err == nil {
				_ = os.Remove(dest)
				_ = os.Rename(source, dest)
			}
		}
		dest := f.path + ".1"
		_ = os.Remove(dest)
		_ = os.Rename(f.path, dest)
	}
	_ = f.open()
}

// write appends one formatted line (including its newline); errors are
// swallowed like logging's handleError does.
func (f *rotatingFile) write(line string) {
	if f.file == nil {
		if err := f.open(); err != nil {
			return
		}
	}
	if f.shouldRollover(line) {
		f.doRollover()
	}
	if f.file != nil {
		_, _ = f.file.WriteString(line)
	}
}
