// nolint:goconst,unparam
package helm_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	. "github.com/openmcp-project/controller-utils/pkg/testing/matchers"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	fluxhelmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxsourcev1 "github.com/fluxcd/source-controller/api/v1"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/collections"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	testutils "github.com/openmcp-project/controller-utils/pkg/testing"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"
	helmv1alpha1 "github.com/openmcp-project/openmcp-operator/api/helm/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/api/install"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/helm"
)

const (
	platform          = "platform"
	hdRec             = "helmdeployment"
	cRec              = "cluster"
	providerName      = "helmdeploy"
	providerNamespace = "openmcp-system"
	environment       = "test"
)

// defaultTestSetup initializes a new environment for testing the HelmDeployment controller.
// Expected folder structure is a 'config.yaml' file next to a 'platform' and 'onboarding' directory, containing the manifests for the respective clusters.
func defaultTestSetup(testDirPathSegments ...string) (*testutils.ComplexEnvironment, collections.Queue[*clustersv1alpha1.Cluster], map[string]client.Client) {
	clusterReconcileQueue := collections.NewLinkedList[*clustersv1alpha1.Cluster]()
	fakeClients := map[string]client.Client{}
	// a little bit hacky, because we need to register two reconcilers, but one of them depends on the other
	// and the order in which their constructors are called is not deterministic
	var injectClusterReconciler func(*testutils.ComplexEnvironment)
	env := testutils.NewComplexEnvironmentBuilder().
		WithFakeClient(platform, install.InstallOperatorAPIsPlatform(runtime.NewScheme())).
		WithInitObjectPath(platform, testDirPathSegments...).
		WithDynamicObjectsWithStatus(platform, &clustersv1alpha1.Cluster{}, &clustersv1alpha1.AccessRequest{}, &fluxhelmv2.HelmRelease{}, &fluxsourcev1.GitRepository{}, &fluxsourcev1.HelmRepository{}, &fluxsourcev1.OCIRepository{}).
		WithUIDs(platform).
		WithReconcilerConstructor(hdRec, func(clients ...client.Client) reconcile.Reconciler {
			clusterReconciler := helm.TestHelmDeploymentClusterController(clusters.NewTestClusterFromClient(platform, clients[0]), providerName, fakeClients)
			injectClusterReconciler = func(env *testutils.ComplexEnvironment) {
				env.Reconcilers[cRec] = clusterReconciler
			}
			return helm.TestHelmDeploymentController(clusters.NewTestClusterFromClient(platform, clients[0]), providerName, providerNamespace, environment, clusterReconciler, clusterReconcileQueue)
		}, platform).
		Build()
	injectClusterReconciler(env)

	return env, clusterReconcileQueue, fakeClients
}

