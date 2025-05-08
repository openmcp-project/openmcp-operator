package app

import (
	"context"

	"github.com/spf13/cobra"
)

func NewOpenMCPOperatorCommand(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "openmcp-operator",
		Short: "Commands for interacting with the openmcp-operator",
	}

	cmd.AddCommand(NewInitCommand(ctx))
	cmd.AddCommand(NewRunCommand(ctx))

	return cmd
}
