package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/LoicRU/devreclaim/internal/clean"
	"github.com/LoicRU/devreclaim/internal/core"
	"github.com/LoicRU/devreclaim/internal/platform/exec"
	"github.com/LoicRU/devreclaim/internal/platform/winfile"
	"github.com/LoicRU/devreclaim/internal/report"
	"github.com/LoicRU/devreclaim/internal/scanners/wsl"
	"github.com/LoicRU/devreclaim/internal/wslcompact"
)

type wslCompactDeps struct {
	distros   func(ctx context.Context) ([]wsl.Distro, error)
	compactor func() *wslcompact.Compactor
	logDir    func() string
	now       func() time.Time
}

type wslCompactFlags struct {
	distro string
	all    bool
	method string
	apply  bool
	yes    bool
}

func newWSLCompactCmd(d wslCompactDeps) *cobra.Command {
	var f wslCompactFlags

	cmd := &cobra.Command{
		Use:   "compact",
		Short: "Shrink the virtual disk of WSL2 distros (dry-run unless --apply, needs administrator)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWSLCompact(cmd, d, f)
		},
	}

	fl := cmd.Flags()
	fl.StringVar(&f.distro, "distro", "", "name of the distro to compact")
	fl.BoolVar(&f.all, "all", false, "compact every WSL2 distro")
	fl.StringVar(&f.method, "method", "auto", "auto, optimize-vhd or diskpart")
	fl.BoolVar(&f.apply, "apply", false, "really compact (without it, only show the plan)")
	fl.BoolVar(&f.yes, "yes", false, "do not ask for confirmation")

	return cmd
}

func runWSLCompact(cmd *cobra.Command, d wslCompactDeps, f wslCompactFlags) error {
	if (f.distro == "") == !f.all {
		return errors.New("choose what to compact: --distro <name> or --all")
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	out := cmd.OutOrStdout()
	comp := d.compactor()

	plans, err := buildCompactPlans(ctx, d, comp, f, out)
	if err != nil {
		return err
	}

	if len(plans) == 0 {
		_, err = fmt.Fprintln(out, "Nothing to compact.")

		return err
	}

	if !f.apply {
		_, err = fmt.Fprintln(out, "\nDry run: nothing was changed. Add --apply, from a terminal opened as administrator.")

		return err
	}

	if !comp.IsAdmin() {
		return &ExitError{Code: ExitCodeNeedsAdmin, Err: wslcompact.ErrNeedsAdmin}
	}

	if !f.yes && !askYes(cmd, compactPrompt(plans)) {
		_, err = fmt.Fprintln(out, "Cancelled: nothing was changed.")

		return err
	}

	return applyCompaction(ctx, d, comp, plans, out)
}

func buildCompactPlans(ctx context.Context, d wslCompactDeps, comp *wslcompact.Compactor,
	f wslCompactFlags, out io.Writer,
) ([]wslcompact.Plan, error) {
	distros, err := d.distros(ctx)
	if err != nil {
		return nil, err
	}

	var targets []wsl.Distro

	for _, dist := range distros {
		if f.all || strings.EqualFold(dist.Name, f.distro) {
			targets = append(targets, dist)
		}
	}

	if len(targets) == 0 {
		names := make([]string, 0, len(distros))
		for _, dist := range distros {
			names = append(names, dist.Name)
		}

		return nil, fmt.Errorf("no distro named %q (found: %s)", f.distro, strings.Join(names, ", "))
	}

	var plans []wslcompact.Plan

	for _, dist := range targets {
		p, perr := comp.Prepare(ctx, dist, wslcompact.Method(f.method))
		if perr != nil {
			if !f.all {
				return nil, perr
			}

			fmt.Fprintf(out, "- skipped %s: %v\n", dist.Name, perr)

			continue
		}

		printCompactPlan(out, p)

		plans = append(plans, p)
	}

	return plans, nil
}

func printCompactPlan(out io.Writer, p wslcompact.Plan) {
	fmt.Fprintf(out, "\n%s\n  disk:   %s (%s)\n  method: %s\n", p.Name, p.VHDX, report.HumanSize(p.SizeBefore), p.Method)

	for i, s := range p.Steps {
		fmt.Fprintf(out, "  %d. %s\n", i+1, s)
	}

	for _, w := range p.Warnings {
		fmt.Fprintf(out, "  ! %s\n", w)
	}
}

func compactPrompt(plans []wslcompact.Plan) string {
	names := make([]string, 0, len(plans))
	for _, p := range plans {
		names = append(names, p.Name)
	}

	return fmt.Sprintf("\nThis stops ALL WSL distros (wsl --shutdown): save your work in WSL first.\n"+
		"A backup is advised: wsl --export %s backup.tar\n"+
		"Compact %s? [y/N] ", names[0], strings.Join(names, ", "))
}

func askYes(cmd *cobra.Command, prompt string) bool {
	fmt.Fprint(cmd.OutOrStdout(), prompt)

	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))

	return answer == "y" || answer == "yes"
}

