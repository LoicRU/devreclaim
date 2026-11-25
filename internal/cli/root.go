package cli

import (
	"github.com/spf13/cobra"
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

	return root
}
