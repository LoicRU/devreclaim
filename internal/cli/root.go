package cli

import (
	"github.com/spf13/cobra"

	"github.com/LoicRU/devreclaim/internal/core"
	"github.com/LoicRU/devreclaim/internal/platform/exec"
	"github.com/LoicRU/devreclaim/internal/scanners/pkgcaches"
	"github.com/LoicRU/devreclaim/internal/scanners/projects"
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
	root.AddCommand(newScanCmd(func() []core.Scanner {
		return []core.Scanner{projects.New(), pkgcaches.New(exec.OSRunner{})}
	}))
	root.AddCommand(newWSLCmd(func() core.Scanner { return wsl.New(exec.OSRunner{}) }))

	return root
}
