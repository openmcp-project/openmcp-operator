package advanced_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	testutils "github.com/openmcp-project/controller-utils/pkg/testing"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/openmcp-project/openmcp-operator/api/install"
	"github.com/openmcp-project/openmcp-operator/lib/clusteraccess/advanced"
)

func TestAdvancedClusterAccess(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Advanced Cluster Access Test Suite")
}

func defaultTestSetup(testDirPathSegments ...string) *testutils.Environment {
	scheme := install.InstallOperatorAPIsPlatform(runtime.NewScheme())
	env := testutils.NewEnvironmentBuilder().
		WithFakeClient(scheme).
		WithInitObjectPath(testDirPathSegments...).
		WithDynamicObjectsWithStatus(&clustersv1alpha1.ClusterRequest{}, &clustersv1alpha1.AccessRequest{}).
		Build()

	return env
}

//nolint:unparam
func defaultClusterAccessReconciler(env *testutils.Environment, controllerName string) advanced.ClusterAccessReconciler {
	return advanced.NewClusterAccessReconciler(env.Client(), controllerName).
		WithRetryInterval(100*time.Millisecond).
		WithFakingCallback(advanced.FakingCallback_WaitingForAccessRequestDeletion, advanced.FakeAccessRequestDeletion([]string{"*"}, []string{"*"})).
		WithFakingCallback(advanced.FakingCallback_WaitingForClusterRequestDeletion, advanced.FakeClusterRequestDeletion(true, []string{"*"}, []string{"*"})).
		WithFakingCallback(advanced.FakingCallback_WaitingForClusterRequestReadiness, advanced.FakeClusterRequestReadiness(&clustersv1alpha1.ClusterSpec{
			Profile:  "my-profile",
			Purposes: []string{"test"},
			Tenancy:  clustersv1alpha1.TENANCY_EXCLUSIVE,
		})).
		WithFakingCallback(advanced.FakingCallback_WaitingForAccessRequestReadiness, advanced.FakeAccessRequestReadiness()).
		WithFakeClientGenerator(func(ctx context.Context, kcfgData []byte, scheme *runtime.Scheme, additionalData ...any) (client.Client, error) {
			return fake.NewClientBuilder().WithScheme(scheme).Build(), nil
		})
}

