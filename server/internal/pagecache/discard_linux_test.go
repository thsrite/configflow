//go:build linux

package pagecache_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/sys/unix"

	"configflow/internal/agents/upgrades"
	"configflow/internal/api/agents"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/installer"
	"configflow/internal/pagecache"
)

type discard struct {
	path     string
	position int64
}

func observeDiscards(t *testing.T) func(...discard) {
	t.Helper()
	var calls []discard
	pagecache.SetFadviseForTest(t, func(fd int, offset, length int64, advice int) error {
		path, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
		pos, seekErr := unix.Seek(fd, 0, unix.SEEK_CUR)
		if err != nil || seekErr != nil || offset != 0 || length != 0 || advice != unix.FADV_DONTNEED {
			t.Fatalf("fadvise(%d, %d, %d, %d): path %q, seek %v, readlink %v", fd, offset, length, advice, path, seekErr, err)
		}
		calls = append(calls, discard{path, pos})
		return unix.EOPNOTSUPP // A failed hint must leave the caller's result intact.
	})
	return func(want ...discard) {
		t.Helper()
		if !slices.Equal(calls, want) {
			t.Fatalf("discards = %v, want %v", calls, want)
		}
		calls = nil
	}
}

func TestAgentChecksumsShareDigestsAndDiscardAfterReading(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTS_STATIC_DIR", dir)
	t.Cleanup(upgrades.ResetArtifactCache)
	checkDiscards := observeDiscards(t)
	write := func(arch, content string) discard {
		t.Helper()
		path := filepath.Join(dir, "configflow-agent-"+arch)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		return discard{path, int64(len(content))}
	}
	const amd, arm, plain = "\x7fELFamd64", "\x7fELFarm64", "plain installer file"
	sum := func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }

	amdFile := write("linux-amd64", amd)
	model, err := upgrades.ArtifactsModel(t.Context())
	item, ok := model.Get("linux-amd64")
	if err != nil || model.Len() != 1 || !ok || item.SHA256 != sum(amd) || int(item.Size) != len(amd) {
		t.Fatal(model, err)
	}
	checkDiscards(amdFile)

	armFile := write("linux-arm64", plain)
	for _, content := range []string{plain, arm} {
		if content == arm {
			armFile = write("linux-arm64", arm)
		}
		want := map[string]string{"linux-amd64": sum(amd), "linux-arm64": sum(content)}
		for i := 0; i < 2; i++ {
			got, err := installer.LocalAgentChecksums()
			if err != nil || !maps.Equal(got, want) {
				t.Fatal(got, err)
			}
			if i == 0 {
				checkDiscards(armFile)
			} else {
				checkDiscards()
			}
		}
		model, err := upgrades.ArtifactsModel(t.Context())
		item, ok := model.Get("linux-arm64")
		if err != nil || ok != (content == arm) || (ok && (item.SHA256 != sum(arm) || int(item.Size) != len(arm))) {
			t.Fatal(model, err)
		}
		checkDiscards()
	}
}

func TestAgentDownloadDiscardsAfterServing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTS_STATIC_DIR", dir)
	const name, body = "configflow-agent-linux-armv7", "\x7fELFsynthetic"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(agents.NewHandler(agents.Deps{}).Register,
		func(r *httpx.Router) {
			r.GET("/api/reference", "agents.reference", func(*httpx.Ctx) (*httpx.Response, error) {
				return httpx.File(httpx.FileOptions{Path: path, MimeType: "application/octet-stream", AsAttachment: true, DownloadName: name})
			})
		})})
	checkDiscards := observeDiscards(t)
	etag := srv.Get("/api/reference").WantStatus(200).Header.Get("ETag")
	for _, tc := range []struct {
		method  string
		headers []httpxtest.Option
		pos     int64
	}{
		{"GET", nil, int64(len(body))},
		{"GET", []httpxtest.Option{httpxtest.Header("Range", "bytes=2-4"), httpxtest.Header("If-Range", etag)}, 5},
		{"GET", []httpxtest.Option{httpxtest.Header("If-None-Match", etag)}, 0},
		{"HEAD", nil, 0},
	} {
		want := srv.Do(tc.method, "/api/reference", tc.headers...)
		checkDiscards()
		got := srv.Do(tc.method, "/api/agents/download/"+name, tc.headers...)
		if got.Status != want.Status || !reflect.DeepEqual(got.Header, want.Header) || !bytes.Equal(got.Body, want.Body) {
			t.Fatalf("got %d %v %q, want %d %v %q", got.Status, got.Header, got.Body, want.Status, want.Header, want.Body)
		}
		checkDiscards(discard{path, tc.pos})
	}
}
