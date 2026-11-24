package rulefetch

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestReviewN89AllLegacyReceiptsBlockMigration(t *testing.T) {
	for _, kind := range []string{"source", "id", "name"} {
		t.Run(kind, func(t *testing.T) {
			ctx, r, rule, old, native := migrationFixture(t)
			candidate := old
			switch kind {
			case "id":
				candidate = filepath.Join(filepath.Dir(filepath.Dir(old)), "by-id", fmt.Sprintf("%x.list", sha256.Sum256([]byte("r1"))))
			case "name":
				candidate = filepath.Join(filepath.Dir(filepath.Dir(old)), "Synthetic.list")
			}
			if candidate != old {
				if err := os.MkdirAll(filepath.Dir(candidate), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(old, candidate); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(candidate+".invalid", []byte("pending edit"), 0600); err != nil {
				t.Fatal(err)
			}
			data, err := ReadCacheWithFormat(ctx, r, rule, nil, "yaml")
			if err != nil || data != nil {
				t.Fatalf("invalid %s cache returned %q: %v", kind, data, err)
			}
			if _, err := os.Stat(native); !os.IsNotExist(err) {
				t.Fatalf("invalid %s cache migrated: %v", kind, err)
			}
			// Prove that refusal is the receipt check, not an unreachable path.
			if err := os.Remove(candidate + ".invalid"); err != nil {
				t.Fatal(err)
			}
			data, err = ReadCacheWithFormat(ctx, r, rule, nil, "yaml")
			if err != nil || string(data) != "payload: [example.com]\n" {
				t.Fatalf("valid %s fallback lost: %q %v", kind, data, err)
			}
			if _, err := os.Stat(native); err != nil {
				t.Fatalf("valid %s cache not migrated: %v", kind, err)
			}
		})
	}
}

func TestReviewN89MigrationReadStaysInsideRulesDirectory(t *testing.T) {
	ctx, r, rule, old, native := migrationFixture(t)
	outside := filepath.Join(t.TempDir(), "outside.list")
	if err := os.Rename(old, outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, old); err != nil {
		t.Fatal(err)
	}
	data, err := ReadCacheWithFormat(ctx, r, rule, nil, "yaml")
	if data != nil {
		t.Fatalf("cache read followed escaping symlink: %q (%v)", data, err)
	}
	if _, err := os.Stat(native); !os.IsNotExist(err) {
		t.Fatalf("outside file migrated: %v", err)
	}
	data, err = os.ReadFile(outside)
	if err != nil || string(data) != "payload: [example.com]\n" {
		t.Fatalf("outside source changed: %q %v", data, err)
	}
}

func TestReviewN89ReceiptCannotBecomeReadableSentinel(t *testing.T) {
	ctx, r, rule, old, native := migrationFixture(t)
	// A receipt with the wrong filesystem type must still fail closed. The
	// former sentinel path was an ordinary readable child of this directory.
	if err := os.MkdirAll(native+".invalid", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(native+".invalid", "unavailable"), []byte("payload: [receipt.example]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := ReadCacheWithFormat(ctx, r, rule, nil, "yaml")
	if err != nil || data != nil {
		t.Fatalf("receipt supplied cache bytes: %q %v", data, err)
	}
	if _, err := os.Stat(native); !os.IsNotExist(err) {
		t.Fatal("receipt bytes migrated", err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("old fallback changed", err)
	}
}

func TestReviewN89DanglingReceiptStillBlocksCache(t *testing.T) {
	ctx, r, rule, _, native := migrationFixture(t)
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), native+".invalid"); err != nil {
		t.Fatal(err)
	}
	data, err := ReadCacheWithFormat(ctx, r, rule, nil, "yaml")
	if err != nil || data != nil {
		t.Fatalf("dangling invalid receipt was treated as absent: %q %v", data, err)
	}
	if _, err := os.Stat(native); !os.IsNotExist(err) {
		t.Fatal("invalid cache migrated", err)
	}
}
