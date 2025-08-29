package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	"github.com/openmcp-project/controller-utils/pkg/collections"
	crdutil "github.com/openmcp-project/controller-utils/pkg/crds"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	apiconst "github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/openmcp-project/openmcp-operator/api/crds"
	"github.com/openmcp-project/openmcp-operator/api/install"
	providerv1alpha1 "github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/cmd/openmcp-operator/app/options"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/managedcontrolplane"
)

const OpenMCPOperatorName = "openmcp-operator"

func NewInitCommand(po *options.PersistentOptions) *cobra.Command {
	opts := &InitOptions{
		PersistentOptions: po,
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
	*options.PersistentOptions
	RawInitOptions
}

type RawInitOptions struct {
	SkipMCPPlatformService bool `json:"skip-mcp-platform-service"`
}

func (o *InitOptions) AddFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&o.SkipMCPPlatformService, "skip-mcp-platform-service", false, "If true, the PlatformService for the ManagedControlPlane controller will not be created/updated.")
}

func (o *InitOptions) Complete(ctx context.Context) error {
	if err := o.PersistentOptions.Complete(); err != nil {
		return err
	}
	return nil
}

func (o *InitOptions) Run(ctx context.Context) error {
	if err := o.PlatformCluster.InitializeClient(install.InstallCRDAPIs(runtime.NewScheme())); err != nil {
		return err
	}
	podName := os.Getenv(apiconst.EnvVariablePodName)
	if podName == "" {
		return fmt.Errorf("environment variable %s is not set", apiconst.EnvVariablePodNamespace)
	}
	podNamespace := os.Getenv(apiconst.EnvVariablePodNamespace)
	if podNamespace == "" {
		return fmt.Errorf("environment variable %s is not set", apiconst.EnvVariablePodNamespace)
	}

	log := o.Log.WithName("main")
	log.Info("Environment", "value", o.Environment)

	// apply CRDs
	crdManager := crdutil.NewCRDManager(apiconst.ClusterLabel, crds.CRDs)
	crdManager.AddCRDLabelToClusterMapping(clustersv1alpha1.PURPOSE_PLATFORM, o.PlatformCluster)
	crdManager.SkipCRDsWithClusterLabel(clustersv1alpha1.PURPOSE_ONBOARDING)

	if err := crdManager.CreateOrUpdateCRDs(ctx, &log); err != nil {
		return fmt.Errorf("error creating/updating CRDs: %w", err)
	}

	// create PlatformService for MCP controller (unless disabled)
	if o.SkipMCPPlatformService {
		log.Info("Skipping creation/update of PlatformService for ManagedControlPlane controller")
	} else {
		log.Info("Creating/updating PlatformService for ManagedControlPlane controller")

		log.Info("Fetching own pod to determine image", "name", podName, "namespace", podNamespace)
		pod := &corev1.Pod{}
		pod.Name = podName
		pod.Namespace = podNamespace
		if err := o.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(pod), pod); err != nil {
			return fmt.Errorf("error fetching own pod %s/%s: %w", podNamespace, podName, err)
		}
		image := ""
		if len(pod.Spec.Containers) == 1 {
			image = pod.Spec.Containers[0].Image
		} else {
			for _, c := range pod.Spec.Containers {
				if c.Name == OpenMCPOperatorName {
					image = c.Image
					break
				}
			}
		}
		if image == "" {
			return fmt.Errorf("unable to determine own image from pod %s/%s", podNamespace, podName)
		}
		verbosity := "INFO"
		if log.Enabled(logging.DEBUG) {
			verbosity = "DEBUG"
		}
		pullSecrets := pod.Spec.ImagePullSecrets

		mcpPSName := o.ProviderName
		if mcpPSName == "" {
			mcpPSName = strings.ToLower(managedcontrolplane.ControllerName)
		}

		expectedLabels := map[string]string{
			apiconst.ManagedByLabel: OpenMCPOperatorName,
			"platformservice." + apiconst.OpenMCPGroupName + "/purpose": managedcontrolplane.ControllerName,
		}
		psl := &providerv1alpha1.PlatformServiceList{}
		if err := o.PlatformCluster.Client().List(ctx, psl, client.MatchingLabels(expectedLabels)); err != nil {
			return fmt.Errorf("error listing PlatformServices: %w", err)
		}
		var ps *providerv1alpha1.PlatformService
		psToDelete := []providerv1alpha1.PlatformService{}
		for _, item := range psl.Items {
			if item.DeletionTimestamp.IsZero() { // ignore platform services in deletion
				if item.Name == mcpPSName {
					ps = &item
				} else {
					psToDelete = append(psToDelete, item)
				}
			}
		}
		if ps == nil {
			log.Info("Creating PlatformService for ManagedControlPlane controller", "name", mcpPSName)
			ps = &providerv1alpha1.PlatformService{}
			ps.Name = mcpPSName
		} else {
			log.Info("Updating PlatformService for ManagedControlPlane controller", "name", mcpPSName)
		}
		if _, err := controllerutil.CreateOrUpdate(ctx, o.PlatformCluster.Client(), ps, func() error {
			ps.Labels = expectedLabels
			ps.Spec.Image = image
			ps.Spec.ImagePullSecrets = collections.ProjectSliceToSlice(pullSecrets, func(ref corev1.LocalObjectReference) commonapi.LocalObjectReference {
				return commonapi.LocalObjectReference{
					Name: ref.Name,
				}
			})
			ps.Spec.InitCommand = []string{"mcp", "init"}
			ps.Spec.RunCommand = []string{"mcp", "run"}
			ps.Spec.Verbosity = verbosity
			return nil
		}); err != nil {
			return fmt.Errorf("error creating/updating PlatformService %s: %w", ps.Name, err)
		}
		if len(psToDelete) > 0 {
			log.Info("Deleting obsolete PlatformServices for ManagedControlPlane controller", "count", len(psToDelete))
			for _, psDel := range psToDelete {
				if err := o.PlatformCluster.Client().Delete(ctx, &psDel); err != nil {
					return fmt.Errorf("error deleting obsolete PlatformService %s: %w", psDel.Name, err)
				}
				log.Info("Deleted obsolete PlatformService", "name", psDel.Name)
			}
		}
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
