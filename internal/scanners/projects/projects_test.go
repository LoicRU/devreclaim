package projects

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/LoicRU/devreclaim/internal/core"
)

var now = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

func newScanner() *Scanner {
	return &Scanner{Now: func() time.Time { return now }, InactiveAfter: 30 * day, Workers: 2}
}

// write creates files (slash separated relative path -> content) under root.
func write(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))

		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// age sets the modification time of everything under dir, dir included.
func age(t *testing.T, dir string, d time.Duration) {
	t.Helper()

	ts := now.Add(-d)

	err := filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		return os.Chtimes(p, ts, ts)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func touch(t *testing.T, path string, d time.Duration) {
	t.Helper()

	ts := now.Add(-d)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
}

func scan(t *testing.T, minSize int64, roots ...string) []core.Candidate {
	t.Helper()

	ch, err := newScanner().Scan(context.Background(), core.ScanOptions{Roots: roots, MinSize: minSize})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := core.Collect(ch)
	sort.Slice(got, func(i, j int) bool { return got[i].Label < got[j].Label })

	return got
}

func kb() string { return strings.Repeat("x", 1000) }

func TestNodeModulesWithLockfileIsReclaimable(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"proj/package.json":             "{}",
		"proj/package-lock.json":        "{}",
		"proj/src/main.js":              "x",
		"proj/node_modules/a/index.js":  kb(),
		"proj/node_modules/a/other.txt": "",
	})
	age(t, filepath.Join(root, "proj"), 60*day)

	got := scan(t, 0, root)
	if len(got) != 1 {
		t.Fatalf("expected 1 candidate, got %d: %+v", len(got), got)
	}

	c := got[0]
	if c.Kind != "node_modules" || c.Label != "proj/node_modules" || c.Path != filepath.Join(root, "proj", "node_modules") {
		t.Fatalf("unexpected identity: %+v", c)
	}

	if c.SizeBytes != 1000 || c.ReclaimBytes != 1000 || !c.Regenerable || c.Risk != core.RiskLow {
		t.Fatalf("unexpected sizes/risk: %+v", c)
	}

	if c.Action.Type != "delete-dir" || c.LastModified.IsZero() || c.Category != core.CategoryProjectArtifacts {
		t.Fatalf("unexpected action/category: %+v", c)
	}
}

func TestLabelIsRelativeToTheScanRoot(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"mono/api/package.json": "{}", "mono/api/yarn.lock": "", "mono/api/node_modules/a": kb(),
		"mono/web/package.json": "{}", "mono/web/yarn.lock": "", "mono/web/node_modules/a": kb(),
		"package.json": "{}", "yarn.lock": "", "node_modules/a": kb(),
	})
	age(t, root, 60*day)

	got := scan(t, 0, root)

	want := []string{filepath.Base(root) + "/node_modules", "mono/api/node_modules", "mono/web/node_modules"}
	if len(got) != len(want) {
		t.Fatalf("expected %d candidates, got %+v", len(want), got)
	}

	sort.Strings(want)

	for i, c := range got {
		if c.Label != want[i] {
			t.Fatalf("label %d: got %q, want %q", i, c.Label, want[i])
		}
	}
}

func TestNodeModulesWithoutLockfileIsNotCounted(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"proj/package.json":            "{}",
		"proj/node_modules/a/index.js": kb(),
	})
	age(t, filepath.Join(root, "proj"), 60*day)

	got := scan(t, 0, root)
	if len(got) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(got))
	}

	if got[0].ReclaimBytes != 0 || got[0].Regenerable || got[0].Risk != core.RiskMedium ||
		!strings.Contains(got[0].ReclaimNote, "no lockfile") {
		t.Fatalf("unexpected candidate: %+v", got[0])
	}
}

func TestActiveProjectIsNotCounted(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"proj/package.json":            "{}",
		"proj/yarn.lock":               "",
		"proj/node_modules/a/index.js": kb(),
	})
	age(t, filepath.Join(root, "proj"), 2*day)

	got := scan(t, 0, root)
	if len(got) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(got))
	}

	if got[0].ReclaimBytes != 0 || got[0].SizeBytes != 1000 || !strings.Contains(got[0].ReclaimNote, "active 2 days ago") {
		t.Fatalf("unexpected candidate: %+v", got[0])
	}
}