var _ = Describe("HelmDeployment Controller", func() {

	It("should not create any HelmReleases if there are no fitting clusters", func() {
		env, cq, _ := defaultTestSetup("testdata", "test-01")

		hd := &helmv1alpha1.HelmDeployment{}
		hd.Name = "hd-0"
		hd.Namespace = "default"
		env.ShouldReconcile(hdRec, testutils.RequestFromObject(hd))

		// verify HelmDeployment status
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Phase).To(Equal(commonapi.StatusPhaseReady))
		// verify that no cluster was queued for reconciliation
		Expect(cq.Size()).To(Equal(0))
	})

	It("should create HelmReleases for all fitting clusters", func() {
		env, cq, _ := defaultTestSetup("testdata", "test-02")

		hd := &helmv1alpha1.HelmDeployment{}
		hd.Name = "hd-0"
		hd.Namespace = "default"
		hdreq := testutils.RequestFromObject(hd)

		Eventually(func(g Gomega) {
			// reconcile a cluster, if requested
			if cq.Size() > 0 {
				cluster := cq.Poll()
				_, err := env.Reconciler(cRec).Reconcile(env.Ctx, testutils.RequestFromObject(cluster))
				g.Expect(err).ToNot(HaveOccurred())
			}
			// reconcile HelmDeployment
			rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
			g.Expect(err).ToNot(HaveOccurred())
			// mock flux source and HelmRelease readiness
			mockAllFluxResourcesReady(g, env)
			// at one point, the HelmDeployment should not be requeued anymore
			g.Expect(rr.RequeueAfter).To(BeZero())
			// verify that no more clusters are queued for reconciliation
			g.Expect(cq.Size()).To(Equal(0))
		}).Should(Succeed())

		// list HelmReleases
		hrList := &fluxhelmv2.HelmReleaseList{}
		Expect(env.Client(platform).List(env.Ctx, hrList)).To(Succeed())
		Expect(hrList.Items).To(ConsistOf(
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Labels": Equal(map[string]string{
						openmcpconst.ManagedByLabel:      providerName + "." + helm.ControllerName,
						openmcpconst.ManagedPurposeLabel: "default.hd-0",
						helmv1alpha1.ClusterNameLabel:    "cluster-0",
					}),
				}),
			}),
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Labels": Equal(map[string]string{
						openmcpconst.ManagedByLabel:      providerName + "." + helm.ControllerName,
						openmcpconst.ManagedPurposeLabel: "default.hd-0",
						helmv1alpha1.ClusterNameLabel:    "cluster-1",
					}),
				}),
			}),
		))
		// verify HelmDeployment status
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Finalizers).To(ContainElements(helmv1alpha1.Finalizer))
		Expect(hd.Status.Phase).To(Equal(commonapi.StatusPhaseReady))
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType("Cluster.default_cluster-0").WithStatus(metav1.ConditionTrue)),
			MatchCondition(TestCondition().WithType("Cluster.default_cluster-1").WithStatus(metav1.ConditionTrue)),
		))
		// list clusters to verify finalizers
		clusters := &clustersv1alpha1.ClusterList{}
		Expect(env.Client(platform).List(env.Ctx, clusters)).To(Succeed())
		Expect(clusters.Items).To(ConsistOf(
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Name":       Equal("cluster-0"),
					"Namespace":  Equal("default"),
					"Finalizers": ContainElements(hd.Finalizer()),
				}),
			}),
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Name":       Equal("cluster-1"),
					"Namespace":  Equal("default"),
					"Finalizers": ContainElements(hd.Finalizer()),
				}),
			}),
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Name":       Equal("cluster-nomatch"),
					"Namespace":  Equal("default"),
					"Finalizers": Not(ContainElements(hd.Finalizer())),
				}),
			}),
		))
		// verify that no cluster was queued for reconciliation
		Expect(cq.Size()).To(Equal(0))
	})

	It("should create further HelmReleases if a new fitting cluster appears", func() {
		env, cq, _ := defaultTestSetup("testdata", "test-02")

		hd := &helmv1alpha1.HelmDeployment{}
		hd.Name = "hd-0"
		hd.Namespace = "default"
		hdreq := testutils.RequestFromObject(hd)

		Eventually(func(g Gomega) {
			// reconcile a cluster, if requested
			if cq.Size() > 0 {
				cluster := cq.Poll()
				_, err := env.Reconciler(cRec).Reconcile(env.Ctx, testutils.RequestFromObject(cluster))
				g.Expect(err).ToNot(HaveOccurred())
			}
			// reconcile HelmDeployment
			rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
			g.Expect(err).ToNot(HaveOccurred())
			// mock flux source and HelmRelease readiness
			mockAllFluxResourcesReady(g, env)
			// at one point, the HelmDeployment should not be requeued anymore
			g.Expect(rr.RequeueAfter).To(BeZero())
			// verify that no more clusters are queued for reconciliation
			g.Expect(cq.Size()).To(Equal(0))
		}).Should(Succeed())

		// list HelmReleases
		hrList := &fluxhelmv2.HelmReleaseList{}
		Expect(env.Client(platform).List(env.Ctx, hrList)).To(Succeed())
		Expect(hrList.Items).To(HaveLen(2))
		// verify HelmDeployment status
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Phase).To(Equal(commonapi.StatusPhaseReady))
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType("Cluster.default_cluster-0").WithStatus(metav1.ConditionTrue)),
			MatchCondition(TestCondition().WithType("Cluster.default_cluster-1").WithStatus(metav1.ConditionTrue)),
		))
		// verify that no cluster was queued for reconciliation
		Expect(cq.Size()).To(Equal(0))

		// add a new cluster that fits the HelmDeployment's selector
		newCluster := &clustersv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-2",
				Namespace: "default",
			},
			Spec: clustersv1alpha1.ClusterSpec{
				Profile:  "myprofile",
				Purposes: []string{"workload"},
				Tenancy:  clustersv1alpha1.TENANCY_SHARED,
			},
		}
		Expect(env.Client(platform).Create(env.Ctx, newCluster)).To(Succeed())

		Eventually(func(g Gomega) {
			// reconcile a cluster, if requested
			if cq.Size() > 0 {
				cluster := cq.Poll()
				_, err := env.Reconciler(cRec).Reconcile(env.Ctx, testutils.RequestFromObject(cluster))
				g.Expect(err).ToNot(HaveOccurred())
			}
			// reconcile HelmDeployment
			rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
			g.Expect(err).ToNot(HaveOccurred())
			// mock flux source and HelmRelease readiness
			mockAllFluxResourcesReady(g, env)
			// at one point, the HelmDeployment should not be requeued anymore
			g.Expect(rr.RequeueAfter).To(BeZero())
			// verify that no more clusters are queued for reconciliation
			g.Expect(cq.Size()).To(Equal(0))
		}).Should(Succeed())

		// list HelmReleases
		Expect(env.Client(platform).List(env.Ctx, hrList)).To(Succeed())
		Expect(hrList.Items).To(HaveLen(3))
		// verify HelmDeployment status
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Phase).To(Equal(commonapi.StatusPhaseReady))
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType("Cluster.default_cluster-0").WithStatus(metav1.ConditionTrue)),
			MatchCondition(TestCondition().WithType("Cluster.default_cluster-1").WithStatus(metav1.ConditionTrue)),
			MatchCondition(TestCondition().WithType("Cluster.default_cluster-2").WithStatus(metav1.ConditionTrue)),
		))
		// verify that no cluster was queued for reconciliation
		Expect(cq.Size()).To(Equal(0))
	})

	It("should correctly set the HelmDeployment conditions during reconciliation (create/update)", func() {
		env, _, _ := defaultTestSetup("testdata", "test-02")

		hd := &helmv1alpha1.HelmDeployment{}
		hd.Name = "hd-1"
		hd.Namespace = "default"
		hdreq := testutils.RequestFromObject(hd)

		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Conditions).To(BeEmpty())

		// reconcile HelmDeployment
		// expected condition: false with reason 'ClusterAccessNotAvailable'
		ccon := "Cluster.default_cluster-0"
		rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).ToNot(BeZero())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType(ccon).WithStatus(metav1.ConditionFalse).WithReason(helmv1alpha1.ReasonClusterAccessNotAvailable)),
		))

		// reconcile cluster once (should not be enough to fix the access)
		creq := testutils.RequestFromStrings("cluster-0", "default")
		rr, err = env.Reconciler(cRec).Reconcile(env.Ctx, creq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).ToNot(BeZero())

		// reconcile HelmDeployment
		// expected condition: still false with reason 'ClusterAccessNotAvailable'
		rr, err = env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).ToNot(BeZero())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType(ccon).WithStatus(metav1.ConditionFalse).WithReason(helmv1alpha1.ReasonClusterAccessNotAvailable)),
		))

		// reconcile cluster until AccessRequest is ready
		Eventually(func(g Gomega) {
			rr, err := env.Reconciler(cRec).Reconcile(env.Ctx, creq)
			Expect(err).ToNot(HaveOccurred())
			Expect(rr.RequeueAfter).To(BeZero())
		}).Should(Succeed())

		// reconcile HelmDeployment
		// expected condition: false with reason 'WaitingForHelmChartSourceHealthy'
		rr, err = env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).ToNot(BeZero())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType(ccon).WithStatus(metav1.ConditionFalse).WithReason(helmv1alpha1.ReasonWaitingForHelmChartSourceHealthy)),
		))

		// mock flux source healthiness
		mockAllFluxResourcesReady(Default, env)

		// reconcile HelmDeployment
		// expected condition: false with reason 'WaitingForHelmReleaseHealthy'
		rr, err = env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).ToNot(BeZero())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType(ccon).WithStatus(metav1.ConditionFalse).WithReason(helmv1alpha1.ReasonWaitingForHelmReleaseHealthy)),
		))

		// mock HelmRelease healthiness
		mockAllFluxResourcesReady(Default, env)

		// reconcile HelmDeployment
		// expected condition: true with reason 'FluxResourcesDeployedAndHealthy'
		rr, err = env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).To(BeZero())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType(ccon).WithStatus(metav1.ConditionTrue).WithReason(helmv1alpha1.ReasonFluxResourcesDeployedAndHealthy)),
		))
	})

	It("should correctly set the HelmDeployment conditions during reconciliation (delete HelmDeployment)", func() {
		env, cq, _ := defaultTestSetup("testdata", "test-02")

		hd := &helmv1alpha1.HelmDeployment{}
		hd.Name = "hd-1"
		hd.Namespace = "default"
		hdreq := testutils.RequestFromObject(hd)

		Eventually(func(g Gomega) {
			// reconcile a cluster, if requested
			if cq.Size() > 0 {
				cluster := cq.Poll()
				_, err := env.Reconciler(cRec).Reconcile(env.Ctx, testutils.RequestFromObject(cluster))
				g.Expect(err).ToNot(HaveOccurred())
			}
			// reconcile HelmDeployment
			rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
			g.Expect(err).ToNot(HaveOccurred())
			// mock flux source and HelmRelease readiness
			mockAllFluxResourcesReady(g, env)
			// at one point, the HelmDeployment should not be requeued anymore
			g.Expect(rr.RequeueAfter).To(BeZero())
			// verify that no more clusters are queued for reconciliation
			g.Expect(cq.Size()).To(Equal(0))
		}).Should(Succeed())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Finalizers).To(ContainElements(helmv1alpha1.Finalizer))

		// mock finalizers on the flux resources to simulate flux behavior
		oci := &fluxsourcev1.OCIRepository{}
		oci.Name = helm.FluxResourceName("cluster-0", hd.Name, providerName)
		oci.Namespace = "default"
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(oci), oci)).To(Succeed())
		oci.Finalizers = []string{"dummy"}
		Expect(env.Client(platform).Update(env.Ctx, oci)).To(Succeed())
		hr := &fluxhelmv2.HelmRelease{}
		hr.Name = helm.FluxResourceName("cluster-0", hd.Name, providerName)
		hr.Namespace = "default"
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hr), hr)).To(Succeed())
		hr.Finalizers = []string{"dummy"}
		Expect(env.Client(platform).Update(env.Ctx, hr)).To(Succeed())

		// delete the HelmDeployment to trigger deletion of flux resources
		Expect(env.Client(platform).Delete(env.Ctx, hd)).To(Succeed())

		// reconcile HelmDeployment
		// expected condition: false with reason 'WaitingForHelmReleaseDeletion' (due to finalizer)
		ccon := "Cluster.default_cluster-0"
		rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).ToNot(BeZero())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType(ccon).WithStatus(metav1.ConditionFalse).WithReason(helmv1alpha1.ReasonWaitingForHelmReleaseDeletion)),
		))

		// remove HelmRelease finalizer
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hr), hr)).To(Succeed())
		hr.Finalizers = nil
		Expect(env.Client(platform).Update(env.Ctx, hr)).To(Succeed())

		// reconcile HelmDeployment
		// expected condition: false with reason 'WaitingForHelmChartSourceDeletion' (due to finalizer)
		rr, err = env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).ToNot(BeZero())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType(ccon).WithStatus(metav1.ConditionFalse).WithReason(helmv1alpha1.ReasonWaitingForHelmChartSourceDeletion)),
		))

		// remove OCIRepository finalizer
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(oci), oci)).To(Succeed())
		oci.Finalizers = nil
		Expect(env.Client(platform).Update(env.Ctx, oci)).To(Succeed())

		// reconcile HelmDeployment
		// should be gone afterwards
		rr, err = env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).To(BeZero())
		cluster := &clustersv1alpha1.Cluster{}
		cluster.Name = "cluster-0"
		cluster.Namespace = "default"
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
		Expect(cluster.Finalizers).ToNot(ContainElements(hd.Finalizer()))
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
	})

	It("should correctly set the HelmDeployment conditions during reconciliation (delete Cluster)", func() {
		env, cq, _ := defaultTestSetup("testdata", "test-02")

		hd := &helmv1alpha1.HelmDeployment{}
		hd.Name = "hd-1"
		hd.Namespace = "default"
		hdreq := testutils.RequestFromObject(hd)

		Eventually(func(g Gomega) {
			// reconcile a cluster, if requested
			if cq.Size() > 0 {
				cluster := cq.Poll()
				_, err := env.Reconciler(cRec).Reconcile(env.Ctx, testutils.RequestFromObject(cluster))
				g.Expect(err).ToNot(HaveOccurred())
			}
			// reconcile HelmDeployment
			rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
			g.Expect(err).ToNot(HaveOccurred())
			// mock flux source and HelmRelease readiness
			mockAllFluxResourcesReady(g, env)
			// at one point, the HelmDeployment should not be requeued anymore
			g.Expect(rr.RequeueAfter).To(BeZero())
			// verify that no more clusters are queued for reconciliation
			g.Expect(cq.Size()).To(Equal(0))
		}).Should(Succeed())

		// mock finalizers on the flux resources to simulate flux behavior
		oci := &fluxsourcev1.OCIRepository{}
		oci.Name = helm.FluxResourceName("cluster-0", hd.Name, providerName)
		oci.Namespace = "default"
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(oci), oci)).To(Succeed())
		oci.Finalizers = []string{"dummy"}
		Expect(env.Client(platform).Update(env.Ctx, oci)).To(Succeed())
		hr := &fluxhelmv2.HelmRelease{}
		hr.Name = helm.FluxResourceName("cluster-0", hd.Name, providerName)
		hr.Namespace = "default"
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hr), hr)).To(Succeed())
		hr.Finalizers = []string{"dummy"}
		Expect(env.Client(platform).Update(env.Ctx, hr)).To(Succeed())

		// delete the Cluster to trigger deletion of flux resources
		cluster := &clustersv1alpha1.Cluster{}
		cluster.Name = "cluster-0"
		cluster.Namespace = "default"
		Expect(env.Client(platform).Delete(env.Ctx, cluster)).To(Succeed())

		// reconcile HelmDeployment
		// expected condition: false with reason 'WaitingForHelmReleaseDeletion' (due to finalizer)
		ccon := "Cluster.default_cluster-0"
		rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).ToNot(BeZero())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType(ccon).WithStatus(metav1.ConditionFalse).WithReason(helmv1alpha1.ReasonWaitingForHelmReleaseDeletion)),
		))
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())

		// remove HelmRelease finalizer
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hr), hr)).To(Succeed())
		hr.Finalizers = nil
		Expect(env.Client(platform).Update(env.Ctx, hr)).To(Succeed())

		// reconcile HelmDeployment
		// expected condition: false with reason 'WaitingForHelmChartSourceDeletion' (due to finalizer)
		rr, err = env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).ToNot(BeZero())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType(ccon).WithStatus(metav1.ConditionFalse).WithReason(helmv1alpha1.ReasonWaitingForHelmChartSourceDeletion)),
		))
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())

		// remove OCIRepository finalizer
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(oci), oci)).To(Succeed())
		oci.Finalizers = nil
		Expect(env.Client(platform).Update(env.Ctx, oci)).To(Succeed())

		// reconcile HelmDeployment
		// the Cluster should be gone afterwards (or at least it should not have the HelmDeployment's finalizer anymore), as should its condition
		rr, err = env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).To(BeZero())
		err = env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(cluster), cluster)
		if err == nil {
			Expect(cluster.Finalizers).ToNot(ContainElements(hd.Finalizer()))
		} else {
			Expect(err).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
		}
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Conditions).ToNot(ContainElement(MatchCondition(TestCondition().WithType(ccon))))
	})

	It("should correctly set the HelmDeployment conditions during reconciliation (delete HelmDeployment)", func() {
		env, cq, _ := defaultTestSetup("testdata", "test-02")

		hd := &helmv1alpha1.HelmDeployment{}
		hd.Name = "hd-1"
		hd.Namespace = "default"
		hdreq := testutils.RequestFromObject(hd)

		Eventually(func(g Gomega) {
			// reconcile a cluster, if requested
			if cq.Size() > 0 {
				cluster := cq.Poll()
				_, err := env.Reconciler(cRec).Reconcile(env.Ctx, testutils.RequestFromObject(cluster))
				g.Expect(err).ToNot(HaveOccurred())
			}
			// reconcile HelmDeployment
			rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
			g.Expect(err).ToNot(HaveOccurred())
			// mock flux source and HelmRelease readiness
			mockAllFluxResourcesReady(g, env)
			// at one point, the HelmDeployment should not be requeued anymore
			g.Expect(rr.RequeueAfter).To(BeZero())
			// verify that no more clusters are queued for reconciliation
			g.Expect(cq.Size()).To(Equal(0))
		}).Should(Succeed())

		// mock finalizers on the flux resources to simulate flux behavior
		oci := &fluxsourcev1.OCIRepository{}
		oci.Name = helm.FluxResourceName("cluster-0", hd.Name, providerName)
		oci.Namespace = "default"
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(oci), oci)).To(Succeed())
		oci.Finalizers = []string{"dummy"}
		Expect(env.Client(platform).Update(env.Ctx, oci)).To(Succeed())
		hr := &fluxhelmv2.HelmRelease{}
		hr.Name = helm.FluxResourceName("cluster-0", hd.Name, providerName)
		hr.Namespace = "default"
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hr), hr)).To(Succeed())
		hr.Finalizers = []string{"dummy"}
		Expect(env.Client(platform).Update(env.Ctx, hr)).To(Succeed())

		// delete the HelmDeployment to trigger deletion of flux resources
		Expect(env.Client(platform).Delete(env.Ctx, hd)).To(Succeed())

		// reconcile HelmDeployment
		// expected condition: false with reason 'WaitingForHelmReleaseDeletion' (due to finalizer)
		ccon := "Cluster.default_cluster-0"
		rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).ToNot(BeZero())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType(ccon).WithStatus(metav1.ConditionFalse).WithReason(helmv1alpha1.ReasonWaitingForHelmReleaseDeletion)),
		))

		// remove HelmRelease finalizer
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hr), hr)).To(Succeed())
		hr.Finalizers = nil
		Expect(env.Client(platform).Update(env.Ctx, hr)).To(Succeed())

		// reconcile HelmDeployment
		// expected condition: false with reason 'WaitingForHelmChartSourceDeletion' (due to finalizer)
		rr, err = env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).ToNot(BeZero())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType(ccon).WithStatus(metav1.ConditionFalse).WithReason(helmv1alpha1.ReasonWaitingForHelmChartSourceDeletion)),
		))

		// remove OCIRepository finalizer
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(oci), oci)).To(Succeed())
		oci.Finalizers = nil
		Expect(env.Client(platform).Update(env.Ctx, oci)).To(Succeed())

		// reconcile HelmDeployment
		// the Cluster should be gone afterwards (or at least it should not have the HelmDeployment's finalizer anymore), as should its condition
		rr, err = env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).ToNot(HaveOccurred())
		Expect(rr.RequeueAfter).To(BeZero())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
	})

	It("should correctly add and remove resources if clusters change to match or not match the HelmDeployment selector", func() {
		env, cq, _ := defaultTestSetup("testdata", "test-02")

		hd := &helmv1alpha1.HelmDeployment{}
		hd.Name = "hd-2"
		hd.Namespace = "default"
		hdreq := testutils.RequestFromObject(hd)

		Eventually(func(g Gomega) {
			// reconcile a cluster, if requested
			if cq.Size() > 0 {
				cluster := cq.Poll()
				_, err := env.Reconciler(cRec).Reconcile(env.Ctx, testutils.RequestFromObject(cluster))
				g.Expect(err).ToNot(HaveOccurred())
			}
			// reconcile HelmDeployment
			rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
			g.Expect(err).ToNot(HaveOccurred())
			// mock flux source and HelmRelease readiness
			mockAllFluxResourcesReady(g, env)
			// at one point, the HelmDeployment should not be requeued anymore
			g.Expect(rr.RequeueAfter).To(BeZero())
			// verify that no more clusters are queued for reconciliation
			g.Expect(cq.Size()).To(Equal(0))
		}).Should(Succeed())

		// list HelmReleases
		hrList := &fluxhelmv2.HelmReleaseList{}
		Expect(env.Client(platform).List(env.Ctx, hrList)).To(Succeed())
		Expect(hrList.Items).To(ConsistOf(
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Labels": Equal(map[string]string{
						openmcpconst.ManagedByLabel:      providerName + "." + helm.ControllerName,
						openmcpconst.ManagedPurposeLabel: "default.hd-2",
						helmv1alpha1.ClusterNameLabel:    "cluster-0",
					}),
				}),
			}),
		))
		// verify HelmDeployment status
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Phase).To(Equal(commonapi.StatusPhaseReady))
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType("Cluster.default_cluster-0").WithStatus(metav1.ConditionTrue)),
		))

		// add label to cluster-1 to make it match the HelmDeployment selector
		cluster1 := &clustersv1alpha1.Cluster{}
		cluster1.Name = "cluster-1"
		cluster1.Namespace = "default"
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(cluster1), cluster1)).To(Succeed())
		if cluster1.Labels == nil {
			cluster1.Labels = map[string]string{}
		}
		cluster1.Labels["helm.open-control-plane.io/target"] = "true"
		Expect(env.Client(platform).Update(env.Ctx, cluster1)).To(Succeed())

		Eventually(func(g Gomega) {
			// reconcile a cluster, if requested
			if cq.Size() > 0 {
				cluster := cq.Poll()
				_, err := env.Reconciler(cRec).Reconcile(env.Ctx, testutils.RequestFromObject(cluster))
				g.Expect(err).ToNot(HaveOccurred())
			}
			// reconcile HelmDeployment
			rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
			g.Expect(err).ToNot(HaveOccurred())
			// mock flux source and HelmRelease readiness
			mockAllFluxResourcesReady(g, env)
			// at one point, the HelmDeployment should not be requeued anymore
			g.Expect(rr.RequeueAfter).To(BeZero())
			// verify that no more clusters are queued for reconciliation
			g.Expect(cq.Size()).To(Equal(0))
		}).Should(Succeed())

		// list HelmReleases
		Expect(env.Client(platform).List(env.Ctx, hrList)).To(Succeed())
		Expect(hrList.Items).To(ConsistOf(
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Labels": Equal(map[string]string{
						openmcpconst.ManagedByLabel:      providerName + "." + helm.ControllerName,
						openmcpconst.ManagedPurposeLabel: "default.hd-2",
						helmv1alpha1.ClusterNameLabel:    "cluster-0",
					}),
				}),
			}),
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Labels": Equal(map[string]string{
						openmcpconst.ManagedByLabel:      providerName + "." + helm.ControllerName,
						openmcpconst.ManagedPurposeLabel: "default.hd-2",
						helmv1alpha1.ClusterNameLabel:    "cluster-1",
					}),
				}),
			}),
		))
		// verify HelmDeployment status
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Phase).To(Equal(commonapi.StatusPhaseReady))
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType("Cluster.default_cluster-0").WithStatus(metav1.ConditionTrue)),
			MatchCondition(TestCondition().WithType("Cluster.default_cluster-1").WithStatus(metav1.ConditionTrue)),
		))

		// remove label from cluster-0 to make it not match the HelmDeployment selector anymore
		cluster0 := &clustersv1alpha1.Cluster{}
		cluster0.Name = "cluster-0"
		cluster0.Namespace = "default"
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(cluster0), cluster0)).To(Succeed())
		delete(cluster0.Labels, "helm.open-control-plane.io/target")
		Expect(env.Client(platform).Update(env.Ctx, cluster0)).To(Succeed())

		Eventually(func(g Gomega) {
			// reconcile a cluster, if requested
			if cq.Size() > 0 {
				cluster := cq.Poll()
				_, err := env.Reconciler(cRec).Reconcile(env.Ctx, testutils.RequestFromObject(cluster))
				g.Expect(err).ToNot(HaveOccurred())
			}
			// reconcile HelmDeployment
			rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
			g.Expect(err).ToNot(HaveOccurred())
			// mock flux source and HelmRelease readiness
			mockAllFluxResourcesReady(g, env)
			// at one point, the HelmDeployment should not be requeued anymore
			g.Expect(rr.RequeueAfter).To(BeZero())
			// verify that no more clusters are queued for reconciliation
			g.Expect(cq.Size()).To(Equal(0))
		}).Should(Succeed())

		// list HelmReleases
		Expect(env.Client(platform).List(env.Ctx, hrList)).To(Succeed())
		Expect(hrList.Items).To(ConsistOf(
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Labels": Equal(map[string]string{
						openmcpconst.ManagedByLabel:      providerName + "." + helm.ControllerName,
						openmcpconst.ManagedPurposeLabel: "default.hd-2",
						helmv1alpha1.ClusterNameLabel:    "cluster-1",
					}),
				}),
			}),
		))
		// verify HelmDeployment status
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Phase).To(Equal(commonapi.StatusPhaseReady))
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType("Cluster.default_cluster-1").WithStatus(metav1.ConditionTrue)),
		))
	})

	It("should correctly replace the special key words when creating the HelmRelease", func() {
		env, cq, _ := defaultTestSetup("testdata", "test-03")

		hd := &helmv1alpha1.HelmDeployment{}
		hd.Name = "hd-0"
		hd.Namespace = "default"
		hdreq := testutils.RequestFromObject(hd)
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())

		Eventually(func(g Gomega) {
			// reconcile a cluster, if requested
			if cq.Size() > 0 {
				cluster := cq.Poll()
				_, err := env.Reconciler(cRec).Reconcile(env.Ctx, testutils.RequestFromObject(cluster))
				g.Expect(err).ToNot(HaveOccurred())
			}
			// reconcile HelmDeployment
			rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
			g.Expect(err).ToNot(HaveOccurred())
			// mock flux source and HelmRelease readiness
			mockAllFluxResourcesReady(g, env)
			// at one point, the HelmDeployment should not be requeued anymore
			g.Expect(rr.RequeueAfter).To(BeZero())
			// verify that no more clusters are queued for reconciliation
			g.Expect(cq.Size()).To(Equal(0))
		}).Should(Succeed())

		// list HelmReleases
		hrList := &fluxhelmv2.HelmReleaseList{}
		Expect(env.Client(platform).List(env.Ctx, hrList)).To(Succeed())
		Expect(hrList.Items).To(ConsistOf(
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Namespace": Equal("bar"),
					"Labels": Equal(map[string]string{
						openmcpconst.ManagedByLabel:      providerName + "." + helm.ControllerName,
						openmcpconst.ManagedPurposeLabel: "default.hd-0",
						helmv1alpha1.ClusterNameLabel:    "foo",
					}),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"KubeConfig":       Not(BeNil()),
					"TargetNamespace":  Equal(hd.Spec.Namespace),
					"StorageNamespace": Equal(hd.Spec.Namespace),
					"Values": WithTransform(func(raw *apiextensionsv1.JSON) map[string]string {
						res := map[string]string{}
						Expect(yaml.Unmarshal(raw.Raw, &res)).To(Succeed())
						return res
					}, Equal(map[string]string{
						"provider.name":      providerName,
						"provider.namespace": providerNamespace,
						"environment":        environment,
						"helm.name":          hd.Name,
						"helm.namespace":     hd.Namespace,
						"cluster.name":       "foo",
						"cluster.namespace":  "bar",
					})),
				}),
			}),
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Namespace": Equal("qwer"),
					"Labels": Equal(map[string]string{
						openmcpconst.ManagedByLabel:      providerName + "." + helm.ControllerName,
						openmcpconst.ManagedPurposeLabel: "default.hd-0",
						helmv1alpha1.ClusterNameLabel:    "asdf",
					}),
				}),
				"Spec": MatchFields(IgnoreExtras, Fields{
					"KubeConfig":       Not(BeNil()),
					"TargetNamespace":  Equal(hd.Spec.Namespace),
					"StorageNamespace": Equal(hd.Spec.Namespace),
					"Values": WithTransform(func(raw *apiextensionsv1.JSON) map[string]string {
						res := map[string]string{}
						Expect(yaml.Unmarshal(raw.Raw, &res)).To(Succeed())
						return res
					}, Equal(map[string]string{
						"provider.name":      providerName,
						"provider.namespace": providerNamespace,
						"environment":        environment,
						"helm.name":          hd.Name,
						"helm.namespace":     hd.Namespace,
						"cluster.name":       "asdf",
						"cluster.namespace":  "qwer",
					})),
				}),
			}),
		))
	})

	It("should resolve selector references correctly", func() {
		env, cq, _ := defaultTestSetup("testdata", "test-04")

		hd := &helmv1alpha1.HelmDeployment{}
		hd.Name = "hd-0"
		hd.Namespace = "default"
		hdreq := testutils.RequestFromObject(hd)

		Eventually(func(g Gomega) {
			// reconcile a cluster, if requested
			if cq.Size() > 0 {
				cluster := cq.Poll()
				_, err := env.Reconciler(cRec).Reconcile(env.Ctx, testutils.RequestFromObject(cluster))
				g.Expect(err).ToNot(HaveOccurred())
			}
			// reconcile HelmDeployment
			rr, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
			g.Expect(err).ToNot(HaveOccurred())
			// mock flux source and HelmRelease readiness
			mockAllFluxResourcesReady(g, env)
			// at one point, the HelmDeployment should not be requeued anymore
			g.Expect(rr.RequeueAfter).To(BeZero())
			// verify that no more clusters are queued for reconciliation
			g.Expect(cq.Size()).To(Equal(0))
		}).Should(Succeed())

		// list HelmReleases
		hrList := &fluxhelmv2.HelmReleaseList{}
		Expect(env.Client(platform).List(env.Ctx, hrList)).To(Succeed())
		Expect(hrList.Items).To(ConsistOf(
			MatchFields(IgnoreExtras, Fields{
				"ObjectMeta": MatchFields(IgnoreExtras, Fields{
					"Labels": Equal(map[string]string{
						openmcpconst.ManagedByLabel:      providerName + "." + helm.ControllerName,
						openmcpconst.ManagedPurposeLabel: "default.hd-0",
						helmv1alpha1.ClusterNameLabel:    "cluster-0",
					}),
				}),
			}),
		))
		// verify HelmDeployment status
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(hd), hd)).To(Succeed())
		Expect(hd.Status.Phase).To(Equal(commonapi.StatusPhaseReady))
		Expect(hd.Status.Conditions).To(ConsistOf(
			MatchCondition(TestCondition().WithType("Cluster.default_cluster-0").WithStatus(metav1.ConditionTrue)),
		))
	})

	It("should return a fitting error for unknown selector references", func() {
		env, _, _ := defaultTestSetup("testdata", "test-04")

		hd := &helmv1alpha1.HelmDeployment{}
		hd.Name = "hd-1"
		hd.Namespace = "default"
		hdreq := testutils.RequestFromObject(hd)

		_, err := env.Reconciler(hdRec).Reconcile(env.Ctx, hdreq)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("unknown")), "the name of the unresolvable reference should be included in the error message")
		Expect(err).To(WithTransform(func(err error) string {
			rerr, ok := err.(errutils.ReasonableError)
			Expect(ok).To(BeTrue(), "the error should implement ReasonableError")
			return rerr.Reason()
		}, Equal(cconst.ReasonConfigurationProblem)))
	})

})

