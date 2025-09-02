package clusteraccess

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	constv1alpha1 "github.com/openmcp-project/openmcp-operator/api/constants"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

const (
	controllerName        = "ClusterAccess"
	requestSuffixMCP      = "--mcp"
	requestSuffixWorkload = "--wl"
)

// Reconciler is an interface for reconciling access to openMCP clusters.
// It creates and manages AccessRequests and ClusterRequests for MCP and Workload clusters.
// The Reconciler is being used by service providers to manager the access to an MCP and Workload cluster
// that are being manged for an instance of a service provider resource.
type Reconciler interface {
	// WithRetryInterval sets the retry interval
	WithRetryInterval(interval time.Duration) Reconciler
	// WithMCPPermissions sets the permissions for the MCP AccessRequest.
	WithMCPPermissions(permissions []clustersv1alpha1.PermissionsRequest) Reconciler
	// WithMCPRoleRefs sets the RoleRefs for the MCP AccessRequest.
	WithMCPRoleRefs(roleRefs []commonapi.RoleRef) Reconciler
	// WithWorkloadPermissions sets the permissions for the Workload AccessRequest.
	WithWorkloadPermissions(permissions []clustersv1alpha1.PermissionsRequest) Reconciler
	// WithWorkloadRoleRefs sets the RoleRefs for the Workload AccessRequest.
	WithWorkloadRoleRefs(roleRefs []commonapi.RoleRef) Reconciler
	// WithMCPScheme sets the scheme for the MCP Kubernetes client.
	WithMCPScheme(scheme *runtime.Scheme) Reconciler
	// WithWorkloadScheme sets the scheme for the Workload Kubernetes client.
	WithWorkloadScheme(scheme *runtime.Scheme) Reconciler

	// MCPCluster creates a Cluster for the MCP AccessRequest.
	// This function will only be successful if the MCP AccessRequest is granted and Reconcile returned without an error
	// and a reconcile.Result with no RequeueAfter value.
	MCPCluster(ctx context.Context, request reconcile.Request) (*clusters.Cluster, error)
	// WorkloadCluster creates a Cluster for the Workload AccessRequest.
	// This function will only be successful if the Workload AccessRequest is granted and Reconcile returned without an error
	// and a reconcile.Result with no RequeueAfter value.
	WorkloadCluster(ctx context.Context, request reconcile.Request) (*clusters.Cluster, error)

	// Reconcile creates the ClusterRequests and AccessRequests for the MCP and Workload clusters based on the reconciled object.
	// This function should be called during all reconciliations of the reconciled object.
	// ctx is the context for the reconciliation.
	// request is the object that is being reconciled
	// It returns a reconcile.Result and an error if the reconciliation failed.
	// The reconcile.Result may contain a RequeueAfter value to indicate that the reconciliation should be retried after a certain duration.
	// The duration is set by the WithRetryInterval method.
	Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error)
	// ReconcileDelete deletes the AccessRequests and ClusterRequests for the MCP and Workload clusters based on the reconciled object.
	// This function should be called during the deletion of the reconciled object.
	// ctx is the context for the reconciliation.
	// request is the object that is being reconciled.
	// It returns a reconcile.Result and an error if the reconciliation failed.
	// The reconcile.Result may contain a RequeueAfter value to indicate that the reconciliation should be retried after a certain duration.
	// The duration is set by the WithRetryInterval method.
	ReconcileDelete(ctx context.Context, request reconcile.Request) (reconcile.Result, error)
}

type reconcilerImpl struct {
	platformClusterClient client.Client
	controllerName        string
	retryInterval         time.Duration
	mcpPermissions        []clustersv1alpha1.PermissionsRequest
	mcpRoleRefs           []commonapi.RoleRef
	workloadPermissions   []clustersv1alpha1.PermissionsRequest
	workloadRoleRefs      []commonapi.RoleRef
	mcpScheme             *runtime.Scheme
	workloadScheme        *runtime.Scheme
}

