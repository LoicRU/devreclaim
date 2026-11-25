package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/LoicRU/devreclaim/internal/buildinfo"
	"github.com/LoicRU/devreclaim/internal/core"
	"github.com/LoicRU/devreclaim/internal/report"
)

func newWSLCmd(newScanner func() core.Scanner) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wsl",
		Short: "Inspect WSL distributions",
	}

	cmd.AddCommand(newWSLStatusCmd(newScanner))

	return cmd
}

func newWSLStatusCmd(newScanner func() core.Scanner) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show each WSL2 virtual disk and how much of it could be reclaimed (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWSLStatus(cmd, newScanner(), asJSON)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")

	return cmd
}

func runWSLStatus(cmd *cobra.Command, scanner core.Scanner, asJSON bool) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	ch, err := scanner.Scan(ctx, core.ScanOptions{})
	if err != nil {
		return err
	}

	rep := report.New(buildinfo.Version, time.Now().UTC(), core.Collect(ch), nil)
	out := cmd.OutOrStdout()

	if asJSON {
		return rep.WriteJSON(out)
	}

	if len(rep.Candidates) == 0 {
		_, err = fmt.Fprintln(out, "No WSL distribution found.")

		return err
	}

	if err = rep.WriteText(out); err != nil {
		return err
	}

	_, err = fmt.Fprintln(out, "\nRead-only scan. Reading the used space starts distros that are stopped.")

	return err
}
