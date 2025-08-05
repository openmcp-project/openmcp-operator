package managedcontrolplane_test

import (
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	. "github.com/openmcp-project/controller-utils/pkg/testing/matchers"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	testutils "github.com/openmcp-project/controller-utils/pkg/testing"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	corev2alpha1 "github.com/openmcp-project/openmcp-operator/api/core/v2alpha1"
	"github.com/openmcp-project/openmcp-operator/api/install"
	"github.com/openmcp-project/openmcp-operator/internal/config"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/managedcontrolplane"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"
)

var scheme = install.InstallOperatorAPIs(runtime.NewScheme())

const (
	platform   = "platform"
	onboarding = "onboarding"
	mcpRec     = "mcp"
)

// defaultTestSetup initializes a new environment for testing the mcp controller.
// Expected folder structure is a 'config.yaml' file next to a 'platform' and 'onboarding' directory, containing the manifests for the respective clusters.
func defaultTestSetup(testDirPathSegments ...string) (*managedcontrolplane.ManagedControlPlaneReconciler, *testutils.ComplexEnvironment) {
	cfg, err := config.LoadFromFiles(filepath.Join(append(testDirPathSegments, "config.yaml")...))
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg.Default()).To(Succeed())
	Expect(cfg.Validate()).To(Succeed())
	Expect(cfg.Complete()).To(Succeed())
	platformDirExists := true
	_, err = os.Stat(filepath.Join(append(testDirPathSegments, platform)...))
	Expect(err).To(Or(Not(HaveOccurred()), MatchError(os.IsNotExist, "IsNotExist")))
	if err != nil {
		platformDirExists = false
	}
	onboardingDirExists := true
	_, err = os.Stat(filepath.Join(append(testDirPathSegments, onboarding)...))
	Expect(err).To(Or(Not(HaveOccurred()), MatchError(os.IsNotExist, "IsNotExist")))
	if err != nil {
		onboardingDirExists = false
	}
	envB := testutils.NewComplexEnvironmentBuilder().
		WithFakeClient(platform, scheme).
		WithDynamicObjectsWithStatus(platform, &clustersv1alpha1.ClusterRequest{}, &clustersv1alpha1.AccessRequest{}).
		WithFakeClient(onboarding, scheme).
		WithReconcilerConstructor(mcpRec, func(clients ...client.Client) reconcile.Reconciler {
			return managedcontrolplane.NewManagedControlPlaneReconciler(clusters.NewTestClusterFromClient(platform, clients[0]), clusters.NewTestClusterFromClient(onboarding, clients[1]), nil, cfg.ManagedControlPlane)
		}, platform, onboarding)
	if platformDirExists {
		envB.WithInitObjectPath(platform, append(testDirPathSegments, platform)...)
	}
	if onboardingDirExists {
		envB.WithInitObjectPath(onboarding, append(testDirPathSegments, onboarding)...)
	}
	env := envB.Build()
	mcpReconciler, ok := env.Reconciler(mcpRec).(*managedcontrolplane.ManagedControlPlaneReconciler)
	Expect(ok).To(BeTrue(), "Reconciler is not of type ManagedControlPlaneReconciler")
	return mcpReconciler, env
}

