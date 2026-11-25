package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/LoicRU/devreclaim/internal/clean"
	"github.com/LoicRU/devreclaim/internal/core"
	"github.com/LoicRU/devreclaim/internal/safety"
)

type cleanEnv struct {
	root    string
	logDir  string
	trashed []string
	removed []string
}

// newCleanEnv creates a scan root with one real node_modules folder.
func newCleanEnv(t *testing.T) (*cleanEnv, core.Candidate) {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	nm := filepath.Join(root, "web", "node_modules")
	if err = os.MkdirAll(nm, 0o755); err != nil {
		t.Fatal(err)
	}

	c := core.Candidate{
		ID: "1", Category: core.CategoryProjectArtifacts, Kind: "node_modules", Path: nm,
		Label: "web/node_modules", SizeBytes: 2 << 30, ReclaimBytes: 2 << 30, Regenerable: true,
		Risk: core.RiskLow, Action: core.ActionSpec{Type: "delete-dir"},
	}

	return &cleanEnv{root: root, logDir: filepath.Join(t.TempDir(), "logs")}, c
}

func (e *cleanEnv) deps(candidates ...core.Candidate) cleanDeps {
	var got core.ScanOptions

	return cleanDeps{
		scanners: func() []core.Scanner { return []core.Scanner{spyScanner{got: &got, candidates: candidates}} },
		executor: func(roots []string, mode clean.Mode, j *clean.Journal) *clean.Executor {
			return &clean.Executor{
				Guard: safety.Guard{}, Roots: roots, Mode: mode, Journal: j,
				Trash:  func(p string) error { e.trashed = append(e.trashed, p); return os.RemoveAll(p) },
				Remove: func(p string) error { e.removed = append(e.removed, p); return os.RemoveAll(p) },
			}
		},
		logDir: func() string { return e.logDir },
		now:    func() time.Time { return time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC) },
	}
}

func runClean0(t *testing.T, d cleanDeps, stdin string, args ...string) (string, error) {
	t.Helper()

	root := &cobra.Command{Use: "devreclaim"}
	root.AddCommand(newCleanCmd(d))

	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"clean"}, args...))

	err := root.Execute()

	return out.String(), err
}

