package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/LoicRU/devreclaim/internal/clean"
	"github.com/LoicRU/devreclaim/internal/core"
	"github.com/LoicRU/devreclaim/internal/report"
)

// cleanDeps are the parts of clean that touch the real machine.
type cleanDeps struct {
	scanners func() []core.Scanner
	executor func(roots []string, mode clean.Mode, journal *clean.Journal) *clean.Executor
	logDir   func() string
	now      func() time.Time
}

type cleanFlags struct {
	roots         []string
	minSize       string
	category      string
	olderThan     string
	apply         bool
	yes           bool
	trash         bool
	del           bool
	includeMedium bool
	asJSON        bool
}

func newCleanCmd(d cleanDeps) *cobra.Command {
	var f cleanFlags

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Reclaim rebuildable artifacts and caches (dry-run unless --apply)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runClean(cmd, d, f)
		},
	}

	fl := cmd.Flags()
	fl.StringArrayVar(&f.roots, "root", nil, "folder to scan, repeatable (default: current folder)")
	fl.StringVar(&f.minSize, "min-size", defaultMinSize, "ignore results smaller than this, e.g. 100MB")
	fl.StringVar(&f.category, "category", "", "only clean this category: project-artifacts or package-caches")
	fl.StringVar(&f.olderThan, "older-than", "", "only clean projects untouched for this long, e.g. 30d or 2w")
	fl.BoolVar(&f.apply, "apply", false, "really clean (without it, only show the plan)")
	fl.BoolVar(&f.yes, "yes", false, "do not ask for confirmation")
	fl.BoolVar(&f.trash, "trash", false, "send folders to the Recycle Bin (default)")
	fl.BoolVar(&f.del, "delete", false, "delete folders permanently instead of using the Recycle Bin")
	fl.BoolVar(&f.includeMedium, "include-medium-risk", false, "also clean items with a medium risk (Gradle, Maven...)")
	fl.BoolVar(&f.asJSON, "json", false, "print the plan and results as JSON")

	return cmd
}

// cleanOutput is the JSON contract of `clean --json`.
type cleanOutput struct {
	SchemaVersion int            `json:"schemaVersion"`
	DryRun        bool           `json:"dryRun"`
	Mode          clean.Mode     `json:"mode"`
	Plan          clean.Plan     `json:"plan"`
	Results       []clean.Result `json:"results"`
}

func runClean(cmd *cobra.Command, d cleanDeps, f cleanFlags) error {
	if f.trash && f.del {
		return errors.New("--trash and --delete cannot be used together")
	}

	if f.asJSON && f.apply && !f.yes {
		return errors.New("--json with --apply needs --yes: there is no one to answer the confirmation")
	}

	minBytes, err := report.ParseSize(f.minSize)
	if err != nil {
		return fmt.Errorf("invalid --min-size: %w", err)
	}

	olderThan, err := parseAge(f.olderThan)
	if err != nil {
		return fmt.Errorf("invalid --older-than: %w", err)
	}

	scanners, err := filterByCategory(d.scanners(), f.category)
	if err != nil {
		return err
	}

	roots, err := resolveRoots(f.roots)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	candidates, _, err := scanAll(ctx, scanners, core.ScanOptions{Roots: roots, MinSize: minBytes})
	if err != nil {
		return err
	}

	mode := clean.ModeTrash
	if f.del {
		mode = clean.ModeDelete
	}

	plan := clean.BuildPlan(candidates, clean.PlanOptions{
		OlderThan: olderThan, IncludeMedium: f.includeMedium, Now: d.now(),
	})

	out := cmd.OutOrStdout()

	if !f.asJSON {
		if err = writePlan(out, plan, mode); err != nil {
			return err
		}
	}

	if !f.apply || len(plan.Items) == 0 {
		return finishDryRun(out, f, plan, mode)
	}

	if !f.yes && !confirm(cmd, plan, mode) {
		_, err = fmt.Fprintln(out, "Cancelled: nothing was deleted.")

		return err
	}

	return execute(ctx, cmd, d, f, plan, roots, mode)
}

func finishDryRun(out io.Writer, f cleanFlags, plan clean.Plan, mode clean.Mode) error {
	if f.asJSON {
		return writeJSON(out, cleanOutput{SchemaVersion: 1, DryRun: true, Mode: mode, Plan: plan, Results: []clean.Result{}})
	}

	if len(plan.Items) == 0 {
		_, err := fmt.Fprintln(out, "Nothing to clean.")

		return err
	}

	_, err := fmt.Fprintln(out, "\nDry run: nothing was deleted. Add --apply to clean.")

	return err
}

