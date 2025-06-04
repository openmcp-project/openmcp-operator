package install

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
)

var _ = Describe("Installer", func() {

	Context("Env Variables", func() {

		const (
			providerName = "test-provider"
			envName1     = "NAME_1"
			envValue1    = "VALUE_1"
		)

		It("should contain provider variables and predefined openmcp variables", func() {
			provider := &unstructured.Unstructured{}
			provider.SetGroupVersionKind(v1alpha1.ClusterProviderGKV())
			provider.SetName(providerName)
			spec := &v1alpha1.DeploymentSpec{
				Env: []v1alpha1.EnvVar{
					{Name: envName1, Value: envValue1},
				},
			}
			v := NewValues(provider, spec, "test")
			env, err := v.EnvironmentVariables()
			Expect(err).NotTo(HaveOccurred())
			Expect(env).To(ContainElement(corev1.EnvVar{Name: envName1, Value: envValue1}))
			Expect(env).To(ContainElement(corev1.EnvVar{Name: constants.EnvVariableProviderName, Value: providerName}))
			Expect(env).To(ContainElement(corev1.EnvVar{Name: constants.EnvVariablePlatformClusterNamespace, Value: v.Namespace()}))
		})

		It("should detect a name conflict", func() {
			provider := &unstructured.Unstructured{}
			provider.SetGroupVersionKind(v1alpha1.ClusterProviderGKV())
			provider.SetName(providerName)
			spec := &v1alpha1.DeploymentSpec{
				Env: []v1alpha1.EnvVar{
					{Name: constants.EnvVariableProviderName, Value: envValue1},
				},
			}
			v := NewValues(provider, spec, "test")
			_, err := v.EnvironmentVariables()
			Expect(err).To(HaveOccurred())
		})
	})
})
