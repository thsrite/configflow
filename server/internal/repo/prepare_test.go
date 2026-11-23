package repo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareConfigReturnsDefaultView(t *testing.T) {
	r := newRepo(t)
	ctx := WithScope(WithRepository(context.Background(), r))
	defer ResetConfigContext(ctx)
	prepared, err := PrepareConfig(ctx)
	if err != nil || prepared == nil {
		t.Fatalf("prepare: document=%v, err=%v", prepared != nil, err)
	}
}

func TestResolveDataDirMatchesOpenWithoutOpening(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	actual := filepath.Join(home, "data")
	link := filepath.Join(home, "alias")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{actual, "~/data", link} {
		resolved, err := ResolveDataDir(path)
		if err != nil || resolved != actual {
			t.Fatalf("resolve %q: %q %v", path, resolved, err)
		}
		if _, err := os.Stat(actual); !os.IsNotExist(err) {
			t.Fatalf("resolution created storage: %v", err)
		}
	}
	r, err := OpenDefault("~/data")
	if err != nil || r.DataDir() != actual {
		t.Fatalf("Open resolved differently: %v", err)
	}
}
