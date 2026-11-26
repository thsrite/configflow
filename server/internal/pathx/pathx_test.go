package pathx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExistsAndAccessible(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "present.txt")
	if err := os.WriteFile(file, []byte("ok"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	validLink := filepath.Join(dir, "valid-link")
	if err := os.Symlink(file, validLink); err != nil {
		t.Fatalf("seed valid symlink: %v", err)
	}
	danglingLink := filepath.Join(dir, "dangling-link")
	if err := os.Symlink(filepath.Join(dir, "gone"), danglingLink); err != nil {
		t.Fatalf("seed dangling symlink: %v", err)
	}

	tests := []struct {
		name           string
		path           string
		wantExists     bool
		wantErr        bool
		wantAccessible bool
	}{
		{"regular file", file, true, false, true},
		{"directory", sub, true, false, true},
		{"valid symlink", validLink, true, false, true},
		{"dangling symlink", danglingLink, false, false, false},
		{"missing path", filepath.Join(dir, "missing"), false, false, false},
		{"embedded nul byte", filepath.Join(dir, "bad\x00name"), false, true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Exists(tc.path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Exists(%q) err = %v, wantErr %v", tc.path, err, tc.wantErr)
			}
			if got != tc.wantExists {
				t.Fatalf("Exists(%q) = %v, want %v", tc.path, got, tc.wantExists)
			}
			if acc := Accessible(tc.path); acc != tc.wantAccessible {
				t.Fatalf("Accessible(%q) = %v, want %v", tc.path, acc, tc.wantAccessible)
			}
		})
	}
}
