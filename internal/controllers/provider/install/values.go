package install

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
)

const (
	initPrefix                  = "init"
	clusterScopedResourcePrefix = "openmcp.cloud"
)

func NewValues(provider *unstructured.Unstructured, deploymentSpec *v1alpha1.DeploymentSpec, environment, namespace string) *Values {
	return &Values{
		provider:       provider,
		deploymentSpec: deploymentSpec,
		environment:    environment,
		namespace:      namespace,
		providerPrefix: getProviderPrefix(provider),
	}
}

type Values struct {
	provider             *unstructured.Unstructured
	deploymentSpec       *v1alpha1.DeploymentSpec
	environment          string
	namespace            string
	providerPrefix       string
	webhookTLSSecretName string
}

func (v *Values) Environment() string {
	return v.environment
}

func getProviderPrefix(provider *unstructured.Unstructured) string {
	switch provider.GroupVersionKind().Kind {
	case v1alpha1.ServiceProviderGKV().Kind:
		return "sp"
	case v1alpha1.ClusterProviderGKV().Kind:
		return "cp"
	case v1alpha1.PlatformServiceGKV().Kind:
		return "ps"
	default:
		return provider.GroupVersionKind().Kind
	}
}

func (v *Values) Namespace() string {
	return v.namespace
}

func (v *Values) NamespacedDefaultResourceName() string {
	return strings.ToLower(fmt.Sprintf("%s-%s", v.providerPrefix, v.provider.GetName()))
}

func (v *Values) NamespacedResourceName(suffix string) string {
	return strings.ToLower(fmt.Sprintf("%s-%s-%s", v.providerPrefix, v.provider.GetName(), suffix))
}

func (v *Values) ClusterScopedDefaultResourceName() string {
	return strings.ToLower(fmt.Sprintf("%s:%s-%s", clusterScopedResourcePrefix, v.providerPrefix, v.provider.GetName()))
}

func (v *Values) ClusterScopedResourceName(suffix string) string {
	return fmt.Sprintf("%s:%s-%s-%s", clusterScopedResourcePrefix, v.providerPrefix, v.provider.GetName(), suffix)
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
		corev1.EnvVar{
			Name: constants.EnvVariablePodName,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
		corev1.EnvVar{
			Name: constants.EnvVariablePodNamespace,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
		corev1.EnvVar{
			Name: constants.EnvVariablePodIP,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
			},
		},
		corev1.EnvVar{
			Name: constants.EnvVariablePodServiceAccountName,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.serviceAccountName"},
			},
		},
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
