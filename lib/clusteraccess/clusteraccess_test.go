package clusteraccess_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/openmcp-project/controller-utils/pkg/clusters"

	testutils "github.com/openmcp-project/controller-utils/pkg/testing"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/lib/clusteraccess"

	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

const (
	controllerName = "test-controller"
)

func TestUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ClusterAccess Test Suite")
}

func buildTestEnvironmentReconcile(testdataDir string, skipWorkloadCluster bool, objectsWitStatus ...client.Object) *testutils.Environment {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clustersv1alpha1.AddToScheme(scheme))

	return testutils.NewEnvironmentBuilder().
		WithFakeClient(scheme).
		WithInitObjectPath("testdata", testdataDir).
		WithReconcilerConstructor(func(c client.Client) reconcile.Reconciler {
			permissions := []clustersv1alpha1.PermissionsRequest{
				{
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{"*"},
							Resources: []string{"*"},
							Verbs:     []string{"*"},
						},
					},
				},
			}

			roleRefs := []commonapi.RoleRef{
				{
					Kind:      "ClusterRole",
					Name:      "cluster-admin",
					Namespace: "",
				},
			}

			r := clusteraccess.NewClusterAccessReconciler(c, controllerName)
			r.WithMCPScheme(scheme).
				WithWorkloadScheme(scheme).
				WithMCPPermissions(permissions).
				WithMCPRoleRefs(roleRefs).
				WithWorkloadPermissions(permissions).
				WithWorkloadRoleRefs(roleRefs).
				WithRetryInterval(1 * time.Second)
			if skipWorkloadCluster {
				r.SkipWorkloadCluster()
			}
			return r
		}).
		WithDynamicObjectsWithStatus(objectsWitStatus...).
		Build()
}

type deleteReconciler struct {
	r clusteraccess.Reconciler
}

func (dr *deleteReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	return dr.r.ReconcileDelete(ctx, req)
}

func buildTestEnvironmentDelete(testdataDir string, skipWorkloadCluster bool, objectsWitStatus ...client.Object) *testutils.Environment {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clustersv1alpha1.AddToScheme(scheme))

	return testutils.NewEnvironmentBuilder().
		WithFakeClient(scheme).
		WithInitObjectPath("testdata", testdataDir).
		WithReconcilerConstructor(func(c client.Client) reconcile.Reconciler {
			r := clusteraccess.NewClusterAccessReconciler(c, controllerName)
			r.WithRetryInterval(1 * time.Second)

			if skipWorkloadCluster {
				r.SkipWorkloadCluster()
			}

			dr := &deleteReconciler{
				r: r,
			}

			return dr
		}).
		WithDynamicObjectsWithStatus(objectsWitStatus...).
		Build()
}

func buildTestEnvironmentNoReconcile(testdataDir string, objectsWitStatus ...client.Object) *testutils.Environment {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clustersv1alpha1.AddToScheme(scheme))

	return testutils.NewEnvironmentBuilder().
		WithFakeClient(scheme).
		WithInitObjectPath("testdata", testdataDir).
		WithDynamicObjectsWithStatus(objectsWitStatus...).
		Build()
}