// NewClusterAccessReconciler creates a new ClusterAccessReconciler with the given parameters.
// platformClusterClient is the client to the platform cluster where the AccessRequests and ClusterRequests are created.
// controllerName is the name of the Kubernetes controller, used to create stable request names.
func NewClusterAccessReconciler(platformClusterClient client.Client, controllerName string) Reconciler {
	return &reconcilerImpl{
		platformClusterClient: platformClusterClient,
		controllerName:        controllerName,
		retryInterval:         5 * time.Second,
		mcpPermissions:        []clustersv1alpha1.PermissionsRequest{},
		mcpRoleRefs:           []commonapi.RoleRef{},
		workloadPermissions:   []clustersv1alpha1.PermissionsRequest{},
		workloadRoleRefs:      []commonapi.RoleRef{},
		mcpScheme:             runtime.NewScheme(),
		workloadScheme:        runtime.NewScheme(),
	}
}

func (r *reconcilerImpl) WithRetryInterval(interval time.Duration) Reconciler {
	r.retryInterval = interval
	return r
}

func (r *reconcilerImpl) WithMCPPermissions(permissions []clustersv1alpha1.PermissionsRequest) Reconciler {
	r.mcpPermissions = permissions
	return r
}

func (r *reconcilerImpl) WithMCPRoleRefs(roleRefs []commonapi.RoleRef) Reconciler {
	r.mcpRoleRefs = roleRefs
	return r
}

func (r *reconcilerImpl) WithWorkloadPermissions(permissions []clustersv1alpha1.PermissionsRequest) Reconciler {
	r.workloadPermissions = permissions
	return r
}

func (r *reconcilerImpl) WithWorkloadRoleRefs(roleRefs []commonapi.RoleRef) Reconciler {
	r.workloadRoleRefs = roleRefs
	return r
}

func (r *reconcilerImpl) WithMCPScheme(scheme *runtime.Scheme) Reconciler {
	r.mcpScheme = scheme
	return r
}

func (r *reconcilerImpl) WithWorkloadScheme(scheme *runtime.Scheme) Reconciler {
	r.workloadScheme = scheme
	return r
}

func (r *reconcilerImpl) MCPCluster(ctx context.Context, request reconcile.Request) (*clusters.Cluster, error) {
	platformNamespace, err := libutils.StableMCPNamespace(request.Name, request.Namespace)
	if err != nil {
		return nil, err
	}
	mcpAccessRequest := &clustersv1alpha1.AccessRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      StableRequestName(r.controllerName, request) + requestSuffixMCP,
			Namespace: platformNamespace,
		},
	}

	if err := r.platformClusterClient.Get(ctx, client.ObjectKeyFromObject(mcpAccessRequest), mcpAccessRequest); err != nil {
		return nil, fmt.Errorf("failed to get MCP AccessRequest: %w", err)
	}

	mcpCluster, err := createClusterForAccessRequest(ctx, r.platformClusterClient, clustersv1alpha1.PURPOSE_MCP, r.mcpScheme, mcpAccessRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP cluster for AccessRequest: %w", err)
	}

	return mcpCluster, nil
}

func (r *reconcilerImpl) WorkloadCluster(ctx context.Context, request reconcile.Request) (*clusters.Cluster, error) {
	platformNamespace, err := libutils.StableMCPNamespace(request.Name, request.Namespace)
	if err != nil {
		return nil, err
	}
	workloadAccessRequest := &clustersv1alpha1.AccessRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      StableRequestName(r.controllerName, request) + requestSuffixWorkload,
			Namespace: platformNamespace,
		},
	}

	if err := r.platformClusterClient.Get(ctx, client.ObjectKeyFromObject(workloadAccessRequest), workloadAccessRequest); err != nil {
		return nil, fmt.Errorf("failed to get Workload AccessRequest: %w", err)
	}

	workloadCluster, err := createClusterForAccessRequest(ctx, r.platformClusterClient, clustersv1alpha1.PURPOSE_WORKLOAD, r.workloadScheme, workloadAccessRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to create Workload cluster for AccessRequest: %w", err)
	}

	return workloadCluster, nil
}

