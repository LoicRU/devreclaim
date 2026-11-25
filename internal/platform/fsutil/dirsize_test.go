package fsutil

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path string, size int) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDirSizeAddsUpNestedFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"), 100)
	write(t, filepath.Join(dir, "sub", "deep", "b.txt"), 250)

	if got := DirSize(context.Background(), dir); got != 350 {
		t.Fatalf("got %d, want 350", got)
	}
}

func TestDirSizeMissingFolderIsZero(t *testing.T) {
	if got := DirSize(context.Background(), filepath.Join(t.TempDir(), "nope")); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

func TestDirSizeDoesNotFollowSymlinks(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "outside", "big.bin"), 5000)
	write(t, filepath.Join(base, "dir", "small.txt"), 10)

	if err := os.Symlink(filepath.Join(base, "outside"), filepath.Join(base, "dir", "link")); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}

	if got := DirSize(context.Background(), filepath.Join(base, "dir")); got != 10 {
		t.Fatalf("got %d, want 10", got)
	}
}

func TestDirSizeStopsWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"), 100)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got := DirSize(ctx, dir); got != 0 {
		t.Fatalf("expected 0 after cancellation, got %d", got)
	}
}