var _ = Describe("ClusterAccessReconciler", func() {
	Context("Reconcile", func() {
		It("should create MCP-/Workload ClusterRequests/AccessRequests", func() {
			var reconcileResult reconcile.Result

			expectedRequestNamespace := "mcp--80158a25-6874-80a6-a75d-94f57da600c0"

			request := reconcile.Request{
				NamespacedName: client.ObjectKey{
					Name:      "instance",
					Namespace: "test",
				},
			}

			accessRequestMCP := &clustersv1alpha1.AccessRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusteraccess.StableRequestName(controllerName, request) + "--mcp",
					Namespace: expectedRequestNamespace,
				},
			}

			clusterRequestWorkload := &clustersv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusteraccess.StableRequestName(controllerName, request) + "--wl",
					Namespace: expectedRequestNamespace,
				},
			}

			accessRequestWorkload := &clustersv1alpha1.AccessRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusteraccess.StableRequestName(controllerName, request) + "--wl",
					Namespace: expectedRequestNamespace,
				},
			}

			env := buildTestEnvironmentReconcile("test-01", false, accessRequestMCP, clusterRequestWorkload, accessRequestWorkload)

			reconcileResult = env.ShouldReconcile(request, "reconcilerImpl should not return an error")
			Expect(reconcileResult.RequeueAfter).ToNot(BeZero(), "reconcile should requeue after a delay")

			// reconcile now waits until the request namespace is being created
			// the format if the request namespace is "ob-<onboarding-namespace>"
			// create the expected request namespace
			requestNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: expectedRequestNamespace,
				},
			}

			Expect(env.Client().Create(env.Ctx, requestNamespace)).To(Succeed())

			// reconcile again to process the request
			env.ShouldReconcile(request, "reconcilerImpl should not return an error")

			// there should be an access request for the MCP cluster created
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(accessRequestMCP), accessRequestMCP)).To(Succeed())

			// set the access request status to "Granted"
			accessRequestMCP.Status = clustersv1alpha1.AccessRequestStatus{
				Status: commonapi.Status{
					Phase: clustersv1alpha1.REQUEST_GRANTED,
				},
			}
			Expect(env.Client().Status().Update(env.Ctx, accessRequestMCP)).To(Succeed())

			// reconcile again to process the granted access request
			env.ShouldReconcile(request, "reconcilerImpl should not return an error")

			// there should be a cluster request for the workload cluster created
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(clusterRequestWorkload), clusterRequestWorkload)).To(Succeed())

			// set the cluster request status to "Granted"
			clusterRequestWorkload.Status = clustersv1alpha1.ClusterRequestStatus{
				Status: commonapi.Status{
					Phase: clustersv1alpha1.REQUEST_GRANTED,
				},
			}
			Expect(env.Client().Status().Update(env.Ctx, clusterRequestWorkload)).To(Succeed())

			// reconcile again to process the granted cluster request
			env.ShouldReconcile(request, "reconcilerImpl should not return an error")

			// there should be an access request for the workload cluster created
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(accessRequestWorkload), accessRequestWorkload)).To(Succeed())

			// set the access request status to "Granted"
			accessRequestWorkload.Status = clustersv1alpha1.AccessRequestStatus{
				Status: commonapi.Status{
					Phase: clustersv1alpha1.REQUEST_GRANTED,
				},
			}
			Expect(env.Client().Status().Update(env.Ctx, accessRequestWorkload)).To(Succeed())

			// set the secret reference for the MCP access request and the workload access request
			accessRequestMCP.Status.SecretRef = &commonapi.ObjectReference{
				Name:      "mcp-access",
				Namespace: expectedRequestNamespace,
			}
			Expect(env.Client().Status().Update(env.Ctx, accessRequestMCP)).To(Succeed())

			accessRequestWorkload.Status.SecretRef = &commonapi.ObjectReference{
				Name:      "workload-access",
				Namespace: expectedRequestNamespace,
			}
			Expect(env.Client().Status().Update(env.Ctx, accessRequestWorkload)).To(Succeed())

			// reconcile again to process the granted access request
			env.ShouldReconcile(request, "reconcilerImpl should not return an error")

			// cast to ClusterAccessReconciler to access the reconcilerImpl methods
			reconciler, ok := env.Reconciler().(clusteraccess.Reconciler) // nolint:staticcheck
			Expect(ok).To(BeTrue(), "reconcilerImpl should be of type ClusterAccessReconciler")

			mcpCluster, err := reconciler.MCPCluster(env.Ctx, request)
			Expect(err).ToNot(HaveOccurred(), "should not return an error when getting MCP cluster")
			Expect(mcpCluster).ToNot(BeNil(), "should return a valid MCP cluster")

			workloadCluster, err := reconciler.WorkloadCluster(env.Ctx, request)
			Expect(err).ToNot(HaveOccurred(), "should not return an error when getting Workload cluster")
			Expect(workloadCluster).ToNot(BeNil(), "should return a valid Workload cluster")
		})

		It("should create MCP-/Workload ClusterRequests/AccessRequests without Workload Cluster", func() {
			var reconcileResult reconcile.Result

			expectedRequestNamespace := "mcp--80158a25-6874-80a6-a75d-94f57da600c0"

			request := reconcile.Request{
				NamespacedName: client.ObjectKey{
					Name:      "instance",
					Namespace: "test",
				},
			}

			accessRequestMCP := &clustersv1alpha1.AccessRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusteraccess.StableRequestName(controllerName, request) + "--mcp",
					Namespace: expectedRequestNamespace,
				},
			}

			env := buildTestEnvironmentReconcile("test-01", true, accessRequestMCP)

			reconcileResult = env.ShouldReconcile(request, "reconcilerImpl should not return an error")
			Expect(reconcileResult.RequeueAfter).ToNot(BeZero(), "reconcile should requeue after a delay")

			// reconcile now waits until the request namespace is being created
			// the format if the request namespace is "ob-<onboarding-namespace>"
			// create the expected request namespace
			requestNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: expectedRequestNamespace,
				},
			}

			Expect(env.Client().Create(env.Ctx, requestNamespace)).To(Succeed())

			// reconcile again to process the request
			env.ShouldReconcile(request, "reconcilerImpl should not return an error")

			// there should be an access request for the MCP cluster created
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(accessRequestMCP), accessRequestMCP)).To(Succeed())

			// set the access request status to "Granted"
			accessRequestMCP.Status = clustersv1alpha1.AccessRequestStatus{
				Status: commonapi.Status{
					Phase: clustersv1alpha1.REQUEST_GRANTED,
				},
			}
			Expect(env.Client().Status().Update(env.Ctx, accessRequestMCP)).To(Succeed())

			// reconcile again to process the granted access request
			env.ShouldReconcile(request, "reconcilerImpl should not return an error")

			// set the secret reference for the MCP access request
			accessRequestMCP.Status.SecretRef = &commonapi.ObjectReference{
				Name:      "mcp-access",
				Namespace: expectedRequestNamespace,
			}
			Expect(env.Client().Status().Update(env.Ctx, accessRequestMCP)).To(Succeed())

			// reconcile again to process the granted access request
			env.ShouldReconcile(request, "reconcilerImpl should not return an error")

			// cast to ClusterAccessReconciler to access the reconcilerImpl methods
			reconciler, ok := env.Reconciler().(clusteraccess.Reconciler) // nolint:staticcheck
			Expect(ok).To(BeTrue(), "reconcilerImpl should be of type ClusterAccessReconciler")

			mcpCluster, err := reconciler.MCPCluster(env.Ctx, request)
			Expect(err).ToNot(HaveOccurred(), "should not return an error when getting MCP cluster")
			Expect(mcpCluster).ToNot(BeNil(), "should return a valid MCP cluster")

			_, err = reconciler.WorkloadCluster(env.Ctx, request)
			Expect(err).To(HaveOccurred(), "should return an error when trying to get the Workload cluster")

			accessRequestList := &clustersv1alpha1.AccessRequestList{}
			Expect(env.Client().List(env.Ctx, accessRequestList, client.InNamespace(expectedRequestNamespace))).To(Succeed())
			Expect(len(accessRequestList.Items)).To(Equal(1), "there should be only one access request (for the MCP cluster)")
			clusterRequestList := &clustersv1alpha1.ClusterRequestList{}
			Expect(env.Client().List(env.Ctx, clusterRequestList, client.InNamespace(expectedRequestNamespace))).To(Succeed())
			Expect(len(clusterRequestList.Items)).To(Equal(0), "there should be no cluster request (for the Workload cluster)")
		})

		Context("Delete", func() {
			It("should delete MCP-/Workload ClusterRequests/AccessRequests", func() {
				var reconcileResult reconcile.Result

				expectedRequestNamespace := "mcp--80158a25-6874-80a6-a75d-94f57da600c0"

				request := reconcile.Request{
					NamespacedName: client.ObjectKey{
						Name:      "instance",
						Namespace: "test",
					},
				}

				accessRequestMCP := &clustersv1alpha1.AccessRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusteraccess.StableRequestName(controllerName, request) + "--mcp",
						Namespace: expectedRequestNamespace,
					},
				}

				clusterRequestWorkload := &clustersv1alpha1.ClusterRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusteraccess.StableRequestName(controllerName, request) + "--wl",
						Namespace: expectedRequestNamespace,
					},
				}

				accessRequestWorkload := &clustersv1alpha1.AccessRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusteraccess.StableRequestName(controllerName, request) + "--wl",
						Namespace: expectedRequestNamespace,
					},
				}

				env := buildTestEnvironmentDelete("test-02", false)

				reconcileResult = env.ShouldReconcile(request, "reconcilerImpl should not return an error")
				Expect(reconcileResult.RequeueAfter).To(BeZero(), "reconcile should requeue after a delay")

				// all access requests and cluster requests should be deleted
				Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(accessRequestMCP), accessRequestMCP)).ToNot(Succeed(), "access request for MCP cluster should not exist")
				Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(clusterRequestWorkload), clusterRequestWorkload)).ToNot(Succeed(), "cluster request for Workload cluster should not exist")
				Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(accessRequestWorkload), accessRequestWorkload)).ToNot(Succeed(), "access request for Workload cluster should not exist")
			})

			It("should delete only MCP AccessRequest with SkipWorkloadCluster", func() {
				var reconcileResult reconcile.Result

				expectedRequestNamespace := "mcp--80158a25-6874-80a6-a75d-94f57da600c0"

				request := reconcile.Request{
					NamespacedName: client.ObjectKey{
						Name:      "instance",
						Namespace: "test",
					},
				}

				accessRequestMCP := &clustersv1alpha1.AccessRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusteraccess.StableRequestName(controllerName, request) + "--mcp",
						Namespace: expectedRequestNamespace,
					},
				}

				env := buildTestEnvironmentDelete("test-02", true)

				reconcileResult = env.ShouldReconcile(request, "reconcilerImpl should not return an error")
				Expect(reconcileResult.RequeueAfter).To(BeZero(), "reconcile should requeue after a delay")

				// access request should be deleted
				Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(accessRequestMCP), accessRequestMCP)).ToNot(Succeed(), "access request for MCP cluster should not exist")
			})
		})
	})
})

