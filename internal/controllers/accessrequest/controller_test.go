package accessrequest_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	testutils "github.com/openmcp-project/controller-utils/pkg/testing"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	// cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	"github.com/openmcp-project/openmcp-operator/api/install"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/accessrequest"
)

var scheme = install.InstallOperatorAPIsPlatform(runtime.NewScheme())

func arReconciler(c client.Client) reconcile.Reconciler {
	return accessrequest.NewAccessRequestReconciler(clusters.NewTestClusterFromClient("platform", c), nil)
}

var _ = Describe("AccessRequest Controller", func() {

	It("should add the correct labels to the AccessRequest if a Cluster is referenced directly", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-01").WithReconcilerConstructor(arReconciler).Build()
		ar := &clustersv1alpha1.AccessRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("mc-access", "bar"), ar)).To(Succeed())
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProviderLabel))
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProfileLabel))
		env.ShouldReconcile(testutils.RequestFromObject(ar))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		Expect(ar.Labels).To(HaveKeyWithValue(clustersv1alpha1.ProviderLabel, "asdf"))
		Expect(ar.Labels).To(HaveKeyWithValue(clustersv1alpha1.ProfileLabel, "default"))
	})

	It("should add the correct labels and cluster reference to the AccessRequest if a Cluster is referenced via a ClusterRequest", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-01").WithReconcilerConstructor(arReconciler).Build()
		ar := &clustersv1alpha1.AccessRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("mcr-access", "bar"), ar)).To(Succeed())
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProviderLabel))
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProfileLabel))
		Expect(ar.Spec.ClusterRef).To(BeNil())
		env.ShouldReconcile(testutils.RequestFromObject(ar))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		Expect(ar.Labels).To(HaveKeyWithValue(clustersv1alpha1.ProviderLabel, "asdf"))
		Expect(ar.Labels).To(HaveKeyWithValue(clustersv1alpha1.ProfileLabel, "default"))
		Expect(ar.Spec.ClusterRef).ToNot(BeNil())
		Expect(ar.Spec.ClusterRef.Name).To(Equal("my-cluster"))
		Expect(ar.Spec.ClusterRef.Namespace).To(Equal("foo"))
	})

	It("should fail if the AccessRequest references a ClusterRequest which is not Granted", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-02").WithReconcilerConstructor(arReconciler).Build()
		ar := &clustersv1alpha1.AccessRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("mcr-access", "bar"), ar)).To(Succeed())
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProviderLabel))
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProfileLabel))
		Expect(ar.Spec.ClusterRef).To(BeNil())
		env.ShouldNotReconcile(testutils.RequestFromObject(ar))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		// Expect(ar.Status.Message).To(ContainSubstring("not granted"))
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProviderLabel))
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProfileLabel))
		Expect(ar.Spec.ClusterRef).To(BeNil())
	})

	It("should fail if the AccessRequest references an unknown Cluster or ClusterRequest", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-03").WithReconcilerConstructor(arReconciler).Build()

		ar := &clustersv1alpha1.AccessRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("mc-access", "bar"), ar)).To(Succeed())
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProviderLabel))
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProfileLabel))
		env.ShouldNotReconcile(testutils.RequestFromObject(ar))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		// Expect(ar.Status.Reason).To(Equal(cconst.ReasonInvalidReference))
		// Expect(ar.Status.Message).To(ContainSubstring("not found"))
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProviderLabel))
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProfileLabel))

		ar = &clustersv1alpha1.AccessRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("mcr-access", "bar"), ar)).To(Succeed())
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProviderLabel))
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProfileLabel))
		Expect(ar.Spec.ClusterRef).To(BeNil())
		env.ShouldNotReconcile(testutils.RequestFromObject(ar))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		// Expect(ar.Status.Reason).To(Equal(cconst.ReasonInvalidReference))
		// Expect(ar.Status.Message).To(ContainSubstring("not found"))
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProviderLabel))
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProfileLabel))
		Expect(ar.Spec.ClusterRef).To(BeNil())
	})

	It("should add the respective other label if either provider or profile label is already set", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-04").WithReconcilerConstructor(arReconciler).Build()

		ar := &clustersv1alpha1.AccessRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("mc-access-provider", "bar"), ar)).To(Succeed())
		Expect(ar.Labels).To(HaveKey(clustersv1alpha1.ProviderLabel))
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProfileLabel))
		env.ShouldReconcile(testutils.RequestFromObject(ar))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		Expect(ar.Labels).To(HaveKeyWithValue(clustersv1alpha1.ProviderLabel, "asdf"))
		Expect(ar.Labels).To(HaveKeyWithValue(clustersv1alpha1.ProfileLabel, "default"))

		ar = &clustersv1alpha1.AccessRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("mc-access-profile", "bar"), ar)).To(Succeed())
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProviderLabel))
		Expect(ar.Labels).To(HaveKey(clustersv1alpha1.ProfileLabel))
		env.ShouldReconcile(testutils.RequestFromObject(ar))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		Expect(ar.Labels).To(HaveKeyWithValue(clustersv1alpha1.ProviderLabel, "asdf"))
		Expect(ar.Labels).To(HaveKeyWithValue(clustersv1alpha1.ProfileLabel, "default"))
	})

	It("should not overwrite either label if already set to a different value", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-05").WithReconcilerConstructor(arReconciler).Build()

		ar := &clustersv1alpha1.AccessRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("mcr-access-provider", "bar"), ar)).To(Succeed())
		Expect(ar.Labels).To(HaveKey(clustersv1alpha1.ProviderLabel))
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProfileLabel))
		Expect(ar.Spec.ClusterRef).To(BeNil())
		env.ShouldReconcile(testutils.RequestFromObject(ar))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		Expect(ar.Labels).ToNot(HaveKeyWithValue(clustersv1alpha1.ProviderLabel, "asdf"))
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProfileLabel))
		Expect(ar.Spec.ClusterRef).To(BeNil())

		ar = &clustersv1alpha1.AccessRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("mcr-access-profile", "bar"), ar)).To(Succeed())
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProviderLabel))
		Expect(ar.Labels).To(HaveKey(clustersv1alpha1.ProfileLabel))
		Expect(ar.Spec.ClusterRef).To(BeNil())
		env.ShouldReconcile(testutils.RequestFromObject(ar))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		Expect(ar.Labels).ToNot(HaveKey(clustersv1alpha1.ProviderLabel))
		Expect(ar.Labels).ToNot(HaveKeyWithValue(clustersv1alpha1.ProfileLabel, "default"))
		Expect(ar.Spec.ClusterRef).To(BeNil())
	})

	It("should set an already granted AccessRequest to Pending if its generation changed", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-06").WithReconcilerConstructor(arReconciler).Build()
		ar := &clustersv1alpha1.AccessRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("mc-access-granted", "bar"), ar)).To(Succeed())
		arCopy := ar.DeepCopy()
		env.ShouldReconcile(testutils.RequestFromObject(ar))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		Expect(ar).To(Equal(arCopy)) // nothing should have changed, because the request was granted and the generation did not change

		// modify the generation to simulate a spec change
		ar.Generation += 1
		Expect(env.Client().Update(env.Ctx, ar)).To(Succeed())
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		Expect(ar.Status.Phase).To(Equal(clustersv1alpha1.REQUEST_GRANTED))
		Expect(ar.Status.ObservedGeneration).ToNot(Equal(ar.Generation))

		// reconciliation should update the observed generation and set the phase to Pending
		env.ShouldReconcile(testutils.RequestFromObject(ar))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		Expect(ar.Status.Phase).To(Equal(clustersv1alpha1.REQUEST_PENDING))
		Expect(ar.Status.ObservedGeneration).To(Equal(ar.Generation))
	})

	Context("TTL", func() {

		It("should not delete an AccessRequest without TTL", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-06").WithReconcilerConstructor(arReconciler).Build()
			ar := &clustersv1alpha1.AccessRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("mc-access-no-ttl", "bar"), ar)).To(Succeed())
			arCopy := ar.DeepCopy()
			env.ShouldReconcile(testutils.RequestFromObject(ar))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
			Expect(ar.Labels).To(Equal(arCopy.Labels))
			Expect(ar.Annotations).To(Equal(arCopy.Annotations))
			Expect(ar.DeletionTimestamp.IsZero()).To(BeTrue())
			Expect(ar.Spec).To(Equal(arCopy.Spec))
			Expect(ar.Status).To(Equal(arCopy.Status))
		})

		It("should delete an AccessRequest with expired TTL", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-06").WithReconcilerConstructor(arReconciler).Build()
			ar := &clustersv1alpha1.AccessRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("mc-access-ttl", "bar"), ar)).To(Succeed())
			env.ShouldReconcile(testutils.RequestFromObject(ar))
			Eventually(func() error {
				return env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)
			}).Should(MatchError(apierrors.IsNotFound, "IsNotFound"))
		})

		It("should not delete an AccessRequest with non-expired TTL", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-06").WithReconcilerConstructor(arReconciler).Build()
			ar := &clustersv1alpha1.AccessRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("mc-access-ttl", "bar"), ar)).To(Succeed())

			// modify creation timestamp to be recent so that TTL is not expired yet
			now := metav1.Now()
			ar.SetCreationTimestamp(now)
			Expect(env.Client().Update(env.Ctx, ar)).To(Succeed())

			arCopy := ar.DeepCopy()
			rr := env.ShouldReconcile(testutils.RequestFromObject(ar))
			Expect(rr.RequeueAfter).To(BeNumerically("~", time.Hour+time.Second, time.Second))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
			Expect(ar.Labels).To(Equal(arCopy.Labels))
			Expect(ar.Annotations).To(Equal(arCopy.Annotations))
			Expect(ar.DeletionTimestamp.IsZero()).To(BeTrue())
			Expect(ar.Spec).To(Equal(arCopy.Spec))
			Expect(ar.Status).To(Equal(arCopy.Status))
		})

	})

})
