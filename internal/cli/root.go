package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/LoicRU/devreclaim/internal/platform/exec"
	"github.com/LoicRU/devreclaim/internal/scanners/wsl"
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
	root.AddCommand(newScanCmd(allScanners, wslInsideScanners))
	root.AddCommand(newCleanCmd(cleanDeps{
		scanners:    allScanners,
		wslScanners: wslInsideScanners,
		executor:    realExecutor,
		logDir:      defaultLogDir,
		now:         time.Now,
	}))

	wslCmd := newWSLCmd(wslScanner)
	wslCmd.AddCommand(newWSLCompactCmd(wslCompactDeps{
		distros: func(ctx context.Context) ([]wsl.Distro, error) {
			return wsl.ListDistros(ctx, exec.OSRunner{})
		},
		compactor: realCompactor,
		logDir:    defaultLogDir,
		now:       time.Now,
	}))
	root.AddCommand(wslCmd)

	return root
}
