package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/LoicRU/devreclaim/internal/buildinfo"
	"github.com/LoicRU/devreclaim/internal/core"
	"github.com/LoicRU/devreclaim/internal/report"
)

const defaultMinSize = "50MB"

func newScanCmd(newScanners func() []core.Scanner, wslScanners ...func() []core.Scanner) *cobra.Command {
	var (
		roots    []string
		minSize  string
		category string
		asJSON   bool
		withWSL  bool
	)

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Find build artifacts and package caches that can be rebuilt (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runScan(cmd, pickScanners(newScanners, wslScanners, withWSL), scanArgs{roots: roots, minSize: minSize, category: category, asJSON: asJSON})
		},
	}

	cmd.Flags().StringArrayVar(&roots, "root", nil, "folder to scan, repeatable (default: current folder)")
	cmd.Flags().StringVar(&minSize, "min-size", defaultMinSize, "hide results smaller than this, e.g. 100MB")
	cmd.Flags().StringVar(&category, "category", "",
		"only scan this category: project-artifacts or package-caches (default: all)")
	cmd.Flags().BoolVar(&withWSL, "wsl", false, "also look inside WSL distros (starts the distros that are stopped)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")

	return cmd
}

// pickScanners adds the scanners that look inside WSL distros when asked.
func pickScanners(base func() []core.Scanner, wsl []func() []core.Scanner, withWSL bool) []core.Scanner {
	scanners := base()

	if withWSL {
		for _, f := range wsl {
			scanners = append(scanners, f()...)
		}
	}

	return scanners
}

type scanArgs struct {
	roots    []string
	minSize  string
	category string
	asJSON   bool
}

func runScan(cmd *cobra.Command, scanners []core.Scanner, args scanArgs) error {
	minBytes, err := report.ParseSize(args.minSize)
	if err != nil {
		return fmt.Errorf("invalid --min-size: %w", err)
	}

	scanners, err = filterByCategory(scanners, args.category)
	if err != nil {
		return err
	}

	roots, err := resolveRoots(args.roots)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	candidates, warnings, err := scanAll(ctx, scanners, core.ScanOptions{Roots: roots, MinSize: minBytes})
	if err != nil {
		return err
	}

	rep := report.New(buildinfo.Version, time.Now().UTC(), candidates, warnings)
	out := cmd.OutOrStdout()

	if args.asJSON {
		return rep.WriteJSON(out)
	}

	if len(rep.Candidates) == 0 {
		_, err = fmt.Fprintf(out, "No artifact or cache of %s or more found.\n", report.HumanSize(minBytes))

		return err
	}

	if err = rep.WriteText(out); err != nil {
		return err
	}

	_, err = fmt.Fprintln(out, "\nRead-only scan: nothing was deleted. Sizes are logical sizes.")

	return err
}

// filterByCategory keeps the scanners of one category ("" keeps all).
func filterByCategory(scanners []core.Scanner, category string) ([]core.Scanner, error) {
	if category == "" {
		return scanners, nil
	}

	var known []string

	var out []core.Scanner

	for _, s := range scanners {
		known = append(known, string(s.Category()))

		if string(s.Category()) == category {
			out = append(out, s)
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("invalid --category %q (available: %s)", category, strings.Join(slices.Compact(known), ", "))
	}

	return out, nil
}

// scanAll runs the scanners side by side. A scanner that cannot start becomes a
// warning; the scan only fails when none of them could start.
func scanAll(ctx context.Context, scanners []core.Scanner, opts core.ScanOptions) ([]core.Candidate, []string, error) {
	var (
		channels []<-chan core.Candidate
		warnings []string
		errs     []error
	)

	for _, s := range scanners {
		ch, err := s.Scan(ctx, opts)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
			warnings = append(warnings, fmt.Sprintf("%s: %v", s.Name(), err))

			continue
		}

		channels = append(channels, ch)
	}

	if len(channels) == 0 && len(errs) > 0 {
		return nil, nil, errors.Join(errs...)
	}

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		all []core.Candidate
	)

	for _, ch := range channels {
		wg.Add(1)

		go func() {
			defer wg.Done()

			got := core.Collect(ch)

			mu.Lock()
			all = append(all, got...)
			mu.Unlock()
		}()
	}

	wg.Wait()

	return all, warnings, nil
}

// resolveRoots makes roots absolute and defaults to the current folder.
func resolveRoots(roots []string) ([]string, error) {
	if len(roots) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("current folder: %w", err)
		}

		return []string{cwd}, nil
	}

	abs := make([]string, 0, len(roots))

	for _, r := range roots {
		p, err := filepath.Abs(r)
		if err != nil {
			return nil, fmt.Errorf("root %q: %w", r, err)
		}

		abs = append(abs, p)
	}

	return abs, nil
}
