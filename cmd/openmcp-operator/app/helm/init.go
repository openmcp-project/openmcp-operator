package helm

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/openmcp-project/openmcp-operator/cmd/openmcp-operator/app/options"
)

func NewInitCommand(po *options.PersistentOptions) *cobra.Command {
	opts := &InitOptions{
		PersistentOptions: po,
	}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the helm deployer",
		Run: func(cmd *cobra.Command, args []string) {
			opts.PrintRawOptions(cmd)
			if err := opts.Complete(cmd.Context()); err != nil {
				panic(fmt.Errorf("error completing options: %w", err))
			}
			opts.PrintCompletedOptions(cmd)
			if opts.DryRun {
				cmd.Println("=== END OF DRY RUN ===")
				return
			}
			if err := opts.Run(cmd.Context()); err != nil {
				panic(err)
			}
		},
	}
	opts.AddFlags(cmd)

	return cmd
}

type InitOptions struct {
	*options.PersistentOptions
}

func (o *InitOptions) AddFlags(cmd *cobra.Command) {}

func (o *InitOptions) Complete(ctx context.Context) error {
	if err := o.PersistentOptions.Complete(ctx); err != nil {
		return err
	}
	if o.ProviderName == "" {
		return fmt.Errorf("provider-name must not be empty")
	}
	return nil
}

func (o *InitOptions) Run(ctx context.Context) error {
	log := o.Log.WithName("main")

	log.Info("There is nothing to do in this init job, as the CRDs are already deployed by the openmcp-operator during startup.")
	return nil
}

func (o *InitOptions) PrintRaw(cmd *cobra.Command) {}

func (o *InitOptions) PrintRawOptions(cmd *cobra.Command) {
	cmd.Println("########## RAW OPTIONS START ##########")
	o.PersistentOptions.PrintRaw(cmd)
	o.PrintRaw(cmd)
	cmd.Println("########## RAW OPTIONS END ##########")
}

func (o *InitOptions) PrintCompleted(cmd *cobra.Command) {}

func (o *InitOptions) PrintCompletedOptions(cmd *cobra.Command) {
	cmd.Println("########## COMPLETED OPTIONS START ##########")
	o.PersistentOptions.PrintCompleted(cmd)
	o.PrintCompleted(cmd)
	cmd.Println("########## COMPLETED OPTIONS END ##########")
}