func (r *reconcilerImpl) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(controllerName)

	platformNamespace, err := libutils.StableMCPNamespace(request.Name, request.Namespace)
	if err != nil {
		return reconcile.Result{}, err
	}
	requestNamespace := platformNamespace
	requestNameMCP := StableRequestName(r.controllerName, request) + requestSuffixMCP
	requestNameWorkload := StableRequestName(r.controllerName, request) + requestSuffixWorkload

	metadata := requestMetadata(r.controllerName, request)

	// Check if the request namespace already exists.
	// If it does not exist, wait until it is created.

	log.Debug("Wait for request namespace to exist", "requestNamespace", requestNamespace)

	requestNamespaceExists, err := namespaceExists(ctx, r.platformClusterClient, requestNamespace)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to check if request namespace exists: %w", err)
	}

	if !requestNamespaceExists {
		log.Debug("Request namespace does not exist", "requestNamespace", requestNamespace)
		return reconcile.Result{RequeueAfter: r.retryInterval}, nil
	}

	// Create or update the MCP AccessRequest and wait until the MCP cluster is ready.
	// This also prevents creating the Workload AccessRequest before there is even a MCP created on the onboarding cluster.

	log.Debug("Create and wait for MCP cluster access request", "accessRequestName", requestNameMCP, "accessRequestNamespace", requestNamespace)

	mcpAccessRequest, err := ensureAccessRequest(ctx, r.platformClusterClient,
		requestNameMCP, requestNamespace, &commonapi.ObjectReference{
			Name:      request.Name,
			Namespace: requestNamespace,
		}, nil, r.mcpPermissions, r.mcpRoleRefs, metadata)

	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to create or update MCP AccessRequest: %w", err)
	}

	if mcpAccessRequest.Status.IsDenied() {
		return reconcile.Result{}, fmt.Errorf("MCP AccessRequest denied")
	}

	if !mcpAccessRequest.Status.IsGranted() {
		log.Debug("MCP AccessRequest is not yet granted",
			"accessRequestName", requestNameMCP, "accessRequestNamespace", requestNamespace, "requestPhase", mcpAccessRequest.Status.Phase)
		return reconcile.Result{RequeueAfter: r.retryInterval}, nil
	}

	// Create or update the ClusterRequest for the Workload cluster and wait until it is ready.

	log.Debug("Create and wait for Workload cluster request", "clusterRequestName", requestNameWorkload, "clusterRequestNamespace", requestNamespace)

	workloadRequest, err := ensureClusterRequest(ctx, r.platformClusterClient, requestNameWorkload, requestNamespace, clustersv1alpha1.PURPOSE_WORKLOAD, metadata)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to create or update Workload ClusterRequest: %w", err)
	}

	if workloadRequest.Status.IsDenied() {
		return reconcile.Result{}, fmt.Errorf("workload ClusterRequest denied")
	}

	if !workloadRequest.Status.IsGranted() {
		log.Debug("Workload ClusterRequest is not yet granted",
			"clusterRequestName", requestNameWorkload, "clusterRequestNamespace", requestNamespace, "requestPhase", workloadRequest.Status.Phase)
		return reconcile.Result{RequeueAfter: r.retryInterval}, nil
	}

	// Create or update the AccessRequest for the Workload cluster.

	log.Debug("Create and wait for Workload cluster access request", "accessRequestName", requestNameWorkload, "accessRequestNamespace", requestNamespace)

	workloadAccessRequest, err := ensureAccessRequest(ctx, r.platformClusterClient,
		requestNameWorkload, requestNamespace, &commonapi.ObjectReference{
			Name:      requestNameWorkload,
			Namespace: requestNamespace,
		}, nil, r.workloadPermissions, r.workloadRoleRefs, metadata)

	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to create or update Workload AccessRequest: %w", err)
	}

	if workloadAccessRequest.Status.IsDenied() {
		return reconcile.Result{}, fmt.Errorf("workload AccessRequest denied")
	}

	if !workloadAccessRequest.Status.IsGranted() {
		log.Debug("Workload AccessRequest is not yet granted",
			"accessRequestName", requestNameMCP, "accessRequestNamespace", requestNamespace, "requestPhase", workloadAccessRequest.Status.Phase)
		return reconcile.Result{RequeueAfter: r.retryInterval}, nil
	}

	return reconcile.Result{}, nil
}

