package logs

import (
	"bytes"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/httpx"
)

func logFixture(t testing.TB, content []byte) (*Handler, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	return &Handler{LogFilePath: func() (string, error) { return path, nil }}, path
}

func appendLog(t testing.TB, path string, data []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
}

func compareTail(t *testing.T, h *Handler, query string) {
	t.Helper()
	ctx := &httpx.Ctx{Request: httptest.NewRequest("GET", "/api/logs/tail?"+query, nil)}
	want, err := h.getLogsBefore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.getLogs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != want.Status || !bytes.Equal(got.Body(), want.Body()) {
		t.Fatalf("%s: got %d %.500q, want %d %.500q", query, got.Status, got.Body(), want.Status, want.Body())
	}
}

func TestTailMatchesFullFileResponse(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"empty", ""},
		{"unterminated", "INFO: needle"},
		{"mixed endings", "error: old needle\rINFO: two\r\n\n\r\nINFO: new NEEDLE\r"},
		{"invalid UTF-8", "\xff\r\xfe\nWARN: \xc2\r\x80\n\xff"},
		{"CRLF across count blocks", strings.Repeat("x", tailBlockSize-1) + "\r\ninfo: needle\nfinal"},
		{"CRLF across tail blocks", "first\nINFO needle\r\n" + strings.Repeat("x", tailBlockSize-1)},
		{"long line", "old\r\nINFO: " + strings.Repeat("界", tailBlockSize) + " needle\r\nlast"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := logFixture(t, []byte(tc.content))
			for _, query := range []string{"", "lines=-2", "lines=0", "lines=1", "lines=2", "lines=3", "lines=6", "lines=10001",
				"lines=2&search=needle", "lines=3&level=info", "lines=2&search=NEEDLE&level=INFO", "level=%5B"} {
				compareTail(t, h, query)
			}
		})
	}
	t.Run("directory read failure", func(t *testing.T) {
		path := t.TempDir()
		compareTail(t, &Handler{LogFilePath: func() (string, error) { return path, nil }}, "lines=2")
	})
}

func TestTailCountsAcrossFileChanges(t *testing.T) {
	h, path := logFixture(t, []byte("seed\r"))
	compareTail(t, h, "lines=0")
	if h.count.end.offset != 5 || h.count.total != 1 {
		t.Fatal("unbounded read did not save its count", h.count)
	}
	check := func() { t.Helper(); compareTail(t, h, "lines=2") }
	replace := func(data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []string{"\nINFO: started", "\r", "\n", "text", "\xff", "\r\n\r", "last"} {
		appendLog(t, path, []byte(s))
		check()
	}
	// Keep the old boundary bytes across rotation: only the identity changes.
	suffix := strings.Repeat("x", len(h.count.end.suffix))
	replace("a\nb\nc\n" + suffix)
	check()
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	replace("abcdef" + suffix + "\nnew")
	check()
	// A same-size rewrite can change earlier line breaks while preserving the sample.
	replace("a\nb\nc\n" + suffix + "\nnew")
	if err := os.Chtimes(path, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	check()
	replace("short\r")
	check()
	// External truncate and grow beyond the previous offset, without a request in between.
	replace("a\n\n\r\nb\n" + suffix)
	check()
	// The old suffix can recur in a rewritten file. Use the window if it
	// contains the whole file or contradicts the cached count.
	replace("abcd" + suffix)
	check()
	replace("a\nb\n" + suffix + "new")
	check()
	replace("abcd" + suffix + "new and longer")
	check()
	replace("a\n\n\r\nb\n" + suffix)
	check()
	// Clear through the route, then restore the old boundary bytes before the next poll.
	if _, err := h.clearLog(t.Context()); err != nil {
		t.Fatal(err)
	}
	replace("abcdefg" + suffix + " and longer")
	check()
	if _, err := h.clearLog(t.Context()); err != nil {
		t.Fatal(err)
	}
	check()
}

type hookedFile struct {
	*os.File
	readAt func([]byte, int64) (int, error)
}

func (f *hookedFile) ReadAt(p []byte, off int64) (int, error) { return f.readAt(p, off) }

func TestTruncateWhileReadingWindow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		windowRead int
	}{{"finding start", 1}, {"decoding lines", 2}} {
		t.Run(tc.name, func(t *testing.T) {
			h, path := logFixture(t, bytes.Repeat([]byte("record\n"), 10000))
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			counted, reads := false, 0
			withTruncate := &hookedFile{File: file, readAt: func(p []byte, off int64) (int, error) {
				if counted {
					reads++
					if reads == tc.windowRead {
						if err := os.WriteFile(path, []byte("new\r\nlast"), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				n, err := file.ReadAt(p, off)
				if err == io.EOF {
					counted = true
				}
				return n, err
			}}
			lines, total, err := h.readTail(withTruncate, 1)
			if err != nil || total != 2 || len(lines) != 1 || lines[0] != "last" {
				t.Fatal(lines, total, err)
			}
			compareTail(t, h, "lines=2")
		})
	}
}

func TestConcurrentTailAndClear(t *testing.T) {
	h, path := logFixture(t, []byte(strings.Repeat("record\r\n", 10000)))
	start := make(chan struct{})
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			<-start
			for range 3 {
				file, err := os.Open(path)
				if err != nil {
					t.Error(err)
					return
				}
				lines, total, err := h.readTail(file, 1)
				file.Close()
				if err != nil || !(total == 0 && len(lines) == 0 ||
					total == 10000 && len(lines) == 1 && lines[0] == "record\n") {
					t.Errorf("tail during clear = %q, %d, %v", lines, total, err)
					return
				}
			}
		})
	}
	group.Go(func() {
		<-start
		if _, err := h.clearLog(t.Context()); err != nil {
			t.Error(err)
		}
	})
	close(start)
	group.Wait()
	compareTail(t, h, "lines=1")
}

