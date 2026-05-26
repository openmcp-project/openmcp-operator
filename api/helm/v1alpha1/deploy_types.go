package v1alpha1

import (
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fluxhelmv2 "github.com/fluxcd/helm-controller/api/v2"
	"github.com/fluxcd/pkg/apis/kustomize"
	fluxv1 "github.com/fluxcd/source-controller/api/v1"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

type HelmDeploymentSpec struct {
	// ChartSource is the source of the helm chart.
	ChartSource ChartSource `json:"chartSource"`

	// Selector can select based on identity, purposes and/or labels of a Cluster.
	// It can also reference a selector definition from the provider config.
	// An empty selector matches all Clusters.
	// +optional
	Selector *SelectorOrReference `json:"selector,omitempty"`

	// SecretsToCopy defines which secrets should be copied for this HelmDeployment.
	// This is in addition to any secrets to copy specified in a referenced selector definition in the provider config.
	// If there are overlapping definitions, the secrets specified here take precedence.
	// Opposed to secret references in the provider config, references here refer to secrets in the same namespace as the HelmDeployment.
	// TO BE REFACTORED: We want to move secret copying logic into its own controller at some point.
	// +optional
	SecretsToCopy *SecretsToCopy `json:"secretsToCopy,omitempty"`

	// Namespace is the namespace on the target cluster to use for the helm deployment.
	// If secrets are copied onto the target cluster, they will be copied into this namespace.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Namespace string `json:"namespace"`

	// HelmValues are the helm values to deploy the chart with.
	// There are a few special strings which will be replaced before creating the HelmRelease:
	// - <provider.name> will be replaced with the provider name resource.
	// - <provider.namespace> will be replaced with the namespace that hosts the platform service.
	// - <environment> will be replaced with the environment name of the operator.
	// - <helm.name> will be replaced with the name of the HelmDeployment.
	// - <helm.namespace> will be replaced with the namespace of the HelmDeployment.
	// - <cluster.name> will be replaced with the name of the reconciled Cluster.
	// - <cluster.namespace> will be replaced with the namespace of the reconciled Cluster.
	// +kubebuilder:validation:Type=object
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	HelmValues *apiextensionsv1.JSON `json:"helmValues,omitempty"`

	// Interval at which to reconcile the Helm release.
	// It can be used to overwrite the default reconciliation interval specified in the provider config.
	// Inherited from HelmRelease spec.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m|h))+$"
	// +optional
	Interval *metav1.Duration `json:"interval,omitempty"`

	// ReleaseName used for the Helm release.
	// Defaults to <namespace>--<name>--<hash> (shortened to 63 characters) if not set.
	// Inherited from HelmRelease spec.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=53
	// +optional
	ReleaseName string `json:"releaseName,omitempty"`

	// Timeout to set for the HelmRelease.
	// Flux defaults this to 5 minutes, if not overwritten here.
	// Inherited from HelmRelease spec.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m|h))+$"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// Install allows to overwrite the default install options for the HelmRelease.
	// Inherited from HelmRelease spec.
	// +optional
	Install *fluxhelmv2.Install `json:"install,omitempty"`

	// Upgrade allows to overwrite the default upgrade options for the HelmRelease.
	// Inherited from HelmRelease spec.
	// +optional
	Upgrade *fluxhelmv2.Upgrade `json:"upgrade,omitempty"`

	// Test allows to overwrite the default test options for the HelmRelease.
	// Inherited from HelmRelease spec.
	// +optional
	Test *fluxhelmv2.Test `json:"test,omitempty"`

	// Rollback allows to overwrite the default rollback options for the HelmRelease.
	// Inherited from HelmRelease spec.
	// +optional
	Rollback *fluxhelmv2.Rollback `json:"rollback,omitempty"`

	// Uninstall allows to overwrite the default uninstall options for the HelmRelease.
	// Inherited from HelmRelease spec.
	// +optional
	Uninstall *fluxhelmv2.Uninstall `json:"uninstall,omitempty"`

	// CommonMetadata allows to specify common metadata for the HelmRelease, e.g. labels and annotations.
	// Inherited from HelmRelease spec.
	// +optional
	CommonMetadata *fluxhelmv2.CommonMetadata `json:"commonMetadata,omitempty"`

	// WaitStrategy allows to specify the HelmRelease's wait strategy.
	// Inherited from HelmRelease spec.
	// +optional
	WaitStrategy *fluxhelmv2.WaitStrategy `json:"waitStrategy,omitempty"`

	// HealthCheckExprs allows to specify custom health checks for the HelmRelease.
	// Inherited from HelmRelease spec.
	// +optional
	HealthCheckExprs []kustomize.CustomHealthCheck `json:"healthCheckExprs,omitempty"`
}

type SelectorOrReference struct {
	*clustersv1alpha1.IdentityLabelPurposeSelector `json:",inline"`

	// Reference can be used to reference a selector defined in the provider config.
	// If set together with the inline selector, the inline selector takes precedence and the reference is ignored.
	// +optional
	Reference *string `json:"ref,omitempty"`
}

// ChartSource defines the source of the helm chart in form of a Flux source.
// Exactly one of 'HelmRepository', 'GitRepository' or 'OCIRepository' must be set.
// +kubebuilder:validation:ExactlyOneOf=helm;git;oci
// +kubebuilder:validation:XValidation:rule="(has(self.git) || has(self.helm)) ? (has(self.chartName) && size(self.chartName) > 0) : true", message="chartName must be set for git and helm sources"
type ChartSource struct {
	// ChartName specifies the name of the chart.
	// Can be omitted for oci sources, required for git and helm sources.
	// For git sources, this is the path within the git repository to the chart.
	// For helm sources, append the version to the chart name using '@', e.g. 'external-dns@1.10.0' or omit for latest version.
	// +optional
	ChartName string                     `json:"chartName"`
	Helm      *fluxv1.HelmRepositorySpec `json:"helm,omitempty"`
	Git       *fluxv1.GitRepositorySpec  `json:"git,omitempty"`
	OCI       *fluxv1.OCIRepositorySpec  `json:"oci,omitempty"`
}

type HelmDeploymentStatus struct {
	commonapi.Status `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:resource:shortName=hd;hdeploy;helmdeploy
// +kubebuilder:selectablefield:JSONPath=".status.phase"
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string

type HelmDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HelmDeploymentSpec   `json:"spec,omitempty"`
	Status HelmDeploymentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type HelmDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HelmDeployment `json:"items"`
}

func init() {
	RegisterToSchemeBuilder(&HelmDeployment{}, &HelmDeploymentList{})
}

// Finalizer returns the HelmDeployment-specific finalizer string.
// This is e.g. used on Cluster resources.
// The format is 'helm.open-control-plane.io/<uid>'.
func (hd *HelmDeployment) Finalizer() string {
	return fmt.Sprintf("%s/%s", GroupName, hd.UID)
}

// Resolve returns the IdentityLabelPurposeSelector specified by this SelectorOrReference.
// If the struct holds a non-nil IdentityLabelPurposeSelector, it is returned directly.
// If it holds a reference, the selector definition with the given name is looked up in the config and returned.
// Returns an error if the selector is a reference, but the config is either nil or does not contain a selector definition with the given name.
// Note that the returned IdentityLabelPurposeSelector may be nil, in which case it matches all Clusters.
// The second return value contains the secrets that should be copied. It is only non-nil, if the selector is a reference and the referenced selector definition contains secrets to copy.
func (sr *SelectorOrReference) Resolve(cfg *HelmDeployerConfig) (*clustersv1alpha1.IdentityLabelPurposeSelector, *SecretsToCopy, error) {
	if sr == nil {
		return nil, nil, nil
	}
	if sr.IdentityLabelPurposeSelector != nil {
		return sr.IdentityLabelPurposeSelector, nil, nil
	}
	if sr.Reference != nil {
		if cfg == nil {
			return nil, nil, fmt.Errorf("unable to resolve selector reference '%s': config is nil", *sr.Reference)
		}
		sel, ok := cfg.Spec.SelectorDefinitions[*sr.Reference]
		if !ok {
			return nil, nil, fmt.Errorf("unable to resolve selector reference '%s': no selector with this name found in config", *sr.Reference)
		}
		return sel.IdentityLabelPurposeSelector, sel.SecretsToCopy, nil
	}
	return nil, nil, nil
}
