package app

import (
	"context"
	"errors"
	goflag "flag"
	"fmt"
	"os"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	ctrlutil "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/controller-utils/pkg/resources"
	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openmcp-project/openmcp-operator/api/crds"
	"github.com/openmcp-project/openmcp-operator/api/install"
)

const (
	clusterLabel    = "openmcp.cloud/cluster"
	clusterPlatform = "platform"
)

func NewInitCommand(ctx context.Context) *cobra.Command {
	options := &initOptions{
		PlatformCluster: clusters.New("platform"),
	}

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the openmcp-operator",
		Run: func(cmd *cobra.Command, args []string) {
			if err := options.complete(); err != nil {
				fmt.Print(err)
				os.Exit(1)
			}
			if err := options.run(ctx); err != nil {
				options.Log.Error(err, "unable to initialize the openmcp-operator")
				os.Exit(1)
			}
		},
	}

	options.addFlags(cmd.Flags())

	return cmd
}

type initOptions struct {
	PlatformCluster *clusters.Cluster
	Log             logging.Logger
}

func (o *initOptions) addFlags(fs *flag.FlagSet) {
	// register flag '--platform-cluster' for the path to the kubeconfig of the platform cluster
	o.PlatformCluster.RegisterConfigPathFlag(fs)

	logging.InitFlags(fs)
	flag.CommandLine.AddGoFlagSet(goflag.CommandLine)
}

func (o *initOptions) complete() (err error) {
	if err = o.setupLogger(); err != nil {
		return err
	}
	if err = o.setupPlatformClusterClient(); err != nil {
		return err
	}
	return nil
}

func (o *initOptions) setupLogger() error {
	log, err := logging.GetLogger()
	if err != nil {
		return err
	}
	o.Log = log
	ctrl.SetLogger(log.Logr())
	return nil
}

func (o *initOptions) setupPlatformClusterClient() error {
	if err := o.PlatformCluster.InitializeRESTConfig(); err != nil {
		return fmt.Errorf("unable to initialize onboarding cluster rest config: %w", err)
	}
	if err := o.PlatformCluster.InitializeClient(install.InstallCRDAPIs(runtime.NewScheme())); err != nil {
		return fmt.Errorf("unable to initialize onboarding cluster client: %w", err)
	}
	return nil
}

func (o *initOptions) run(ctx context.Context) error {
	o.Log.Info("initializing openmcp-operator", "platform-cluster", o.PlatformCluster.ConfigPath())

	if err := o.createOrUpdateCRDs(ctx); err != nil {
		return err
	}

	o.Log.Info("finished init command")
	return nil
}

func (o *initOptions) createOrUpdateCRDs(ctx context.Context) error {
	crdList := crds.CRDs()
	var errs error
	for _, crd := range crdList {
		c, err := o.clusterForCRD(crd)
		if err != nil {
			return err
		}

		o.Log.Info("creating/updating CRD", "name", crd.Name, "cluster", c.ID())
		err = resources.CreateOrUpdateResource(ctx, c.Client(), resources.NewCRDMutator(crd, nil, nil))
		errs = errors.Join(errs, err)
	}
	if errs != nil {
		return fmt.Errorf("error creating/updating CRDs: %w", errs)
	}
	return nil
}

func (o *initOptions) clusterForCRD(crd *apiextv1.CustomResourceDefinition) (*clusters.Cluster, error) {
	purpose, _ := ctrlutil.GetLabel(crd, clusterLabel)
	switch purpose {
	case clusterPlatform:
		return o.PlatformCluster, nil
	default:
		return nil, fmt.Errorf("missing cluster label '%s' or unsupported value '%s' for CRD '%s'",
			clusterLabel, purpose, crd.Name)
	}
}