func (r *reconcilerImpl) ReconcileDelete(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	platformNamespace, err := libutils.StableMCPNamespace(request.Name, request.Namespace)
	if err != nil {
		return reconcile.Result{}, err
	}
	requestNamespace := platformNamespace
	requestNameMCP := StableRequestName(r.controllerName, request) + requestSuffixMCP
	requestNameWorkload := StableRequestName(r.controllerName, request) + requestSuffixWorkload

	// Delete the Workload AccessRequest if it exists
	workloadAccessDeleted, err := deleteAccessRequest(ctx, r.platformClusterClient, requestNameWorkload, requestNamespace)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to delete Workload AccessRequest: %w", err)
	}

	// Delete the Workload ClusterRequest if it exists
	workloadClusterDeleted, err := deleteClusterRequest(ctx, r.platformClusterClient, requestNameWorkload, requestNamespace)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to delete Workload ClusterRequest: %w", err)
	}

	// Delete the MCP AccessRequest if it exists
	mcpAccessDeleted, err := deleteAccessRequest(ctx, r.platformClusterClient, requestNameMCP, requestNamespace)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to delete MCP AccessRequest: %w", err)
	}

	if !workloadAccessDeleted || !workloadClusterDeleted || !mcpAccessDeleted {
		return reconcile.Result{RequeueAfter: r.retryInterval}, nil
	}

	return reconcile.Result{}, nil
}

// Manager is an interface for managing cluster access.
type Manager interface {
	// WithTimeout sets the timeout for operations.
	WithTimeout(timeout time.Duration) Manager
	// WithInterval sets the interval for polling operations.
	WithInterval(interval time.Duration) Manager
	// WithLogger sets the logger for the manager.
	WithLogger(log *logging.Logger) Manager

	// CreateAndWaitForCluster creates a new ClusterRequest/AccessRequest and waits for it to be ready.
	// It returns the created Cluster if the AccessRequest is granted.
	// ctx is the context for the operation.
	// clusterName is the name of the cluster to create.
	// purpose is the purpose of the cluster (e.g., "onboarding", "mcp", "workload").
	// scheme is the runtime scheme to use for the cluster.
	// permissions are the permissions to request for the AccessRequest.
	CreateAndWaitForCluster(ctx context.Context, clusterName, purpose string,
		scheme *runtime.Scheme, permissions []clustersv1alpha1.PermissionsRequest) (*clusters.Cluster, error)
}

type managerImpl struct {
	platformClusterClient client.Client
	controllerName        string
	controllerNamespace   string // Namespace where the controller is running, used for stable resource names
	timeout               time.Duration
	interval              time.Duration
	log                   *logging.Logger
}

// NewClusterAccessManager creates a new Manager with the given parameters.
func NewClusterAccessManager(platformClusterClient client.Client, controllerName, controllerNamespace string) Manager {
	return &managerImpl{
		platformClusterClient: platformClusterClient,
		controllerName:        controllerName,
		controllerNamespace:   controllerNamespace,
		timeout:               5 * time.Minute,
		interval:              10 * time.Second,
		log:                   nil, // Default to no logging
	}
}

func (m *managerImpl) WithTimeout(timeout time.Duration) Manager {
	m.timeout = timeout
	return m
}

func (m *managerImpl) WithInterval(interval time.Duration) Manager {
	m.interval = interval
	return m
}

func (m *managerImpl) WithLogger(log *logging.Logger) Manager {
	m.log = log
	return m
}

