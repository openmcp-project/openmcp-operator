package app

import (
	"context"
	"fmt"

	crdutil "github.com/openmcp-project/controller-utils/pkg/crds"
	"github.com/spf13/cobra"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/api/crds"
	"github.com/openmcp-project/openmcp-operator/api/install"
)

func NewInitCommand(so *SharedOptions) *cobra.Command {
	opts := &InitOptions{
		SharedOptions: so,
	}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the openMCP Operator",
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
	*SharedOptions
	RawInitOptions
}

type RawInitOptions struct {
	SkipPlatformCRDs   bool `json:"skip-platform-crds"`
	SkipOnboardingCRDs bool `json:"skip-onboarding-crds"`
}

func (o *InitOptions) AddFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&o.SkipPlatformCRDs, "skip-platform-crds", false, "Won't install CRDs for the platform cluster, if true.")
	cmd.Flags().BoolVar(&o.SkipOnboardingCRDs, "skip-onboarding-crds", false, "Won't install CRDs for the onboarding cluster, if true.")
}

func (o *InitOptions) Complete(ctx context.Context) error {
	if err := o.SharedOptions.Complete(); err != nil {
		return err
	}
	return nil
}

func (o *InitOptions) Run(ctx context.Context) error {
	if err := o.Clusters.Onboarding.InitializeClient(install.InstallCRDAPIs(runtime.NewScheme())); err != nil {
		return err
	}
	if err := o.Clusters.Platform.InitializeClient(install.InstallCRDAPIs(runtime.NewScheme())); err != nil {
		return err
	}

	log := o.Log.WithName("main")
	log.Info("Environment", "value", o.Environment)

	// apply CRDs
	crdManager := crdutil.NewCRDManager(clustersv1alpha1.ClusterLabel, func() ([]*apiextv1.CustomResourceDefinition, error) {
		return crds.CRDs(), nil
	})

	crdManager.AddCRDLabelToClusterMapping(clustersv1alpha1.PURPOSE_ONBOARDING, o.Clusters.Onboarding)
	crdManager.AddCRDLabelToClusterMapping(clustersv1alpha1.PURPOSE_PLATFORM, o.Clusters.Platform)

	if err := crdManager.CreateOrUpdateCRDs(ctx, nil); err != nil {
		return fmt.Errorf("error creating/updating CRDs: %w", err)
	}

	log.Info("Finished init command")
	return nil
}

func (o *InitOptions) PrintRaw(cmd *cobra.Command) {
	data, err := yaml.Marshal(o.RawInitOptions)
	if err != nil {
		cmd.Println(fmt.Errorf("error marshalling raw options: %w", err).Error())
		return
	}
	cmd.Print(string(data))
}

func (o *InitOptions) PrintRawOptions(cmd *cobra.Command) {
	cmd.Println("########## RAW OPTIONS START ##########")
	o.SharedOptions.PrintRaw(cmd)
	o.PrintRaw(cmd)
	cmd.Println("########## RAW OPTIONS END ##########")
}

func (o *InitOptions) PrintCompleted(cmd *cobra.Command) {}

func (o *InitOptions) PrintCompletedOptions(cmd *cobra.Command) {
	cmd.Println("########## COMPLETED OPTIONS START ##########")
	o.SharedOptions.PrintCompleted(cmd)
	o.PrintCompleted(cmd)
	cmd.Println("########## COMPLETED OPTIONS END ##########")
}
