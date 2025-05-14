package provider

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
)

var _ = Describe("Deployment Controller", func() {

	Context("Converter", func() {

		It("should convert a deploymentStatus into an unstructured and back", func() {
			originalStatus := &v1alpha1.DeploymentStatus{
				Conditions:         nil,
				ObservedGeneration: 6,
				Phase:              phaseProgressing,
			}
			r := &ProviderReconciler{}
			provider := &unstructured.Unstructured{}
			provider.Object = map[string]interface{}{}
			err := r.deploymentStatusIntoUnstructured(originalStatus, provider)
			Expect(err).NotTo(HaveOccurred())
			status, err := r.deploymentStatusFromUnstructured(provider)
			Expect(err).NotTo(HaveOccurred())
			Expect(status).To(Equal(originalStatus))
		})
	})
})