var _ = Describe("Advanced Cluster Access", func() {

	It("should create and delete a ClusterRequest, if configured", func() {
		env := defaultTestSetup("testdata", "test-01")
		rec := defaultClusterAccessReconciler(env, "foo")
		customNamespace := "custom-namespace"
		rec.Register(advanced.NewClusterRequest("foobar", "fb", advanced.StaticClusterRequestSpecGenerator(&clustersv1alpha1.ClusterRequestSpec{
			Purpose:                "test",
			WaitForClusterDeletion: ptr.To(true),
		})).Build()) // example 1, without access request
		rec.Register(advanced.NewClusterRequest("foobaz", "fz", advanced.StaticClusterRequestSpecGenerator(&clustersv1alpha1.ClusterRequestSpec{
			Purpose:                "test",
			WaitForClusterDeletion: ptr.To(false),
		})).WithNamespaceGenerator(func(request reconcile.Request, _ ...any) (string, error) {
			return customNamespace, nil
		}).WithTokenAccess(&clustersv1alpha1.TokenConfig{}).Build()) // example 2, with access request
		req := testutils.RequestFromStrings("bar", "baz")

		Eventually(expectNoRequeue(env.Ctx, rec, req, false)).Should(Succeed())

		// EXAMPLE 1
		// check namespace
		namespace, err := advanced.DefaultNamespaceGenerator(req)
		Expect(err).ToNot(HaveOccurred())
		ns := &corev1.Namespace{}
		ns.Name = namespace
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())

		// check AccessRequest
		_, err = rec.AccessRequest(env.Ctx, req, "foobar")
		Expect(err).To(HaveOccurred()) // no access was requested, so this should fail

		// check ClusterRequest
		cr, err := rec.ClusterRequest(env.Ctx, req, "foobar")
		Expect(err).ToNot(HaveOccurred())
		Expect(cr.Name).To(Equal(advanced.StableRequestName("foo", req, "fb")))
		Expect(cr.Namespace).To(Equal(namespace))
		Expect(cr.Labels).To(HaveKeyWithValue(openmcpconst.ManagedByLabel, "foo"))
		Expect(cr.Labels).To(HaveKeyWithValue(openmcpconst.ManagedPurposeLabel, req.Namespace+"."+req.Name+".foobar"))
		Expect(cr.Spec.Purpose).To(Equal("test"))
		Expect(cr.Spec.WaitForClusterDeletion).To(PointTo(BeTrue()))
		crCopy := cr.DeepCopy()
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())
		Expect(cr).To(Equal(crCopy)) // the method should have returned the up-to-date object

		// check Cluster
		c, err := rec.Cluster(env.Ctx, req, "foobar")
		Expect(err).ToNot(HaveOccurred())
		Expect(c.Name).To(Equal(cr.Status.Cluster.Name))
		Expect(c.Namespace).To(Equal(cr.Status.Cluster.Namespace))
		Expect(c.Spec.Purposes).To(ContainElement("test"))
		cCopy := c.DeepCopy()
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(c), c)).To(Succeed())
		Expect(c).To(Equal(cCopy)) // the method should have returned the up-to-date object

		// check cluster access
		_, err = rec.Access(env.Ctx, req, "foobar")
		Expect(err).To(HaveOccurred()) // no access was requested, so this should fail

		// EXAMPLE 2
		// check namespace
		ns2 := &corev1.Namespace{}
		ns2.Name = customNamespace
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ns2), ns2)).To(Succeed())

		// check AccessRequest
		ar2, err := rec.AccessRequest(env.Ctx, req, "foobaz")
		Expect(err).ToNot(HaveOccurred())
		Expect(ar2.Name).To(Equal(advanced.StableRequestName("foo", req, "fz")))
		Expect(ar2.Namespace).To(Equal(customNamespace))
		Expect(ar2.Labels).To(HaveKeyWithValue(openmcpconst.ManagedByLabel, "foo"))
		Expect(ar2.Labels).To(HaveKeyWithValue(openmcpconst.ManagedPurposeLabel, req.Namespace+"."+req.Name+".foobaz"))
		Expect(ar2.Spec.RequestRef.Name).To(Equal(advanced.StableRequestName("foo", req, "fz")))
		Expect(ar2.Spec.RequestRef.Namespace).To(Equal(customNamespace))
		arCopy := ar2.DeepCopy()
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar2), ar2)).To(Succeed())
		Expect(ar2).To(Equal(arCopy)) // the method should have returned the up-to-date object

		// check ClusterRequest
		cr2, err := rec.ClusterRequest(env.Ctx, req, "foobaz")
		Expect(err).ToNot(HaveOccurred())
		Expect(cr2.Name).To(Equal(ar2.Spec.RequestRef.Name))
		Expect(cr2.Namespace).To(Equal(ar2.Spec.RequestRef.Namespace))
		Expect(cr2.Labels).To(HaveKeyWithValue(openmcpconst.ManagedByLabel, "foo"))
		Expect(cr2.Labels).To(HaveKeyWithValue(openmcpconst.ManagedPurposeLabel, req.Namespace+"."+req.Name+".foobaz"))
		Expect(cr2.Spec.Purpose).To(Equal("test"))
		cr2Copy := cr2.DeepCopy()
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(cr2), cr2)).To(Succeed())
		Expect(cr2).To(Equal(cr2Copy)) // the method should have returned the up-to-date object

		// check Cluster
		c2, err := rec.Cluster(env.Ctx, req, "foobaz")
		Expect(err).ToNot(HaveOccurred())
		Expect(c2.Name).To(Equal(cr2.Status.Cluster.Name))
		Expect(c2.Namespace).To(Equal(cr2.Status.Cluster.Namespace))
		Expect(c2.Spec.Purposes).To(ContainElement("test"))
		c2Copy := c2.DeepCopy()
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(c2), c2)).To(Succeed())
		Expect(c2).To(Equal(c2Copy)) // the method should have returned the up-to-date object

		// check cluster access
		access, err := rec.Access(env.Ctx, req, "foobaz")
		Expect(err).ToNot(HaveOccurred())
		Expect(access.ID()).To(Equal("foobaz"))
		Expect(access.Client()).ToNot(BeNil())

		// delete everything again, except for namespaces
		Eventually(expectNoRequeue(env.Ctx, rec, req, true)).Should(Succeed())
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(c), c)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ns2), ns2)).To(Succeed())
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar2), ar2)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(cr2), cr2)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(c2), c2)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
	})

	It("should create and delete an AccessRequest from a ClusterRequest, if configured", func() {
		env := defaultTestSetup("testdata", "test-01")
		rec := defaultClusterAccessReconciler(env, "foo")
		permission := clustersv1alpha1.PermissionsRequest{
			Name: "my-custom-admin",
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"*"},
					Resources: []string{"*"},
					Verbs:     []string{"*"},
				},
			},
		}
		roleRef := commonapi.RoleRef{
			Name: "cluster-admin",
			Kind: "ClusterRole",
		}
		rec.Register(advanced.ExistingClusterRequest("foobar", "fb", advanced.StaticReferenceGenerator(&commonapi.ObjectReference{
			Name:      "cr-01",
			Namespace: "test",
		})).WithTokenAccess(&clustersv1alpha1.TokenConfig{
			Permissions: []clustersv1alpha1.PermissionsRequest{permission},
			RoleRefs:    []commonapi.RoleRef{roleRef},
		}).Build())
		req := testutils.RequestFromStrings("bar", "baz")

		Eventually(expectNoRequeue(env.Ctx, rec, req, false)).Should(Succeed())

		// check AccessRequest
		ar, err := rec.AccessRequest(env.Ctx, req, "foobar")
		Expect(err).ToNot(HaveOccurred())
		Expect(ar.Name).To(Equal(advanced.StableRequestName("foo", req, "fb")))
		namespace, err := advanced.DefaultNamespaceGenerator(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(ar.Namespace).To(Equal(namespace))
		Expect(ar.Labels).To(HaveKeyWithValue(openmcpconst.ManagedByLabel, "foo"))
		Expect(ar.Labels).To(HaveKeyWithValue(openmcpconst.ManagedPurposeLabel, req.Namespace+"."+req.Name+".foobar"))
		Expect(ar.Spec.RequestRef.Name).To(Equal("cr-01"))
		Expect(ar.Spec.RequestRef.Namespace).To(Equal("test"))
		Expect(ar.Spec.ClusterRef.Name).To(Equal("cluster-01"))
		Expect(ar.Spec.ClusterRef.Namespace).To(Equal("test"))
		Expect(ar.Spec.OIDC).To(BeNil())
		Expect(ar.Spec.Token).ToNot(BeNil())
		Expect(ar.Spec.Token.Permissions).To(ConsistOf(permission))
		Expect(ar.Spec.Token.RoleRefs).To(ConsistOf(roleRef))
		arCopy := ar.DeepCopy()
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		Expect(ar).To(Equal(arCopy)) // the method should have returned the up-to-date object

		// check ClusterRequest
		cr, err := rec.ClusterRequest(env.Ctx, req, "foobar")
		Expect(err).ToNot(HaveOccurred())
		Expect(cr.Name).To(Equal(ar.Spec.RequestRef.Name))
		Expect(cr.Namespace).To(Equal(ar.Spec.RequestRef.Namespace))
		Expect(cr.Status.Cluster.Name).To(Equal(ar.Spec.ClusterRef.Name))
		Expect(cr.Status.Cluster.Namespace).To(Equal(ar.Spec.ClusterRef.Namespace))
		crCopy := cr.DeepCopy()
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())
		Expect(cr).To(Equal(crCopy)) // the method should have returned the up-to-date object

		// check Cluster
		c, err := rec.Cluster(env.Ctx, req, "foobar")
		Expect(err).ToNot(HaveOccurred())
		Expect(c.Name).To(Equal(cr.Status.Cluster.Name))
		Expect(c.Namespace).To(Equal(cr.Status.Cluster.Namespace))
		cCopy := c.DeepCopy()
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(c), c)).To(Succeed())
		Expect(c).To(Equal(cCopy)) // the method should have returned the up-to-date object

		// check cluster access
		access, err := rec.Access(env.Ctx, req, "foobar")
		Expect(err).ToNot(HaveOccurred())
		Expect(access.ID()).To(Equal("foobar"))
		Expect(access.Client()).ToNot(BeNil())

		// delete everything again
		// ClusterRequest and Cluster were not created by this library, so they should not be deleted
		Eventually(expectNoRequeue(env.Ctx, rec, req, true)).Should(Succeed())
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(c), c)).To(Succeed())
	})

	It("should create and delete an AccessRequest from a Cluster, if configured", func() {
		env := defaultTestSetup("testdata", "test-01")
		rec := defaultClusterAccessReconciler(env, "foo")
		oidc := &clustersv1alpha1.OIDCConfig{
			OIDCProviderConfig: commonapi.OIDCProviderConfig{
				Name:          "my-oidc",
				Issuer:        "https://example.com/oidc",
				ClientID:      "my-client-id",
				UsernameClaim: "email",
				GroupsClaim:   "groups",
				ExtraScopes:   []string{"scope1", "scope2"},
				RoleBindings: []commonapi.RoleBindings{
					{
						Subjects: []rbacv1.Subject{
							{
								Kind: rbacv1.UserKind,
								Name: "user1",
							},
						},
						RoleRefs: []commonapi.RoleRef{
							{
								Name: "my-cluster-admin",
								Kind: "ClusterRole",
							},
						},
					},
				},
			},
			Roles: []clustersv1alpha1.PermissionsRequest{
				{
					Name: "my-custom-admin",
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{"*"},
							Resources: []string{"*"},
							Verbs:     []string{"*"},
						},
					},
				},
			},
		}
		rec.Register(advanced.ExistingCluster("foobar", "fb", advanced.StaticReferenceGenerator(&commonapi.ObjectReference{
			Name:      "cluster-01",
			Namespace: "test",
		})).WithOIDCAccess(oidc).Build())
		req := testutils.RequestFromStrings("bar", "baz")

		Eventually(expectNoRequeue(env.Ctx, rec, req, false)).Should(Succeed())

		// check AccessRequest
		ar, err := rec.AccessRequest(env.Ctx, req, "foobar")
		Expect(err).ToNot(HaveOccurred())
		Expect(ar.Name).To(Equal(advanced.StableRequestName("foo", req, "fb")))
		namespace, err := advanced.DefaultNamespaceGenerator(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(ar.Namespace).To(Equal(namespace))
		Expect(ar.Labels).To(HaveKeyWithValue(openmcpconst.ManagedByLabel, "foo"))
		Expect(ar.Labels).To(HaveKeyWithValue(openmcpconst.ManagedPurposeLabel, req.Namespace+"."+req.Name+".foobar"))
		Expect(ar.Spec.ClusterRef.Name).To(Equal("cluster-01"))
		Expect(ar.Spec.ClusterRef.Namespace).To(Equal("test"))
		Expect(ar.Spec.Token).To(BeNil())
		Expect(ar.Spec.OIDC).ToNot(BeNil())
		Expect(ar.Spec.OIDC).To(Equal(oidc))
		arCopy := ar.DeepCopy()
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		Expect(ar).To(Equal(arCopy)) // the method should have returned the up-to-date object

		// check ClusterRequest
		_, err = rec.ClusterRequest(env.Ctx, req, "foobar")
		Expect(err).To(HaveOccurred()) // no ClusterRequest is involved, so this should fail

		// check Cluster
		c, err := rec.Cluster(env.Ctx, req, "foobar")
		Expect(err).ToNot(HaveOccurred())
		Expect(c.Name).To(Equal(ar.Spec.ClusterRef.Name))
		Expect(c.Namespace).To(Equal(ar.Spec.ClusterRef.Namespace))
		cCopy := c.DeepCopy()
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(c), c)).To(Succeed())
		Expect(c).To(Equal(cCopy)) // the method should have returned the up-to-date object

		// check cluster access
		access, err := rec.Access(env.Ctx, req, "foobar")
		Expect(err).ToNot(HaveOccurred())
		Expect(access.ID()).To(Equal("foobar"))
		Expect(access.Client()).ToNot(BeNil())

		// delete everything again
		// Cluster was not created by this library, so it should not be deleted
		Eventually(expectNoRequeue(env.Ctx, rec, req, true)).Should(Succeed())
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(MatchError(apierrors.IsNotFound, "IsNotFound"))
		Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(c), c)).To(Succeed())
	})

	It("should take overwritten labels into account", func() {
		env := defaultTestSetup("testdata", "test-01")
		rec := defaultClusterAccessReconciler(env, "foo").WithManagedLabels(func(controllerName string, req reconcile.Request, reg advanced.ClusterRegistration) (string, string, map[string]string) {
			return "my-managed-by", "my-purpose", map[string]string{"custom-label": "custom-value"}
		})
		rec.Register(advanced.ExistingClusterRequest("foobar", "fb", advanced.StaticReferenceGenerator(&commonapi.ObjectReference{
			Name:      "cr-01",
			Namespace: "test",
		})).WithTokenAccess(&clustersv1alpha1.TokenConfig{}).Build())
		req := testutils.RequestFromStrings("bar", "baz")

		Eventually(expectNoRequeue(env.Ctx, rec, req, false)).Should(Succeed())

		// check AccessRequest
		ar, err := rec.AccessRequest(env.Ctx, req, "foobar")
		Expect(err).ToNot(HaveOccurred())
		Expect(ar.Name).To(Equal(advanced.StableRequestName("foo", req, "fb")))
		namespace, err := advanced.DefaultNamespaceGenerator(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(ar.Namespace).To(Equal(namespace))
		Expect(ar.Labels).To(HaveKeyWithValue(openmcpconst.ManagedByLabel, "my-managed-by"))
		Expect(ar.Labels).To(HaveKeyWithValue(openmcpconst.ManagedPurposeLabel, "my-purpose"))
		Expect(ar.Labels).To(HaveKeyWithValue("custom-label", "custom-value"))
	})

	It("should requeue if the ClusterRequest or AccessRequest is not yet ready", func() {
		env := defaultTestSetup("testdata", "test-01")
		rec := defaultClusterAccessReconciler(env, "foo")
		rec.Register(advanced.NewClusterRequest("foobar", "fb", advanced.StaticClusterRequestSpecGenerator(&clustersv1alpha1.ClusterRequestSpec{
			Purpose:                "test",
			WaitForClusterDeletion: ptr.To(true),
		})).WithTokenAccess(&clustersv1alpha1.TokenConfig{}).Build())
		req := testutils.RequestFromStrings("bar", "baz")

		// first requeue due to ClusterRequest
		expectRequeue(env.Ctx, rec, req, false)
		// ClusterRequest will be mocked ready now, another requeue for AccessRequest is expected
		expectRequeue(env.Ctx, rec, req, false)
		expectNoRequeue(env.Ctx, rec, req, false) // now everything should be ready

		// deleting should requeue due to AccessRequest deletion
		expectRequeue(env.Ctx, rec, req, true)
		// AccessRequest will be mocked deleted now, another requeue for ClusterRequest deletion is expected
		expectRequeue(env.Ctx, rec, req, true)
		expectNoRequeue(env.Ctx, rec, req, true) // now everything should be deleted
	})

	Context("Conflict Detection", func() {

		It("should correctly handle conflicts with existing ClusterRequests", func() {
			env := defaultTestSetup("testdata", "test-02")
			rec := defaultClusterAccessReconciler(env, "foo")
			rec.Register(advanced.NewClusterRequest("foobar", "fb", advanced.StaticClusterRequestSpecGenerator(&clustersv1alpha1.ClusterRequestSpec{
				Purpose:                "test",
				WaitForClusterDeletion: ptr.To(true),
			})).Build())
			req := testutils.RequestFromStrings("cr-conflict", "baz")

			// ensure that the resource we are trying to conflict with actually exists
			namespace, err := advanced.DefaultNamespaceGenerator(req)
			Expect(err).ToNot(HaveOccurred())
			cr := &clustersv1alpha1.ClusterRequest{}
			cr.Name = advanced.StableRequestName("foo", req, "fb")
			cr.Namespace = namespace
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed(), "conflicting ClusterRequest '%s/%s' does not exist", cr.Namespace, cr.Name)

			Eventually(expectError(env.Ctx, rec, req, false, WithTransform(func(e error) string { return e.Error() }, And(ContainSubstring("already exists"), ContainSubstring("not managed"))))).Should(Succeed())

			// deleting should not remove the ClusterRequest, because it is not managed by us
			Eventually(expectNoRequeue(env.Ctx, rec, req, true)).Should(Succeed())
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(cr), cr)).To(Succeed())
		})

		It("should correctly handle conflicts with existing AccessRequests", func() {
			env := defaultTestSetup("testdata", "test-02")
			rec := defaultClusterAccessReconciler(env, "foo")
			rec.Register(advanced.NewClusterRequest("foobar", "fb", advanced.StaticClusterRequestSpecGenerator(&clustersv1alpha1.ClusterRequestSpec{
				Purpose:                "test",
				WaitForClusterDeletion: ptr.To(true),
			})).WithTokenAccess(&clustersv1alpha1.TokenConfig{}).Build())
			req := testutils.RequestFromStrings("ar-conflict", "baz")

			// ensure that the resource we are trying to conflict with actually exists
			namespace, err := advanced.DefaultNamespaceGenerator(req)
			Expect(err).ToNot(HaveOccurred())
			ar := &clustersv1alpha1.AccessRequest{}
			ar.Name = advanced.StableRequestName("foo", req, "fb")
			ar.Namespace = namespace
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed(), "conflicting ClusterRequest '%s/%s' does not exist", ar.Namespace, ar.Name)

			Eventually(expectError(env.Ctx, rec, req, false, WithTransform(func(e error) string { return e.Error() }, And(ContainSubstring("already exists"), ContainSubstring("not managed"))))).Should(Succeed())

			// deleting should not remove the AccessRequest, because it is not managed by us
			Eventually(expectNoRequeue(env.Ctx, rec, req, true)).Should(Succeed())
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(ar), ar)).To(Succeed())
		})

	})

})