// CreateAndWaitForCluster creates a new ClusterRequest and AccessRequest, waits for it to be ready, and returns the created Cluster.
// The name of the ClusterRequest and AccessRequest is derived from the controller name and the given localName.
// For example, if a provider needs access to the onboarding cluster, it could use the localName "onboarding" (or "onboarding-init").
// The resulting ClusterRequest and AccessRequest would then be named "<controllerName>--onboarding" (or "<controllerName>--onboarding-init").
func (m *managerImpl) CreateAndWaitForCluster(ctx context.Context, localName, purpose string,
	scheme *runtime.Scheme, permissions []clustersv1alpha1.PermissionsRequest) (*clusters.Cluster, error) {

	cr := &clustersv1alpha1.ClusterRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      StableRequestNameFromLocalName(m.controllerName, localName),
			Namespace: m.controllerNamespace,
		},
	}

	clusterRequestMutator := newClusterRequestMutator(cr.Name, cr.Namespace, purpose)
	clusterRequestMutator.WithMetadata(resources.NewMetadataMutator().WithLabels(map[string]string{
		constv1alpha1.ManagedByLabel: m.controllerName,
	}))

	if err := resources.CreateOrUpdateResource(ctx, m.platformClusterClient, clusterRequestMutator); err != nil {
		return nil, fmt.Errorf("failed to create/update ClusterRequest: %w", err)
	}

	err := m.wait(ctx, func(ctx context.Context) (bool, error) {
		if err := m.platformClusterClient.Get(ctx, client.ObjectKeyFromObject(cr), cr); err != nil {
			return false, fmt.Errorf("failed to get ClusterRequest: %w", err)
		}

		if m.log != nil {
			m.log.Info("Waiting for ClusterRequest", "name", cr.Name, "phase", cr.Status.Phase)
		}

		return cr.Status.IsGranted() || cr.Status.IsDenied(), nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to wait for ClusterRequest: %w", err)
	}

	ar := &clustersv1alpha1.AccessRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: m.controllerNamespace,
		},
	}

	accessRequestMutator := newAccessRequestMutator(ar.Name, ar.Namespace)
	accessRequestMutator.WithRequestRef(&commonapi.ObjectReference{
		Name:      cr.Name,
		Namespace: cr.Namespace,
	})
	accessRequestMutator.WithTokenPermissions(permissions)
	accessRequestMutator.WithMetadata(resources.NewMetadataMutator().WithLabels(map[string]string{
		constv1alpha1.ManagedByLabel: m.controllerName,
	}))

	if err := resources.CreateOrUpdateResource(ctx, m.platformClusterClient, accessRequestMutator); err != nil {
		return nil, fmt.Errorf("failed to create/update AccessRequest: %w", err)
	}

	err = m.wait(ctx, func(ctx context.Context) (bool, error) {
		if err := m.platformClusterClient.Get(ctx, client.ObjectKeyFromObject(ar), ar); err != nil {
			return false, fmt.Errorf("failed to get AccessRequest: %w", err)
		}

		if m.log != nil {
			m.log.Info("Waiting for AccessRequest", "name", ar.Name, "phase", ar.Status.Phase)
		}

		return ar.Status.IsGranted() || ar.Status.IsDenied(), nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to wait for AccessRequest: %w", err)
	}

	return createClusterForAccessRequest(ctx, m.platformClusterClient, clustersv1alpha1.PURPOSE_ONBOARDING, scheme, ar)
}

func (m *managerImpl) wait(ctx context.Context, test func(ctx context.Context) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	return wait.PollUntilContextTimeout(ctx, m.interval, m.timeout, true, test)
}

func requestMetadata(controllerName string, request reconcile.Request) resources.MetadataMutator {
	metadata := resources.NewMetadataMutator()
	metadata.WithLabels(map[string]string{
		constv1alpha1.ManagedByLabel:           controllerName,
		constv1alpha1.OnboardingNameLabel:      request.Name,
		constv1alpha1.OnboardingNamespaceLabel: request.Namespace,
	})

	return metadata
}

