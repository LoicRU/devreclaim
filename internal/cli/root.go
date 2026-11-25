package cli

import (
	"time"

	"github.com/spf13/cobra"
)

// NewRootCmd builds the devreclaim root command and wires up subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "devreclaim",
		Short:         "See what's eating your dev disk and reclaim it safely",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newVersionCmd())
	root.AddCommand(newScanCmd(allScanners))
	root.AddCommand(newCleanCmd(cleanDeps{
		scanners: allScanners,
		executor: realExecutor,
		logDir:   defaultLogDir,
		now:      time.Now,
	}))
	root.AddCommand(newWSLCmd(wslScanner))

	return root
}