func FuzzTailMatchesFullFile(f *testing.F) {
	f.Add([]byte("ERROR: old\r\nINFO: needle\xff\rfinal"), uint16(2), uint16(11), true)
	f.Add([]byte("\r\n\r\n\n"), uint16(1), uint16(1), false)
	f.Add([]byte{}, uint16(0), uint16(0), false)
	f.Add([]byte("old\r\nnext\r\n"+strings.Repeat("x", tailBlockSize-1)), uint16(2), uint16(5), false)
	f.Fuzz(func(t *testing.T, data []byte, limit, at uint16, filter bool) {
		split := int(at) % (len(data) + 1)
		h, path := logFixture(t, data[:split])
		query := fmt.Sprintf("lines=%d", limit%32)
		if filter {
			query += "&level=info&search=needle"
		}
		compareTail(t, h, query)
		appendLog(t, path, data[split:])
		compareTail(t, h, query)
	})
}

func BenchmarkLogTail(b *testing.B) {
	client := newLogClient(b)
	client.app.Router.GET("/api/logs/reference", "logs.reference", client.handler.getLogsBefore)
	const line = "INFO example.com completed a synthetic request from 192.0.2.1\n"
	const size = 10 << 20
	content := bytes.Repeat([]byte(line), size/len(line)+1)[:size]
	if err := os.WriteFile(client.path, content, 0600); err != nil {
		b.Fatal(err)
	}
	var meter readMeter
	client.handler.OpenFile = meter.open
	for _, tc := range []struct{ name, path string }{{"before", "/api/logs/reference"}, {"after", "/api/logs/tail"}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			req := httpx.InvokeRequest{Method: "GET", Path: tc.path, Query: [][2]string{{"lines", "60"}}}
			invoke := func() {
				res, err := client.app.Invoke(b.Context(), req)
				if err != nil || res.Status != 200 {
					b.Fatal(res, err)
				}
			}
			invoke()
			meter.bytes = 0
			for b.Loop() {
				invoke()
			}
			b.ReportMetric(float64(meter.bytes)/float64(b.N), "read-B/op")
		})
	}
}