func namespaceExists(ctx context.Context, platformClusterClient client.Client, namespace string) (bool, error) {
	ns := &corev1.Namespace{}
	if err := platformClusterClient.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return false, fmt.Errorf("failed to check if namespace exists: %w", err)
		}
		return false, nil // Namespace does not exist
	}
	return true, nil // Namespace exists
}

func ensureClusterRequest(ctx context.Context, platformClusterClient client.Client,
	requestName, requestNamespace, purpose string, metadata resources.MetadataMutator) (*clustersv1alpha1.ClusterRequest, error) {

	mutator := newClusterRequestMutator(requestName, requestNamespace, purpose)
	mutator.WithMetadata(metadata)

	if err := resources.CreateOrUpdateResource[*clustersv1alpha1.ClusterRequest](ctx, platformClusterClient, mutator); err != nil {
		return nil, fmt.Errorf("failed to create or update ClusterRequest: %w", err)
	}

	cr := mutator.Empty()
	if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(cr), cr); err != nil {
		return nil, fmt.Errorf("failed to get ClusterRequest: %w", err)
	}

	return cr, nil
}

func ensureAccessRequest(ctx context.Context, platformClusterClient client.Client, requestName, requestNamespace string,
	requestRef *commonapi.ObjectReference, clusterRef *commonapi.ObjectReference,
	permissions []clustersv1alpha1.PermissionsRequest, roleRefs []commonapi.RoleRef, metadata resources.MetadataMutator) (*clustersv1alpha1.AccessRequest, error) {

	mutator := newAccessRequestMutator(requestName, requestNamespace).
		WithTokenPermissions(permissions).
		WithTokenRoleRefs(roleRefs).
		WithMetadata(metadata)

	if requestRef != nil {
		mutator = mutator.WithRequestRef(requestRef)
	}

	if clusterRef != nil {
		mutator = mutator.WithClusterRef(clusterRef)
	}

	if err := resources.CreateOrUpdateResource[*clustersv1alpha1.AccessRequest](ctx, platformClusterClient, mutator); err != nil {
		return nil, fmt.Errorf("failed to create or update AccessRequest: %w", err)
	}

	ar := mutator.Empty()
	if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(ar), ar); err != nil {
		return nil, fmt.Errorf("failed to get AccessRequest: %w", err)
	}

	return ar, nil
}

func deleteAccessRequest(ctx context.Context, platformClusterClient client.Client, requestName, requestNamespace string) (bool, error) {
	ar := &clustersv1alpha1.AccessRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      requestName,
			Namespace: requestNamespace,
		},
	}

	if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(ar), ar); err != nil {
		if errors.IsNotFound(err) {
			return true, nil // AccessRequest already deleted
		} else {
			return false, fmt.Errorf("failed to get AccessRequest: %w", err)
		}
	}

	if err := platformClusterClient.Delete(ctx, ar); err != nil {
		if errors.IsNotFound(err) {
			return true, nil // AccessRequest already deleted
		} else {
			return false, fmt.Errorf("failed to delete AccessRequest: %w", err)
		}
	}

	return true, nil // AccessRequest deleted successfully
}

func deleteClusterRequest(ctx context.Context, platformClusterClient client.Client, requestName, requestNamespace string) (bool, error) {
	cr := &clustersv1alpha1.ClusterRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      requestName,
			Namespace: requestNamespace,
		},
	}

	if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(cr), cr); err != nil {
		if errors.IsNotFound(err) {
			return true, nil // ClusterRequest already deleted
		} else {
			return false, fmt.Errorf("failed to get ClusterRequest: %w", err)
		}
	}

	if err := platformClusterClient.Delete(ctx, cr); err != nil {
		if errors.IsNotFound(err) {
			return true, nil // ClusterRequest already deleted
		} else {
			return false, fmt.Errorf("failed to delete ClusterRequest: %w", err)
		}
	}

	return true, nil // ClusterRequest deleted successfully
}

