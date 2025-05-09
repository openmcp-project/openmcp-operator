package app

import (
	"context"
	"fmt"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"

	"github.com/spf13/cobra"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	"github.com/openmcp-project/openmcp-operator/internal/config"
)

func NewOpenMCPOperatorCommand(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "openmcp-operator",
		Short: "Commands for interacting with the openmcp-operator",
	}
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	so := &SharedOptions{
		RawSharedOptions: &RawSharedOptions{},
		Clusters: &Clusters{
			Onboarding: clusters.New("onboarding"),
			Platform:   clusters.New("platform"),
		},
	}
	so.AddPersistentFlags(cmd)
	cmd.AddCommand(NewInitCommand(so))
	cmd.AddCommand(NewRunCommand(so))

	return cmd
}

type RawSharedOptions struct {
	Environment                     string   `json:"environment"`
	DryRun                          bool     `json:"dry-run"`
	ConfigPaths                     []string `json:"configPaths"`
	OnboardingClusterKubeconfigPath string   `json:"onboarding-cluster"` // dummy for printing, actual path is in Clusters
	PlatformClusterKubeconfigPath   string   `json:"platform-cluster"`   // dummy for printing, actual path is in Clusters
}

type SharedOptions struct {
	*RawSharedOptions
	Clusters *Clusters

	// fields filled in Complete()
	Log    logging.Logger
	Config *config.Config
}

func (o *SharedOptions) AddPersistentFlags(cmd *cobra.Command) {
	// logging
	logging.InitFlags(cmd.PersistentFlags())
	// clusters
	o.Clusters.Onboarding.RegisterConfigPathFlag(cmd.PersistentFlags())
	o.Clusters.Platform.RegisterConfigPathFlag(cmd.PersistentFlags())
	// environment
	cmd.PersistentFlags().StringVar(&o.Environment, "environment", "", "Environment name. Required. This is used to distinguish between different environments that are watching the same Onboarding cluster. Must be globally unique.")
	// config
	cmd.PersistentFlags().StringSliceVar(&o.ConfigPaths, "config", nil, "Paths to the config files (separate with comma or specify flag multiple times). Each path can be a file or directory. In the latter case, all files within with '.yaml', '.yml', and '.json' extensions are evaluated. The config is merged together from the different sources, with later configs overriding earlier ones.")
	// misc
	cmd.PersistentFlags().BoolVar(&o.DryRun, "dry-run", false, "If set, the command aborts after evaluation of the given flags.")
}

func (o *SharedOptions) Complete() error {
	if o.Environment == "" {
		return fmt.Errorf("environment must not be empty")
	}
	config.SetEnvironment(o.Environment)

	// build logger
	log, err := logging.GetLogger()
	if err != nil {
		return err
	}
	o.Log = log
	ctrl.SetLogger(o.Log.Logr())

	// construct cluster clients
	if err := o.Clusters.Platform.InitializeRESTConfig(); err != nil {
		return err
	}
	if err := o.Clusters.Onboarding.InitializeRESTConfig(); err != nil {
		return err
	}

	// load config
	if len(o.ConfigPaths) > 0 {
		cfg, err := config.LoadFromFiles(o.ConfigPaths...)
		if err != nil {
			return fmt.Errorf("error loading config from files: %w", err)
		}
		if err := cfg.Default(); err != nil {
			_ = cfg.Dump(os.Stderr)
			return fmt.Errorf("error defaulting config: %w", err)
		}
		if err := cfg.Validate(); err != nil {
			_ = cfg.Dump(os.Stderr)
			return fmt.Errorf("config is invalid: %w", err)
		}
		if err := cfg.Complete(); err != nil {
			_ = cfg.Dump(os.Stderr)
			return fmt.Errorf("error completing config: %w", err)
		}
		o.Config = cfg
	}

	return nil
}

type Clusters struct {
	Onboarding *clusters.Cluster
	Platform   *clusters.Cluster
}

func (o *SharedOptions) PrintRaw(cmd *cobra.Command) {
	// fill dummy paths
	o.OnboardingClusterKubeconfigPath = o.Clusters.Onboarding.ConfigPath()
	o.PlatformClusterKubeconfigPath = o.Clusters.Platform.ConfigPath()

	data, err := yaml.Marshal(o.RawSharedOptions)
	if err != nil {
		cmd.Println(fmt.Errorf("error marshalling raw shared options: %w", err).Error())
		return
	}
	cmd.Print(string(data))
}

func (o *SharedOptions) PrintCompleted(cmd *cobra.Command) {
	raw := map[string]any{
		"clusters": map[string]any{
			"onboarding": o.Clusters.Onboarding.APIServerEndpoint(),
			"platform":   o.Clusters.Platform.APIServerEndpoint(),
		},
		"config": o.Config,
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		cmd.Println(fmt.Errorf("error marshalling completed shared options: %w", err).Error())
		return
	}
	cmd.Print(string(data))
}