// mockAllFluxResourcesReady sets the Ready condition to True for all Flux resources (GitRepository, HelmRepository, OCIRepository, HelmRelease).
func mockAllFluxResourcesReady(g Gomega, env *testutils.ComplexEnvironment, opts ...client.ListOption) {
	resources := []client.Object{} // nolint:prealloc

	existingHelm := &fluxsourcev1.HelmRepositoryList{}
	g.ExpectWithOffset(1, env.Client(platform).List(env.Ctx, existingHelm, opts...)).To(Succeed())
	for _, obj := range existingHelm.Items {
		resources = append(resources, &obj)
	}
	existingGit := &fluxsourcev1.GitRepositoryList{}
	g.ExpectWithOffset(1, env.Client(platform).List(env.Ctx, existingGit, opts...)).To(Succeed())
	for _, obj := range existingGit.Items {
		resources = append(resources, &obj)
	}
	existingOCI := &fluxsourcev1.OCIRepositoryList{}
	g.ExpectWithOffset(1, env.Client(platform).List(env.Ctx, existingOCI, opts...)).To(Succeed())
	for _, obj := range existingOCI.Items {
		resources = append(resources, &obj)
	}
	existingHR := &fluxhelmv2.HelmReleaseList{}
	g.ExpectWithOffset(1, env.Client(platform).List(env.Ctx, existingHR, opts...)).To(Succeed())
	for _, obj := range existingHR.Items {
		resources = append(resources, &obj)
	}

	for _, res := range resources {
		ctrlutils.SetField(res, "Status.Conditions", []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				ObservedGeneration: res.GetGeneration(),
			},
		})
		g.ExpectWithOffset(1, env.Client(platform).Status().Update(env.Ctx, res)).To(Succeed())
	}
}