func createClusterForAccessRequest(ctx context.Context, platformClusterClient client.Client,
	purpose string, scheme *runtime.Scheme, accessRequest *clustersv1alpha1.AccessRequest) (*clusters.Cluster, error) {

	if accessRequest.Status.SecretRef == nil {
		return nil, fmt.Errorf("access request %q has no accessSecret reference", accessRequest.Name)
	}

	accessSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      accessRequest.Status.SecretRef.Name,
			Namespace: accessRequest.Status.SecretRef.Namespace,
		},
	}

	if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(accessSecret), accessSecret); err != nil {
		return nil, fmt.Errorf("failed to get access accessSecret: %w", err)
	}

	kubeconfigBytes, ok := accessSecret.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("kubeconfig not found in accessSecret %s", accessSecret.Name)
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create rest config from kubeconfig bytes: %w", err)
	}

	c := clusters.New(purpose).WithRESTConfig(config)

	if err = c.InitializeClient(scheme); err != nil {
		return nil, fmt.Errorf("failed to initialize client for purpose %q: %w", purpose, err)
	}

	return c, nil
}

type clusterRequestMutator struct {
	name      string
	namespace string
	purpose   string
	metadata  resources.MetadataMutator
}

func newClusterRequestMutator(name, namespace, purpose string) *clusterRequestMutator {
	return &clusterRequestMutator{
		name:      name,
		namespace: namespace,
		purpose:   purpose,
	}
}

func (m *clusterRequestMutator) WithMetadata(metadata resources.MetadataMutator) *clusterRequestMutator {
	m.metadata = metadata
	return m
}

func (m *clusterRequestMutator) String() string {
	return fmt.Sprint("ClusterRequest",
		", name ", m.name,
		", namespace", m.namespace,
		", purpose", m.purpose)
}

func (m *clusterRequestMutator) Empty() *clustersv1alpha1.ClusterRequest {
	return &clustersv1alpha1.ClusterRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.name,
			Namespace: m.namespace,
		},
	}
}

func (m *clusterRequestMutator) MetadataMutator() resources.MetadataMutator {
	return m.metadata
}

func (m *clusterRequestMutator) Mutate(clusterRequest *clustersv1alpha1.ClusterRequest) error {
	clusterRequest.Spec.Purpose = m.purpose

	if m.metadata != nil {
		return m.metadata.Mutate(clusterRequest)
	} else {
		return nil
	}
}

type accessRequestMutator struct {
	name                string
	namespace           string
	requestRef          *commonapi.ObjectReference
	clusterRef          *commonapi.ObjectReference
	tokenPermissions    []clustersv1alpha1.PermissionsRequest
	tokenRoleRefs       []commonapi.RoleRef
	oidcProvider        *commonapi.OIDCProviderConfig
	oidcAdditionalRoles []clustersv1alpha1.PermissionsRequest
	metadata            resources.MetadataMutator
}

func newAccessRequestMutator(name, namespace string) *accessRequestMutator {
	return &accessRequestMutator{
		name:      name,
		namespace: namespace,
	}
}

func (m *accessRequestMutator) WithRequestRef(requestRef *commonapi.ObjectReference) *accessRequestMutator {
	m.requestRef = requestRef
	return m
}

func (m *accessRequestMutator) WithClusterRef(clusterRef *commonapi.ObjectReference) *accessRequestMutator {
	m.clusterRef = clusterRef
	return m
}

func (m *accessRequestMutator) WithTokenPermissions(permissions []clustersv1alpha1.PermissionsRequest) *accessRequestMutator {
	m.tokenPermissions = permissions
	return m
}

func (m *accessRequestMutator) WithTokenRoleRefs(roleRefs []commonapi.RoleRef) *accessRequestMutator {
	m.tokenRoleRefs = roleRefs
	return m
}

func (m *accessRequestMutator) WithOIDCProvider(oidcProvider *commonapi.OIDCProviderConfig) *accessRequestMutator {
	m.oidcProvider = oidcProvider
	return m
}

