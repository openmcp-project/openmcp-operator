package app

import (
	"context"
	goflag "flag"
	"fmt"
	"os"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd/api"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/openmcp-project/openmcp-operator/api/install"
	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/provider"
)

func NewRunCommand(_ context.Context) *cobra.Command {
	options := &runOptions{
		PlatformCluster: clusters.New("platform"),
	}

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start the openmcp-operator",
		Run: func(cmd *cobra.Command, args []string) {
			if err := options.complete(); err != nil {
				fmt.Print(err)
				os.Exit(1)
			}
			if err := options.run(); err != nil {
				options.Log.Error(err, "unable to run the openmcp-operator")
				os.Exit(1)
			}
		},
	}

	options.addFlags(cmd.Flags())

	return cmd
}

type runOptions struct {
	PlatformCluster *clusters.Cluster
	ProviderGVKList []schema.GroupVersionKind
	Log             logging.Logger
}

func (o *runOptions) addFlags(fs *flag.FlagSet) {
	// register flag '--platform-cluster' for the path to the kubeconfig of the platform cluster
	o.PlatformCluster.RegisterConfigPathFlag(fs)

	logging.InitFlags(fs)
	flag.CommandLine.AddGoFlagSet(goflag.CommandLine)
}

func (o *runOptions) complete() (err error) {
	if err = o.setupLogger(); err != nil {
		return err
	}
	if err = o.setupPlatformClusterClient(); err != nil {
		return err
	}
	o.setupGVKList()

	return nil
}

func (o *runOptions) setupLogger() error {
	log, err := logging.GetLogger()
	if err != nil {
		return err
	}
	o.Log = log
	ctrl.SetLogger(log.Logr())
	return nil
}

func (o *runOptions) setupPlatformClusterClient() error {
	if err := o.PlatformCluster.InitializeRESTConfig(); err != nil {
		return fmt.Errorf("unable to initialize onboarding cluster rest config: %w", err)
	}
	if err := o.PlatformCluster.InitializeClient(install.InstallCRDAPIs(runtime.NewScheme())); err != nil {
		return fmt.Errorf("unable to initialize onboarding cluster client: %w", err)
	}
	return nil
}

func (o *runOptions) setupGVKList() {
	o.ProviderGVKList = []schema.GroupVersionKind{
		v1alpha1.ClusterProviderGKV(),
		v1alpha1.PlatformServiceGKV(),
		v1alpha1.ServiceProviderGKV(),
	}
}

func (o *runOptions) run() error {
	o.Log.Info("starting openmcp-operator", "platform-cluster", o.PlatformCluster.ConfigPath())

	mgrOptions := ctrl.Options{
		Metrics:        metricsserver.Options{BindAddress: "0"},
		LeaderElection: false,
	}

	mgr, err := ctrl.NewManager(o.PlatformCluster.RESTConfig(), mgrOptions)
	if err != nil {
		return fmt.Errorf("unable to setup manager: %w", err)
	}

	utilruntime.Must(clientgoscheme.AddToScheme(mgr.GetScheme()))
	utilruntime.Must(api.AddToScheme(mgr.GetScheme()))

	if err = (&provider.ProviderReconcilerList{
		PlatformClient: mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
	}).SetupWithManager(mgr, o.ProviderGVKList); err != nil {
		return fmt.Errorf("unable to setup provider controllers: %w", err)
	}

	o.Log.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		o.Log.Error(err, "error while running manager")
		os.Exit(1)
	}

	return nil
}