// expectRequeue runs the reconciler's Reconcile method (or ReconcileDelete, if del is true) with the given request and expects a requeueAfter duration greater than zero in the returned result.
// If match is non-empty, the first element is matched against the requeueAfter duration instead of just checking that it's greater than zero. Further elements are ignored.
// It fails if the reconcile returns an error.
//
//nolint:unparam
func expectRequeue(ctx context.Context, rec advanced.ClusterAccessReconciler, req reconcile.Request, del bool, match ...types.GomegaMatcher) func(Gomega) {
	return func(g Gomega) {
		var res reconcile.Result
		var err error
		if del {
			res, err = rec.ReconcileDelete(ctx, req)
		} else {
			res, err = rec.Reconcile(ctx, req)
		}
		g.Expect(err).ToNot(HaveOccurred())
		if len(match) > 0 {
			g.Expect(res.RequeueAfter).To(match[0], fmt.Sprintf("expected requeueAfter to match %v", match[0]))
		} else {
			g.Expect(res.RequeueAfter).To(BeNumerically(">", 0), "expected requeueAfter > 0")
		}
	}
}

// expectNoRequeue runs the reconciler's Reconcile method (or ReconcileDelete, if del is true) with the given request and expects no requeue in the returned result.
// It fails if the reconcile returns an error.
func expectNoRequeue(ctx context.Context, rec advanced.ClusterAccessReconciler, req reconcile.Request, del bool) func(Gomega) {
	return func(g Gomega) {
		var res reconcile.Result
		var err error
		if del {
			res, err = rec.ReconcileDelete(ctx, req)
		} else {
			res, err = rec.Reconcile(ctx, req)
		}
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(res.RequeueAfter).To(BeZero(), "expected requeueAfter to be zero")
	}
}

// expectError runs the reconciler's Reconcile method (or ReconcileDelete, if del is true) with the given request and expects an error to be returned.
// If match is non-empty, the first element is matched against the error instead of just checking that an error occurred. Further elements are ignored.
func expectError(ctx context.Context, rec advanced.ClusterAccessReconciler, req reconcile.Request, del bool, match ...types.GomegaMatcher) func(Gomega) {
	return func(g Gomega) {
		var err error
		if del {
			_, err = rec.ReconcileDelete(ctx, req)
		} else {
			_, err = rec.Reconcile(ctx, req)
		}
		g.Expect(err).To(HaveOccurred())
		if len(match) > 0 {
			g.Expect(err).To(match[0], fmt.Sprintf("expected error to match %v", match[0]))
		}
	}
}