func TestMarkersAreRequired(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"a/node_modules/x/index.js": kb(), // no package.json
		"b/target/debug/bin":        kb(), // no Cargo.toml or pom.xml
		"c/.venv/lib/x":             kb(), // no pyvenv.cfg
		"d/build/out":               kb(), // no project file
		"e/bin/x":                   kb(), // no .csproj
		"f/.gradle/x":               kb(), // no build.gradle
	})

	if got := scan(t, 0, root); len(got) != 0 {
		t.Fatalf("expected nothing, got %+v", got)
	}
}

func TestKnownEcosystems(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  map[string]string // kind -> regen hint
	}{
		{
			name:  "rust",
			files: map[string]string{"p/Cargo.toml": "", "p/target/debug/a": kb()},
			want:  map[string]string{"target": "cargo build"},
		},
		{
			name:  "maven",
			files: map[string]string{"p/pom.xml": "", "p/target/classes/a": kb()},
			want:  map[string]string{"target": "mvn package"},
		},
		{
			name: "python",
			files: map[string]string{
				"p/requirements.txt": "", "p/.venv/pyvenv.cfg": "", "p/.venv/lib/a": kb(),
			},
			want: map[string]string{".venv": "recreate it from the requirements file"},
		},
		{
			name: "dotnet",
			files: map[string]string{
				"p/App.csproj": "", "p/bin/Debug/a": kb(), "p/obj/b": kb(),
			},
			want: map[string]string{"bin": "dotnet build", "obj": "dotnet build"},
		},
		{
			name:  "gradle",
			files: map[string]string{"p/build.gradle": "", "p/.gradle/caches/a": kb()},
			want:  map[string]string{".gradle": "gradle build"},
		},
		{
			name:  "pycache",
			files: map[string]string{"p/main.py": "", "p/__pycache__/a.pyc": kb()},
			want:  map[string]string{"__pycache__": "regenerated automatically by Python"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, tt.files)
			age(t, filepath.Join(root, "p"), 60*day)

			got := scan(t, 0, root)
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d candidates, got %d: %+v", len(tt.want), len(got), got)
			}

			for _, c := range got {
				hint, ok := tt.want[c.Kind]
				if !ok || c.RegenHint != hint || !c.Regenerable || c.ReclaimBytes != 1000 {
					t.Fatalf("unexpected candidate %+v", c)
				}
			}
		})
	}
}

func TestBuildFolderNeedsGitignore(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"loose/package.json": "{}", "loose/build/out.js": kb(),
		"ignored/package.json": "{}", "ignored/build/out.js": kb(), "ignored/.gitignore": "# build output\nbuild/\n",
	})
	age(t, root, 60*day)

	got := scan(t, 0, root)
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %+v", len(got), got)
	}

	// sorted by label: ignored/build, loose/build
	if got[0].ReclaimBytes != 1000 || !got[0].Regenerable {
		t.Fatalf("ignored build folder should be counted: %+v", got[0])
	}

	if got[1].ReclaimBytes != 0 || got[1].Risk != core.RiskMedium || !strings.Contains(got[1].ReclaimNote, ".gitignore") {
		t.Fatalf("tracked build folder must not be counted: %+v", got[1])
	}
}

func TestDoesNotWalkIntoArtifacts(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"proj/package.json":                               "{}",
		"proj/package-lock.json":                          "{}",
		"proj/node_modules/dep/package.json":              "{}",
		"proj/node_modules/dep/node_modules/inner/a.js":   kb(),
		"proj/node_modules/dep/package-lock.json":         "{}",
		"proj/node_modules/other/target/debug/a":          kb(),
		"proj/node_modules/other/Cargo.toml":              "",
		"proj/node_modules/other/node_modules/deep/b.js":  kb(),
		"proj/node_modules/other/node_modules/deep/p.txt": "",
	})
	age(t, filepath.Join(root, "proj"), 60*day)

	got := scan(t, 0, root)
	if len(got) != 1 || got[0].Label != "proj/node_modules" {
		t.Fatalf("expected only the outer node_modules, got %+v", got)
	}
}