func (m *accessRequestMutator) WithOIDCAdditionalRoles(permissions []clustersv1alpha1.PermissionsRequest) *accessRequestMutator {
	m.oidcAdditionalRoles = permissions
	return m
}

func (m *accessRequestMutator) WithMetadata(metadata resources.MetadataMutator) *accessRequestMutator {
	m.metadata = metadata
	return m
}

func (m *accessRequestMutator) String() string {
	requestRef := "<nil>"
	clusterRef := "<nil>"

	if m.requestRef != nil {
		requestRef = fmt.Sprintf("%s/%s", m.requestRef.Name, m.requestRef.Namespace)
	}

	if m.clusterRef != nil {
		clusterRef = fmt.Sprintf("%s/%s", m.clusterRef.Name, m.clusterRef.Namespace)
	}

	return fmt.Sprint("AccessRequest",
		", name ", m.name,
		", namespace ", m.namespace,
		", requestRef ", requestRef,
		", clusterRef ", clusterRef)
}

func (m *accessRequestMutator) Empty() *clustersv1alpha1.AccessRequest {
	return &clustersv1alpha1.AccessRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.name,
			Namespace: m.namespace,
		},
	}
}

func (m *accessRequestMutator) MetadataMutator() resources.MetadataMutator {
	return m.metadata
}

func (m *accessRequestMutator) Mutate(accessRequest *clustersv1alpha1.AccessRequest) error {
	if m.tokenPermissions != nil {
		if accessRequest.Spec.Token == nil {
			accessRequest.Spec.Token = &clustersv1alpha1.TokenConfig{}
		}
		accessRequest.Spec.Token.Permissions = make([]clustersv1alpha1.PermissionsRequest, len(m.tokenPermissions))
		copy(accessRequest.Spec.Token.Permissions, m.tokenPermissions)
	}
	if m.tokenRoleRefs != nil {
		if accessRequest.Spec.Token == nil {
			accessRequest.Spec.Token = &clustersv1alpha1.TokenConfig{}
		}
		accessRequest.Spec.Token.RoleRefs = make([]commonapi.RoleRef, len(m.tokenRoleRefs))
		copy(accessRequest.Spec.Token.RoleRefs, m.tokenRoleRefs)
	}

	if m.oidcProvider != nil {
		if accessRequest.Spec.OIDC == nil {
			accessRequest.Spec.OIDC = &clustersv1alpha1.OIDCConfig{}
		}
		accessRequest.Spec.OIDC.OIDCProviderConfig = *m.oidcProvider.DeepCopy()
	}
	if m.oidcAdditionalRoles != nil {
		if accessRequest.Spec.OIDC == nil {
			accessRequest.Spec.OIDC = &clustersv1alpha1.OIDCConfig{}
		}
		accessRequest.Spec.OIDC.AdditionalRoles = make([]clustersv1alpha1.PermissionsRequest, len(m.oidcAdditionalRoles))
		copy(accessRequest.Spec.OIDC.AdditionalRoles, m.oidcAdditionalRoles)
	}

	if m.requestRef != nil {
		accessRequest.Spec.RequestRef = m.requestRef.DeepCopy()
	}

	if m.clusterRef != nil {
		accessRequest.Spec.ClusterRef = m.clusterRef.DeepCopy()
	}

	if m.metadata != nil {
		return m.metadata.Mutate(accessRequest)
	} else {
		return nil
	}
}

// StableRequestName generates a stable name for a Cluster- or AccessRequest related to an MCP.
// This basically results in '<lowercase_controller_name>--<request_name>'.
func StableRequestName(controllerName string, request reconcile.Request) string {
	return StableRequestNameFromLocalName(controllerName, request.Name)
}

func StableRequestNameFromLocalName(controllerName, localName string) string {
	controllerName = strings.ToLower(controllerName)
	return fmt.Sprintf("%s--%s", controllerName, localName)
}