func applyCompaction(ctx context.Context, d wslCompactDeps, comp *wslcompact.Compactor,
	plans []wslcompact.Plan, out io.Writer,
) error {
	journal, err := clean.OpenJournal(d.logDir(), d.now())
	if err != nil {
		return fmt.Errorf("cannot start compaction without a journal: %w", err)
	}
	defer journal.Close()

	failed := 0

	for _, p := range plans {
		res, runErr := comp.Run(ctx, p, func(s string) { fmt.Fprintf(out, "  %s: %s...\n", p.Name, s) })

		if errors.Is(runErr, wslcompact.ErrNeedsAdmin) {
			return &ExitError{Code: ExitCodeNeedsAdmin, Err: runErr}
		}

		writeCompactResult(out, p, res, runErr)
		logCompaction(journal, d.now(), p, res, runErr)

		if runErr != nil {
			failed++
		}
	}

	if journal.Path() != "" {
		fmt.Fprintf(out, "\nJournal: %s\n", journal.Path())
	}

	if failed > 0 {
		return fmt.Errorf("%d distro(s) could not be compacted", failed)
	}

	return nil
}

func writeCompactResult(out io.Writer, p wslcompact.Plan, res wslcompact.Result, err error) {
	for _, w := range res.Warnings {
		fmt.Fprintf(out, "  ! %s: %s\n", p.Name, w)
	}

	if err != nil {
		fmt.Fprintf(out, "! %s: %v\n", p.Name, err)

		return
	}

	fmt.Fprintf(out, "ok %s: %s -> %s, reclaimed %s\n", p.Name,
		report.HumanSize(res.Before), report.HumanSize(res.After), report.HumanSize(res.Gain))

	if res.LowGain {
		fmt.Fprintf(out, "  ! Compaction ran, but little space was recovered: fstrim probably could not free "+
			"enough blocks (or the files are really in use). The numbers above are the real, measured ones.\n")
	}
}

func logCompaction(j *clean.Journal, now time.Time, p wslcompact.Plan, res wslcompact.Result, err error) {
	e := clean.Entry{
		Time: now.UTC(), Category: string(core.CategoryWSL), Label: p.Name, Path: p.VHDX,
		Action: "wsl-compact", Mode: string(p.Method), Bytes: res.Gain, Result: clean.StatusDone,
	}

	if err != nil {
		e.Result, e.Error = clean.StatusFailed, err.Error()
	}

	_ = j.Write(e)
}

func realCompactor() *wslcompact.Compactor {
	return &wslcompact.Compactor{
		Runner: exec.OSRunner{},
		FileSize: func(path string) (int64, error) {
			info, err := os.Stat(path)
			if err != nil {
				return 0, err
			}

			return info.Size(), nil
		},
		FreeSpace:   winfile.FreeSpace,
		IsAdmin:     winfile.IsAdmin,
		IsSparse:    winfile.IsSparse,
		WriteScript: writeTempScript,
		Sleep:       time.Sleep,
		IsLocked:    isLocked,
	}
}

// isLocked reports whether the file cannot be opened for writing right now.
func isLocked(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return true
	}

	_ = f.Close()

	return false
}

func writeTempScript(content string) (string, func(), error) {
	f, err := os.CreateTemp("", "devreclaim-*.txt")
	if err != nil {
		return "", nil, err
	}

	cleanup := func() { _ = os.Remove(f.Name()) }

	if _, err = f.WriteString(content); err != nil {
		_ = f.Close()
		cleanup()

		return "", nil, err
	}

	if err = f.Close(); err != nil {
		cleanup()

		return "", nil, err
	}

	return f.Name(), cleanup, nil
}
