package scheduler_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmcp-project/controller-utils/pkg/collections/filters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	testutils "github.com/openmcp-project/controller-utils/pkg/testing"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	apiconst "github.com/openmcp-project/openmcp-operator/api/constants"
)

var _ = Describe("Preemptive Scheduler", func() {

	Context("Workload = 1", func() {

		It("should create a new exclusive cluster if no cluster exists", func() {
			clusterNamespace := exclusiveString
			cfg, env := defaultTestSetup("testdata", "test-01")
			Expect(env.Client(platform).DeleteAllOf(env.Ctx, &clustersv1alpha1.Cluster{}, client.InNamespace(clusterNamespace))).To(Succeed())
			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(BeEmpty())

			req := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("exclusive-p", "foo"), req)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())

			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(1))
			cluster := existingClusters.Items[0]
			Expect(cluster.Namespace).To(Equal(clusterNamespace))
			Expect(cluster.Name).To(HavePrefix(fmt.Sprintf("%s-", req.Spec.Purpose)))
			Expect(cluster.Namespace).To(Equal(cfg.PurposeMappings[req.Spec.Purpose].Template.Namespace))
			Expect(cluster.Spec.Tenancy).To(BeEquivalentTo(cfg.PurposeMappings[req.Spec.Purpose].Template.Spec.Tenancy))
			Expect(cluster.Finalizers).To(ContainElements(req.FinalizerForCluster()))
		})

		It("should create a new exclusive cluster if a cluster exists", func() {
			clusterNamespace := exclusiveString
			cfg, env := defaultTestSetup("testdata", "test-01")
			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			oldCount := len(existingClusters.Items)

			req := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("exclusive-p", "foo"), req)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())

			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount + 1))
			Expect(existingClusters.Items).To(ContainElements(MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Finalizers": ContainElements(req.FinalizerForCluster()),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"Tenancy": BeEquivalentTo(cfg.PurposeMappings[req.Spec.Purpose].Template.Spec.Tenancy),
				}),
			})))
		})

		It("should create a new shared cluster if no cluster exists", func() {
			clusterNamespace := sharedTwiceString
			cfg, env := defaultTestSetup("testdata", "test-01")
			Expect(env.Client(platform).DeleteAllOf(env.Ctx, &clustersv1alpha1.Cluster{}, client.InNamespace(clusterNamespace))).To(Succeed())
			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(BeEmpty())

			req := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared-p", "foo"), req)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())

			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(1))
			cluster := existingClusters.Items[0]
			Expect(cluster.Namespace).To(Equal(clusterNamespace))
			Expect(cluster.Name).To(HavePrefix(fmt.Sprintf("%s-", req.Spec.Purpose)))
			Expect(cluster.Namespace).To(Equal(cfg.PurposeMappings[req.Spec.Purpose].Template.Namespace))
			Expect(cluster.Spec.Tenancy).To(BeEquivalentTo(cfg.PurposeMappings[req.Spec.Purpose].Template.Spec.Tenancy))
			Expect(cluster.Finalizers).To(ContainElements(req.FinalizerForCluster()))
		})

		It("should share a shared cluster if it still has capacity and create a new one otherwise", func() {
			clusterNamespace := sharedTwiceString
			cfg, env := defaultTestSetup("testdata", "test-01")
			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			oldCount := len(existingClusters.Items)

			// first request
			// should use existing cluster
			req := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared-p", "foo"), req)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())

			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount))
			Expect(existingClusters.Items).To(ContainElements(MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Finalizers": ContainElements(req.FinalizerForCluster()),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"Tenancy": BeEquivalentTo(cfg.PurposeMappings[req.Spec.Purpose].Template.Spec.Tenancy),
				}),
			})))

			// second request
			// should use existing cluster
			req2 := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared2-p", "foo"), req2)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req2))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req2), req2)).To(Succeed())

			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount))
			Expect(existingClusters.Items).To(ContainElements(MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Finalizers": ContainElements(req2.FinalizerForCluster(), req.FinalizerForCluster()),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"Tenancy": BeEquivalentTo(cfg.PurposeMappings[req2.Spec.Purpose].Template.Spec.Tenancy),
				}),
			})))

			// third request
			// should create a new cluster
			req3 := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared3-p", "foo"), req3)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req3))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req3), req3)).To(Succeed())

			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount + 1))
			Expect(existingClusters.Items).To(ContainElements(MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Finalizers": And(ContainElements(req3.FinalizerForCluster()), Not(Or(ContainElements(req.FinalizerForCluster()), ContainElements(req2.FinalizerForCluster())))),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"Tenancy": BeEquivalentTo(cfg.PurposeMappings[req3.Spec.Purpose].Template.Spec.Tenancy),
				}),
			})))
		})

		It("should evict preemptive requests to make space for regular ones", func() {
			clusterNamespace := sharedTwiceString
			cfg, env := defaultTestSetup("testdata", "test-01")
			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			oldCount := len(existingClusters.Items)

			// first preemptive request
			// should use existing cluster
			reqp1 := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared-p", "foo"), reqp1)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(reqp1))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(reqp1), reqp1)).To(Succeed())

			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount))
			Expect(existingClusters.Items).To(ContainElements(MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Finalizers": ContainElements(reqp1.FinalizerForCluster()),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"Tenancy": BeEquivalentTo(cfg.PurposeMappings[reqp1.Spec.Purpose].Template.Spec.Tenancy),
				}),
			})))

			// second preemptive request
			// should use existing cluster
			reqp2 := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared2-p", "foo"), reqp2)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(reqp2))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(reqp2), reqp2)).To(Succeed())

			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount))
			Expect(existingClusters.Items).To(ContainElements(MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Finalizers": ContainElements(reqp2.FinalizerForCluster(), reqp1.FinalizerForCluster()),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"Tenancy": BeEquivalentTo(cfg.PurposeMappings[reqp2.Spec.Purpose].Template.Spec.Tenancy),
				}),
			})))
			var exClusterName string
			for _, c := range existingClusters.Items {
				if slices.Contains(c.Finalizers, reqp2.FinalizerForCluster()) {
					exClusterName = c.Name
					break
				}
			}
			Expect(exClusterName).ToNot(BeEmpty())

			// regular request
			// should be hosted on the existing cluster, but a new cluster should be created for either of the two preemptive requests
			req1 := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared", "foo"), req1)).To(Succeed())
			Expect(req1.Status.Cluster).To(BeNil())

			env.ShouldReconcile(schedulerRec, testutils.RequestFromObject(req1))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req1), req1)).To(Succeed())
			Expect(req1.Status.Cluster).ToNot(BeNil())

			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(reqp1), reqp1)).To(Succeed())
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(reqp2), reqp2)).To(Succeed())
			replacedReqp1 := ctrlutils.HasAnnotationWithValue(reqp1, apiconst.OperationAnnotation, apiconst.OperationAnnotationValueReconcile)
			if !replacedReqp1 {
				Expect(ctrlutils.HasAnnotationWithValue(reqp2, apiconst.OperationAnnotation, apiconst.OperationAnnotationValueReconcile)).To(BeTrue(), "one of the preemptive requests should have a reconcile annotation")
			}

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(reqp1))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(reqp1), reqp1)).To(Succeed())
			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(reqp2))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(reqp2), reqp2)).To(Succeed())

			// the previously existing cluster should now have the finalizer from the regular request
			// and one of the two previous preemptive finalizers
			// and there should be a new cluster with the other preemptive finalizer
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount + 1))

			exCluster := &clustersv1alpha1.Cluster{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey(exClusterName, clusterNamespace), exCluster)).To(Succeed())
			Expect(exCluster.Finalizers).To(ConsistOf(req1.FinalizerForCluster(), ContainSubstring(clustersv1alpha1.PreemptiveRequestFinalizerOnClusterPrefix)))
			if replacedReqp1 {
				Expect(exCluster.Finalizers).To(ContainElement(reqp2.FinalizerForCluster()))
				Expect(existingClusters.Items).To(ContainElements(MatchFields(IgnoreExtras, Fields{
					"ObjectMeta": MatchFields(IgnoreExtras, Fields{
						"Name":       Not(Equal(exClusterName)),
						"Finalizers": ConsistOf(reqp1.FinalizerForCluster()),
					}),
				})))
			} else {
				Expect(exCluster.Finalizers).To(ContainElement(reqp1.FinalizerForCluster()))
				Expect(existingClusters.Items).To(ContainElements(MatchFields(IgnoreExtras, Fields{
					"ObjectMeta": MatchFields(IgnoreExtras, Fields{
						"Name":       Not(Equal(exClusterName)),
						"Finalizers": ConsistOf(reqp2.FinalizerForCluster()),
					}),
				})))
			}
		})

		It("should handle the delete-without-requests label correctly", func() {
			clusterNamespace := "foo"
			_, env := defaultTestSetup("testdata", "test-05")

			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(BeEmpty())

			// should create a new cluster
			req := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("delete-p", "foo"), req)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(1))

			// should delete the cluster
			Expect(env.Client(platform).Delete(env.Ctx, req)).To(Succeed())
			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req))
			Eventually(func() bool {
				return apierrors.IsNotFound(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req), req))
			}, 3).Should(BeTrue(), "Request should be deleted")
			Eventually(func() []clustersv1alpha1.Cluster {
				clusters := &clustersv1alpha1.ClusterList{}
				Expect(env.Client(platform).List(env.Ctx, clusters, client.InNamespace(clusterNamespace))).To(Succeed())
				return clusters.Items
			}, 3).Should(BeEmpty(), "Cluster should be deleted")

			// should create a new cluster
			req2 := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("no-delete-p", "foo"), req2)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req2))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req2), req2)).To(Succeed())
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(1))

			// should not delete the cluster
			Expect(env.Client(platform).Delete(env.Ctx, req2)).To(Succeed())
			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req2))
			Eventually(func() bool {
				return apierrors.IsNotFound(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req2), req2))
			}, 3).Should(BeTrue(), "Request should be deleted")
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(1))
			Expect(existingClusters.Items[0].DeletionTimestamp).To(BeZero(), "Cluster should not be marked for deletion")
		})

	})

	Context("Workload > 1", func() {

		It("should create new exclusive clusters", func() {
			clusterNamespace := exclusiveString
			_, env := defaultTestSetup("testdata", "test-01")
			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			oldCount := len(existingClusters.Items)

			req := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("exclusive-p-m", "foo"), req)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())

			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount + 3))
			Expect(countFinalizers(existingClusters.Items, req.FinalizerForCluster())).To(Equal(3))
		})

		It("should share a shared cluster if it still has capacity and create a new one otherwise, which is deleted again", func() {
			clusterNamespace := sharedFourString
			_, env := defaultTestSetup("testdata", "test-01")
			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			oldCount := len(existingClusters.Items)

			// first request
			// should use existing cluster
			req := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared-p-m", "foo"), req)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())

			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount))
			Expect(countFinalizers(existingClusters.Items, req.FinalizerForCluster())).To(Equal(3))

			// second request
			// should create a new cluster
			req2 := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared2-p-m", "foo"), req2)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req2))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req2), req2)).To(Succeed())

			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount + 1))
			Expect(countFinalizers(existingClusters.Items, req.FinalizerForCluster())).To(Equal(3))
			Expect(countFinalizers(existingClusters.Items, req2.FinalizerForCluster())).To(Equal(3))

			// third request
			// should create a new cluster
			req3 := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared3-p-m", "foo"), req3)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req3))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req3), req3)).To(Succeed())

			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount + 2))
			Expect(countFinalizers(existingClusters.Items, req.FinalizerForCluster())).To(Equal(3))
			Expect(countFinalizers(existingClusters.Items, req2.FinalizerForCluster())).To(Equal(3))
			Expect(countFinalizers(existingClusters.Items, req3.FinalizerForCluster())).To(Equal(3))

			// if the last request is deleted, the cluster should be deleted as well
			Expect(env.Client(platform).Delete(env.Ctx, req3)).To(Succeed())
			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(req3))
			Eventually(func() bool {
				return apierrors.IsNotFound(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req3), req3))
			}, 3).Should(BeTrue(), "Request should be deleted")
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount + 1))
			Expect(countFinalizers(existingClusters.Items, req.FinalizerForCluster())).To(Equal(3))
			Expect(countFinalizers(existingClusters.Items, req2.FinalizerForCluster())).To(Equal(3))
			Expect(countFinalizers(existingClusters.Items, req3.FinalizerForCluster())).To(Equal(0))
		})

		It("should only create a single unlimitedly shared cluster and not remove its preemptive request finalizers", func() {
			clusterNamespace := sharedUnlimitedString
			cfg, env := defaultTestSetup("testdata", "test-01")
			reqCount := 20
			prequests := make([]*clustersv1alpha1.PreemptiveClusterRequest, reqCount)
			for i := range reqCount {
				prequests[i] = &clustersv1alpha1.PreemptiveClusterRequest{}
				prequests[i].SetName(fmt.Sprintf("reqp-%d", i))
				prequests[i].SetNamespace("foo")
				prequests[i].SetUID(uuid.NewUUID())
				prequests[i].Spec.Purpose = sharedUnlimitedString
				prequests[i].Spec.Workload = 3
				Expect(env.Client(platform).Create(env.Ctx, prequests[i])).To(Succeed())
				env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(prequests[i]))
			}
			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(1))
			cluster := existingClusters.Items[0]
			Expect(cluster.Name).To(Equal(prequests[0].Spec.Purpose))
			Expect(cluster.Namespace).To(Equal(cfg.PurposeMappings[prequests[0].Spec.Purpose].Template.Namespace))
			Expect(cluster.Spec.Tenancy).To(BeEquivalentTo(clustersv1alpha1.TENANCY_SHARED))
			Expect(cluster.Finalizers).To(ContainElements(prequests[0].FinalizerForCluster()))
			Expect(cluster.GetTenancyCount()).To(Equal(0))
			Expect(cluster.GetPreemptiveTenancyCount()).To(Equal(reqCount * 3)) // each preemptive request has a workload of 3
			requests := make([]*clustersv1alpha1.ClusterRequest, reqCount)
			for i := range reqCount {
				requests[i] = &clustersv1alpha1.ClusterRequest{}
				requests[i].SetName(fmt.Sprintf("req-%d", i))
				requests[i].SetNamespace("foo")
				requests[i].SetUID(uuid.NewUUID())
				requests[i].Spec.Purpose = sharedUnlimitedString
				Expect(env.Client(platform).Create(env.Ctx, requests[i])).To(Succeed())
				env.ShouldReconcile(schedulerRec, testutils.RequestFromObject(requests[i]))
			}
			for _, req := range requests {
				Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req), req)).To(Succeed())
				Expect(req.Status.Cluster).ToNot(BeNil())
				Expect(req.Status.Cluster.Name).To(Equal(requests[0].Status.Cluster.Name))
				Expect(req.Status.Cluster.Namespace).To(Equal(requests[0].Status.Cluster.Namespace))
			}
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(1))
			cluster = existingClusters.Items[0]
			Expect(cluster.Name).To(Equal(requests[0].Status.Cluster.Name))
			Expect(cluster.Finalizers).To(ContainElements(requests[0].FinalizerForCluster()))
			Expect(cluster.GetTenancyCount()).To(Equal(reqCount))
			Expect(cluster.GetPreemptiveTenancyCount()).To(Equal(reqCount * 3))
		})

		It("should evict preemptive requests to make space for regular ones", func() {
			clusterNamespace := sharedFourString
			_, env := defaultTestSetup("testdata", "test-01")
			existingClusters := &clustersv1alpha1.ClusterList{}
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			oldCount := len(existingClusters.Items)

			// first preemptive request
			// should use existing cluster
			reqp1 := &clustersv1alpha1.PreemptiveClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared-p-m", "foo"), reqp1)).To(Succeed())

			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(reqp1))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(reqp1), reqp1)).To(Succeed())

			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount))
			Expect(countFinalizers(existingClusters.Items, reqp1.FinalizerForCluster())).To(Equal(3))
			var cluster *clustersv1alpha1.Cluster
			clustersWithRequestFinalizers := filters.FilterSlice(existingClusters.Items, func(args ...any) bool {
				c := args[0].(clustersv1alpha1.Cluster)
				return slices.Contains(c.Finalizers, reqp1.FinalizerForCluster())
			})
			Expect(clustersWithRequestFinalizers).To(HaveLen(1))
			cluster = &clustersWithRequestFinalizers[0]

			// first regular request
			// should be hosted on the existing cluster and no new cluster should be created yet
			req1 := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared4", "foo"), req1)).To(Succeed())
			Expect(req1.Status.Cluster).To(BeNil())

			env.ShouldReconcile(schedulerRec, testutils.RequestFromObject(req1))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req1), req1)).To(Succeed())
			Expect(req1.Status.Cluster).ToNot(BeNil())
			Expect(req1.Status.Cluster.Name).To(Equal(cluster.Name))
			Expect(req1.Status.Cluster.Namespace).To(Equal(cluster.Namespace))

			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount))

			// second regular request
			// should be hosted on the existing cluster, but a new cluster should be created holding one of the preemptive request finalizers
			req2 := &clustersv1alpha1.ClusterRequest{}
			Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared5", "foo"), req2)).To(Succeed())
			Expect(req2.Status.Cluster).To(BeNil())

			env.ShouldReconcile(schedulerRec, testutils.RequestFromObject(req2))
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req2), req2)).To(Succeed())
			Expect(req2.Status.Cluster).ToNot(BeNil())
			Expect(req2.Status.Cluster.Name).To(Equal(cluster.Name))
			Expect(req2.Status.Cluster.Namespace).To(Equal(cluster.Namespace))

			// the preemptive request should have a reconcile annotation
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(reqp1), reqp1)).To(Succeed())
			Expect(ctrlutils.HasAnnotationWithValue(reqp1, apiconst.OperationAnnotation, apiconst.OperationAnnotationValueReconcile)).To(BeTrue(), "the preemptive request should have a reconcile annotation")
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount))
			Expect(countFinalizers(existingClusters.Items, reqp1.FinalizerForCluster())).To(Equal(2))

			// after reconciliation, a new cluster should have been created
			env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(reqp1))
			Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
			Expect(existingClusters.Items).To(HaveLen(oldCount + 1))
			Expect(countFinalizers(existingClusters.Items, reqp1.FinalizerForCluster())).To(Equal(3))
			Expect(existingClusters.Items).To(ContainElements(
				MatchFields(IgnoreExtras, Fields{
					"ObjectMeta": MatchFields(IgnoreExtras, Fields{
						"Finalizers": ConsistOf(reqp1.FinalizerForCluster(), reqp1.FinalizerForCluster(), req1.FinalizerForCluster(), req2.FinalizerForCluster()),
					}),
				}),
				MatchFields(IgnoreExtras, Fields{
					"ObjectMeta": MatchFields(IgnoreExtras, Fields{
						"Finalizers": ConsistOf(reqp1.FinalizerForCluster()),
					}),
				}),
			))
		})

	})

	It("should avoid unnecessary clusters by rescheduling preemptive requests from otherwise empty clusters with the deletion label", func() {
		clusterNamespace := sharedFourString
		_, env := defaultTestSetup("testdata", "test-06")
		existingClusters := &clustersv1alpha1.ClusterList{}
		Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
		oldCount := len(existingClusters.Items)

		// let's reconcile all requests to have a clean state
		// although this shouldn't change anything, except for adding finalizers to the requests
		req0 := &clustersv1alpha1.ClusterRequest{}
		Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared-0", "foo"), req0)).To(Succeed())
		env.ShouldReconcile(schedulerRec, testutils.RequestFromObject(req0))
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req0), req0)).To(Succeed())

		req1 := &clustersv1alpha1.ClusterRequest{}
		Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared-1", "foo"), req1)).To(Succeed())
		env.ShouldReconcile(schedulerRec, testutils.RequestFromObject(req1))
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req1), req1)).To(Succeed())

		req2 := &clustersv1alpha1.ClusterRequest{}
		Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared-2", "foo"), req2)).To(Succeed())
		env.ShouldReconcile(schedulerRec, testutils.RequestFromObject(req2))
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req2), req2)).To(Succeed())

		reqp0 := &clustersv1alpha1.PreemptiveClusterRequest{}
		Expect(env.Client(platform).Get(env.Ctx, ctrlutils.ObjectKey("shared-0-p-m", "foo"), reqp0)).To(Succeed())
		env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(reqp0))
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(reqp0), reqp0)).To(Succeed())

		Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
		Expect(existingClusters.Items).To(ConsistOf(
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Finalizers": ConsistOf(reqp0.FinalizerForCluster(), req0.FinalizerForCluster()),
				}),
			}),
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Finalizers": ConsistOf(reqp0.FinalizerForCluster(), req1.FinalizerForCluster()),
				}),
			}),
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Finalizers": ConsistOf(reqp0.FinalizerForCluster(), req2.FinalizerForCluster()),
				}),
			}),
		))

		// if req2 is removed, nothing should happen, because the cluster it is scheduled on doesn't have the deletion label
		Expect(env.Client(platform).Delete(env.Ctx, req2)).To(Succeed())
		env.ShouldReconcile(schedulerRec, testutils.RequestFromObject(req2))
		Eventually(func() bool {
			return apierrors.IsNotFound(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req2), req2))
		}, 3).Should(BeTrue(), "Request should be deleted")
		Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
		Expect(countFinalizers(existingClusters.Items, req2.FinalizerForCluster())).To(Equal(0))
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(reqp0), reqp0)).To(Succeed())
		Expect(ctrlutils.HasAnnotationWithValue(reqp0, apiconst.OperationAnnotation, apiconst.OperationAnnotationValueReconcile)).To(BeFalse(), "the preemptive request should not have a reconcile annotation")
		env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(reqp0))
		Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
		Expect(existingClusters.Items).To(ConsistOf(
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Finalizers": ConsistOf(reqp0.FinalizerForCluster(), req0.FinalizerForCluster()),
				}),
			}),
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Finalizers": ConsistOf(reqp0.FinalizerForCluster(), req1.FinalizerForCluster()),
				}),
			}),
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Finalizers": ConsistOf(reqp0.FinalizerForCluster()),
				}),
			}),
		))

		// if req1 is removed, the preemptive request should be rescheduled, so that the corresponding cluster can be deleted
		// so, now there should be only two clusters, one of which has two finalizers from the preemptive request while the other one has one
		Expect(env.Client(platform).Delete(env.Ctx, req1)).To(Succeed())
		env.ShouldReconcile(schedulerRec, testutils.RequestFromObject(req1))
		Eventually(func() bool {
			return apierrors.IsNotFound(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(req1), req1))
		}, 3).Should(BeTrue(), "Request should be deleted")
		Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
		Expect(countFinalizers(existingClusters.Items, req1.FinalizerForCluster())).To(Equal(0))
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(reqp0), reqp0)).To(Succeed())
		Expect(ctrlutils.HasAnnotationWithValue(reqp0, apiconst.OperationAnnotation, apiconst.OperationAnnotationValueReconcile)).To(BeTrue(), "the preemptive request should have a reconcile annotation")
		env.ShouldReconcile(pSchedulerRec, testutils.RequestFromObject(reqp0))
		Expect(env.Client(platform).List(env.Ctx, existingClusters, client.InNamespace(clusterNamespace))).To(Succeed())
		Expect(existingClusters.Items).To(HaveLen(oldCount - 1))
		Expect(countFinalizers(existingClusters.Items, reqp0.FinalizerForCluster())).To(Equal(3))
		Expect(countFinalizers(existingClusters.Items, req0.FinalizerForCluster())).To(Equal(1))
		Expect(countFinalizers(existingClusters.Items, req1.FinalizerForCluster())).To(Equal(0))
		Expect(countFinalizers(existingClusters.Items, req2.FinalizerForCluster())).To(Equal(0))
		Expect(existingClusters.Items).To(ConsistOf(
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Finalizers": Or(
						ConsistOf(reqp0.FinalizerForCluster(), reqp0.FinalizerForCluster()),
						ConsistOf(reqp0.FinalizerForCluster(), reqp0.FinalizerForCluster(), req0.FinalizerForCluster()),
					),
				}),
			}),
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Finalizers": Or(
						ConsistOf(reqp0.FinalizerForCluster()),
						ConsistOf(reqp0.FinalizerForCluster(), req0.FinalizerForCluster()),
					),
				}),
			}),
		))
	})

})

func countFinalizers(clusters []clustersv1alpha1.Cluster, finalizer string) int {
	count := 0
	for _, c := range clusters {
		for _, fin := range c.Finalizers {
			if fin == finalizer {
				count++
			}
		}
	}
	return count
}
