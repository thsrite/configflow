package deploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

// Bundle is an immutable delivery. Its archive is packed once on first access;
// after packing only the archive retains the file bytes. mu protects that swap.
type Bundle struct {
	manifest      Manifest
	files         map[string]Content
	configVersion string
	comparison    *File
	mu            sync.Mutex
	once          sync.Once
	archive       []byte
	sha256        string
	packErr       error
}

// Pack captures manifest and content copies without compressing them yet.
func Pack(m Manifest, files map[string]Content, configVersion string, comparison *File) (*Bundle, error) {
	if err := checkContents(m, files); err != nil {
		return nil, err
	}
	b := &Bundle{manifest: m, configVersion: configVersion, files: make(map[string]Content, len(files))}
	b.manifest.Files = append([]File(nil), m.Files...)
	if comparison != nil {
		f := *comparison
		b.comparison = &f
	}
	for p, v := range files {
		b.files[p] = Content{bytes.Clone(v.Data), v.Role}
	}
	return b, nil
}

// Open validates a persisted archive, including every listed file and its digest.
// Python PAX and ustar encodings are read structurally.
func Open(m Manifest, archive []byte, sha, configVersion string, comparison *File) (*Bundle, error) {
	if len(archive) > MaxTotalBytes+8<<20 {
		return nil, preparation("Deployment exceeds size limit")
	}
	if digest(archive) != sha {
		return nil, preparation("Invalid deployment archive digest")
	}
	if _, err := readArchive(m, archive); err != nil {
		return nil, err
	}
	b := &Bundle{manifest: m, archive: bytes.Clone(archive), sha256: sha, configVersion: configVersion}
	b.manifest.Files = append([]File(nil), m.Files...)
	if comparison != nil {
		f := *comparison
		b.comparison = &f
	}
	b.once.Do(func() {})
	return b, nil
}

// Manifest returns an independent manifest copy.
func (b *Bundle) Manifest() Manifest {
	m := b.manifest
	m.Files = append([]File(nil), m.Files...)
	return m
}

// ConfigVersion is the captured main-file version.
func (b *Bundle) ConfigVersion() string { return b.configVersion }

// Comparison returns an independent original server main-file entry.
func (b *Bundle) Comparison() *File {
	if b.comparison == nil {
		return nil
	}
	f := *b.comparison
	return &f
}

func (b *Bundle) packed() error {
	b.once.Do(func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.archive, b.packErr = writeArchive(b.manifest, b.files)
		if b.packErr == nil {
			b.sha256 = digest(b.archive)
			b.files = nil
		}
	})
	return b.packErr
}

// Archive returns a copy of the deterministic gzip tar, packing on first use.
func (b *Bundle) Archive() ([]byte, error) {
	if err := b.packed(); err != nil {
		return nil, err
	}
	return bytes.Clone(b.archive), nil
}

// SHA256 returns the archive digest, packing on first use.
func (b *Bundle) SHA256() (string, error) {
	if err := b.packed(); err != nil {
		return "", err
	}
	return b.sha256, nil
}

// Files reads selected paths (all when no paths are supplied), returning copies.
func (b *Bundle) Files(paths ...string) (map[string][]byte, error) {
	wanted := map[string]bool{}
	if len(paths) == 0 {
		for _, f := range b.manifest.Files {
			wanted[f.Path] = true
		}
	} else {
		for _, p := range paths {
			wanted[p] = true
		}
	}
	b.mu.Lock()
	if b.files != nil {
		out := map[string][]byte{}
		for p := range wanted {
			if v, ok := b.files[p]; ok {
				out[p] = bytes.Clone(v.Data)
			}
		}
		b.mu.Unlock()
		return out, nil
	}
	b.mu.Unlock()
	if err := b.packed(); err != nil {
		return nil, err
	}
	files, err := readArchive(b.manifest, b.archive)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for p := range wanted {
		if v, ok := files[p]; ok {
			out[p] = v.Data
		}
	}
	return out, nil
}

// Contents returns all file bytes and roles in an independent map.
func (b *Bundle) Contents() (map[string]Content, error) {
	files, err := b.Files()
	if err != nil {
		return nil, err
	}
	out := make(map[string]Content, len(files))
	for _, f := range b.manifest.Files {
		v, ok := files[f.Path]
		if !ok {
			return nil, preparation("Deployment file missing from archive")
		}
		out[f.Path] = Content{v, f.Role}
	}
	return out, nil
}