func TestSkipsHiddenFolders(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		".cache/proj/package.json":             "{}",
		".cache/proj/node_modules/a":           kb(),
		"AppData/proj/package.json":            "{}",
		"AppData/proj/node_modules/a":          kb(),
		"visible/proj/package.json":            "{}",
		"visible/proj/package-lock.json":       "{}",
		"visible/proj/node_modules/a/index.js": kb(),
	})
	age(t, root, 60*day)

	got := scan(t, 0, root)
	if len(got) != 1 || got[0].Label != "visible/proj/node_modules" {
		t.Fatalf("expected only the visible project, got %+v", got)
	}
}

func TestMinSize(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"big/package.json": "{}", "big/yarn.lock": "", "big/node_modules/a": strings.Repeat("x", 2000),
		"small/package.json": "{}", "small/yarn.lock": "", "small/node_modules/a": strings.Repeat("x", 100),
	})
	age(t, root, 60*day)

	if got := scan(t, 0, root); len(got) != 2 {
		t.Fatalf("expected 2 candidates without a minimum, got %d", len(got))
	}

	got := scan(t, 500, root)
	if len(got) != 1 || got[0].Label != "big/node_modules" {
		t.Fatalf("expected only the big one, got %+v", got)
	}
}

func TestSymlinksAreNotFollowed(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")

	write(t, base, map[string]string{
		"outside/package.json":            "{}",
		"outside/yarn.lock":               "",
		"outside/node_modules/a/index.js": kb(),
	})
	age(t, outside, 60*day)

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}

	if got := scan(t, 0, root); len(got) != 0 {
		t.Fatalf("a symlink must not be followed, got %+v", got)
	}
}

func TestProjectActivityIgnoresArtifactsAndRootFolder(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"proj/package.json":            "{}",
		"proj/yarn.lock":               "",
		"proj/node_modules/a/index.js": kb(),
	})
	age(t, filepath.Join(root, "proj"), 60*day)

	// Installing dependencies touches the artifact and the project folder
	// itself, which is not a sign that someone works on the project.
	touch(t, filepath.Join(root, "proj", "node_modules", "a", "index.js"), day)
	touch(t, filepath.Join(root, "proj", "node_modules"), day)
	touch(t, filepath.Join(root, "proj"), day)

	got := scan(t, 0, root)
	if len(got) != 1 || got[0].ReclaimBytes != 1000 {
		t.Fatalf("project should still count as inactive: %+v", got)
	}
}

func TestGitIndexCountsAsActivity(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"proj/package.json":            "{}",
		"proj/yarn.lock":               "",
		"proj/.git/index":              "",
		"proj/node_modules/a/index.js": kb(),
	})
	age(t, filepath.Join(root, "proj"), 60*day)
	touch(t, filepath.Join(root, "proj", ".git", "index"), 3*day)

	got := scan(t, 0, root)
	if len(got) != 1 || got[0].ReclaimBytes != 0 || !strings.Contains(got[0].ReclaimNote, "active 3 days ago") {
		t.Fatalf("a recent git index means an active project: %+v", got)
	}
}

func TestScanRejectsBadRoots(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file.txt")
	write(t, filepath.Dir(file), map[string]string{"file.txt": "x"})

	for name, roots := range map[string][]string{
		"none":    nil,
		"missing": {filepath.Join(t.TempDir(), "nope")},
		"file":    {file},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := newScanner().Scan(context.Background(), core.ScanOptions{Roots: roots}); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestCancelledScanEndsWithoutHanging(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"proj/package.json": "{}", "proj/yarn.lock": "", "proj/node_modules/a": kb()})
	age(t, root, 60*day)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch, err := newScanner().Scan(ctx, core.ScanOptions{Roots: []string{root}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	done := make(chan struct{})

	go func() {
		core.Collect(ch)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scan did not stop after cancellation")
	}
}

func TestScannerMetadata(t *testing.T) {
	s := New()

	if s.Name() != "projects" || s.Category() != core.CategoryProjectArtifacts || s.InactiveAfter != 30*day || s.Workers < 2 {
		t.Fatalf("unexpected scanner: %+v", s)
	}
}
