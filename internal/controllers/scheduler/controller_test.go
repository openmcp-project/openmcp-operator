package scheduler_test

import (
	"fmt"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	testutils "github.com/openmcp-project/controller-utils/pkg/testing"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/api/install"
	"github.com/openmcp-project/openmcp-operator/internal/config"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/scheduler"
)

var scheme = install.InstallOperatorAPIs(runtime.NewScheme())

// defaultTestSetup initializes a new environment for testing the scheduler controller.
// Expected folder structure is a 'config.yaml' file next to a folder named 'cluster' containing the manifests.
func defaultTestSetup(testDirPathSegments ...string) (*scheduler.ClusterScheduler, *testutils.Environment) {
	cfg, err := config.LoadFromFiles(filepath.Join(append(testDirPathSegments, "config.yaml")...))
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg.Default()).To(Succeed())
	Expect(cfg.Validate()).To(Succeed())
	Expect(cfg.Complete()).To(Succeed())
	env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath(append(testDirPathSegments, "cluster")...).WithReconcilerConstructor(func(c client.Client) reconcile.Reconciler {
		r, err := scheduler.NewClusterScheduler(nil, clusters.NewTestClusterFromClient("onboarding", c), cfg.Scheduler)
		Expect(err).ToNot(HaveOccurred())
		return r
	}).Build()
	sc, ok := env.Reconciler().(*scheduler.ClusterScheduler)
	Expect(ok).To(BeTrue(), "Reconciler is not of type ClusterScheduler")
	return sc, env
}

