package options

import (
	"fmt"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"

	"github.com/spf13/cobra"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	"github.com/openmcp-project/openmcp-operator/internal/config"
)

func NewPersistentOptions() *PersistentOptions {
	return &PersistentOptions{
		RawPersistentOptions: &RawPersistentOptions{},
		PlatformCluster:      clusters.New("platform"),
	}
}

type RawPersistentOptions struct {
	Environment                   string   `json:"environment"`
	ProviderName                  string   `json:"provider-name"`
	DryRun                        bool     `json:"dry-run"`
	ConfigPaths                   []string `json:"configPaths"`
	PlatformClusterKubeconfigPath string   `json:"kubeconfig"` // dummy for printing, actual path is in Clusters
}

type PersistentOptions struct {
	*RawPersistentOptions
	PlatformCluster *clusters.Cluster

	// fields filled in Complete()
	Log    logging.Logger
	Config *config.Config
}

func (o *PersistentOptions) AddPersistentFlags(cmd *cobra.Command) {
	// logging
	logging.InitFlags(cmd.PersistentFlags())
	// clusters
	o.PlatformCluster.RegisterSingleConfigPathFlag(cmd.PersistentFlags())
	// environment
	cmd.PersistentFlags().StringVar(&o.Environment, "environment", "", "Environment name. Required. This is used to distinguish between different environments that are watching the same Onboarding cluster. Must be globally unique.")
	cmd.PersistentFlags().StringVar(&o.ProviderName, "provider-name", "", "Provider name. Optional for the top-level run and init commands, where it can be used to override the default name for the generated MCP PlatformService. Required for the MCP controller subcommand, where it must match the provider name of the PlatformService in the Platform cluster.")
	// config
	cmd.PersistentFlags().StringSliceVar(&o.ConfigPaths, "config", nil, "Paths to the config files (separate with comma or specify flag multiple times). Each path can be a file or directory. In the latter case, all files within with '.yaml', '.yml', and '.json' extensions are evaluated. The config is merged together from the different sources, with later configs overriding earlier ones.")
	// misc
	cmd.PersistentFlags().BoolVar(&o.DryRun, "dry-run", false, "If set, the command aborts after evaluation of the given flags.")
}

func (o *PersistentOptions) Complete() error {
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
	if err := o.PlatformCluster.InitializeRESTConfig(); err != nil {
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

func (o *PersistentOptions) PrintRaw(cmd *cobra.Command) {
	// fill dummy paths
	o.PlatformClusterKubeconfigPath = o.PlatformCluster.ConfigPath()

	data, err := yaml.Marshal(o.RawPersistentOptions)
	if err != nil {
		cmd.Println(fmt.Errorf("error marshalling raw shared options: %w", err).Error())
		return
	}
	cmd.Print(string(data))
}

func (o *PersistentOptions) PrintCompleted(cmd *cobra.Command) {
	raw := map[string]any{
		"platformCluster": o.PlatformCluster.APIServerEndpoint(),
		"config":          o.Config,
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		cmd.Println(fmt.Errorf("error marshalling completed shared options: %w", err).Error())
		return
	}
	cmd.Print(string(data))
}
