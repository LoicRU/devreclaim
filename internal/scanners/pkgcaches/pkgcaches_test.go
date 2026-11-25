package pkgcaches

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LoicRU/devreclaim/internal/core"
)

type fakeRunner struct {
	answers map[string]string // "name arg arg" -> stdout
}

func (f fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	if out, ok := f.answers[key]; ok {
		return []byte(out), nil, nil
	}

	return nil, nil, errors.New("command not found")
}

func fill(t *testing.T, dir string, size int) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "data.bin"), []byte(strings.Repeat("x", size)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newScanner(runner fakeRunner, env map[string]string) *Scanner {
	return &Scanner{
		Runner: runner,
		Env:    func(k string) string { return env[k] },
		IsDir: func(p string) bool {
			info, err := os.Stat(p)

			return err == nil && info.IsDir()
		},
	}
}

func scan(t *testing.T, s *Scanner, minSize int64) map[string]core.Candidate {
	t.Helper()

	ch, err := s.Scan(context.Background(), core.ScanOptions{MinSize: minSize})
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]core.Candidate{}
	for _, c := range core.Collect(ch) {
		got[c.Label] = c
	}

	return got
}

func TestToolAnswerGivesRunCommandAction(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "npm-cache")
	fill(t, dir, 1000)

	s := newScanner(fakeRunner{answers: map[string]string{"npm config get cache": dir + "\n"}}, nil)
	c, ok := scan(t, s, 0)["npm"]

	if !ok {
		t.Fatal("npm cache not found")
	}

	if c.SizeBytes != 1000 || c.ReclaimBytes != 1000 {
		t.Fatalf("size/reclaim = %d/%d", c.SizeBytes, c.ReclaimBytes)
	}

	if c.Action.Type != "run-command" || strings.Join(c.Action.Command, " ") != "npm cache clean --force" {
		t.Fatalf("action = %+v", c.Action)
	}

	if len(c.Warnings) != 0 {
		t.Fatalf("unexpected warnings %v", c.Warnings)
	}
}

func TestFallbackToDefaultFolderWarnsAndDeletesDir(t *testing.T) {
	local := t.TempDir()
	fill(t, filepath.Join(local, "pip", "Cache"), 500)

	s := newScanner(fakeRunner{}, map[string]string{"LOCALAPPDATA": local})
	c, ok := scan(t, s, 0)["pip"]

	if !ok {
		t.Fatal("pip cache not found")
	}

	if c.Action.Type != "delete-dir" {
		t.Fatalf("action = %+v", c.Action)
	}

	if len(c.Warnings) == 0 {
		t.Fatal("expected a warning about the default folder")
	}
}

func TestPnpmStoreIsShownButNotCounted(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	fill(t, dir, 4000)

	s := newScanner(fakeRunner{answers: map[string]string{"pnpm store path": dir}}, nil)
	c, ok := scan(t, s, 0)["pnpm store"]

	if !ok {
		t.Fatal("pnpm store not found")
	}

	if c.SizeBytes != 4000 || c.ReclaimBytes != 0 || c.ReclaimNote == "" {
		t.Fatalf("candidate = %+v", c)
	}
}

func TestNuGetOutputIsParsed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "packages")
	fill(t, dir, 300)

	out := "info : global-packages: " + dir + string(os.PathSeparator) + "\r\n"
	s := newScanner(fakeRunner{answers: map[string]string{
		"dotnet nuget locals global-packages --list": out,
	}}, nil)

	if _, ok := scan(t, s, 0)["NuGet packages"]; !ok {
		t.Fatal("NuGet cache not found")
	}
}

func TestCargoUsesCargoHome(t *testing.T) {
	home := t.TempDir()
	fill(t, filepath.Join(home, "registry"), 700)
	fill(t, filepath.Join(home, "git"), 200)

	got := scan(t, newScanner(fakeRunner{}, map[string]string{"CARGO_HOME": home}), 0)

	if got["cargo registry"].SizeBytes != 700 || got["cargo git"].SizeBytes != 200 {
		t.Fatalf("got %+v", got)
	}

	if len(got["cargo registry"].Warnings) != 0 {
		t.Fatalf("a cache without locate command must not warn: %v", got["cargo registry"].Warnings)
	}
}

