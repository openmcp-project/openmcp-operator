package install

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
)

const initPrefix = "init"

func NewValues(provider *unstructured.Unstructured, deploymentSpec *v1alpha1.DeploymentSpec, environment string) *Values {
	return &Values{
		provider:       provider,
		deploymentSpec: deploymentSpec,
		namespace:      determineNamespace(provider),
		environment:    environment,
	}
}

type Values struct {
	provider       *unstructured.Unstructured
	deploymentSpec *v1alpha1.DeploymentSpec
	namespace      string
	environment    string
}

func (v *Values) Environment() string {
	return v.environment
}

func determineNamespace(provider *unstructured.Unstructured) string {
	var namespacePrefix string
	switch provider.GroupVersionKind().Kind {
	case v1alpha1.ServiceProviderGKV().Kind:
		namespacePrefix = "sp"
	case v1alpha1.ClusterProviderGKV().Kind:
		namespacePrefix = "cp"
	case v1alpha1.PlatformServiceGKV().Kind:
		namespacePrefix = "ps"
	default:
		namespacePrefix = provider.GroupVersionKind().Kind
	}
	return strings.ToLower(fmt.Sprintf("%s-%s", namespacePrefix, provider.GetName()))
}

func (v *Values) Namespace() string {
	return v.namespace
}

func (v *Values) NamespacedDefaultResourceName() string {
	return v.provider.GetName()
}

func (v *Values) NamespacedResourceName(suffix string) string {
	return fmt.Sprintf("%s-%s", v.provider.GetName(), suffix)
}

func (v *Values) ClusterScopedDefaultResourceName() string {
	return fmt.Sprintf("%s:%s", v.Namespace(), v.NamespacedDefaultResourceName())
}

func (v *Values) ClusterScopedResourceName(suffix string) string {
	return fmt.Sprintf("%s:%s", v.Namespace(), v.NamespacedResourceName(suffix))
}

func (v *Values) Image() string {
	return v.deploymentSpec.Image
}

func (v *Values) ImagePullSecrets() []corev1.LocalObjectReference {
	secrets := make([]corev1.LocalObjectReference, len(v.deploymentSpec.ImagePullSecrets))
	for i, s := range v.deploymentSpec.ImagePullSecrets {
		secrets[i] = corev1.LocalObjectReference{
			Name: s.Name,
		}
	}
	return secrets
}

func (v *Values) LabelsCommon() map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "openmcp-operator",
		"app.kubernetes.io/name":       v.provider.GroupVersionKind().Kind,
		"app.kubernetes.io/instance":   v.provider.GetName(),
	}
}

func (v *Values) LabelsInitJob() map[string]string {
	m := v.LabelsCommon()
	m["app.kubernetes.io/component"] = "init-job"
	return m
}

func (v *Values) LabelsController() map[string]string {
	m := v.LabelsCommon()
	m["app.kubernetes.io/component"] = "controller"
	return m
}

func (v *Values) Verbosity() string {
	return v.deploymentSpec.Verbosity
}

// EnvironmentVariables returns the environment variables set in the provider resource, enriched by the following:
// - OPENMCP_PROVIDER_NAME: the name of the provider resource,
// - OPENMCP_PROVIDER_NAMESPACE: the namespace in which the provider will be deployed.
func (v *Values) EnvironmentVariables() ([]corev1.EnvVar, error) {
	varList := append(
		v.providerEnvironmentVariables(),
		corev1.EnvVar{Name: constants.EnvVariableProviderName, Value: v.provider.GetName()},
		corev1.EnvVar{Name: constants.EnvVariablePlatformClusterNamespace, Value: v.namespace},
	)

	if err := v.checkUniquenessOfVariableNames(varList); err != nil {
		return nil, err
	}
	return varList, nil
}

func (v *Values) providerEnvironmentVariables() []corev1.EnvVar {
	env := make([]corev1.EnvVar, len(v.deploymentSpec.Env))
	for i, e := range v.deploymentSpec.Env {
		env[i] = corev1.EnvVar{
			Name:  e.Name,
			Value: e.Value,
		}
	}
	return env
}

func (v *Values) checkUniquenessOfVariableNames(varList []corev1.EnvVar) error {
	varMap := make(map[string]bool)
	for _, e := range varList {
		if varMap[e.Name] {
			return fmt.Errorf("environment variable is not unique: %s", e.Name)
		}
		varMap[e.Name] = true
	}
	return nil
}
