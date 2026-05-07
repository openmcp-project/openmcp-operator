package v1alpha1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

// HelmDeployerConfigSpec defines the desired state of HelmDeployerConfig
type HelmDeployerConfigSpec struct {
	// HelmReleaseReconciliationIntervals specifies the reconciliation intervals for the HelmReleases deployed by the operator.
	// +optional
	HelmReleaseReconciliationIntervals HelmReleaseReconciliationIntervalConfig `json:"helmReleaseReconciliationIntervals"`

	// SelectorDefinitions is a list of selector definitions that can be referenced from HelmDeployments.
	// +optional
	SelectorDefinitions map[string]SelectorDefinition `json:"selectorDefinitions,omitempty"`
}

type HelmReleaseReconciliationIntervalConfig struct {
	// Default is the default interval in which flux reconciles the HelmRelease resource.
	// It applies whenever no specific interval is defined.
	// The default is 1h.
	// +optional
	Default *metav1.Duration `json:"default,omitempty"`

	// Helm is the reconciliation interval for HelmReleases which use a helm repository as source.
	// If not set, the default interval will be used.
	// +optional
	Helm *metav1.Duration `json:"helm,omitempty"`

	// Git is the reconciliation interval for HelmReleases which use a git repository as source.
	// If not set, the default interval will be used.
	// +optional
	Git *metav1.Duration `json:"git,omitempty"`

	// OCI is the reconciliation interval for HelmReleases which use an OCI repository as source.
	// If not set, the default interval will be used.
	// +optional
	OCI *metav1.Duration `json:"oci,omitempty"`
}

type SelectorDefinition struct {
	*clustersv1alpha1.IdentityLabelPurposeSelector `json:",inline"`

	// SecretsToCopy defines which secrets should be copied when this selector is used for a HelmDeployment.
	// +optional
	SecretsToCopy *SecretsToCopy `json:"secretsToCopy,omitempty"`
}

type SecretsToCopy struct {
	// ToPlatformCluster lists secrets from the provider namespace that should be copied into the cluster's namespace on the platform cluster.
	// This is useful e.g. for pull secrets for the helm chart registry.
	// +optional
	ToPlatformCluster []SecretCopy `json:"toPlatformCluster,omitempty"`
	// ToTargetCluster lists secrets from the provider namespace that should be copied onto the target cluster.
	// The secrets will end up in the namespace that is defined in the HelmDeployment's spec.
	// This allows propagating secrets that are required by the helm chart to the target cluster.
	// +optional
	ToTargetCluster []SecretCopy `json:"toTargetCluster,omitempty"`
}

// SecretCopy defines the name of the secret to copy and the name of the copied secret.
// If target is nil or target.name is empty, the secret will be copied with the same name as the source secret.
type SecretCopy struct {
	// Source references the source secret to copy.
	// It has to be in the namespace the provider pod is running in.
	Source commonapi.LocalObjectReference `json:"source"`
	// Target is the name of the copied secret.
	// If not set, the secret will be copied with the same name as the source secret.
	// +optional
	Target *commonapi.LocalObjectReference `json:"target"`
}

// +kubebuilder:object:root=true
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:resource:scope=Cluster,shortName=hdcfg

type HelmDeployerConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec HelmDeployerConfigSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

type HelmDeployerConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HelmDeployerConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HelmDeployerConfig{}, &HelmDeployerConfigList{})
}

func (h *HelmReleaseReconciliationIntervalConfig) IntervalForSourceKind(sourceKind string) metav1.Duration {
	if h != nil {
		switch sourceKind {
		case SourceKindHelmRepository:
			if h.Helm != nil {
				return *h.Helm
			}
		case SourceKindGitRepository:
			if h.Git != nil {
				return *h.Git
			}
		case SourceKindOCIRepository:
			if h.OCI != nil {
				return *h.OCI
			}
		}
		if h.Default != nil {
			return *h.Default
		}
	}
	return metav1.Duration{Duration: 1 * time.Hour}
}
