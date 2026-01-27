package utils_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"

	"github.com/openmcp-project/openmcp-operator/lib/utils"
)

var _ = Describe("Provider Utils", func() {

	Context("IsClusterProviderResponsibleForAccessRequest", func() {

		It("should return the expected values", func() {
			matrix := []struct {
				msg          string
				ar           *clustersv1alpha1.AccessRequest
				providerName string
				expected     bool
			}{
				{
					msg:      "should return false for nil AccessRequest",
					expected: false,
				},
				{
					msg: "should return false if provider label is missing",
					ar: &clustersv1alpha1.AccessRequest{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								clustersv1alpha1.ProfileLabel: "test-profile",
							},
						},
					},
					expected: false,
				},
				{
					msg: "should return false if profile label is missing",
					ar: &clustersv1alpha1.AccessRequest{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								clustersv1alpha1.ProviderLabel: "test-provider",
							},
						},
					},
					expected: false,
				},
				{
					msg: "should return false if provider name does not match",
					ar: &clustersv1alpha1.AccessRequest{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								clustersv1alpha1.ProviderLabel: "other-provider",
								clustersv1alpha1.ProfileLabel:  "test-profile",
							},
						},
					},
					providerName: "test-provider",
					expected:     false,
				},
				{
					msg: "should return false if phase is not Pending or Granted",
					ar: &clustersv1alpha1.AccessRequest{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								clustersv1alpha1.ProviderLabel: "test-provider",
								clustersv1alpha1.ProfileLabel:  "test-profile",
							},
						},
					},
					providerName: "test-provider",
					expected:     false,
				},
				{
					msg: "should return false if phase is Granted but observed generation differs",
					ar: &clustersv1alpha1.AccessRequest{
						ObjectMeta: metav1.ObjectMeta{
							Generation: 5,
							Labels: map[string]string{
								clustersv1alpha1.ProviderLabel: "test-provider",
								clustersv1alpha1.ProfileLabel:  "test-profile",
							},
						},
						Status: clustersv1alpha1.AccessRequestStatus{
							Status: commonapi.Status{
								Phase:              clustersv1alpha1.REQUEST_GRANTED,
								ObservedGeneration: 3,
							},
						},
					},
					providerName: "test-provider",
				},
				{
					msg: "should return true for Pending phase with matching labels and generation",
					ar: &clustersv1alpha1.AccessRequest{
						ObjectMeta: metav1.ObjectMeta{
							Generation: 5,
							Labels: map[string]string{
								clustersv1alpha1.ProviderLabel: "test-provider",
								clustersv1alpha1.ProfileLabel:  "test-profile",
							},
						},
						Status: clustersv1alpha1.AccessRequestStatus{
							Status: commonapi.Status{
								Phase:              clustersv1alpha1.REQUEST_PENDING,
								ObservedGeneration: 5,
							},
						},
					},
					providerName: "test-provider",
					expected:     true,
				},
				{
					msg: "should return true for Granted phase with matching labels and generation",
					ar: &clustersv1alpha1.AccessRequest{
						ObjectMeta: metav1.ObjectMeta{
							Generation: 5,
							Labels: map[string]string{
								clustersv1alpha1.ProviderLabel: "test-provider",
								clustersv1alpha1.ProfileLabel:  "test-profile",
							},
						},
						Status: clustersv1alpha1.AccessRequestStatus{
							Status: commonapi.Status{
								Phase:              clustersv1alpha1.REQUEST_GRANTED,
								ObservedGeneration: 5,
							},
						},
					},
					providerName: "test-provider",
					expected:     true,
				},
				{
					msg: "should return true when provider name is empty and other conditions match",
					ar: &clustersv1alpha1.AccessRequest{
						ObjectMeta: metav1.ObjectMeta{
							Generation: 5,
							Labels: map[string]string{
								clustersv1alpha1.ProviderLabel: "test-provider",
								clustersv1alpha1.ProfileLabel:  "test-profile",
							},
						},
						Status: clustersv1alpha1.AccessRequestStatus{
							Status: commonapi.Status{
								Phase:              clustersv1alpha1.REQUEST_GRANTED,
								ObservedGeneration: 5,
							},
						},
					},
					expected: true,
				},
			}

			for _, testcase := range matrix {
				res := utils.IsClusterProviderResponsibleForAccessRequest(testcase.ar, testcase.providerName)
				Expect(res).To(Equal(testcase.expected), testcase.msg)
			}
		})

	})

})
