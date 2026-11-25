package pkgcaches

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/LoicRU/devreclaim/internal/core"
	"github.com/LoicRU/devreclaim/internal/platform/exec"
	"github.com/LoicRU/devreclaim/internal/platform/fsutil"
)

// Scanner measures the global caches of package managers. It asks each tool
// where its cache is, falls back to the usual Windows folder, and never
// modifies anything.
type Scanner struct {
	Runner exec.CommandRunner
	Env    Env
	// Stat reports whether path is an existing folder.
	IsDir func(path string) bool
}

// New returns a Scanner that uses the real machine.
func New(runner exec.CommandRunner) *Scanner {
	return &Scanner{Runner: runner, Env: os.Getenv, IsDir: isDir}
}

func isDir(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}

// Name implements core.Scanner.
func (*Scanner) Name() string { return "package-caches" }

// Category implements core.Scanner.
func (*Scanner) Category() core.Category { return core.CategoryPackageCaches }

// Scan implements core.Scanner.
func (s *Scanner) Scan(ctx context.Context, opts core.ScanOptions) (<-chan core.Candidate, error) {
	ch := make(chan core.Candidate)

	var wg sync.WaitGroup

	for _, sp := range specs() {
		wg.Add(1)

		go func() {
			defer wg.Done()

			c, ok := s.inspect(ctx, sp, opts.MinSize)
			if !ok {
				return
			}

			select {
			case ch <- c:
			case <-ctx.Done():
			}
		}()
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return ch, nil
}

// locate returns the cache folder and whether the tool itself answered.
func (s *Scanner) locate(ctx context.Context, sp spec) (dir string, fromTool bool) {
	if len(sp.locate) > 0 {
		stdout, _, err := s.Runner.Run(ctx, sp.locate[0], sp.locate[1:]...)
		if err == nil {
			out := string(stdout)

			if sp.parse != nil {
				dir = sp.parse(out)
			} else {
				dir = lastLine(out)
			}

			if dir != "" {
				return dir, true
			}
		}
	}

	return sp.fallback(s.Env), false
}

func lastLine(out string) string {
	lines := strings.Split(strings.ReplaceAll(out, "\r", ""), "\n")

	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}

	return ""
}

func (s *Scanner) inspect(ctx context.Context, sp spec, minSize int64) (core.Candidate, bool) {
	dir, fromTool := s.locate(ctx, sp)
	if dir == "" || !s.IsDir(dir) {
		return core.Candidate{}, false
	}

	size := fsutil.DirSize(ctx, dir)
	if size == 0 || size < minSize || ctx.Err() != nil {
		return core.Candidate{}, false
	}

	c := core.Candidate{
		ID:          core.NewID(core.CategoryPackageCaches, sp.name),
		Category:    core.CategoryPackageCaches,
		Kind:        "package-cache",
		Path:        dir,
		Label:       sp.name,
		SizeBytes:   size,
		Regenerable: true,
		RegenHint:   sp.hint,
		Risk:        sp.risk,
		Reasons:     []string{"cache of a package manager, rebuilt on demand"},
		Action:      core.ActionSpec{Type: "delete-dir"},
	}

	if fromTool && len(sp.clean) > 0 {
		c.Action = core.ActionSpec{Type: "run-command", Command: sp.clean}
	}

	if !fromTool {
		c.Warnings = append(c.Warnings, "default folder, the tool did not answer: check the path before cleaning")
	}

	if sp.warning != "" {
		c.Warnings = append(c.Warnings, sp.warning)
	}

	if sp.counted {
		c.ReclaimBytes = size
	} else {
		c.ReclaimNote = sp.note
	}

	return c, true
}