var _ = Describe("Scheduler", func() {

	Context("Scope: Namespaced", func() {

		It("should create a new exclusive cluster if no cluster exists", func() {
			clusterNamespace := "exclusive"
			sc, env := defaultTestSetup("testdata", "test-01")
			Expect(env.Client().DeleteAllOf(env.Ctx, &clustersv1alpha1.Cluster{}, client.InNamespace(clusterNamespace))).To(Succeed())
			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client().List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(BeEmpty())

			req := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("exclusive", "foo"), req)).To(Succeed())
			Expect(req.Status.Cluster).To(BeNil())

			env.ShouldReconcile(testutils.RequestFromObject(req))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())
			Expect(req.Status.Cluster).ToNot(BeNil())

			Expect(env.Client().List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(1))
			cluster := existingClusters.Items[0]
			Expect(cluster.Name).To(Equal(req.Status.Cluster.Name))
			Expect(cluster.Namespace).To(Equal(clusterNamespace))
			Expect(cluster.Name).To(HavePrefix(fmt.Sprintf("%s-", req.Spec.Purpose)))
			Expect(cluster.Namespace).To(Equal(sc.Config.PurposeMappings[req.Spec.Purpose].Template.Namespace))
			Expect(cluster.Spec.Tenancy).To(BeEquivalentTo(sc.Config.PurposeMappings[req.Spec.Purpose].Template.Spec.Tenancy))
			Expect(cluster.Finalizers).To(ContainElements(req.FinalizerForCluster()))
		})

		It("should create a new exclusive cluster if a cluster exists", func() {
			clusterNamespace := "exclusive"
			sc, env := defaultTestSetup("testdata", "test-01")
			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client().List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			oldCount := len(existingClusters.Items)

			req := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("exclusive", "foo"), req)).To(Succeed())
			Expect(req.Status.Cluster).To(BeNil())

			env.ShouldReconcile(testutils.RequestFromObject(req))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())
			Expect(req.Status.Cluster).ToNot(BeNil())

			Expect(env.Client().List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount + 1))
			Expect(existingClusters.Items).To(ContainElements(MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Name":       Equal(req.Status.Cluster.Name),
					"Namespace":  Equal(req.Status.Cluster.Namespace),
					"Finalizers": ContainElements(req.FinalizerForCluster()),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"Tenancy": BeEquivalentTo(sc.Config.PurposeMappings[req.Spec.Purpose].Template.Spec.Tenancy),
				}),
			})))
		})

		It("should create a new shared cluster if no cluster exists", func() {
			clusterNamespace := "shared-twice"
			sc, env := defaultTestSetup("testdata", "test-01")
			Expect(env.Client().DeleteAllOf(env.Ctx, &clustersv1alpha1.Cluster{}, client.InNamespace(clusterNamespace))).To(Succeed())
			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client().List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(BeEmpty())

			req := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("shared", "foo"), req)).To(Succeed())
			Expect(req.Status.Cluster).To(BeNil())

			env.ShouldReconcile(testutils.RequestFromObject(req))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())
			Expect(req.Status.Cluster).ToNot(BeNil())

			Expect(env.Client().List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(1))
			cluster := existingClusters.Items[0]
			Expect(cluster.Name).To(Equal(req.Status.Cluster.Name))
			Expect(cluster.Namespace).To(Equal(clusterNamespace))
			Expect(cluster.Name).To(HavePrefix(fmt.Sprintf("%s-", req.Spec.Purpose)))
			Expect(cluster.Namespace).To(Equal(sc.Config.PurposeMappings[req.Spec.Purpose].Template.Namespace))
			Expect(cluster.Spec.Tenancy).To(BeEquivalentTo(sc.Config.PurposeMappings[req.Spec.Purpose].Template.Spec.Tenancy))
			Expect(cluster.Finalizers).To(ContainElements(req.FinalizerForCluster()))
		})

		It("should share a shared cluster if it still has capacity and create a new one otherwise", func() {
			clusterNamespace := "shared-twice"
			sc, env := defaultTestSetup("testdata", "test-01")
			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client().List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			oldCount := len(existingClusters.Items)

			// first request
			// should use existing cluster
			req := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("shared", "foo"), req)).To(Succeed())
			Expect(req.Status.Cluster).To(BeNil())

			env.ShouldReconcile(testutils.RequestFromObject(req))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())
			Expect(req.Status.Cluster).ToNot(BeNil())

			Expect(env.Client().List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount))
			Expect(existingClusters.Items).To(ContainElements(MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Name":       Equal(req.Status.Cluster.Name),
					"Namespace":  Equal(req.Status.Cluster.Namespace),
					"Finalizers": ContainElements(req.FinalizerForCluster()),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"Tenancy": BeEquivalentTo(sc.Config.PurposeMappings[req.Spec.Purpose].Template.Spec.Tenancy),
				}),
			})))

			// second request
			// should use existing cluster
			req2 := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("shared2", "foo"), req2)).To(Succeed())
			Expect(req2.Status.Cluster).To(BeNil())

			env.ShouldReconcile(testutils.RequestFromObject(req2))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req2), req2)).To(Succeed())
			Expect(req2.Status.Cluster).ToNot(BeNil())

			Expect(env.Client().List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount))
			Expect(existingClusters.Items).To(ContainElements(MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Name":       Equal(req2.Status.Cluster.Name),
					"Namespace":  Equal(req2.Status.Cluster.Namespace),
					"Finalizers": ContainElements(req2.FinalizerForCluster()),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"Tenancy": BeEquivalentTo(sc.Config.PurposeMappings[req2.Spec.Purpose].Template.Spec.Tenancy),
				}),
			})))
			Expect(req2.Status.Cluster.Name).To(Equal(req.Status.Cluster.Name))

			// third request
			// should create a new cluster
			req3 := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("shared3", "foo"), req3)).To(Succeed())
			Expect(req3.Status.Cluster).To(BeNil())

			env.ShouldReconcile(testutils.RequestFromObject(req3))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req3), req3)).To(Succeed())
			Expect(req3.Status.Cluster).ToNot(BeNil())

			Expect(env.Client().List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount + 1))
			Expect(existingClusters.Items).To(ContainElements(MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Name":       Equal(req3.Status.Cluster.Name),
					"Namespace":  Equal(req3.Status.Cluster.Namespace),
					"Finalizers": ContainElements(req3.FinalizerForCluster()),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"Tenancy": BeEquivalentTo(sc.Config.PurposeMappings[req3.Spec.Purpose].Template.Spec.Tenancy),
				}),
			})))
			Expect(req3.Status.Cluster.Name).ToNot(Equal(req.Status.Cluster.Name))
			Expect(req3.Status.Cluster.Name).ToNot(Equal(req2.Status.Cluster.Name))
		})

		It("should only create a new cluster if none exists for unlimitedly shared clusters", func() {
			clusterNamespace := "shared-unlimited"
			sc, env := defaultTestSetup("testdata", "test-01")
			reqCount := 20
			requests := make([]*clustersv1alpha1.ClusterRequest, reqCount)
			for i := range reqCount {
				requests[i] = &clustersv1alpha1.ClusterRequest{}
				requests[i].SetName(fmt.Sprintf("req-%d", i))
				requests[i].SetNamespace("foo")
				requests[i].SetUID(uuid.NewUUID())
				requests[i].Spec.Purpose = "shared-unlimited"
				Expect(env.Client().Create(env.Ctx, requests[i])).To(Succeed())
				env.ShouldReconcile(testutils.RequestFromObject(requests[i]))
			}
			for _, req := range requests {
				Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())
				Expect(req.Status.Cluster).ToNot(BeNil())
				Expect(req.Status.Cluster.Name).To(Equal(requests[0].Status.Cluster.Name))
				Expect(req.Status.Cluster.Namespace).To(Equal(requests[0].Status.Cluster.Namespace))
			}
			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client().List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(1))
			cluster := existingClusters.Items[0]
			Expect(cluster.Name).To(Equal(requests[0].Status.Cluster.Name))
			Expect(cluster.Namespace).To(Equal(clusterNamespace))
			Expect(cluster.Name).To(Equal(requests[0].Spec.Purpose))
			Expect(cluster.Namespace).To(Equal(sc.Config.PurposeMappings[requests[0].Spec.Purpose].Template.Namespace))
			Expect(cluster.Spec.Tenancy).To(BeEquivalentTo(clustersv1alpha1.TENANCY_SHARED))
			Expect(cluster.Finalizers).To(ContainElements(requests[0].FinalizerForCluster()))
		})

		It("should take over annotations and labels from the cluster template", func() {
			_, env := defaultTestSetup("testdata", "test-02")

			req := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("exclusive", "foo"), req)).To(Succeed())
			Expect(req.Status.Cluster).To(BeNil())

			env.ShouldReconcile(testutils.RequestFromObject(req))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())
			Expect(req.Status.Cluster).ToNot(BeNil())

			cluster := &clustersv1alpha1.Cluster{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey(req.Status.Cluster.Name, req.Status.Cluster.Namespace), cluster)).To(Succeed())
			Expect(cluster.Labels).To(HaveKeyWithValue("foo.bar.baz/foobar", "true"))
			Expect(cluster.Annotations).To(HaveKeyWithValue("foo.bar.baz/foobar", "false"))
		})

		It("should use the request's namespace if none is specified in the template and ignore clusters that don't match the label selector", func() {
			clusterNamespace := "foo"
			_, env := defaultTestSetup("testdata", "test-02")

			req := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("shared", "foo"), req)).To(Succeed())
			Expect(req.Status.Cluster).To(BeNil())

			fooClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client().List(env.Ctx, fooClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(fooClusters.Items).ToNot(BeEmpty())
			oldCountFoo := len(fooClusters.Items)
			for _, cluster := range fooClusters.Items {
				Expect(cluster.Labels).ToNot(HaveKeyWithValue("foo.bar.baz/foobar", "true"))
			}

			// this should create a new cluster in 'foo'
			// because the existing ones' labels don't match the selector
			env.ShouldReconcile(testutils.RequestFromObject(req))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())
			Expect(req.Status.Cluster).ToNot(BeNil())
			Expect(env.Client().List(env.Ctx, fooClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(fooClusters.Items).To(HaveLen(oldCountFoo + 1))
			oldCountFoo = len(fooClusters.Items)

			// this should create a new cluster in 'bar'
			req2 := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("shared", "bar"), req2)).To(Succeed())
			Expect(req2.Status.Cluster).To(BeNil())
			barClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client().List(env.Ctx, barClusters, client.InNamespace("bar"))).To(Succeed())
			oldCountBar := len(barClusters.Items)
			env.ShouldReconcile(testutils.RequestFromObject(req2))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req2), req2)).To(Succeed())
			Expect(req2.Status.Cluster).ToNot(BeNil())
			Expect(env.Client().List(env.Ctx, barClusters, client.InNamespace("bar"))).To(Succeed())
			Expect(barClusters.Items).To(HaveLen(oldCountBar + 1))

			// this should re-use the existing cluster in 'foo'
			req3 := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("shared2", "foo"), req3)).To(Succeed())
			Expect(req3.Status.Cluster).To(BeNil())
			env.ShouldReconcile(testutils.RequestFromObject(req3))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req3), req3)).To(Succeed())
			Expect(req3.Status.Cluster).ToNot(BeNil())
			Expect(req3.Status.Cluster.Name).To(Equal(req.Status.Cluster.Name))
			Expect(req3.Status.Cluster.Namespace).To(Equal(req.Status.Cluster.Namespace))
			Expect(env.Client().List(env.Ctx, fooClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(fooClusters.Items).To(HaveLen(oldCountFoo))
		})

	})

	Context("Scope: Cluster", func() {

		It("should evaluate all namespaces in cluster scope", func() {
			_, env := defaultTestSetup("testdata", "test-03")

			req := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("shared", "foo"), req)).To(Succeed())
			Expect(req.Status.Cluster).To(BeNil())

			clusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client().List(env.Ctx, clusters)).To(Succeed())
			Expect(clusters.Items).ToNot(BeEmpty())
			oldCount := len(clusters.Items)
			for _, cluster := range clusters.Items {
				Expect(cluster.Labels).ToNot(HaveKeyWithValue("foo.bar.baz/foobar", "true"))
			}

			// this should create a new cluster in 'foo'
			// because the existing ones' labels don't match the selector
			env.ShouldReconcile(testutils.RequestFromObject(req))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())
			Expect(req.Status.Cluster).ToNot(BeNil())
			Expect(env.Client().List(env.Ctx, clusters)).To(Succeed())
			Expect(clusters.Items).To(HaveLen(oldCount + 1))
			oldCount = len(clusters.Items)

			// this should re-use the existing cluster
			req2 := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("shared2", "bar"), req2)).To(Succeed())
			Expect(req2.Status.Cluster).To(BeNil())
			env.ShouldReconcile(testutils.RequestFromObject(req2))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req2), req2)).To(Succeed())
			Expect(req2.Status.Cluster).ToNot(BeNil())
			Expect(req2.Status.Cluster.Name).To(Equal(req.Status.Cluster.Name))
			Expect(req2.Status.Cluster.Namespace).To(Equal(req.Status.Cluster.Namespace))
			Expect(env.Client().List(env.Ctx, clusters)).To(Succeed())
			Expect(clusters.Items).To(HaveLen(oldCount))

			// this should re-use the existing cluster
			req3 := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("shared3", "baz"), req3)).To(Succeed())
			Expect(req3.Status.Cluster).To(BeNil())
			env.ShouldReconcile(testutils.RequestFromObject(req3))
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req3), req3)).To(Succeed())
			Expect(req3.Status.Cluster).ToNot(BeNil())
			Expect(req3.Status.Cluster.Name).To(Equal(req.Status.Cluster.Name))
			Expect(req3.Status.Cluster.Namespace).To(Equal(req.Status.Cluster.Namespace))
			Expect(env.Client().List(env.Ctx, clusters)).To(Succeed())
			Expect(clusters.Items).To(HaveLen(oldCount))
		})

	})

	It("should combine cluster label selectors correctly", func() {
		_, env := defaultTestSetup("testdata", "test-04")

		// should use the existing cluster
		req := &clustersv1alpha1.ClusterRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("shared", "foo"), req)).To(Succeed())
		Expect(req.Status.Cluster).To(BeNil())
		env.ShouldReconcile(testutils.RequestFromObject(req))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())
		Expect(req.Status.Cluster).ToNot(BeNil())
		Expect(req.Status.Cluster.Name).To(Equal("shared"))
		Expect(req.Status.Cluster.Namespace).To(Equal("foo"))

		// should create a new cluster
		req2 := &clustersv1alpha1.ClusterRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("shared2", "foo"), req2)).To(Succeed())
		Expect(req2.Status.Cluster).To(BeNil())
		env.ShouldReconcile(testutils.RequestFromObject(req2))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req2), req2)).To(Succeed())
		Expect(req2.Status.Cluster).ToNot(BeNil())
		Expect(req2.Status.Cluster.Name).To(Equal("shared2"))
		Expect(req2.Status.Cluster.Namespace).To(Equal("foo"))

		// should use the existing cluster
		req3 := &clustersv1alpha1.ClusterRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("shared3", "foo"), req3)).To(Succeed())
		Expect(req3.Status.Cluster).To(BeNil())
		env.ShouldReconcile(testutils.RequestFromObject(req3))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req3), req3)).To(Succeed())
		Expect(req3.Status.Cluster).ToNot(BeNil())
		Expect(req3.Status.Cluster.Name).To(Equal("shared2"))
		Expect(req3.Status.Cluster.Namespace).To(Equal("foo"))
	})

	It("should handle the delete-without-requests label correctly", func() {
		_, env := defaultTestSetup("testdata", "test-05")

		// should create a new cluster
		req := &clustersv1alpha1.ClusterRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("delete", "foo"), req)).To(Succeed())
		Expect(req.Status.Cluster).To(BeNil())

		env.ShouldReconcile(testutils.RequestFromObject(req))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())
		Expect(req.Status.Cluster).ToNot(BeNil())
		cluster := &clustersv1alpha1.Cluster{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey(req.Status.Cluster.Name, req.Status.Cluster.Namespace), cluster)).To(Succeed())
		Expect(cluster.Labels).To(HaveKeyWithValue(clustersv1alpha1.DeleteWithoutRequestsLabel, "true"))

		// should delete the cluster
		Expect(env.Client().Delete(env.Ctx, req)).To(Succeed())
		env.ShouldReconcile(testutils.RequestFromObject(req))
		Eventually(func() bool {
			return apierrors.IsNotFound(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req), req))
		}, 3).Should(BeTrue(), "Request should be deleted")
		Eventually(func() bool {
			return apierrors.IsNotFound(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(cluster), cluster))
		}, 3).Should(BeTrue(), "Cluster should be deleted")

		// should create a new cluster
		req2 := &clustersv1alpha1.ClusterRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("no-delete", "foo"), req2)).To(Succeed())
		Expect(req2.Status.Cluster).To(BeNil())

		env.ShouldReconcile(testutils.RequestFromObject(req2))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req2), req2)).To(Succeed())
		Expect(req2.Status.Cluster).ToNot(BeNil())
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey(req2.Status.Cluster.Name, req2.Status.Cluster.Namespace), cluster)).To(Succeed())
		Expect(cluster.Labels).To(HaveKeyWithValue(clustersv1alpha1.DeleteWithoutRequestsLabel, "false"))

		// should not delete the cluster
		Expect(env.Client().Delete(env.Ctx, req2)).To(Succeed())
		env.ShouldReconcile(testutils.RequestFromObject(req2))
		Eventually(func() bool {
			return apierrors.IsNotFound(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(req2), req2))
		}, 3).Should(BeTrue(), "Request should be deleted")
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed(), "Cluster should not be deleted")
		Expect(cluster.DeletionTimestamp).To(BeZero(), "Cluster should not be marked for deletion")
	})

	It("should not consider clusters that are in deletion for scheduling", func() {
		// verify that the cluster is usually considered for scheduling
		_, env := defaultTestSetup("testdata", "test-01")

		c := &clustersv1alpha1.Cluster{}
		c.SetName("shared-1")
		c.SetNamespace("shared-twice")
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(c), c)).To(Succeed())

		cr := &clustersv1alpha1.ClusterRequest{}
		cr.SetName("shared")
		cr.SetNamespace("foo")
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())
		env.ShouldReconcile(testutils.RequestFromObject(cr))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())
		Expect(cr.Status.Cluster).ToNot(BeNil())
		Expect(cr.Status.Cluster.Name).To(Equal(c.Name))
		Expect(cr.Status.Cluster.Namespace).To(Equal(c.Namespace))

		// repeat, but with the cluster in deletion
		_, env = defaultTestSetup("testdata", "test-01")

		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(c), c)).To(Succeed())
		c.Finalizers = []string{"foo"}
		Expect(env.Client().Update(env.Ctx, c)).To(Succeed())
		Expect(env.Client().Delete(env.Ctx, c)).To(Succeed())
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(c), c)).To(Succeed())
		Expect(c.DeletionTimestamp).ToNot(BeZero(), "Cluster should be marked for deletion")

		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())
		env.ShouldReconcile(testutils.RequestFromObject(cr))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())
		Expect(cr.Status.Cluster).ToNot(BeNil())
		Expect(cr.Status.Cluster.Name).ToNot(Equal(c.Name), "Cluster is in deletion and should not be considered for scheduling")
		Expect(cr.Status.Cluster.Namespace).To(Equal(c.Namespace))

	})

})