func TestCleanWithoutApplyIsADryRun(t *testing.T) {
	env, c := newCleanEnv(t)

	out, err := runClean0(t, env.deps(c), "", "--root", env.root)
	if err != nil {
		t.Fatal(err)
	}

	if len(env.trashed)+len(env.removed) != 0 {
		t.Fatal("a dry run must not remove anything")
	}

	if _, err = os.Stat(c.Path); err != nil {
		t.Fatal("the folder must still exist")
	}

	if _, err = os.Stat(env.logDir); err == nil {
		t.Fatal("a dry run must not even create the journal")
	}

	for _, want := range []string{"web/node_modules", "Dry run", "Recycle Bin"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestCleanApplyYesTrashesAndLogs(t *testing.T) {
	env, c := newCleanEnv(t)

	out, err := runClean0(t, env.deps(c), "", "--root", env.root, "--apply", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}

	if len(env.trashed) != 1 || len(env.removed) != 0 {
		t.Fatalf("trashed=%v removed=%v", env.trashed, env.removed)
	}

	data, err := os.ReadFile(filepath.Join(env.logDir, "2026-11-25.jsonl"))
	if err != nil || !strings.Contains(string(data), `"result":"done"`) {
		t.Fatalf("journal = %q (%v)", data, err)
	}

	if !strings.Contains(out, "Reclaimed (estimate): 2.0 GB") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestCleanDeleteFlagRemovesPermanently(t *testing.T) {
	env, c := newCleanEnv(t)

	if _, err := runClean0(t, env.deps(c), "", "--root", env.root, "--apply", "--yes", "--delete"); err != nil {
		t.Fatal(err)
	}

	if len(env.removed) != 1 || len(env.trashed) != 0 {
		t.Fatalf("trashed=%v removed=%v", env.trashed, env.removed)
	}
}

func TestCleanConfirmationDecides(t *testing.T) {
	env, c := newCleanEnv(t)

	out, err := runClean0(t, env.deps(c), "n\n", "--root", env.root, "--apply")
	if err != nil {
		t.Fatal(err)
	}

	if len(env.trashed) != 0 || !strings.Contains(out, "Cancelled") {
		t.Fatalf("answering no must cancel: %v\n%s", env.trashed, out)
	}

	if _, err = runClean0(t, env.deps(c), "y\n", "--root", env.root, "--apply"); err != nil {
		t.Fatal(err)
	}

	if len(env.trashed) != 1 {
		t.Fatal("answering yes must clean")
	}
}

func TestCleanEmptyAnswerCancels(t *testing.T) {
	env, c := newCleanEnv(t)

	if _, err := runClean0(t, env.deps(c), "", "--root", env.root, "--apply"); err != nil {
		t.Fatal(err)
	}

	if len(env.trashed) != 0 {
		t.Fatal("no answer must not clean")
	}
}

func TestCleanRejectsConflictingFlags(t *testing.T) {
	env, c := newCleanEnv(t)

	if _, err := runClean0(t, env.deps(c), "", "--trash", "--delete"); err == nil {
		t.Fatal("--trash with --delete must fail")
	}

	if _, err := runClean0(t, env.deps(c), "", "--json", "--apply"); err == nil {
		t.Fatal("--json --apply without --yes must fail")
	}

	if _, err := runClean0(t, env.deps(c), "", "--older-than", "soon"); err == nil {
		t.Fatal("a bad --older-than must fail")
	}
}

func TestCleanSkipsNotRebuildableAndReportsWhy(t *testing.T) {
	env, c := newCleanEnv(t)

	risky := c
	risky.Label, risky.Regenerable, risky.ReclaimBytes = "api/node_modules", false, 0

	out, err := runClean0(t, env.deps(c, risky), "", "--root", env.root)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "skipped api/node_modules") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestCleanNothingToClean(t *testing.T) {
	env, _ := newCleanEnv(t)

	out, err := runClean0(t, env.deps(), "", "--root", env.root, "--apply", "--yes")
	if err != nil || !strings.Contains(out, "Nothing to clean") {
		t.Fatalf("unexpected: %v\n%s", err, out)
	}
}

func TestCleanReportsFailuresWithAnError(t *testing.T) {
	env, c := newCleanEnv(t)

	c.Path = filepath.Join(env.root, "missing", "node_modules")

	out, err := runClean0(t, env.deps(c), "", "--root", env.root, "--apply", "--yes")
	if err == nil || !strings.Contains(out, "! web/node_modules") {
		t.Fatalf("expected a failure, got %v\n%s", err, out)
	}
}

func TestCleanJSONDryRun(t *testing.T) {
	env, c := newCleanEnv(t)

	out, err := runClean0(t, env.deps(c), "", "--root", env.root, "--json")
	if err != nil {
		t.Fatal(err)
	}

	var got cleanOutput
	if err = json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}

	if !got.DryRun || got.SchemaVersion != 1 || len(got.Plan.Items) != 1 || got.Mode != clean.ModeTrash {
		t.Fatalf("unexpected output: %+v", got)
	}
}

func TestParseAge(t *testing.T) {
	cases := map[string]time.Duration{
		"": 0, "30d": 30 * 24 * time.Hour, "2w": 14 * 24 * time.Hour, "36h": 36 * time.Hour,
	}

	for in, want := range cases {
		got, err := parseAge(in)
		if err != nil || got != want {
			t.Fatalf("parseAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}

	for _, bad := range []string{"d", "-3d", "abc", "1y"} {
		if _, err := parseAge(bad); err == nil {
			t.Fatalf("parseAge(%q) must fail", bad)
		}
	}
}
