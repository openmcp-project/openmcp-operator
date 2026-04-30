package v1alpha1

import (
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fluxv1 "github.com/fluxcd/source-controller/api/v1"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

// +kubebuilder:validation:ExactlyOneOf=selectorName;selector
type HelmDeploymentSpec struct {
	// ChartSource is the source of the helm chart.
	ChartSource ChartSource `json:"chartSource"`

	// SelectorName is a reference to a selector defined in the provider config.
	// Exactly one of 'selectorName' or 'selector' must be set.
	// If the referenced selector comes with secret copy instructions, the secrets will be copied accordingly.
	// +optional
	SelectorName *string `json:"selectorName,omitempty"`

	// Selector can select based on identity, purposes and/or labels of a Cluster.
	// Exactly one of 'selectorName' or 'selector' must be set.
	// +optional
	Selector *clustersv1alpha1.IdentityLabelPurposeSelector `json:"selector,omitempty"`

	// Namespace is the namespace on the target cluster to use for the helm deployment.
	// If secrets are copied onto the target cluster, they will be copied into this namespace.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Namespace string `json:"namespace"`

	// HelmValues are the helm values to deploy external-dns with, if the purpose selector matches.
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
	HelmValues *apiextensionsv1.JSON `json:"helmValues"`
}

// ChartSource defines the source of the helm chart in form of a Flux source.
// Exactly one of 'HelmRepository', 'GitRepository' or 'OCIRepository' must be set.
// +kubebuilder:validation:ExactlyOneOf=helm;git;oci
// +kubebuilder:validation:XValidation:rule="(has(self.git) || has(self.helm)) ? (has(self.chartName) && size(self.chartName) > 0) : true", message="chartName must be set if git is used as source"
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
	SchemeBuilder.Register(&HelmDeployment{}, &HelmDeploymentList{})
}

// Finalizer returns the HelmDeployment-specific finalizer string.
// This is e.g. used on Cluster resources.
// The format is 'helm.open-control-plane.io/<uid>'.
func (hd *HelmDeployment) Finalizer() string {
	return fmt.Sprintf("%s/%s", GroupName, hd.UID)
}
