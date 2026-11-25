package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/LoicRU/devreclaim/internal/core"
)

// spyScanner records the options it receives.
type spyScanner struct {
	got        *core.ScanOptions
	candidates []core.Candidate
	err        error
}

func (spyScanner) Name() string            { return "spy" }
func (spyScanner) Category() core.Category { return core.CategoryProjectArtifacts }

func (s spyScanner) Scan(_ context.Context, opts core.ScanOptions) (<-chan core.Candidate, error) {
	*s.got = opts

	if s.err != nil {
		return nil, s.err
	}

	ch := make(chan core.Candidate, len(s.candidates))
	for _, c := range s.candidates {
		ch <- c
	}

	close(ch)

	return ch, nil
}

func runScanCmd(t *testing.T, s spyScanner, args ...string) (string, error) {
	t.Helper()

	return runScanCmdWith(t, s, nil, args...)
}

func runScanCmdWith(t *testing.T, s spyScanner, extra []core.Scanner, args ...string) (string, error) {
	t.Helper()

	root := &cobra.Command{Use: "devreclaim"}
	root.AddCommand(newScanCmd(func() []core.Scanner { return append([]core.Scanner{s}, extra...) }))

	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(append([]string{"scan"}, args...))

	err := root.Execute()

	return out.String(), err
}

var nodeModules = core.Candidate{
	Label:        "web/node_modules",
	SizeBytes:    2 << 30,
	ReclaimBytes: 2 << 30,
}

func TestScanDefaultsToCurrentFolderAndMinSize(t *testing.T) {
	var got core.ScanOptions

	if _, err := runScanCmd(t, spyScanner{got: &got}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cwd, _ := os.Getwd()
	if len(got.Roots) != 1 || got.Roots[0] != cwd {
		t.Fatalf("expected the current folder %q, got %v", cwd, got.Roots)
	}

	if got.MinSize != 50<<20 {
		t.Fatalf("expected a 50MB default, got %d", got.MinSize)
	}
}

func TestScanPassesRootsAndMinSize(t *testing.T) {
	var got core.ScanOptions

	a, b := t.TempDir(), t.TempDir()

	if _, err := runScanCmd(t, spyScanner{got: &got}, "--root", a, "--root", b, "--min-size", "1GB"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got.Roots) != 2 || got.Roots[0] != a || got.Roots[1] != b || got.MinSize != 1<<30 {
		t.Fatalf("unexpected options: %+v", got)
	}
}

func TestScanMakesRootsAbsolute(t *testing.T) {
	var got core.ScanOptions

	if _, err := runScanCmd(t, spyScanner{got: &got}, "--root", "."); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !filepath.IsAbs(got.Roots[0]) {
		t.Fatalf("expected an absolute root, got %q", got.Roots[0])
	}
}

func TestScanText(t *testing.T) {
	var got core.ScanOptions

	out, err := runScanCmd(t, spyScanner{got: &got, candidates: []core.Candidate{nodeModules}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{"web/node_modules", "2.0 GB", "nothing was deleted"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestScanJSON(t *testing.T) {
	var got core.ScanOptions

	out, err := runScanCmd(t, spyScanner{got: &got, candidates: []core.Candidate{nodeModules}}, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rep struct {
		SchemaVersion int `json:"schemaVersion"`
		Summary       struct {
			ReclaimableBytes int64 `json:"reclaimableBytes"`
		} `json:"summary"`
	}

	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}

	if rep.SchemaVersion != 1 || rep.Summary.ReclaimableBytes != 2<<30 {
		t.Fatalf("unexpected report: %+v", rep)
	}
}

func TestScanWithoutResult(t *testing.T) {
	var got core.ScanOptions

	out, err := runScanCmd(t, spyScanner{got: &got}, "--min-size", "100MB")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "No artifact or cache of 100.0 MB or more") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestScanRejectsBadMinSize(t *testing.T) {
	var got core.ScanOptions

	_, err := runScanCmd(t, spyScanner{got: &got}, "--min-size", "lots")
	if err == nil || !strings.Contains(err.Error(), "--min-size") {
		t.Fatalf("expected a --min-size error, got %v", err)
	}
}

func TestScanReturnsScannerError(t *testing.T) {
	var got core.ScanOptions

	_, err := runScanCmd(t, spyScanner{got: &got, err: errors.New("boom")})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected the scanner error, got %v", err)
	}
}

// cacheScanner is a second scanner of another category.
type cacheScanner struct{ spyScanner }

func (cacheScanner) Name() string            { return "caches" }
func (cacheScanner) Category() core.Category { return core.CategoryPackageCaches }

func TestScanMergesScanners(t *testing.T) {
	var a, b core.ScanOptions

	npm := core.Candidate{Label: "npm", SizeBytes: 1 << 30, ReclaimBytes: 1 << 30}
	extra := []core.Scanner{cacheScanner{spyScanner{got: &b, candidates: []core.Candidate{npm}}}}

	out, err := runScanCmdWith(t, spyScanner{got: &a, candidates: []core.Candidate{nodeModules}}, extra)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{"web/node_modules", "npm"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestScanCategoryFiltersScanners(t *testing.T) {
	var a, b core.ScanOptions

	npm := core.Candidate{Label: "npm", SizeBytes: 1 << 30, ReclaimBytes: 1 << 30}
	extra := []core.Scanner{cacheScanner{spyScanner{got: &b, candidates: []core.Candidate{npm}}}}

	out, err := runScanCmdWith(t, spyScanner{got: &a, candidates: []core.Candidate{nodeModules}}, extra,
		"--category", "package-caches")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(out, "web/node_modules") || !strings.Contains(out, "npm") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestScanRejectsUnknownCategory(t *testing.T) {
	var got core.ScanOptions

	_, err := runScanCmd(t, spyScanner{got: &got}, "--category", "nope")
	if err == nil || !strings.Contains(err.Error(), "--category") {
		t.Fatalf("expected a --category error, got %v", err)
	}
}

func TestScanKeepsGoingWhenOneScannerFails(t *testing.T) {
	var a, b core.ScanOptions

	extra := []core.Scanner{cacheScanner{spyScanner{got: &b, err: errors.New("boom")}}}

	out, err := runScanCmdWith(t, spyScanner{got: &a, candidates: []core.Candidate{nodeModules}}, extra)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "web/node_modules") || !strings.Contains(out, "boom") {
		t.Fatalf("expected results and a warning:\n%s", out)
	}
}

func TestScanWSLFlagAddsTheWSLScanners(t *testing.T) {
	var a, b core.ScanOptions

	inside := core.Candidate{Label: "Ubuntu: npm", SizeBytes: 1 << 30, ReclaimBytes: 1 << 30}

	root := &cobra.Command{Use: "devreclaim"}
	root.AddCommand(newScanCmd(
		func() []core.Scanner { return []core.Scanner{spyScanner{got: &a}} },
		func() []core.Scanner {
			return []core.Scanner{cacheScanner{spyScanner{got: &b, candidates: []core.Candidate{inside}}}}
		},
	))

	run := func(args ...string) string {
		out := &bytes.Buffer{}
		root.SetOut(out)
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(append([]string{"scan"}, args...))

		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}

		return out.String()
	}

	if out := run(); strings.Contains(out, "Ubuntu: npm") {
		t.Fatalf("WSL must be off by default:\n%s", out)
	}

	if out := run("--wsl"); !strings.Contains(out, "Ubuntu: npm") {
		t.Fatalf("--wsl must add the WSL scanners:\n%s", out)
	}
}