func execute(ctx context.Context, cmd *cobra.Command, d cleanDeps, f cleanFlags, plan clean.Plan, roots []string, mode clean.Mode) error {
	out := cmd.OutOrStdout()

	journal, err := clean.OpenJournal(d.logDir(), d.now())
	if err != nil {
		return fmt.Errorf("cannot start cleaning without a journal: %w", err)
	}
	defer journal.Close()

	results, runErr := d.executor(roots, mode, journal).Run(ctx, plan.Items)

	failed := 0

	var reclaimed int64

	for _, r := range results {
		if r.Status == clean.StatusFailed {
			failed++
		} else {
			reclaimed += r.Bytes
		}
	}

	if f.asJSON {
		if err = writeJSON(out, cleanOutput{SchemaVersion: 1, Mode: mode, Plan: plan, Results: results}); err != nil {
			return err
		}
	} else {
		writeResults(out, results, reclaimed, journal.Path())
	}

	if runErr != nil {
		return runErr
	}

	if failed > 0 {
		return fmt.Errorf("%d item(s) could not be cleaned (see the journal)", failed)
	}

	return nil
}

func actionLabel(c core.Candidate, mode clean.Mode) string {
	if c.Action.Type == "run-command" {
		return strings.Join(c.Action.Command, " ")
	}

	if mode == clean.ModeDelete {
		return "delete permanently"
	}

	return "Recycle Bin"
}

func writePlan(out io.Writer, plan clean.Plan, mode clean.Mode) error {
	if len(plan.Items) > 0 {
		tw := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
		fmt.Fprintln(tw, "NAME\tRECLAIMABLE (est.)\tRISK\tACTION")

		for _, c := range plan.Items {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.Label, report.HumanSize(c.ReclaimBytes), c.Risk, actionLabel(c, mode))
		}

		if err := tw.Flush(); err != nil {
			return err
		}

		fmt.Fprintf(out, "\n%d item(s), %s reclaimable (estimate)\n", len(plan.Items), report.HumanSize(plan.ReclaimBytes))
	}

	for _, s := range plan.Skipped {
		fmt.Fprintf(out, "- skipped %s: %s\n", s.Label, s.Reason)
	}

	return nil
}

func writeResults(out io.Writer, results []clean.Result, reclaimed int64, journalPath string) {
	fmt.Fprintln(out)

	for _, r := range results {
		if r.Status == clean.StatusFailed {
			fmt.Fprintf(out, "! %s: %s\n", r.Label, r.Error)
		} else {
			fmt.Fprintf(out, "ok %s (%s)\n", r.Label, report.HumanSize(r.Bytes))
		}
	}

	fmt.Fprintf(out, "\nReclaimed (estimate): %s\n", report.HumanSize(reclaimed))

	if journalPath != "" {
		fmt.Fprintf(out, "Journal: %s\n", journalPath)
	}
}

func confirm(cmd *cobra.Command, plan clean.Plan, mode clean.Mode) bool {
	where := "to the Recycle Bin"
	if mode == clean.ModeDelete {
		where = "PERMANENTLY (no way back)"
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nClean %d item(s), %s, %s? [y/N] ",
		len(plan.Items), report.HumanSize(plan.ReclaimBytes), where)

	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))

	return answer == "y" || answer == "yes"
}

func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")

	return enc.Encode(v)
}

// parseAge reads durations like "30d", "2w" or anything time.ParseDuration accepts.
func parseAge(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	unit := time.Duration(0)

	switch {
	case strings.HasSuffix(s, "d"):
		unit = 24 * time.Hour
	case strings.HasSuffix(s, "w"):
		unit = 7 * 24 * time.Hour
	}

	if unit == 0 {
		d, err := time.ParseDuration(s)
		if err != nil || d < 0 {
			return 0, fmt.Errorf("%q is not a duration like 30d or 2w", s)
		}

		return d, nil
	}

	n, err := strconv.Atoi(strings.TrimSpace(s[:len(s)-1]))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%q is not a duration like 30d or 2w", s)
	}

	return time.Duration(n) * unit, nil
}