var _ = Describe("ManagedControlPlane Controller", func() {

	It("should correctly handle create and update operations for the MCP", func() {
		rec, env := defaultTestSetup("testdata", "test-01")

		mcp := &corev2alpha1.ManagedControlPlane{}
		mcp.SetName("mcp-01")
		mcp.SetNamespace("test")
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())

		By("=== CREATE ===")

		// reconcile the MCP
		// expected outcome:
		// - mcp has an mcp finalizer
		// - mcp has a cluster request finalizer
		// - a cluster request was created on the platform cluster
		// - the mcp has conditions that reflect that it is waiting for the cluster request
		// - the mcp should be requeued with a short requeueAfter duration
		By("first MCP reconciliation")
		platformNamespace := libutils.StableRequestNamespace(mcp.Namespace)
		res := env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(res.RequeueAfter).To(BeNumerically("<", 1*time.Minute))
		Expect(mcp.Finalizers).To(ConsistOf(
			corev2alpha1.MCPFinalizer,
			corev2alpha1.ClusterRequestFinalizerPrefix+mcp.Name,
		))
		cr := &clustersv1alpha1.ClusterRequest{}
		cr.SetName(mcp.Name)
		cr.SetNamespace(platformNamespace)
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())
		Expect(cr.Spec.Purpose).To(Equal(rec.Config.MCPClusterPurpose))
		Expect(cr.Spec.WaitForClusterDeletion).To(PointTo(BeTrue()))
		Expect(mcp.Status.Conditions).To(ContainElements(
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionClusterRequestReady).
				WithStatus(metav1.ConditionFalse).
				WithReason(cconst.ReasonWaitingForClusterRequest)),
		))

		// fake ClusterRequest ready status
		By("faking ClusterRequest readiness")
		cr.Status.Phase = commonapi.StatusPhaseReady
		Expect(env.Client(platform).Status().Update(env.Ctx, cr)).To(Succeed())

		// reconcile the MCP again
		// expected outcome:
		// - multiple access requests have been created on the platform cluster, one for each configured OIDC provider
		// - the mcp has conditions that reflect that it is waiting for the access requests (one for each OIDC provider and one overall one)
		// - the mcp should be requeued with a short requeueAfter duration
		By("second MCP reconciliation")
		res = env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(res.RequeueAfter).To(BeNumerically("<", 1*time.Minute))
		Expect(mcp.Status.Conditions).To(ContainElements(
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionClusterRequestReady).
				WithStatus(metav1.ConditionTrue)),
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionAllAccessReady).
				WithStatus(metav1.ConditionFalse).
				WithReason(cconst.ReasonWaitingForAccessRequest)),
		))
		oidcProviders := []commonapi.OIDCProviderConfig{*rec.Config.StandardOIDCProvider.DeepCopy()}
		oidcProviders[0].RoleBindings = mcp.Spec.IAM.RoleBindings
		for _, addProv := range mcp.Spec.IAM.OIDCProviders {
			oidcProviders = append(oidcProviders, *addProv.DeepCopy())
		}
		Expect(oidcProviders).To(HaveLen(3))
		for _, oidc := range oidcProviders {
			By("verifying that the AccessRequest is not ready for oidc provider: " + oidc.Name)
			Expect(mcp.Status.Conditions).To(ContainElements(
				MatchCondition(TestCondition().
					WithType(corev2alpha1.ConditionPrefixOIDCAccessReady + oidc.Name).
					WithStatus(metav1.ConditionFalse).
					WithReason(cconst.ReasonWaitingForAccessRequest)),
			))
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.K8sNameHash(mcp.Name, oidc.Name))
			ar.SetNamespace(platformNamespace)
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
			Expect(ar.Spec.RequestRef.Name).To(Equal(cr.Name))
			Expect(ar.Spec.RequestRef.Namespace).To(Equal(cr.Namespace))
			Expect(ar.Spec.OIDCProvider).To(PointTo(Equal(oidc)))
		}

		// fake AccessRequest ready status
		By("faking AccessRequest readiness")
		for _, oidc := range oidcProviders {
			By("faking AccessRequest readiness for oidc provider: " + oidc.Name)
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.K8sNameHash(mcp.Name, oidc.Name))
			ar.SetNamespace(platformNamespace)
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
			ar.Status.Phase = clustersv1alpha1.REQUEST_GRANTED
			ar.Status.SecretRef = &commonapi.ObjectReference{
				Name:      ar.Name,
				Namespace: ar.Namespace,
			}
			sec := &corev1.Secret{}
			sec.SetName(ar.Status.SecretRef.Name)
			sec.SetNamespace(ar.Namespace)
			sec.Data = map[string][]byte{
				clustersv1alpha1.SecretKeyKubeconfig: []byte(oidc.Name),
			}
			Expect(env.Client(platform).Status().Update(env.Ctx, ar)).To(Succeed())
			Expect(env.Client(platform).Create(env.Ctx, sec)).To(Succeed())
		}

		// reconcile the MCP again
		// expected outcome:
		// - the mcp has conditions that reflect that all access requests are ready
		// - the mcp has copied the kubeconfig secrets from the access requests into the onboarding cluster and references them in its status
		// - the mcp should be requeued with a requeueAfter duration that matches the reconcile interval from the controller config
		By("third MCP reconciliation")
		res = env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())
		Expect(res.RequeueAfter).To(BeNumerically("~", int64(rec.Config.ReconcileMCPEveryXDays)*24*int64(time.Hour), int64(time.Second)))
		Expect(mcp.Status.Conditions).To(ContainElements(
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionClusterRequestReady).
				WithStatus(metav1.ConditionTrue)),
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionAllAccessReady).
				WithStatus(metav1.ConditionTrue)),
		))
		for _, oidc := range oidcProviders {
			Expect(mcp.Status.Conditions).To(ContainElements(
				MatchCondition(TestCondition().
					WithType(corev2alpha1.ConditionPrefixOIDCAccessReady + oidc.Name).
					WithStatus(metav1.ConditionTrue)),
			))
		}
		Expect(mcp.Status.Access).To(HaveLen(len(oidcProviders)))
		for providerName, secretRef := range mcp.Status.Access {
			By("verifying MCP access secret for oidc provider: " + providerName)
			sec := &corev1.Secret{}
			sec.SetName(secretRef.Name)
			sec.SetNamespace(mcp.Namespace)
			Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(sec), sec)).To(Succeed())
			Expect(sec.Data).To(HaveKeyWithValue(clustersv1alpha1.SecretKeyKubeconfig, []byte(providerName)))
		}

		By("=== UPDATE ===")

		// change the rolebindings in the MCP spec and remove one OIDC provider
		By("update MCP spec")
		mcp.Spec.IAM.RoleBindings = mcp.Spec.IAM.RoleBindings[:len(mcp.Spec.IAM.RoleBindings)-1]
		removedOIDCProviderName := mcp.Spec.IAM.OIDCProviders[len(mcp.Spec.IAM.OIDCProviders)-1].Name
		toBeRemovedSecretName := mcp.Status.Access[removedOIDCProviderName].Name
		mcp.Spec.IAM.OIDCProviders = mcp.Spec.IAM.OIDCProviders[:len(mcp.Spec.IAM.OIDCProviders)-1]
		Expect(env.Client(onboarding).Update(env.Ctx, mcp)).To(Succeed())

		By("add finalizers to AccessRequests")
		for _, oidc := range oidcProviders {
			By("adding finalizer to AccessRequest for oidc provider: " + oidc.Name)
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.K8sNameHash(mcp.Name, oidc.Name))
			ar.SetNamespace(platformNamespace)
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
			controllerutil.AddFinalizer(ar, "dummy")
			Expect(env.Client(platform).Update(env.Ctx, ar)).To(Succeed())
		}

		// reconcile the MCP
		// expected outcome:
		// - the rolebindings in the AccessRequest for the standard OIDC provider have been updated
		// - the access secret for the removed OIDC provider have been deleted
		// - the AccessRequest for the removed OIDC provider has a deletion timestamp
		// - the condition for the removed OIDC provider is false and indicating that it is waiting for the AccessRequest
		// - the mcp should be requeued with a short requeueAfter duration
		By("first MCP reconciliation after update")
		res = env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(res.RequeueAfter).To(BeNumerically("<", 1*time.Minute))
		Expect(mcp.Status.Conditions).To(ContainElements(
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionClusterRequestReady).
				WithStatus(metav1.ConditionTrue)),
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionAllAccessReady).
				WithStatus(metav1.ConditionFalse).
				WithReason(cconst.ReasonWaitingForAccessRequest)),
		))
		removedOIDCIdx := -1
		for i, oidc := range oidcProviders {
			By("verifying condition for oidc provider: " + oidc.Name)
			if oidc.Name == removedOIDCProviderName {
				removedOIDCIdx = i
				Expect(mcp.Status.Conditions).To(ContainElements(
					MatchCondition(TestCondition().
						WithType(corev2alpha1.ConditionPrefixOIDCAccessReady + oidc.Name).
						WithStatus(metav1.ConditionFalse).
						WithReason(cconst.ReasonWaitingForAccessRequest),
					)))
				Expect(mcp.Status.Access).ToNot(HaveKey(oidc.Name))
			} else {
				Expect(mcp.Status.Conditions).To(ContainElements(
					MatchCondition(TestCondition().
						WithType(corev2alpha1.ConditionPrefixOIDCAccessReady + oidc.Name).
						WithStatus(metav1.ConditionTrue),
					)))
				Expect(mcp.Status.Access).To(HaveKey(oidc.Name))
			}
		}
		Expect(removedOIDCIdx).To(BeNumerically(">", -1))
		oidcProviders = append(oidcProviders[:removedOIDCIdx], oidcProviders[removedOIDCIdx+1:]...)
		Expect(mcp.Status.Access).ToNot(HaveKey(removedOIDCProviderName))
		sec := &corev1.Secret{}
		sec.SetName(toBeRemovedSecretName)
		sec.SetNamespace(mcp.Namespace)
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(sec), sec)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
		ar := &clustersv1alpha1.AccessRequest{}
		ar.SetName(ctrlutils.K8sNameHash(mcp.Name, removedOIDCProviderName))
		ar.SetNamespace(platformNamespace)
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		Expect(ar.GetDeletionTimestamp().IsZero()).To(BeFalse())

		// remove dummy finalizer from AccessRequest belonging to the removed OIDC provider
		By("removing dummy finalizer from AccessRequest for removed OIDC provider: " + removedOIDCProviderName)
		controllerutil.RemoveFinalizer(ar, "dummy")
		Expect(env.Client(platform).Update(env.Ctx, ar)).To(Succeed())

		// reconcile the MCP again
		// expected outcome:
		// - the AccessRequest for the removed OIDC provider has been deleted
		// - the condition for the removed OIDC provider is gone
		// - the mcp should be requeued with a requeueAfter duration that matches the reconcile interval from the controller config
		By("second MCP reconciliation after update")
		res = env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())
		Expect(res.RequeueAfter).To(BeNumerically("~", int64(rec.Config.ReconcileMCPEveryXDays)*24*int64(time.Hour), int64(time.Second)))
		Expect(mcp.Status.Conditions).To(ContainElements(
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionClusterRequestReady).
				WithStatus(metav1.ConditionTrue)),
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionAllAccessReady).
				WithStatus(metav1.ConditionTrue)),
		))
		for _, oidc := range oidcProviders {
			By("verifying condition for oidc provider: " + oidc.Name)
			Expect(mcp.Status.Conditions).To(ContainElements(
				MatchCondition(TestCondition().
					WithType(corev2alpha1.ConditionPrefixOIDCAccessReady + oidc.Name).
					WithStatus(metav1.ConditionTrue)),
			))
		}
		Expect(mcp.Status.Access).ToNot(HaveKey(removedOIDCProviderName))
		Expect(mcp.Status.Conditions).ToNot(ContainElements(
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionPrefixOIDCAccessReady + removedOIDCProviderName),
			),
		))
	})

})
