package mcp

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"

	crdutil "github.com/openmcp-project/controller-utils/pkg/crds"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	apiconst "github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/openmcp-project/openmcp-operator/api/crds"
	"github.com/openmcp-project/openmcp-operator/api/install"
	"github.com/openmcp-project/openmcp-operator/cmd/openmcp-operator/app/options"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/managedcontrolplane"
	"github.com/openmcp-project/openmcp-operator/lib/clusteraccess"
)

func NewInitCommand(po *options.PersistentOptions) *cobra.Command {
	opts := &InitOptions{
		PersistentOptions: po,
	}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the MCP controller",
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
	if err := o.PersistentOptions.Complete(); err != nil {
		return err
	}
	if o.ProviderName == "" {
		return fmt.Errorf("provider-name must not be empty")
	}
	return nil
}

func (o *InitOptions) Run(ctx context.Context) error {
	if err := o.PlatformCluster.InitializeClient(install.InstallOperatorAPIsPlatform(runtime.NewScheme())); err != nil {
		return err
	}

	log := o.Log.WithName("main")

	onboardingScheme := runtime.NewScheme()
	install.InstallCRDAPIs(onboardingScheme)

	providerSystemNamespace := os.Getenv(apiconst.EnvVariablePodNamespace)
	if providerSystemNamespace == "" {
		return fmt.Errorf("environment variable %s is not set", apiconst.EnvVariablePodNamespace)
	}

	clusterAccessManager := clusteraccess.NewClusterAccessManager(o.PlatformCluster.Client(), managedcontrolplane.ControllerName, providerSystemNamespace)
	clusterAccessManager.WithLogger(&log).
		WithInterval(10 * time.Second).
		WithTimeout(30 * time.Minute)

	onboardingCluster, err := clusterAccessManager.CreateAndWaitForCluster(ctx, clustersv1alpha1.PURPOSE_ONBOARDING+"-init", clustersv1alpha1.PURPOSE_ONBOARDING,
		onboardingScheme, []clustersv1alpha1.PermissionsRequest{
			{
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{"apiextensions.k8s.io/v1"},
						Resources: []string{"customresourcedefinitions"},
						Verbs:     []string{"*"},
					},
				},
			},
		})

	if err != nil {
		return fmt.Errorf("error creating/updating onboarding cluster: %w", err)
	}

	crdManager := crdutil.NewCRDManager(apiconst.ClusterLabel, crds.CRDs)

	// deploy only onboarding CRDs here, because the openMCP operator already deployed the platform CRDs
	crdManager.AddCRDLabelToClusterMapping(clustersv1alpha1.PURPOSE_ONBOARDING, onboardingCluster)
	crdManager.SkipCRDsWithClusterLabel(clustersv1alpha1.PURPOSE_PLATFORM)

	if err := crdManager.CreateOrUpdateCRDs(ctx, &o.Log); err != nil {
		return fmt.Errorf("error creating/updating CRDs: %w", err)
	}
	log.Info("Finished init command")
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
