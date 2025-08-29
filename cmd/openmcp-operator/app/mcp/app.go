package mcp

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/openmcp-project/openmcp-operator/cmd/openmcp-operator/app/options"
)

func NewMCPControllerSubcommand(ctx context.Context, po *options.PersistentOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Commands running the MCP controller",
	}
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	cmd.AddCommand(NewInitCommand(po))
	cmd.AddCommand(NewRunCommand(po))

	return cmd
}
