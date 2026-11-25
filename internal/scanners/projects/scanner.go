package projects

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/LoicRU/devreclaim/internal/core"
)

const (
	defaultInactiveAfter = 30 * 24 * time.Hour
	activityMaxDepth     = 3
	hardlinkNote         = "logical size; pnpm/uv hardlinks may lower the real gain"
)

// Scanner lists rebuildable artifacts under the scan roots. It never modifies
// anything.
//
// An artifact only counts as reclaimable when it can be rebuilt (lockfile,
// requirements file, ignored by git...) and its project has been untouched for
// InactiveAfter. Project activity is estimated from the newest file found in
// the first levels of the project plus the git index, not from a full walk.
type Scanner struct {
	Now           func() time.Time
	InactiveAfter time.Duration
	Workers       int
}

// New returns a Scanner with the default 30 day inactivity threshold.
func New() *Scanner {
	return &Scanner{
		Now:           time.Now,
		InactiveAfter: defaultInactiveAfter,
		Workers:       runtime.NumCPU() * 2,
	}
}

// Name implements core.Scanner.
func (*Scanner) Name() string { return "projects" }

// Category implements core.Scanner.
func (*Scanner) Category() core.Category { return core.CategoryProjectArtifacts }

// Scan implements core.Scanner.
func (s *Scanner) Scan(ctx context.Context, opts core.ScanOptions) (<-chan core.Candidate, error) {
	if len(opts.Roots) == 0 {
		return nil, errors.New("no scan root given")
	}

	for _, root := range opts.Roots {
		info, err := os.Stat(root)
		if err != nil {
			return nil, fmt.Errorf("scan root: %w", err)
		}

		if !info.IsDir() {
			return nil, fmt.Errorf("scan root %q is not a folder", root)
		}
	}

	artifacts := s.discover(ctx, opts.Roots)
	out := make(chan core.Candidate)

	go s.measure(ctx, artifacts, opts.MinSize, out)

	return out, nil
}

// discover walks the roots and returns every artifact folder, without
// measuring it. Unreadable folders are skipped.
func (s *Scanner) discover(ctx context.Context, roots []string) []found {
	var (
		list []found
		seen = map[string]bool{}
	)

	for _, root := range roots {
		s.walk(ctx, root, root, seen, &list)
	}

	return list
}

func (s *Scanner) walk(ctx context.Context, root, dir string, seen map[string]bool, list *[]found) {
	if ctx.Err() != nil {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	siblings := make(map[string]bool, len(entries))
	for _, e := range entries {
		siblings[e.Name()] = true
	}

	for _, e := range entries {
		// IsDir is false for symlinks and junctions, so they are never followed.
		if !e.IsDir() {
			continue
		}

		name := e.Name()
		path := filepath.Join(dir, name)

		if f, ok := classify(dir, name, siblings); ok {
			if !seen[path] {
				seen[path] = true
				f.Path, f.ProjectDir, f.Root = path, dir, root
				*list = append(*list, f)
			}

			continue // never walk into an artifact
		}

		if skipDir(name) {
			continue
		}

		s.walk(ctx, root, path, seen, list)
	}
}

// measure sizes the artifacts with a pool of workers and sends the candidates
// to out, which it closes at the end.
func (s *Scanner) measure(ctx context.Context, list []found, minSize int64, out chan<- core.Candidate) {
	defer close(out)

	jobs := make(chan found)

	var wg sync.WaitGroup

	for range max(s.Workers, 1) {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for f := range jobs {
				size := dirSize(ctx, f.Path)
				if size == 0 || size < minSize {
					continue
				}

				select {
				case out <- s.candidate(f, size):
				case <-ctx.Done():
					return
				}
			}
		}()
	}

feed:
	for _, f := range list {
		select {
		case jobs <- f:
		case <-ctx.Done():
			break feed
		}
	}

	close(jobs)
	wg.Wait()
}

func (s *Scanner) candidate(f found, size int64) core.Candidate {
	now := s.Now()
	activity := projectActivity(f.ProjectDir)

	c := core.Candidate{
		ID:           core.NewID(core.CategoryProjectArtifacts, f.Path),
		Category:     core.CategoryProjectArtifacts,
		Kind:         f.Kind,
		Path:         f.Path,
		Label:        label(f),
		SizeBytes:    size,
		LastModified: activity,
		Regenerable:  f.Regenerable,
		RegenHint:    f.RegenHint,
		Risk:         f.Risk,
		Action:       core.ActionSpec{Type: "delete-dir"},
	}

	if f.Regenerable {
		c.Reasons = append(c.Reasons, "can be rebuilt: "+f.RegenHint)
	}

	switch {
	case !f.Regenerable:
		c.ReclaimNote = f.NotCounted
	case activity.IsZero():
		c.ReclaimNote = "cannot tell if the project is active: not counted"
	case now.Sub(activity) < s.InactiveAfter:
		c.ReclaimNote = fmt.Sprintf("project active %d days ago: not counted", days(now.Sub(activity)))
	default:
		c.ReclaimBytes = size
		c.ReclaimNote = hardlinkNote
		c.Reasons = append(c.Reasons, fmt.Sprintf("project untouched for %d days", days(now.Sub(activity))))
	}

	return c
}

func days(d time.Duration) int { return int(d.Hours() / 24) }

// label names an artifact by its project path relative to the scan root, so
// that two projects both called "api" can be told apart.
func label(f found) string {
	rel, err := filepath.Rel(f.Root, f.ProjectDir)
	if err != nil || rel == "." {
		rel = filepath.Base(f.ProjectDir)
	}

	return filepath.ToSlash(rel) + "/" + f.Kind
}

// dirSize adds up the size of the regular files under dir without following
// symlinks. Unreadable entries are ignored.
func dirSize(ctx context.Context, dir string) int64 {
	var total int64

	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}

			return nil
		}

		if !d.Type().IsRegular() {
			return nil
		}

		if info, infoErr := d.Info(); infoErr == nil {
			total += info.Size()
		}

		return nil
	})

	return total
}

// projectActivity returns the newest modification time found in the first
// levels of dir, ignoring artifact folders, plus the git index. The folder dir
// itself is ignored because installing dependencies changes its mtime.
func projectActivity(dir string) time.Time {
	var newest time.Time

	bump := func(t time.Time) {
		if t.After(newest) {
			newest = t
		}
	}

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}

			return nil
		}

		if path == dir {
			return nil
		}

		if d.IsDir() {
			rel, relErr := filepath.Rel(dir, path)
			if relErr != nil || artifactNames[d.Name()] || d.Name() == ".git" ||
				strings.Count(rel, string(filepath.Separator)) >= activityMaxDepth {
				return fs.SkipDir
			}
		}

		if info, infoErr := d.Info(); infoErr == nil {
			bump(info.ModTime())
		}

		return nil
	})

	for _, name := range []string{"index", "HEAD"} {
		if info, err := os.Stat(filepath.Join(dir, ".git", name)); err == nil {
			bump(info.ModTime())
		}
	}

	return newest
}
