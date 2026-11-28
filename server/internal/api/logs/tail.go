package logs

import (
	"bytes"
	"io"
	"io/fs"
	"os"

	"configflow/internal/pagecache"
)

const tailBlockSize = 16 << 10

// fileEnd records the offset and the bytes immediately preceding it.
type fileEnd struct {
	offset int64
	suffix [64]byte // right-aligned, including for short files
}

// Write advances the recorded end alongside a sequential read.
func (e *fileEnd) Write(p []byte) (int, error) {
	n := len(p)
	keep := min(n, len(e.suffix))
	copy(e.suffix[:], e.suffix[keep:])
	copy(e.suffix[len(e.suffix)-keep:], p[n-keep:])
	e.offset += int64(n)
	return n, nil
}

type lineCount struct {
	file  fs.FileInfo
	end   fileEnd
	total int64
}

func (s *lineCount) matches(file io.ReaderAt, info fs.FileInfo, buf []byte) (bool, error) {
	if s.file == nil || !os.SameFile(s.file, info) || info.Size() < s.end.offset {
		return false, nil
	}
	if info.Size() == s.end.offset {
		oldMod, oldChange := fileTimes(s.file)
		mod, change := fileTimes(info)
		if !mod.Equal(oldMod) || !change.Equal(oldChange) {
			return false, nil
		}
	}
	// A truncate followed by regrowth can leave both inode and size plausible.
	n := min(s.end.offset, int64(len(s.end.suffix)))
	_, err := file.ReadAt(buf[:n], s.end.offset-n)
	if err == io.EOF {
		return false, nil
	}
	return bytes.Equal(buf[:n], s.end.suffix[int64(len(s.end.suffix))-n:]), err
}

func (s *lineCount) advance(file io.ReaderAt, buf []byte) error {
	for {
		n, err := file.ReadAt(buf, s.end.offset)
		if n > 0 {
			// Count line starts so appending to an unfinished line leaves its count intact.
			if s.end.offset == 0 {
				s.total++
			}
			last := s.end.suffix[len(s.end.suffix)-1]
			for _, b := range buf[:n] {
				if last == '\n' || (last == '\r' && b != '\n') {
					s.total++
				}
				last = b
			}
			s.end.Write(buf[:n])
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// tailStart finds the delimiter before the first requested line. Only the
// last byte of CRLF is a delimiter, even when its bytes are in different blocks.
func tailStart(file io.ReaderAt, size, lines int64, buf []byte) (int64, error) {
	nextLF := false
	for end := size; end > 0; {
		start := max(0, end-int64(len(buf)))
		block := buf[:end-start]
		if _, err := file.ReadAt(block, start); err != nil {
			return 0, err
		}
		for i := len(block) - 1; i >= 0; i-- {
			b := block[i]
			after := start + int64(i) + 1
			if after < size && (b == '\n' || (b == '\r' && !nextLF)) {
				lines--
				if lines == 0 {
					return after, nil
				}
			}
			nextLF = b == '\n'
		}
		end = start
	}
	return 0, nil
}

func collectLines(reader io.Reader) ([]string, error) {
	var lines []string
	err := readLines(reader, func(line string) { lines = append(lines, line) })
	return lines, err
}

func (h *Handler) readTail(file LogFile, limit int64) ([]string, int, error) {
	h.countMu.Lock()
	defer h.countMu.Unlock()
	var fullRead bool
	defer func() {
		if f, ok := file.(*os.File); ok && fullRead {
			pagecache.Discard(f)
		}
	}()
	if limit <= 0 {
		var state lineCount
		state.file, _ = file.Stat() // Only needed to reuse the count on a later tail.
		lines, err := collectLines(io.TeeReader(file, &state.end))
		if err == nil {
			fullRead = true
			state.total = int64(len(lines))
			h.count = state
		}
		return lines, len(lines), err
	}
	buf := make([]byte, tailBlockSize)
	for {
		info, err := file.Stat()
		if err != nil {
			return nil, 0, err
		}
		state := h.count
		matches, err := state.matches(file, info, buf)
		if err != nil {
			return nil, 0, err
		}
		if !matches {
			state = lineCount{}
		}
		state.file = info
		previousOffset := state.end.offset
		if err := state.advance(file, buf); err != nil {
			return nil, 0, err
		}
		if !matches || (previousOffset == 0 && state.end.offset > 0) {
			fullRead = true
		}
		start, err := tailStart(file, state.end.offset, limit, buf)
		if err == io.EOF {
			h.count = lineCount{}
			continue // Truncated between the count and the backward read.
		}
		if err != nil {
			return nil, 0, err
		}
		window := io.NewSectionReader(file, start, state.end.offset-start)
		lines, err := collectLines(window)
		if err != nil {
			return nil, 0, err
		}
		if pos, _ := window.Seek(0, io.SeekCurrent); pos < window.Size() {
			h.count = lineCount{}
			continue // Truncated while decoding the window.
		}
		if start == 0 {
			state.total = int64(len(lines))
		} else if state.total <= int64(len(lines)) {
			// The window proves there are earlier lines, even if a rewrite repeated the sample.
			h.count = lineCount{}
			continue
		}
		h.count = state
		return lines, int(state.total), nil
	}
}