var _ = Describe("ClusterAccessManager", func() {
	It("should create and wait for onboarding cluster access", func() {

		const (
			clusterName         = "onboarding-cluster"
			controllerName      = "test-controller"
			controllerNamespace = "test-namespace"
			timeout             = 1 * time.Second
			interval            = 20 * time.Millisecond
		)

		requestName := clusteraccess.StableRequestNameFromLocalName(controllerName, clusterName)

		clusterRequest := &clustersv1alpha1.ClusterRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      requestName,
				Namespace: controllerNamespace,
			},
		}

		accessRequest := &clustersv1alpha1.AccessRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      requestName,
				Namespace: controllerNamespace,
			},
		}

		env := buildTestEnvironmentNoReconcile("test-03", accessRequest, clusterRequest)

		manager := clusteraccess.NewClusterAccessManager(env.Client(), controllerName, controllerNamespace)
		Expect(manager).ToNot(BeNil(), "should create a ClusterAccessManager")

		manager.WithInterval(interval).WithTimeout(timeout)

		scheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		utilruntime.Must(clustersv1alpha1.AddToScheme(scheme))

		var cluster *clusters.Cluster
		var err error
		var wg sync.WaitGroup
		ctx, cancel := context.WithTimeout(env.Ctx, 1*time.Second)

		defer cancel()
		wg.Add(1)

		go func() {
			defer wg.Done()
			cluster, err = manager.CreateAndWaitForCluster(ctx, clusterName, clustersv1alpha1.PURPOSE_ONBOARDING, scheme, []clustersv1alpha1.PermissionsRequest{
				{
					Rules: []rbacv1.PolicyRule{
						{
							APIGroups: []string{"*"},
							Resources: []string{"*"},
							Verbs:     []string{"*"},
						},
					},
				},
			})
		}()

		Eventually(func() bool {
			// read rhe cluster request
			if err := env.Client().Get(ctx, client.ObjectKeyFromObject(clusterRequest), clusterRequest); err != nil {
				return false
			}

			// set status phase to REQUEST_GRANTED
			if clusterRequest.Status.Phase != clustersv1alpha1.REQUEST_GRANTED {
				clusterRequest.Status.Phase = clustersv1alpha1.REQUEST_GRANTED
				if err := env.Client().Status().Update(ctx, clusterRequest); err != nil {
					return false
				}
				return false
			}

			// read the access request
			if err := env.Client().Get(ctx, client.ObjectKeyFromObject(accessRequest), accessRequest); err != nil {
				return false
			}

			// set status phase to REQUEST_GRANTED and set the secret reference
			if accessRequest.Status.Phase != clustersv1alpha1.REQUEST_GRANTED {
				accessRequest.Status.Phase = clustersv1alpha1.REQUEST_GRANTED

				accessRequest.Status.SecretRef = &commonapi.ObjectReference{
					Name:      "access",
					Namespace: "default",
				}

				if err := env.Client().Status().Update(ctx, accessRequest); err != nil {
					return false
				}
			}

			return true
		}, timeout, interval).Should(BeTrue())

		wg.Wait()

		Expect(err).ToNot(HaveOccurred(), "should not return an error when creating/waiting for onboarding cluster access")
		Expect(cluster).ToNot(BeNil(), "should return a valid onboarding cluster")
	})
})
