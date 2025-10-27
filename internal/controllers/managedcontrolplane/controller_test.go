package managedcontrolplane_test

import (
	"os"
	"path/filepath"
	"strings"
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
	apiconst "github.com/openmcp-project/openmcp-operator/api/constants"
	corev2alpha1 "github.com/openmcp-project/openmcp-operator/api/core/v2alpha1"
	"github.com/openmcp-project/openmcp-operator/api/install"
	"github.com/openmcp-project/openmcp-operator/internal/config"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/managedcontrolplane"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"
)

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
		WithFakeClient(platform, install.InstallOperatorAPIsPlatform(runtime.NewScheme())).
		WithDynamicObjectsWithStatus(platform, &clustersv1alpha1.ClusterRequest{}, &clustersv1alpha1.AccessRequest{}, &clustersv1alpha1.Cluster{}).
		WithFakeClient(onboarding, install.InstallOperatorAPIsOnboarding(runtime.NewScheme())).
		WithReconcilerConstructor(mcpRec, func(clients ...client.Client) reconcile.Reconciler {
			mcpr, err := managedcontrolplane.NewManagedControlPlaneReconciler(clusters.NewTestClusterFromClient(platform, clients[0]), clusters.NewTestClusterFromClient(onboarding, clients[1]), nil, cfg.ManagedControlPlane)
			Expect(err).ToNot(HaveOccurred())
			return mcpr
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

	It("should correctly handle the creation, update, and deletion flow for MCP resources", func() {
		rec, env := defaultTestSetup("testdata", "test-01")

		mcp := &corev2alpha1.ManagedControlPlaneV2{}
		mcp.SetName("mcp-01")
		mcp.SetNamespace("test")
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())

		By("=== CREATE ===")

		// reconcile the MCP
		// expected outcome:
		// - mcp has an mcp finalizer
		// - mcp has a cluster request finalizer
		// - a namespace was created for the MCP on the platform cluster
		// - a cluster request was created on the platform cluster
		// - the mcp has conditions that reflect that it is waiting for the cluster request
		// - the mcp should be requeued with a short requeueAfter duration
		By("first MCP reconciliation")
		platformNamespace, err := libutils.StableMCPNamespace(mcp.Name, mcp.Namespace)
		Expect(err).ToNot(HaveOccurred())
		res := env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(res.RequeueAfter).To(BeNumerically("<", 1*time.Minute))
		Expect(mcp.Finalizers).To(ContainElements(
			corev2alpha1.MCPFinalizer,
			corev2alpha1.ClusterRequestFinalizerPrefix+mcp.Name,
		))
		ns := &corev1.Namespace{}
		ns.SetName(platformNamespace)
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
		Expect(ns.Labels).To(HaveKeyWithValue(corev2alpha1.MCPNameLabel, mcp.Name))
		Expect(ns.Labels).To(HaveKeyWithValue(corev2alpha1.MCPNamespaceLabel, mcp.Namespace))
		Expect(ns.Labels).To(HaveKeyWithValue(apiconst.ManagedByLabel, managedcontrolplane.ControllerName))
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

		// fake ClusterRequest ready status and Cluster resource
		By("fake: ClusterRequest readiness")
		cluster := &clustersv1alpha1.Cluster{}
		cluster.SetName("cluster-01")
		cluster.SetNamespace(platformNamespace)
		cluster.Spec.Purposes = []string{rec.Config.MCPClusterPurpose}
		Expect(env.Client(platform).Create(env.Ctx, cluster)).To(Succeed())
		cluster.Status.Conditions = []metav1.Condition{
			{
				Type:               "TestCondition1",
				Status:             metav1.ConditionTrue,
				Reason:             "TestReason",
				Message:            "This is a test condition",
				LastTransitionTime: metav1.Now(),
				ObservedGeneration: 1,
			},
			{
				Type:               "TestCondition2",
				Status:             metav1.ConditionFalse,
				Reason:             "TestReason",
				Message:            "This is another test condition",
				LastTransitionTime: metav1.Now(),
				ObservedGeneration: 1,
			},
		}
		Expect(env.Client(platform).Status().Update(env.Ctx, cluster)).To(Succeed())
		cr.Status.Phase = clustersv1alpha1.REQUEST_GRANTED
		cr.Status.Cluster = &commonapi.ObjectReference{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		}
		Expect(env.Client(platform).Status().Update(env.Ctx, cr)).To(Succeed())

		// reconcile the MCP again
		// expected outcome:
		// - multiple access requests have been created on the platform cluster, one for each configured OIDC provider
		// - the mcp has conditions that reflect that it is waiting for the access requests (one for each OIDC provider and one overall one)
		// - the mcp has taken over the conditions from the Cluster resource with a prefix
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
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionPrefixClusterCondition+"TestCondition1").
				WithStatus(metav1.ConditionTrue).
				WithReason("TestReason").
				WithMessage("This is a test condition")),
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionPrefixClusterCondition+"TestCondition2").
				WithStatus(metav1.ConditionFalse).
				WithReason("TestReason").
				WithMessage("This is another test condition")),
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionClusterConditionsSynced).
				WithStatus(metav1.ConditionTrue)),
		))
		oidcProviders := []commonapi.OIDCProviderConfig{*rec.Config.DefaultOIDCProvider.DeepCopy()}
		oidcProviders[0].RoleBindings = mcp.Spec.IAM.OIDC.DefaultProvider.RoleBindings
		for _, addProv := range mcp.Spec.IAM.OIDC.ExtraProviders {
			oidcProviders = append(oidcProviders, *addProv.DeepCopy())
		}
		Expect(oidcProviders).To(HaveLen(3))
		for _, oidc := range oidcProviders {
			By("verifying that the AccessRequest is not ready for oidc provider: " + oidc.Name)
			Expect(mcp.Status.Conditions).To(ContainElements(
				MatchCondition(TestCondition().
					WithType(corev2alpha1.ConditionPrefixAccessReady + corev2alpha1.OIDCNamePrefix + oidc.Name).
					WithStatus(metav1.ConditionFalse).
					WithReason(cconst.ReasonWaitingForAccessRequest)),
			))
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.OIDCNamePrefix+oidc.Name))
			ar.SetNamespace(platformNamespace)
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
			Expect(ar.Spec.RequestRef.Name).To(Equal(cr.Name))
			Expect(ar.Spec.RequestRef.Namespace).To(Equal(cr.Namespace))
			Expect(ar.Spec.OIDC).ToNot(BeNil())
			Expect(ar.Spec.OIDC.OIDCProviderConfig).To(Equal(oidc))
		}
		for _, token := range mcp.Spec.IAM.Tokens {
			By("verifying that the AccessRequest is not ready for token: " + token.Name)
			Expect(mcp.Status.Conditions).To(ContainElements(
				MatchCondition(TestCondition().
					WithType(corev2alpha1.ConditionPrefixAccessReady + corev2alpha1.TokenNamePrefix + token.Name).
					WithStatus(metav1.ConditionFalse).
					WithReason(cconst.ReasonWaitingForAccessRequest)),
			))
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.TokenNamePrefix+token.Name))
			ar.SetNamespace(platformNamespace)
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
			Expect(ar.Spec.RequestRef.Name).To(Equal(cr.Name))
			Expect(ar.Spec.RequestRef.Namespace).To(Equal(cr.Namespace))
			Expect(ar.Spec.Token).ToNot(BeNil())
			Expect(ar.Spec.Token.Permissions).To(Equal(token.Permissions))
			Expect(ar.Spec.Token.RoleRefs).To(Equal(token.RoleRefs))
		}

		// fake AccessRequest ready status
		By("fake: AccessRequest readiness")
		for _, oidc := range oidcProviders {
			By("fake: AccessRequest readiness for oidc provider: " + oidc.Name)
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.OIDCNamePrefix+oidc.Name))
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
				clustersv1alpha1.SecretKeyKubeconfig: []byte(corev2alpha1.OIDCNamePrefix + oidc.Name),
			}
			Expect(env.Client(platform).Status().Update(env.Ctx, ar)).To(Succeed())
			Expect(env.Client(platform).Create(env.Ctx, sec)).To(Succeed())
		}

		tokens := mcp.Spec.IAM.Tokens
		for _, token := range tokens {
			By("fake: Token AccessRequest readiness for token: " + token.Name)
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.TokenNamePrefix+token.Name))
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
				clustersv1alpha1.SecretKeyKubeconfig: []byte(corev2alpha1.TokenNamePrefix + token.Name),
			}
			Expect(env.Client(platform).Status().Update(env.Ctx, ar)).To(Succeed())
			Expect(env.Client(platform).Create(env.Ctx, sec)).To(Succeed())
		}

		// remove non-ready cluster condition because it will prevent the MCP from becoming ready
		cluster.Status.Conditions[1].Status = metav1.ConditionTrue
		Expect(env.Client(platform).Status().Update(env.Ctx, cluster)).To(Succeed())

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
					WithType(corev2alpha1.ConditionPrefixAccessReady + corev2alpha1.OIDCNamePrefix + oidc.Name).
					WithStatus(metav1.ConditionTrue)),
			))
		}
		for _, token := range tokens {
			Expect(mcp.Status.Conditions).To(ContainElements(
				MatchCondition(TestCondition().
					WithType(corev2alpha1.ConditionPrefixAccessReady + corev2alpha1.TokenNamePrefix + token.Name).
					WithStatus(metav1.ConditionTrue)),
			))
		}
		Expect(mcp.Status.Access).To(HaveLen(len(oidcProviders) + len(tokens)))
		for providerName, secretRef := range mcp.Status.Access {
			By("verifying MCP access secret for oidc provider/token: " + providerName)
			sec := &corev1.Secret{}
			sec.SetName(secretRef.Name)
			sec.SetNamespace(mcp.Namespace)
			Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(sec), sec)).To(Succeed())
			Expect(sec.Data).To(HaveKeyWithValue(clustersv1alpha1.SecretKeyKubeconfig, []byte(providerName)))
			providerPrefix := strings.ReplaceAll(providerName, "_", "-")
			Expect(sec.Name).To(Equal(strings.Join([]string{providerPrefix, mcp.Name, "kubeconfig"}, ".")))
		}

		By("=== UPDATE ===")

		// change the rolebindings in the MCP spec and remove one OIDC provider and one token
		By("updating MCP spec")
		// oidc
		mcp.Spec.IAM.OIDC.DefaultProvider.RoleBindings = mcp.Spec.IAM.OIDC.DefaultProvider.RoleBindings[:len(mcp.Spec.IAM.OIDC.DefaultProvider.RoleBindings)-1]
		removedOIDCProviderName := mcp.Spec.IAM.OIDC.ExtraProviders[len(mcp.Spec.IAM.OIDC.ExtraProviders)-1].Name
		toBeRemovedSecretNameOIDC := mcp.Status.Access[removedOIDCProviderName].Name
		mcp.Spec.IAM.OIDC.ExtraProviders = mcp.Spec.IAM.OIDC.ExtraProviders[:len(mcp.Spec.IAM.OIDC.ExtraProviders)-1]
		// token
		mcp.Spec.IAM.Tokens = mcp.Spec.IAM.Tokens[:len(mcp.Spec.IAM.Tokens)-1]
		removedTokenName := tokens[len(tokens)-1].Name
		toBeRemovedSecretNameToken := mcp.Status.Access[removedTokenName].Name
		Expect(env.Client(onboarding).Update(env.Ctx, mcp)).To(Succeed())

		By("fake: adding finalizers to AccessRequests")
		for _, oidc := range oidcProviders {
			By("fake: adding finalizer to AccessRequest for oidc provider: " + oidc.Name)
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.OIDCNamePrefix+oidc.Name))
			ar.SetNamespace(platformNamespace)
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
			controllerutil.AddFinalizer(ar, "dummy")
			Expect(env.Client(platform).Update(env.Ctx, ar)).To(Succeed())
		}
		for _, token := range tokens {
			By("fake: adding finalizer to AccessRequest for token: " + token.Name)
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.TokenNamePrefix+token.Name))
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
				WithReason(cconst.ReasonWaitingForAccessRequestDeletion)),
		))
		removedOIDCIdx := -1
		for i, oidc := range oidcProviders {
			By("verifying condition for oidc provider: " + oidc.Name)
			if oidc.Name == removedOIDCProviderName {
				removedOIDCIdx = i
				Expect(mcp.Status.Conditions).To(ContainElements(
					MatchCondition(TestCondition().
						WithType(corev2alpha1.ConditionPrefixAccessReady + corev2alpha1.OIDCNamePrefix + oidc.Name).
						WithStatus(metav1.ConditionFalse).
						WithReason(cconst.ReasonWaitingForAccessRequestDeletion),
					)))
				Expect(mcp.Status.Access).ToNot(HaveKey(oidc.Name))
			} else {
				Expect(mcp.Status.Conditions).To(ContainElements(
					MatchCondition(TestCondition().
						WithType(corev2alpha1.ConditionPrefixAccessReady + corev2alpha1.OIDCNamePrefix + oidc.Name).
						WithStatus(metav1.ConditionTrue),
					)))
				Expect(mcp.Status.Access).To(HaveKey(corev2alpha1.OIDCNamePrefix + oidc.Name))
			}
		}
		Expect(removedOIDCIdx).To(BeNumerically(">", -1))
		oidcProviders = append(oidcProviders[:removedOIDCIdx], oidcProviders[removedOIDCIdx+1:]...)
		Expect(mcp.Status.Access).ToNot(HaveKey(corev2alpha1.OIDCNamePrefix + removedOIDCProviderName))

		removedTokenIdx := -1
		for i, token := range tokens {
			By("verifying condition for token: " + token.Name)
			if token.Name == removedTokenName {
				removedTokenIdx = i
				Expect(mcp.Status.Conditions).To(ContainElements(
					MatchCondition(TestCondition().
						WithType(corev2alpha1.ConditionPrefixAccessReady + corev2alpha1.TokenNamePrefix + token.Name).
						WithStatus(metav1.ConditionFalse).
						WithReason(cconst.ReasonWaitingForAccessRequestDeletion),
					)))
				Expect(mcp.Status.Access).ToNot(HaveKey(token.Name))
			} else {
				Expect(mcp.Status.Conditions).To(ContainElements(
					MatchCondition(TestCondition().
						WithType(corev2alpha1.ConditionPrefixAccessReady + corev2alpha1.TokenNamePrefix + token.Name).
						WithStatus(metav1.ConditionTrue),
					)))
				Expect(mcp.Status.Access).To(HaveKey(corev2alpha1.TokenNamePrefix + token.Name))
			}
		}
		Expect(removedTokenIdx).To(BeNumerically(">", -1))
		tokens = append(tokens[:removedTokenIdx], tokens[removedTokenIdx+1:]...)
		Expect(mcp.Status.Access).ToNot(HaveKey(corev2alpha1.TokenNamePrefix + removedTokenName))

		secOIDC := &corev1.Secret{}
		secOIDC.SetName(toBeRemovedSecretNameOIDC)
		secOIDC.SetNamespace(mcp.Namespace)
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(secOIDC), secOIDC)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
		arOIDC := &clustersv1alpha1.AccessRequest{}
		arOIDC.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.OIDCNamePrefix+removedOIDCProviderName))
		arOIDC.SetNamespace(platformNamespace)
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(arOIDC), arOIDC)).To(Succeed())
		Expect(arOIDC.GetDeletionTimestamp().IsZero()).To(BeFalse())

		secToken := &corev1.Secret{}
		secToken.SetName(toBeRemovedSecretNameToken)
		secToken.SetNamespace(mcp.Namespace)
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(secToken), secToken)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
		arToken := &clustersv1alpha1.AccessRequest{}
		arToken.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.TokenNamePrefix+removedTokenName))
		arToken.SetNamespace(platformNamespace)
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(arToken), arToken)).To(Succeed())
		Expect(arToken.GetDeletionTimestamp().IsZero()).To(BeFalse())

		// remove dummy finalizer from AccessRequest belonging to the removed OIDC provider
		By("fake: removing dummy finalizer from AccessRequest for removed OIDC provider: " + removedOIDCProviderName)
		controllerutil.RemoveFinalizer(arOIDC, "dummy")
		Expect(env.Client(platform).Update(env.Ctx, arOIDC)).To(Succeed())
		// remove dummy finalizer from AccessRequest belonging to the removed token
		By("fake: removing dummy finalizer from AccessRequest for removed token: " + removedTokenName)
		controllerutil.RemoveFinalizer(arToken, "dummy")
		Expect(env.Client(platform).Update(env.Ctx, arToken)).To(Succeed())

		// reconcile the MCP again
		// expected outcome:
		// - the AccessRequest for the removed OIDC provider has been deleted
		// - the AccessRequest for the removed token has been deleted
		// - the condition for the removed OIDC provider is gone
		// - the condition for the removed token is gone
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
					WithType(corev2alpha1.ConditionPrefixAccessReady + corev2alpha1.OIDCNamePrefix + oidc.Name).
					WithStatus(metav1.ConditionTrue)),
			))
		}
		for _, token := range tokens {
			By("verifying condition for token: " + token.Name)
			Expect(mcp.Status.Conditions).To(ContainElements(
				MatchCondition(TestCondition().
					WithType(corev2alpha1.ConditionPrefixAccessReady + corev2alpha1.TokenNamePrefix + token.Name).
					WithStatus(metav1.ConditionTrue)),
			))
		}
		Expect(mcp.Status.Access).ToNot(HaveKey(removedOIDCProviderName))
		Expect(mcp.Status.Conditions).ToNot(ContainElements(
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionPrefixAccessReady + removedOIDCProviderName),
			),
		))
		Expect(mcp.Status.Access).ToNot(HaveKey(removedTokenName))
		Expect(mcp.Status.Conditions).ToNot(ContainElements(
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionPrefixAccessReady + removedTokenName),
			),
		))

		By("=== DELETE ===")

		// fake some more ClusterRequests
		By("fake: some more ClusterRequests")
		cr2 := &clustersv1alpha1.ClusterRequest{}
		cr2.SetName("cr2")
		cr2.SetNamespace(platformNamespace)
		cr2.Finalizers = []string{"dummy"}
		Expect(env.Client(platform).Create(env.Ctx, cr2)).To(Succeed())
		cr3 := &clustersv1alpha1.ClusterRequest{}
		cr3.SetName("cr3")
		cr3.SetNamespace(platformNamespace)
		cr3.Finalizers = []string{"dummy"}
		Expect(env.Client(platform).Create(env.Ctx, cr3)).To(Succeed())
		mcp.Finalizers = append(mcp.Finalizers, corev2alpha1.ClusterRequestFinalizerPrefix+cr2.Name, corev2alpha1.ClusterRequestFinalizerPrefix+cr3.Name)
		Expect(env.Client(onboarding).Update(env.Ctx, mcp)).To(Succeed())

		// put a finalizer on the MCP cr
		cr.Finalizers = append(cr.Finalizers, "dummy")
		Expect(env.Client(platform).Update(env.Ctx, cr)).To(Succeed())

		// delete the MCP
		By("deleting the MCP")
		Expect(env.Client(onboarding).Delete(env.Ctx, mcp)).To(Succeed())

		// reconcile the MCP
		// expected outcome:
		// - all service resources that depend on the MCP have a deletion timestamp
		// - the MCP conditions reflect that it is waiting for services to be deleted
		// - neither ClusterRequests nor AccessRequests have deletion timestamps
		// - the MCP should be requeued with a short requeueAfter duration
		By("first MCP reconciliation after delete")
		res = env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(res.RequeueAfter).To(BeNumerically("<", 1*time.Minute))
		serviceResources := []client.Object{
			&corev1.ConfigMap{},
			&corev1.ServiceAccount{},
			&corev1.Secret{},
		}
		for _, obj := range serviceResources {
			obj.SetName(mcp.Name)
			obj.SetNamespace(mcp.Namespace)
			Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			Expect(obj.GetDeletionTimestamp().IsZero()).To(BeFalse())
		}
		Expect(mcp.Status.Conditions).To(ContainElements(
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionAllServicesDeleted).
				WithStatus(metav1.ConditionFalse).
				WithReason(cconst.ReasonWaitingForServiceDeletion)),
		))
		for _, oidc := range oidcProviders {
			By("verifying AccessRequest does not have a deletion timestamp for oidc provider: " + oidc.Name)
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.OIDCNamePrefix+oidc.Name))
			ar.SetNamespace(platformNamespace)
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
			Expect(ar.DeletionTimestamp.IsZero()).To(BeTrue())
		}
		for _, token := range tokens {
			By("verifying AccessRequest does not have a deletion timestamp for token: " + token.Name)
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.TokenNamePrefix+token.Name))
			ar.SetNamespace(platformNamespace)
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
			Expect(ar.DeletionTimestamp.IsZero()).To(BeTrue())
		}
		for _, obj := range []client.Object{cr, cr2, cr3} {
			By("verifying ClusterRequest does not have a deletion timestamp: " + obj.GetName())
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			Expect(obj.GetDeletionTimestamp().IsZero()).To(BeTrue())
		}

		// remove service finalizers
		By("fake: removing service finalizers")
		for _, obj := range serviceResources {
			By("fake: removing finalizer from service resource: " + obj.GetObjectKind().GroupVersionKind().Kind)
			Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			controllerutil.RemoveFinalizer(obj, "dummy")
			Expect(env.Client(onboarding).Update(env.Ctx, obj)).To(Succeed())
		}
		newFins := []string{}
		for _, fin := range mcp.Finalizers {
			if !strings.HasPrefix(fin, corev2alpha1.ServiceDependencyFinalizerPrefix) {
				newFins = append(newFins, fin)
			}
		}
		mcp.Finalizers = newFins
		Expect(env.Client(onboarding).Update(env.Ctx, mcp)).To(Succeed())

		// reconcile the MCP again
		// expected outcome:
		// - all AccessRequests have deletion timestamps
		// - all access secrets have been deleted
		// - the MCP conditions reflect that it is waiting for AccessRequests to be deleted
		// - no ClusterRequests should have deletion timestamps
		// - the MCP should be requeued with a short requeueAfter duration
		By("second MCP reconciliation after delete")
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
				WithReason(cconst.ReasonWaitingForAccessRequestDeletion)),
		))
		for _, oidc := range oidcProviders {
			By("verifying AccessRequest and access secret deletion status for oidc provider: " + oidc.Name)
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.OIDCNamePrefix+oidc.Name))
			ar.SetNamespace(platformNamespace)
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
			Expect(ar.DeletionTimestamp.IsZero()).To(BeFalse())
			Expect(mcp.Status.Conditions).To(ContainElements(
				MatchCondition(TestCondition().
					WithType(corev2alpha1.ConditionPrefixAccessReady + corev2alpha1.OIDCNamePrefix + oidc.Name).
					WithStatus(metav1.ConditionFalse).
					WithReason(cconst.ReasonWaitingForAccessRequestDeletion),
				),
			))
		}
		Expect(mcp.Status.Access).To(BeEmpty())
		secs := &corev1.SecretList{}
		Expect(env.Client(onboarding).List(env.Ctx, secs, client.InNamespace(mcp.Namespace))).To(Succeed())
		Expect(secs.Items).To(BeEmpty())
		for _, obj := range []client.Object{cr, cr2, cr3} {
			By("verifying that ClusterRequest has not been deleted: " + obj.GetName())
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			Expect(obj.GetDeletionTimestamp().IsZero()).To(BeTrue())
		}

		// remove AccessRequest finalizers
		By("fake: removing AccessRequest finalizers")
		for _, oidc := range oidcProviders {
			By("fake: removing finalizer from AccessRequest for oidc provider: " + oidc.Name)
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.OIDCNamePrefix+oidc.Name))
			ar.SetNamespace(platformNamespace)
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
			controllerutil.RemoveFinalizer(ar, "dummy")
			Expect(env.Client(platform).Update(env.Ctx, ar)).To(Succeed())
		}
		for _, token := range tokens {
			By("fake: removing finalizer from AccessRequest for token: " + token.Name)
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.TokenNamePrefix+token.Name))
			ar.SetNamespace(platformNamespace)
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
			controllerutil.RemoveFinalizer(ar, "dummy")
			Expect(env.Client(platform).Update(env.Ctx, ar)).To(Succeed())
		}

		// reconcile the MCP again
		// expected outcome:
		// - cr2 and cr3 have deletion timestamps, cr has not
		// - the AccessRequests are deleted
		// - the MCP has a condition stating that it is waiting for the ClusterRequests to be deleted
		// - the MCP should be requeued with a short requeueAfter duration
		By("third MCP reconciliation after delete")
		res = env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(res.RequeueAfter).To(BeNumerically("<", 1*time.Minute))
		By("verifying ClusterRequest deletion status")
		for _, obj := range []client.Object{cr, cr2, cr3} {
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
		}
		Expect(cr.GetDeletionTimestamp().IsZero()).To(BeTrue(), "ClusterRequest should not be marked for deletion")
		Expect(cr2.GetDeletionTimestamp().IsZero()).To(BeFalse())
		Expect(cr3.GetDeletionTimestamp().IsZero()).To(BeFalse())
		By("verifying AccessRequest deletion")
		for _, oidc := range oidcProviders {
			By("verifying AccessRequest deletion for oidc provider: " + oidc.Name)
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, oidc.Name))
			ar.SetNamespace(platformNamespace)
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
		}
		for _, token := range tokens {
			By("verifying AccessRequest deletion for token: " + token.Name)
			ar := &clustersv1alpha1.AccessRequest{}
			ar.SetName(ctrlutils.NameHashSHAKE128Base32(mcp.Name, token.Name))
			ar.SetNamespace(platformNamespace)
			Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
		}
		Expect(mcp.Status.Conditions).To(ContainElements(
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionAllClusterRequestsDeleted).
				WithStatus(metav1.ConditionFalse).
				WithReason(cconst.ReasonWaitingForClusterRequestDeletion)),
		))

		// remove finalizers from cr2 and cr3
		By("fake: removing finalizers from additional ClusterRequests")
		controllerutil.RemoveFinalizer(cr2, "dummy")
		Expect(env.Client(platform).Update(env.Ctx, cr2)).To(Succeed())
		controllerutil.RemoveFinalizer(cr3, "dummy")
		Expect(env.Client(platform).Update(env.Ctx, cr3)).To(Succeed())

		// reconcile the MCP again
		// expected outcome:
		// - cr2 and cr3 have been deleted
		// - cr has a deletion timestamp
		// - the MCP has a condition stating that it is waiting for the ClusterRequest to be deleted
		// - the MCP should be requeued with a short requeueAfter duration
		By("fourth MCP reconciliation after delete")
		res = env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(res.RequeueAfter).To(BeNumerically("<", 1*time.Minute))
		By("verifying ClusterRequest deletion status")
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())
		Expect(cr.GetDeletionTimestamp().IsZero()).To(BeFalse(), "ClusterRequest should be marked for deletion")
		Expect(mcp.Status.Conditions).To(ContainElements(
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionAllClusterRequestsDeleted).
				WithStatus(metav1.ConditionFalse).
				WithReason(cconst.ReasonWaitingForClusterRequestDeletion)),
		))

		// remove finalizer from cr and remove Cluster resource
		By("fake: removing finalizer from primary ClusterRequest")
		controllerutil.RemoveFinalizer(cr, "dummy")
		Expect(env.Client(platform).Update(env.Ctx, cr)).To(Succeed())
		Expect(env.Client(platform).Delete(env.Ctx, cluster)).To(Succeed())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(cluster), cluster)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))

		// add finalizer to MCP namespace
		By("fake: adding finalizer to MCP namespace")
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
		controllerutil.AddFinalizer(ns, "dummy")
		Expect(env.Client(platform).Update(env.Ctx, ns)).To(Succeed())

		// reconcile the MCP again
		// expected outcome:
		// - the MCP namespace has a deletion timestamp
		// - the MCP has a condition stating that it is waiting for the MCP namespace to be deleted
		// - the Cluster conditions should have been removed
		// - the MCP should be requeued with a short requeueAfter duration
		By("fifth MCP reconciliation after delete")
		res = env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(res.RequeueAfter).To(BeNumerically("<", 1*time.Minute))
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
		Expect(ns.GetDeletionTimestamp().IsZero()).To(BeFalse(), "MCP namespace should be marked for deletion")
		Expect(mcp.Status.Conditions).To(ContainElements(
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionMeta).
				WithStatus(metav1.ConditionFalse).
				WithReason(cconst.ReasonWaitingForNamespaceDeletion)),
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionClusterConditionsSynced).
				WithStatus(metav1.ConditionTrue)),
		))
		Expect(mcp.Status.Conditions).ToNot(ContainElements(
			MatchFields(IgnoreExtras, Fields{
				"Type": ContainSubstring(corev2alpha1.ConditionPrefixClusterCondition),
			}),
		))

		// remove finalizer from MCP namespace
		By("fake: removing finalizer from MCP namespace")
		controllerutil.RemoveFinalizer(ns, "dummy")
		Expect(env.Client(platform).Update(env.Ctx, ns)).To(Succeed())

		// reconcile the MCP again
		// expected outcome:
		// - cr has been deleted
		// - mcp has been deleted
		// - the MCP should not be requeued
		By("sixth MCP reconciliation after delete")
		res = env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
		Expect(res.IsZero()).To(BeTrue())
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
	})

	It("should correctly set the purpose if the MCP has the override label", func() {
		rec, env := defaultTestSetup("testdata", "test-01")

		mcp := &corev2alpha1.ManagedControlPlaneV2{}
		mcp.SetName("mcp-02")
		mcp.SetNamespace("test")
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())

		platformNamespace, err := libutils.StableMCPNamespace(mcp.Name, mcp.Namespace)
		Expect(err).ToNot(HaveOccurred())
		env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))
		cr := &clustersv1alpha1.ClusterRequest{}
		cr.SetName(mcp.Name)
		cr.SetNamespace(platformNamespace)
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())
		Expect(cr.Spec.Purpose).ToNot(Equal(rec.Config.MCPClusterPurpose))
		Expect(cr.Spec.Purpose).To(Equal("my-mcp-purpose"))
		Expect(cr.Spec.WaitForClusterDeletion).To(PointTo(BeTrue()))
	})

	It("should correctly handle an MCP without OIDC providers", func() {
		rec, env := defaultTestSetup("testdata", "test-01")

		mcp := &corev2alpha1.ManagedControlPlaneV2{}
		mcp.SetName("mcp-03")
		mcp.SetNamespace("test")
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())
		env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))

		platformNamespace, err := libutils.StableMCPNamespace(mcp.Name, mcp.Namespace)
		Expect(err).ToNot(HaveOccurred())

		cr := &clustersv1alpha1.ClusterRequest{}
		cr.SetName(mcp.Name)
		cr.SetNamespace(platformNamespace)
		Expect(env.Client(platform).Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())

		// fake ClusterRequest ready status and Cluster resource
		By("fake: ClusterRequest readiness")
		cluster := &clustersv1alpha1.Cluster{}
		cluster.SetName("cluster-01")
		cluster.SetNamespace(platformNamespace)
		cluster.Spec.Purposes = []string{rec.Config.MCPClusterPurpose}
		Expect(env.Client(platform).Create(env.Ctx, cluster)).To(Succeed())
		cluster.Status.Conditions = []metav1.Condition{
			{
				Type:               "TestCondition1",
				Status:             metav1.ConditionTrue,
				Reason:             "TestReason",
				Message:            "This is a test condition",
				LastTransitionTime: metav1.Now(),
				ObservedGeneration: 1,
			},
			{
				Type:               "TestCondition2",
				Status:             metav1.ConditionFalse,
				Reason:             "TestReason",
				Message:            "This is another test condition",
				LastTransitionTime: metav1.Now(),
				ObservedGeneration: 1,
			},
		}
		Expect(env.Client(platform).Status().Update(env.Ctx, cluster)).To(Succeed())
		cr.Status.Phase = clustersv1alpha1.REQUEST_GRANTED
		cr.Status.Cluster = &commonapi.ObjectReference{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		}
		Expect(env.Client(platform).Status().Update(env.Ctx, cr)).To(Succeed())

		env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))
	})

	It("should requeue the MCP if its phase is not 'Ready', even if all accesses are ready", func() {
		_, env := defaultTestSetup("testdata", "test-02")

		mcp := &corev2alpha1.ManagedControlPlaneV2{}
		mcp.SetName("mcp-01")
		mcp.SetNamespace("test")
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())

		res := env.ShouldReconcile(mcpRec, testutils.RequestFromObject(mcp))
		Expect(env.Client(onboarding).Get(env.Ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())
		Expect(mcp.Status.Conditions).To(ContainElements(
			MatchCondition(TestCondition().
				WithType("Cluster.Ready").
				WithStatus(metav1.ConditionFalse)),
			MatchCondition(TestCondition().
				WithType(corev2alpha1.ConditionAllAccessReady).
				WithStatus(metav1.ConditionTrue)),
			MatchCondition(TestCondition().
				WithType("AccessReady.token_viewer").
				WithStatus(metav1.ConditionTrue)),
		))
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(res.RequeueAfter).To(BeNumerically("<", 1*time.Minute), "MCP has not been requeued")
	})

})
