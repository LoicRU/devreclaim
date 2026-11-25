package cli

import (
	"github.com/spf13/cobra"

	"github.com/LoicRU/reclaim/internal/core"
	"github.com/LoicRU/reclaim/internal/platform/exec"
	"github.com/LoicRU/reclaim/internal/scanners/wsl"
)

// NewRootCmd builds the reclaim root command and wires up subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "reclaim",
		Short:         "See what's eating your dev disk and reclaim it safely",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newVersionCmd())
	root.AddCommand(newWSLCmd(func() core.Scanner { return wsl.New(exec.OSRunner{}) }))

	return root
}