// Equal compares all delivery fields, including the archive encoding.
func (b *Bundle) Equal(o *Bundle) (bool, error) {
	if o == nil {
		return false, nil
	}
	if err := b.packed(); err != nil {
		return false, err
	}
	if err := o.packed(); err != nil {
		return false, err
	}
	return reflect.DeepEqual(b.manifest, o.manifest) && bytes.Equal(b.archive, o.archive) && b.sha256 == o.sha256 && b.configVersion == o.configVersion && reflect.DeepEqual(b.comparison, o.comparison), nil
}

func writeArchive(m Manifest, files map[string]Content) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	write := func(name string, data []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0600, Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0), Format: tar.FormatPAX}); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}
	meta, err := encodeJSON(m)
	if err != nil {
		return nil, err
	}
	if len(meta) > 4<<20 {
		return nil, preparation("Deployment manifest exceeds size limit")
	}
	if err = write("manifest.json", meta); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err = write("files/"+p, files[p].Data); err != nil {
			return nil, err
		}
	}
	if err = tw.Close(); err != nil {
		return nil, err
	}
	if err = gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func readArchive(m Manifest, archive []byte) (map[string]Content, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open deployment gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, MaxTotalBytes+16<<20))
	files := map[string]Content{}
	seenManifest := false
	seen := map[string]bool{}
	headers := 0
	var total int64
	roles := map[string]Role{}
	for _, f := range m.Files {
		roles[f.Path] = f.Role
	}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read deployment tar: %w", err)
		}
		headers++
		if headers > MaxFiles*2 {
			return nil, preparation("Too many deployment archive entries")
		}
		if h.Typeflag == tar.TypeDir {
			n := strings.TrimSuffix(h.Name, "/")
			if n == "files" {
				continue
			}
			if !strings.HasPrefix(n, "files/") {
				return nil, preparation("Invalid deployment archive directory")
			}
			if _, err := RelativePath(strings.TrimPrefix(n, "files/")); err != nil {
				return nil, err
			}
			continue
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			return nil, preparation("Deployment archive contains a non-regular file")
		}
		if seen[h.Name] {
			return nil, preparation("Duplicate deployment archive member")
		}
		seen[h.Name] = true
		limit := int64(MaxFileBytes)
		if h.Name == "manifest.json" {
			limit = 4 << 20
		}
		if h.Size < 0 || h.Size > limit {
			return nil, preparation("Deployment file exceeds size limit")
		}
		total += h.Size
		if total > MaxTotalBytes+4<<20 {
			return nil, preparation("Deployment exceeds size limit")
		}
		data, err := io.ReadAll(io.LimitReader(tr, limit+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) != h.Size {
			return nil, preparation("Incomplete deployment archive member")
		}
		if h.Name == "manifest.json" {
			var packed Manifest
			if err := json.Unmarshal(data, &packed); err != nil {
				return nil, fmt.Errorf("decode deployment manifest: %w", err)
			}
			if !reflect.DeepEqual(m, packed) {
				return nil, preparation("Deployment manifest does not match its archive")
			}
			seenManifest = true
			continue
		}
		if !strings.HasPrefix(h.Name, "files/") {
			return nil, preparation("Unexpected deployment archive member")
		}
		p := strings.TrimPrefix(h.Name, "files/")
		if _, err := canonical(p); err != nil {
			return nil, err
		}
		role, ok := roles[p]
		if !ok {
			return nil, preparation("Unlisted deployment archive member")
		}
		files[p] = Content{data, role}
		if len(files) > MaxFiles {
			return nil, preparation("Deployment exceeds size limit")
		}
	}
	if !seenManifest {
		return nil, preparation("Deployment archive has no manifest")
	}
	// Consume the gzip footer to reject truncation even after tar's end markers.
	if n, err := io.Copy(io.Discard, io.LimitReader(gz, (1<<20)+1)); err != nil || n > 1<<20 {
		if err == nil {
			return nil, preparation("Invalid deployment gzip trailer")
		}
		return nil, fmt.Errorf("read deployment gzip footer: %w", err)
	}
	if err := checkContents(m, files); err != nil {
		return nil, err
	}
	return files, nil
}
