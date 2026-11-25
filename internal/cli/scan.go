package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/LoicRU/devreclaim/internal/buildinfo"
	"github.com/LoicRU/devreclaim/internal/core"
	"github.com/LoicRU/devreclaim/internal/report"
)

const defaultMinSize = "50MB"

func newScanCmd(newScanner func() core.Scanner) *cobra.Command {
	var (
		roots   []string
		minSize string
		asJSON  bool
	)

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Find project build artifacts that can be rebuilt (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runScan(cmd, newScanner(), roots, minSize, asJSON)
		},
	}

	cmd.Flags().StringArrayVar(&roots, "root", nil, "folder to scan, repeatable (default: current folder)")
	cmd.Flags().StringVar(&minSize, "min-size", defaultMinSize, "hide artifacts smaller than this, e.g. 100MB")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")

	return cmd
}

func runScan(cmd *cobra.Command, scanner core.Scanner, roots []string, minSize string, asJSON bool) error {
	minBytes, err := report.ParseSize(minSize)
	if err != nil {
		return fmt.Errorf("invalid --min-size: %w", err)
	}

	roots, err = resolveRoots(roots)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	ch, err := scanner.Scan(ctx, core.ScanOptions{Roots: roots, MinSize: minBytes})
	if err != nil {
		return err
	}

	rep := report.New(buildinfo.Version, time.Now().UTC(), core.Collect(ch), nil)
	out := cmd.OutOrStdout()

	if asJSON {
		return rep.WriteJSON(out)
	}

	if len(rep.Candidates) == 0 {
		_, err = fmt.Fprintf(out, "No rebuildable artifact of %s or more found in %v.\n", report.HumanSize(minBytes), roots)

		return err
	}

	if err = rep.WriteText(out); err != nil {
		return err
	}

	_, err = fmt.Fprintln(out, "\nRead-only scan: nothing was deleted. Sizes are logical sizes.")

	return err
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