func TestMediumRiskCachesCarryAWarning(t *testing.T) {
	home := t.TempDir()
	fill(t, filepath.Join(home, ".gradle", "caches"), 100)
	fill(t, filepath.Join(home, ".m2", "repository"), 100)

	got := scan(t, newScanner(fakeRunner{}, map[string]string{"USERPROFILE": home}), 0)

	for _, name := range []string{"Gradle caches", "Maven repository"} {
		c, ok := got[name]
		if !ok {
			t.Fatalf("%s not found", name)
		}

		if c.Risk != core.RiskMedium || len(c.Warnings) != 1 {
			t.Fatalf("%s: risk %s warnings %v", name, c.Risk, c.Warnings)
		}
	}
}

func TestMissingEmptyAndSmallCachesAreSkipped(t *testing.T) {
	home := t.TempDir()
	fill(t, filepath.Join(home, ".cargo", "registry"), 100)
	if err := os.MkdirAll(filepath.Join(home, ".cargo", "git"), 0o755); err != nil {
		t.Fatal(err)
	}

	s := newScanner(fakeRunner{}, map[string]string{"USERPROFILE": home})

	got := scan(t, s, 0)
	if _, ok := got["cargo git"]; ok {
		t.Fatal("empty cache must be skipped")
	}

	if _, ok := got["cargo registry"]; !ok {
		t.Fatal("cargo registry expected")
	}

	if len(scan(t, s, 1000)) != 0 {
		t.Fatal("caches below min size must be skipped")
	}
}

func TestMetadata(t *testing.T) {
	s := New(fakeRunner{})
	if s.Name() != "package-caches" || s.Category() != core.CategoryPackageCaches {
		t.Fatal("wrong metadata")
	}
}

func TestLinuxModeUsesHomeFoldersPrefixAndInjectedMeasure(t *testing.T) {
	s := &Scanner{
		Runner: fakeRunner{},
		Env:    func(k string) string { return map[string]string{"HOME": "/home/me"}[k] },
		IsDir:  func(p string) bool { return p == "/home/me/.npm" || p == "/home/me/.cargo/registry" },
		DirSize: func(_ context.Context, dir string) int64 {
			if dir == "/home/me/.npm" {
				return 5000
			}

			return 300
		},
		Linux: true, Prefix: "Ubuntu: ", CommandOnly: true,
	}

	got := scan(t, s, 0)

	npm, ok := got["Ubuntu: npm"]
	if !ok || npm.SizeBytes != 5000 || npm.Path != "/home/me/.npm" {
		t.Fatalf("unexpected npm candidate: %+v", got)
	}

	if npm.Action.Type != "" {
		t.Fatalf("with CommandOnly a default folder must not get a delete action: %+v", npm.Action)
	}

	if _, ok = got["Ubuntu: cargo registry"]; !ok {
		t.Fatalf("cargo registry not found: %v", got)
	}

	if _, ok = got["Ubuntu: Windows only"]; ok {
		t.Fatal("unexpected entry")
	}
}

func TestWrapCommandRewritesTheCleaningCommand(t *testing.T) {
	s := &Scanner{
		Runner:  fakeRunner{answers: map[string]string{"npm config get cache": "/home/me/.npm\n"}},
		Env:     func(string) string { return "" },
		IsDir:   func(string) bool { return true },
		DirSize: func(context.Context, string) int64 { return 100 },
		Linux:   true, CommandOnly: true,
		WrapCommand: func(c []string) []string { return append([]string{"wsl.exe", "-d", "U", "--"}, c...) },
	}

	npm := scan(t, s, 0)["npm"]

	if npm.Action.Type != "run-command" || strings.Join(npm.Action.Command, " ") != "wsl.exe -d U -- npm cache clean --force" {
		t.Fatalf("unexpected action: %+v", npm.Action)
	}
}
