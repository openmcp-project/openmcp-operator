//nolint:revive
package advanced

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	maputils "github.com/openmcp-project/controller-utils/pkg/collections/maps"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"
)

const caControllerName = "ClusterAccess"

//////////////////
/// INTERFACES ///
//////////////////

// ClusterAccessReconciler is an interface for reconciling access k8s clusters based on the openMCP 'Cluster' API.
// It can create ClusterRequests and/or AccessRequests for an amount of clusters.
type ClusterAccessReconciler interface {
	// Register registers a cluster to be managed by the reconciler.
	// No-op if reg is nil.
	// Overwrites any previous registration with the same ID.
	Register(reg ClusterRegistration) ClusterAccessReconciler
	// Unregister unregisters a cluster from being managed by the reconciler.
	// No-op if no registration with the given ID exists.
	Unregister(id string) ClusterAccessReconciler
	// WithRetryInterval sets the retry interval.
	WithRetryInterval(interval time.Duration) ClusterAccessReconciler
	// WithManagedLabels allows to overwrite the managed-by and managed-purpose labels that are set on the created resources.
	// Note that the implementation might depend on these labels to identify the resources it created,
	// so changing them might lead to unexpected behavior. They also must be unique within the context of this reconciler.
	// Use with caution.
	WithManagedLabels(gen ManagedLabelGenerator) ClusterAccessReconciler

	// Access returns an internal Cluster object granting access to the cluster for the specified request with the specified id.
	// Will fail if the cluster is not registered or no AccessRequest is registered for the cluster, or if some other error occurs.
	Access(ctx context.Context, request reconcile.Request, id string, additionalData ...any) (*clusters.Cluster, error)
	// AccessRequest fetches the AccessRequest object for the cluster for the specified request with the specified id.
	// Will fail if the cluster is not registered or no AccessRequest is registered for the cluster, or if some other error occurs.
	// The same additionalData must be passed into all methods of this ClusterAccessReconciler for the same request and id.
	AccessRequest(ctx context.Context, request reconcile.Request, id string, additionalData ...any) (*clustersv1alpha1.AccessRequest, error)
	// ClusterRequest fetches the ClusterRequest object for the cluster for the specified request with the specified id.
	// Will fail if the cluster is not registered or no ClusterRequest is registered for the cluster, or if some other error occurs.
	// The same additionalData must be passed into all methods of this ClusterAccessReconciler for the same request and id.
	ClusterRequest(ctx context.Context, request reconcile.Request, id string, additionalData ...any) (*clustersv1alpha1.ClusterRequest, error)
	// Cluster fetches the external Cluster object for the cluster for the specified request with the specified id.
	// Will fail if the cluster is not registered or no Cluster can be determined, or if some other error occurs.
	// The same additionalData must be passed into all methods of this ClusterAccessReconciler for the same request and id.
	Cluster(ctx context.Context, request reconcile.Request, id string, additionalData ...any) (*clustersv1alpha1.Cluster, error)

	// Reconcile creates the ClusterRequests and/or AccessRequests for the registered clusters.
	// This function should be called during all reconciliations of the reconciled object.
	// ctx is the context for the reconciliation.
	// request is the object that is being reconciled
	// It returns a reconcile.Result and an error if the reconciliation failed.
	// The reconcile.Result may contain a RequeueAfter value to indicate that the reconciliation should be retried after a certain duration.
	// The duration is set by the WithRetryInterval method.
	// Any additional arguments provided are passed into all methods of the ClusterRegistration objects that are called.
	Reconcile(ctx context.Context, request reconcile.Request, additionalData ...any) (reconcile.Result, error)
	// ReconcileDelete deletes the ClusterRequests and/or AccessRequests for the registered clusters.
	// This function should be called during the deletion of the reconciled object.
	// ctx is the context for the reconciliation.
	// request is the object that is being reconciled
	// It returns a reconcile.Result and an error if the reconciliation failed.
	// The reconcile.Result may contain a RequeueAfter value to indicate that the reconciliation should be retried after a certain duration.
	// The duration is set by the WithRetryInterval method.
	// Any additional arguments provided are passed into all methods of the ClusterRegistration objects that are called.
	ReconcileDelete(ctx context.Context, request reconcile.Request, additionalData ...any) (reconcile.Result, error)

	// WithFakingCallback passes a callback function with a specific key.
	// The available keys depend on the implementation.
	// The key determines when the callback function is executed.
	// This feature is meant for unit testing, where usually no ClusterProvider, which could answer ClusterRequests and AccessRequests, is running.
	WithFakingCallback(key string, callback FakingCallback) ClusterAccessReconciler
}

// FakingCallback is a function that allows to mock a desired state for unit testing.
//
// The idea behind this is: Before running any reconciliation logic in a unit tests, make sure to pass these callback functions to the cluster access reconciler implementation.
// During the reocncile, the cluster access reconciler will call these functions at specific points, e.g. when it is waiting for an AccessRequest to be approved.
// The time of the execution depends on the key and the implementation of the cluster access reconciler.
// In the callback function, modify the cluster state as desired by mocking actions usually performed by external operators, e.g. set the AccessRequest to approved.
//
// Note that most of the arguments can be nil, depending on when the callback is executed.
// The arguments are:
// - ctx is the context, which is required for interacting with the cluster.
// - platformClusterClient is the client for the platform cluster.
// - key is the key that determined the execution of the callback.
// - req is the reconcile.Request that triggered the reconciliation, if known.
// - cr is the ClusterRequest related to the cluster access reconciliation, if known.
// - ar is the AccessRequest related to the cluster access reconciliation, if known.
// - c is the Cluster related to the cluster access reconciliation, if known.
// - access is the access to the cluster, if already retrieved.
type FakingCallback func(ctx context.Context, platformClusterClient client.Client, key string, req *reconcile.Request, cr *clustersv1alpha1.ClusterRequest, ar *clustersv1alpha1.AccessRequest, c *clustersv1alpha1.Cluster, access *clusters.Cluster) error

// ManagedLabelGenerator is a function that generates the managed-by and managed-purpose labels for the created resources.
// The first return value is the value for the managed-by label, the second one is the value for the managed-purpose label.
// The third return value can be nil, or it can contain additional labels to be set on the created resources.
type ManagedLabelGenerator func(controllerName string, req reconcile.Request, reg ClusterRegistration) (string, string, map[string]string)

func DefaultManagedLabelGenerator(controllerName string, req reconcile.Request, reg ClusterRegistration) (string, string, map[string]string) {
	return controllerName, fmt.Sprintf("%s.%s.%s", req.Namespace, req.Name, reg.ID()), nil
}

type ClusterRegistration interface {
	// ID is the unique identifier for the cluster.
	ID() string
	// Suffix is the suffix to be used for the names of the created resources.
	// It must be unique within the context of the reconciler.
	// If empty, the ID will be used as suffix.
	Suffix() string
	// Scheme is the scheme for the Kubernetes client of the cluster.
	// If nil, the default scheme will be used.
	Scheme() *runtime.Scheme
	// AccessRequestAvailable returns true if an AccessRequest can be retrieved from this registration.
	AccessRequestAvailable() bool
	// ClusterRequestAvailable returns true if a ClusterRequest can be retrieved from this registration.
	ClusterRequestAvailable() bool
	// Parameterize turns this ClusterRegistration into a ParamterizedClusterRegistration.
	Parameterize(req reconcile.Request, additionalData ...any) ParameterizedClusterRegistration
}

type ParameterizedClusterRegistration interface {
	ClusterRegistration

	// AccessRequestTokenConfig is the token configuration for the AccessRequest to be created for the cluster.
	// Might be nil if no AccessRequest should be created or if OIDC access is used.
	// Only one of AccessRequestTokenConfig and AccessRequestOIDCConfig should be non-nil.
	AccessRequestTokenConfig() (*clustersv1alpha1.TokenConfig, error)
	// AccessRequestOIDCConfig is the OIDC configuration for the AccessRequest to be created for the cluster.
	// Might be nil if no AccessRequest should be created or if token-based access is used.
	// Only one of AccessRequestTokenConfig and AccessRequestOIDCConfig should be non-nil.
	AccessRequestOIDCConfig() (*clustersv1alpha1.OIDCConfig, error)
	// ClusterRequestSpec is the spec for the ClusterRequest to be created for the cluster.
	// It is nil if no ClusterRequest should be created.
	// Only one of ClusterRequestSpec, ClusterReference and ClusterRequestReference should be non-nil.
	ClusterRequestSpec() (*clustersv1alpha1.ClusterRequestSpec, error)
	// ClusterReference returns name and namespace of an existing Cluster resource.
	// It is nil if a new ClusterRequest should be created or if the Cluster is referenced by an existing ClusterRequest.
	// Only one of ClusterRequestSpec, ClusterReference and ClusterRequestReference should be non-nil.
	ClusterReference() (*commonapi.ObjectReference, error)
	// ClusterRequestReference returns name and namespace of the ClusterRequest resource.
	// It is nil if a new ClusterRequest should be created or if the Cluster is referenced directly.
	// Only one of ClusterRequestSpec, ClusterReference and ClusterRequestReference should be non-nil.
	ClusterRequestReference() (*commonapi.ObjectReference, error)
	// Namespace generates the namespace on the Platform cluster to use for the created requests.
	// The generated namespace is expected be unique within the context of the reconciler.
	Namespace() (string, error)
}

type ClusterRegistrationBuilder interface {
	// WithTokenAccess enables an AccessRequest for token-based access to be created for the cluster.
	// Use this method, if the token configuration does not depend on the reconcile request, use WithTokenAccessGenerator otherwise.
	// Calling this method will override any previous calls to WithTokenAccess, WithTokenAccessGenerator, WithOIDCAccess, or WithOIDCAccessGenerator.
	// Passing in a nil cfg will disable AccessRequest creation, if it was token-based before, and have no effect if it was OIDC-based before.
	WithTokenAccess(cfg *clustersv1alpha1.TokenConfig) ClusterRegistrationBuilder
	// WithTokenAccessGenerator is like WithTokenAccess, but takes a function that generates the TokenConfig based on the reconcile request.
	// Calling this method will override any previous calls to WithTokenAccess, WithTokenAccessGenerator, WithOIDCAccess, or WithOIDCAccessGenerator.
	// Passing in a nil function will disable AccessRequest creation, if it was token-based before, and have no effect if it was OIDC-based before.
	WithTokenAccessGenerator(f func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.TokenConfig, error)) ClusterRegistrationBuilder
	// WithOIDCAccess enables an AccessRequest for OIDC-based access to be created for the cluster.
	// Use this method, if the OIDC configuration does not depend on the reconcile request, use WithOIDCAccessGenerator otherwise.
	// Calling this method will override any previous calls to WithTokenAccess, WithTokenAccessGenerator, WithOIDCAccess, or WithOIDCAccessGenerator.
	// Passing in a nil cfg will disable AccessRequest creation, if it was OIDC-based before, and have no effect if it was token-based before.
	WithOIDCAccess(cfg *clustersv1alpha1.OIDCConfig) ClusterRegistrationBuilder
	// WithOIDCAccessGenerator is like WithOIDCAccess, but takes a function that generates the OIDCConfig based on the reconcile request.
	// Calling this method will override any previous calls to WithTokenAccess, WithTokenAccessGenerator, WithOIDCAccess, or WithOIDCAccessGenerator.
	// Passing in a nil function will disable AccessRequest creation, if it was OIDC-based before, and have no effect if it was token-based before.
	WithOIDCAccessGenerator(f func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.OIDCConfig, error)) ClusterRegistrationBuilder
	// WithScheme sets the scheme for the Kubernetes client of the cluster.
	// If not set or set to nil, the default scheme will be used.
	WithScheme(scheme *runtime.Scheme) ClusterRegistrationBuilder
	// WithNamespaceGenerator sets the function that generates the namespace on the Platform cluster to use for the created requests.
	WithNamespaceGenerator(f func(req reconcile.Request, additionalData ...any) (string, error)) ClusterRegistrationBuilder
	// Build builds the ClusterRegistration object.
	Build() ClusterRegistration
}

///////////////////////////////////////////
/// IMPLEMENTATION: ClusterRegistration ///
///////////////////////////////////////////

type clusterRegistrationImpl struct {
	id                         string
	suffix                     string
	scheme                     *runtime.Scheme
	generateAccessTokenConfig  func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.TokenConfig, error)
	generateAccessOIDCConfig   func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.OIDCConfig, error)
	generateClusterRequestSpec func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.ClusterRequestSpec, error)
	generateClusterRef         func(req reconcile.Request, additionalData ...any) (*commonapi.ObjectReference, error)
	generateClusterRequestRef  func(req reconcile.Request, additionalData ...any) (*commonapi.ObjectReference, error)
	generateNamespace          func(req reconcile.Request, additionalData ...any) (string, error)
}

type parameterizedClusterRegistrationImpl struct {
	clusterRegistrationImpl
	req            reconcile.Request
	additionalData []any
}

var _ ClusterRegistration = &clusterRegistrationImpl{}
var _ ParameterizedClusterRegistration = &parameterizedClusterRegistrationImpl{}

// ID implements ClusterRegistration.
func (c *clusterRegistrationImpl) ID() string {
	return c.id
}

// Suffix implements ClusterRegistration.
func (c *clusterRegistrationImpl) Suffix() string {
	if c.suffix != "" {
		return c.suffix
	}
	return c.id
}

// Scheme implements ClusterRegistration.
func (c *clusterRegistrationImpl) Scheme() *runtime.Scheme {
	return c.scheme
}

// AccessRequestAvailable implements ClusterRegistration.
func (c *clusterRegistrationImpl) AccessRequestAvailable() bool {
	return c.generateAccessOIDCConfig != nil || c.generateAccessTokenConfig != nil
}

// ClusterRequestAvailable implements ClusterRegistration.
func (c *clusterRegistrationImpl) ClusterRequestAvailable() bool {
	return c.generateClusterRequestSpec != nil || c.generateClusterRequestRef != nil
}

func (c *clusterRegistrationImpl) Parameterize(req reconcile.Request, additionalData ...any) ParameterizedClusterRegistration {
	return &parameterizedClusterRegistrationImpl{
		clusterRegistrationImpl: *c,
		req:                     req,
		additionalData:          additionalData,
	}
}

// AccessRequestTokenConfig implements ParameterizedClusterRegistration.
func (c *parameterizedClusterRegistrationImpl) AccessRequestTokenConfig() (*clustersv1alpha1.TokenConfig, error) {
	if c.generateAccessTokenConfig == nil {
		return nil, nil
	}
	return c.generateAccessTokenConfig(c.req, c.additionalData...)
}

// AccessRequestOIDCConfig implements ParameterizedClusterRegistration.
func (c *parameterizedClusterRegistrationImpl) AccessRequestOIDCConfig() (*clustersv1alpha1.OIDCConfig, error) {
	if c.generateAccessOIDCConfig == nil {
		return nil, nil
	}
	return c.generateAccessOIDCConfig(c.req, c.additionalData...)
}

// ClusterRequestSpec implements ParameterizedClusterRegistration.
func (c *parameterizedClusterRegistrationImpl) ClusterRequestSpec() (*clustersv1alpha1.ClusterRequestSpec, error) {
	if c.generateClusterRequestSpec == nil {
		return nil, nil
	}
	return c.generateClusterRequestSpec(c.req, c.additionalData...)
}

// ClusterReference implements ParameterizedClusterRegistration.
func (c *parameterizedClusterRegistrationImpl) ClusterReference() (*commonapi.ObjectReference, error) {
	if c.generateClusterRef == nil {
		return nil, nil
	}
	return c.generateClusterRef(c.req, c.additionalData...)
}

// ClusterRequestReference implements ParameterizedClusterRegistration.
func (c *parameterizedClusterRegistrationImpl) ClusterRequestReference() (*commonapi.ObjectReference, error) {
	if c.generateClusterRequestRef == nil {
		return nil, nil
	}
	return c.generateClusterRequestRef(c.req, c.additionalData...)
}

// Namespace implements ParameterizedClusterRegistration.
func (c *parameterizedClusterRegistrationImpl) Namespace() (string, error) {
	if c.generateNamespace == nil {
		return DefaultNamespaceGenerator(c.req, c.additionalData...)
	}
	return c.generateNamespace(c.req, c.additionalData...)
}

//////////////////////////////////////////////////
/// IMPLEMENTATION: ClusterRegistrationBuilder ///
//////////////////////////////////////////////////

type clusterRegistrationBuilderImpl struct {
	clusterRegistrationImpl
}

var _ ClusterRegistrationBuilder = &clusterRegistrationBuilderImpl{}

// Build implements ClusterRegistrationBuilder.
func (c *clusterRegistrationBuilderImpl) Build() ClusterRegistration {
	return &c.clusterRegistrationImpl
}

// WithOIDCAccess implements ClusterRegistrationBuilder.
func (c *clusterRegistrationBuilderImpl) WithOIDCAccess(cfg *clustersv1alpha1.OIDCConfig) ClusterRegistrationBuilder {
	var f func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.OIDCConfig, error)
	if cfg != nil {
		f = func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.OIDCConfig, error) {
			return cfg.DeepCopy(), nil
		}
	}
	return c.WithOIDCAccessGenerator(f)
}

// WithOIDCAccessGenerator implements ClusterRegistrationBuilder.
func (c *clusterRegistrationBuilderImpl) WithOIDCAccessGenerator(f func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.OIDCConfig, error)) ClusterRegistrationBuilder {
	c.generateAccessOIDCConfig = f
	if f != nil && c.generateAccessTokenConfig != nil {
		// both access methods configured, disable token
		c.generateAccessTokenConfig = nil
	}
	return c
}

// WithTokenAccess implements ClusterRegistrationBuilder.
func (c *clusterRegistrationBuilderImpl) WithTokenAccess(cfg *clustersv1alpha1.TokenConfig) ClusterRegistrationBuilder {
	var f func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.TokenConfig, error)
	if cfg != nil {
		f = func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.TokenConfig, error) {
			return cfg.DeepCopy(), nil
		}
	}
	return c.WithTokenAccessGenerator(f)
}

// WithTokenAccessGenerator implements ClusterRegistrationBuilder.
func (c *clusterRegistrationBuilderImpl) WithTokenAccessGenerator(f func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.TokenConfig, error)) ClusterRegistrationBuilder {
	c.generateAccessTokenConfig = f
	if f != nil && c.generateAccessOIDCConfig != nil {
		// both access methods configured, disable OIDC
		c.generateAccessOIDCConfig = nil
	}
	return c
}

// WithScheme implements ClusterRegistrationBuilder.
func (c *clusterRegistrationBuilderImpl) WithScheme(scheme *runtime.Scheme) ClusterRegistrationBuilder {
	c.scheme = scheme
	return c
}

// WithNamespaceGenerator implements ClusterRegistrationBuilder.
func (c *clusterRegistrationBuilderImpl) WithNamespaceGenerator(f func(req reconcile.Request, additionalData ...any) (string, error)) ClusterRegistrationBuilder {
	if f == nil {
		f = DefaultNamespaceGenerator
	}
	c.generateNamespace = f
	return c
}

// NewClusterRequest instructs the Reconciler to create and manage a new ClusterRequest.
func NewClusterRequest(id, suffix string, generateClusterRequestSpec func(reconcile.Request, ...any) (*clustersv1alpha1.ClusterRequestSpec, error)) ClusterRegistrationBuilder {
	return &clusterRegistrationBuilderImpl{
		clusterRegistrationImpl: clusterRegistrationImpl{
			id:                         id,
			suffix:                     suffix,
			generateClusterRequestSpec: generateClusterRequestSpec,
			generateNamespace:          DefaultNamespaceGenerator,
		},
	}
}

// ExistingCluster instructs the Reconciler to use an existing Cluster resource.
func ExistingCluster(id, suffix string, generateClusterRef func(reconcile.Request, ...any) (*commonapi.ObjectReference, error)) ClusterRegistrationBuilder {
	return &clusterRegistrationBuilderImpl{
		clusterRegistrationImpl: clusterRegistrationImpl{
			id:                 id,
			suffix:             suffix,
			generateClusterRef: generateClusterRef,
			generateNamespace:  DefaultNamespaceGenerator,
		},
	}
}

// ExistingClusterRequest instructs the Reconciler to use an existing Cluster resource that is referenced by the given ClusterRequest.
func ExistingClusterRequest(id, suffix string, generateClusterRequestRef func(reconcile.Request, ...any) (*commonapi.ObjectReference, error)) ClusterRegistrationBuilder {
	return &clusterRegistrationBuilderImpl{
		clusterRegistrationImpl: clusterRegistrationImpl{
			id:                        id,
			suffix:                    suffix,
			generateClusterRequestRef: generateClusterRequestRef,
			generateNamespace:         DefaultNamespaceGenerator,
		},
	}
}

//////////////////////////////////
/// IMPLEMENTATION: Reconciler ///
//////////////////////////////////

type reconcilerImpl struct {
	controllerName        string
	platformClusterClient client.Client

	interval      time.Duration
	registrations map[string]ClusterRegistration
	managedBy     ManagedLabelGenerator

	fakingCallbacks map[string]FakingCallback
}

// NewClusterAccessReconciler creates a new Cluster Access Reconciler.
// Note that it needs to be configured further by calling its Register method and optionally its builder-like With* methods.
// This is meant to be instantiated and configured once during controller setup and then its Reconcile or ReconcileDelete methods should be called during each reconciliation of the controller.
func NewClusterAccessReconciler(platformClusterClient client.Client, controllerName string) ClusterAccessReconciler {
	return &reconcilerImpl{
		controllerName:        controllerName,
		platformClusterClient: platformClusterClient,
		interval:              5 * time.Second,
		registrations:         map[string]ClusterRegistration{},
		managedBy:             DefaultManagedLabelGenerator,
	}
}

var _ ClusterAccessReconciler = &reconcilerImpl{}

// Access implements Reconciler.
func (r *reconcilerImpl) Access(ctx context.Context, request reconcile.Request, id string, additionalData ...any) (*clusters.Cluster, error) {
	reg, ok := r.registrations[id]
	if !ok {
		return nil, fmt.Errorf("no registration found for request '%s' with id '%s'", request.String(), id)
	}
	ar, err := r.AccessRequest(ctx, request, id, additionalData...)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch AccessRequest for request '%s' with id '%s': %w", request.String(), id, err)
	}
	if !ar.Status.IsGranted() {
		return nil, fmt.Errorf("AccessRequest '%s/%s' for request '%s' with id '%s' is not granted", ar.Namespace, ar.Name, request.String(), id)
	}
	access, err := AccessFromAccessRequest(ctx, r.platformClusterClient, id, reg.Scheme(), ar)
	if err != nil {
		return nil, fmt.Errorf("unable to get access for request '%s' with id '%s': %w", request.String(), id, err)
	}
	return access, nil
}

// AccessRequest implements Reconciler.
func (r *reconcilerImpl) AccessRequest(ctx context.Context, request reconcile.Request, id string, additionalData ...any) (*clustersv1alpha1.AccessRequest, error) {
	rawReg, ok := r.registrations[id]
	if !ok {
		return nil, fmt.Errorf("no registration found for request '%s' with id '%s'", request.String(), id)
	}
	if !rawReg.AccessRequestAvailable() {
		return nil, fmt.Errorf("no AccessRequest configured for request '%s' with id '%s'", request.String(), id)
	}
	suffix := rawReg.Suffix()
	if suffix == "" {
		suffix = rawReg.ID()
	}
	reg := rawReg.Parameterize(request, additionalData...)
	namespace, err := reg.Namespace()
	if err != nil {
		return nil, fmt.Errorf("unable to generate name of platform cluster namespace for request '%s': %w", request.String(), err)
	}
	ar := &clustersv1alpha1.AccessRequest{}
	ar.Name = StableRequestName(r.controllerName, request, suffix)
	ar.Namespace = namespace
	if err := r.platformClusterClient.Get(ctx, client.ObjectKeyFromObject(ar), ar); err != nil {
		return nil, fmt.Errorf("unable to get AccessRequest '%s/%s' for request '%s' with id '%s': %w", ar.Namespace, ar.Name, request.String(), id, err)
	}
	return ar, nil
}

// Cluster implements Reconciler.
func (r *reconcilerImpl) Cluster(ctx context.Context, request reconcile.Request, id string, additionalData ...any) (*clustersv1alpha1.Cluster, error) {
	rawReg, ok := r.registrations[id]
	if !ok {
		return nil, fmt.Errorf("no registration found for request '%s' with id '%s'", request.String(), id)
	}
	reg := rawReg.Parameterize(request, additionalData...)
	clusterRef, err := reg.ClusterReference()
	if err != nil {
		return nil, fmt.Errorf("unable to generate Cluster reference from registration for request '%s' with id '%s': %w", request.String(), id, err)
	}
	if clusterRef == nil {
		// if no ClusterReference is specified, try to get it from the ClusterRequest
		if reg.ClusterRequestAvailable() {
			cr, err := r.ClusterRequest(ctx, request, id)
			if err != nil {
				return nil, fmt.Errorf("unable to fetch ClusterRequest for request '%s' with id '%s': %w", request.String(), id, err)
			}
			if cr.Status.Cluster == nil {
				return nil, fmt.Errorf("ClusterRequest '%s/%s' for request '%s' with id '%s' has no cluster reference", cr.Namespace, cr.Name, request.String(), id)
			}
			clusterRef = cr.Status.Cluster.DeepCopy()
		} else {
			// try to get the Cluster from the AccessRequest
			ar, err := r.AccessRequest(ctx, request, id)
			if err != nil {
				return nil, fmt.Errorf("unable to fetch AccessRequest for request '%s' with id '%s': %w", request.String(), id, err)
			}
			if ar.Spec.ClusterRef == nil {
				return nil, fmt.Errorf("AccessRequest '%s/%s' for request '%s' with id '%s' has no cluster reference", ar.Namespace, ar.Name, request.String(), id)
			}
			clusterRef = ar.Spec.ClusterRef.DeepCopy()
		}
	}

	c := &clustersv1alpha1.Cluster{}
	if err := r.platformClusterClient.Get(ctx, client.ObjectKey{Name: clusterRef.Name, Namespace: clusterRef.Namespace}, c); err != nil {
		return nil, fmt.Errorf("unable to get Cluster '%s/%s' for request '%s' with id '%s': %w", clusterRef.Namespace, clusterRef.Name, request.String(), id, err)
	}
	return c, nil
}

// ClusterRequest implements Reconciler.
func (r *reconcilerImpl) ClusterRequest(ctx context.Context, request reconcile.Request, id string, additionalData ...any) (*clustersv1alpha1.ClusterRequest, error) {
	rawReg, ok := r.registrations[id]
	if !ok {
		return nil, fmt.Errorf("no registration found for request '%s' with id '%s'", request.String(), id)
	}
	reg := rawReg.Parameterize(request, additionalData...)
	crRef, err := reg.ClusterRequestReference()
	if err != nil {
		return nil, fmt.Errorf("unable to generate ClusterRequest reference from registration for request '%s' with id '%s': %w", request.String(), id, err)
	}
	if crRef == nil {
		// if the ClusterRequest is not referenced directly, check if was created or can be retrieved via an AccessRequest
		crSpec, err := reg.ClusterRequestSpec()
		if err != nil {
			return nil, fmt.Errorf("unable to generate ClusterRequest spec from registration for request '%s' with id '%s': %w", request.String(), id, err)
		}
		if crSpec != nil {
			// the ClusterRequest was created by this library
			suffix := reg.Suffix()
			if suffix == "" {
				suffix = reg.ID()
			}
			namespace, err := reg.Namespace()
			if err != nil {
				return nil, fmt.Errorf("unable to generate name of platform cluster namespace for request '%s': %w", request.String(), err)
			}
			crRef = &commonapi.ObjectReference{
				Name:      StableRequestName(r.controllerName, request, suffix),
				Namespace: namespace,
			}
		} else {
			// check if an AccessRequest can be found and if it contains a ClusterRequest reference
			ar, err := r.AccessRequest(ctx, request, id)
			if err != nil {
				return nil, fmt.Errorf("unable to fetch AccessRequest for request '%s' with id '%s': %w", request.String(), id, err)
			}
			if ar.Spec.RequestRef == nil {
				return nil, fmt.Errorf("no ClusterRequest configured or referenced for request '%s' with id '%s'", request.String(), id)
			}
			crRef = ar.Spec.RequestRef.DeepCopy()
		}
	}

	cr := &clustersv1alpha1.ClusterRequest{}
	if err := r.platformClusterClient.Get(ctx, client.ObjectKey{Name: crRef.Name, Namespace: crRef.Namespace}, cr); err != nil {
		return nil, fmt.Errorf("unable to get ClusterRequest '%s/%s' for request '%s' with id '%s': %w", crRef.Namespace, crRef.Name, request.String(), id, err)
	}
	return cr, nil
}

// Reconcile implements Reconciler.
//
//nolint:gocyclo
func (r *reconcilerImpl) Reconcile(ctx context.Context, request reconcile.Request, additionalData ...any) (reconcile.Result, error) {
	log := logging.FromContextOrDiscard(ctx).WithName(caControllerName)
	ctx = logging.NewContext(ctx, log)
	log.Info("Reconciling cluster access")

	// iterate over registrations and ensure that the corresponding resources are ready
	for _, rawReg := range r.registrations {
		rlog := log.WithValues("id", rawReg.ID())
		rlog.Debug("Processing registration")
		suffix := rawReg.Suffix()
		if suffix == "" {
			suffix = rawReg.ID()
		}
		reg := rawReg.Parameterize(request, additionalData...)
		managedBy, managedPurpose, additionalLabels := r.managedBy(r.controllerName, request, reg)
		expectedLabels := map[string]string{}
		expectedLabels[openmcpconst.ManagedByLabel] = managedBy
		expectedLabels[openmcpconst.ManagedPurposeLabel] = managedPurpose
		expectedLabels = maputils.Merge(additionalLabels, expectedLabels)

		// ensure namespace exists
		namespace, err := reg.Namespace()
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("unable to generate name of platform cluster namespace for request '%s': %w", request.String(), err)
		}
		rlog.Debug("Ensuring platform cluster namespace exists", "namespaceName", namespace)
		ns := &corev1.Namespace{}
		ns.Name = namespace
		if err := r.platformClusterClient.Get(ctx, client.ObjectKeyFromObject(ns), ns); err != nil {
			if !apierrors.IsNotFound(err) {
				return reconcile.Result{}, fmt.Errorf("unable to get platform cluster namespace '%s': %w", ns.Name, err)
			}
			rlog.Info("Creating platform cluster namespace", "namespaceName", ns.Name)
			if err := r.platformClusterClient.Create(ctx, ns); err != nil {
				return reconcile.Result{}, fmt.Errorf("unable to create platform cluster namespace '%s': %w", ns.Name, err)
			}
		}

		// if a ClusterRequest is requested, ensure it exists and is ready
		var cr *clustersv1alpha1.ClusterRequest
		crSpec, err := reg.ClusterRequestSpec()
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("unable to generate ClusterRequest spec from registration for request '%s' with id '%s': %w", request.String(), reg.ID(), err)
		}
		if crSpec != nil {
			rlog.Debug("Ensuring ClusterRequest exists")
			cr = &clustersv1alpha1.ClusterRequest{}
			cr.Name = StableRequestName(r.controllerName, request, suffix)
			cr.Namespace = namespace
			// check if ClusterRequest exists
			exists := true
			if err := r.platformClusterClient.Get(ctx, client.ObjectKeyFromObject(cr), cr); err != nil {
				if !apierrors.IsNotFound(err) {
					return reconcile.Result{}, fmt.Errorf("unable to get ClusterRequest '%s/%s': %w", cr.Namespace, cr.Name, err)
				}
				exists = false
			}
			if !exists {
				rlog.Info("Creating ClusterRequest", "crName", cr.Name, "crNamespace", cr.Namespace)
				cr.Labels = map[string]string{}
				maps.Copy(cr.Labels, expectedLabels)
				cr.Spec = *crSpec
				if err := r.platformClusterClient.Create(ctx, cr); err != nil {
					return reconcile.Result{}, fmt.Errorf("unable to create ClusterRequest '%s/%s': %w", cr.Namespace, cr.Name, err)
				}
			} else {
				for k, v := range expectedLabels {
					if v2, ok := cr.Labels[k]; !ok || v2 != v {
						if !ok {
							v2 = "<null>"
						}
						return reconcile.Result{}, fmt.Errorf("ClusterRequest '%s/%s' already exists but is not managed by this controller (label '%s' is expected to have value '%s', but is '%s')", cr.Namespace, cr.Name, k, v, v2)
					}
				}
			}
			if !cr.Status.IsGranted() {
				rlog.Info("Waiting for ClusterRequest to be granted", "crName", cr.Name, "crNamespace", cr.Namespace)
				if fc := r.fakingCallbacks[FakingCallback_WaitingForClusterRequestReadiness]; fc != nil {
					rlog.Info("Executing faking callback, this message should only appear in unit tests", "key", FakingCallback_WaitingForClusterRequestReadiness)
					if err := fc(ctx, r.platformClusterClient, FakingCallback_WaitingForClusterRequestReadiness, &request, cr, nil, nil, nil); err != nil {
						return reconcile.Result{}, fmt.Errorf("faking callback for key '%s' failed: %w", FakingCallback_WaitingForClusterRequestReadiness, err)
					}
				}
				return reconcile.Result{RequeueAfter: r.interval}, nil
			}
			rlog.Debug("ClusterRequest is granted", "crName", cr.Name, "crNamespace", cr.Namespace)
		}

		// if an AccessRequest is requested, ensure it exists and is ready
		var ar *clustersv1alpha1.AccessRequest
		if reg.AccessRequestAvailable() {
			rlog.Debug("Ensuring AccessRequest exists")
			ar = &clustersv1alpha1.AccessRequest{}
			ar.Name = StableRequestName(r.controllerName, request, suffix)
			ar.Namespace = namespace
			// check if AccessRequest exists
			exists := true
			if err := r.platformClusterClient.Get(ctx, client.ObjectKeyFromObject(ar), ar); err != nil {
				if !apierrors.IsNotFound(err) {
					return reconcile.Result{}, fmt.Errorf("unable to get AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
				}
				exists = false
			}
			if !exists {
				rlog.Info("Creating AccessRequest", "arName", ar.Name, "arNamespace", ar.Namespace)
				// cluster and cluster request references are immutable, so set them only when creating new AccessRequests
				referenced := false
				if cr != nil {
					ar.Spec.RequestRef = &commonapi.ObjectReference{
						Name:      cr.Name,
						Namespace: cr.Namespace,
					}
					referenced = true
				}
				if !referenced {
					cRef, err := reg.ClusterReference()
					if err != nil {
						return reconcile.Result{}, fmt.Errorf("unable to generate Cluster reference from registration for request '%s' with id '%s': %w", request.String(), reg.ID(), err)
					}
					if cRef != nil {
						ar.Spec.ClusterRef = cRef
						referenced = true
					}
				}
				if !referenced {
					crRef, err := reg.ClusterRequestReference()
					if err != nil {
						return reconcile.Result{}, fmt.Errorf("unable to generate ClusterRequest reference from registration for request '%s' with id '%s': %w", request.String(), reg.ID(), err)
					}
					if crRef != nil {
						ar.Spec.RequestRef = crRef
						referenced = true
					}
				}
				if !referenced {
					return reconcile.Result{}, fmt.Errorf("no ClusterRequestSpec, ClusterReference or ClusterRequestReference specified for registration with id '%s'", reg.ID())
				}
			} else {
				for k, v := range expectedLabels {
					if v2, ok := ar.Labels[k]; !ok || v2 != v {
						if !ok {
							v2 = "<null>"
						}
						return reconcile.Result{}, fmt.Errorf("AccessRequest '%s/%s' already exists but is not managed by this controller (label '%s' is expected to have value '%s', but is '%s')", ar.Namespace, ar.Name, k, v, v2)
					}
				}
				rlog.Debug("Updating AccessRequest", "arName", ar.Name, "arNamespace", ar.Namespace)
			}
			if _, err := controllerutil.CreateOrUpdate(ctx, r.platformClusterClient, ar, func() error {
				ar.Labels = maputils.Merge(ar.Labels, expectedLabels)
				var err error
				ar.Spec.Token, err = reg.AccessRequestTokenConfig()
				if err != nil {
					return fmt.Errorf("unable to generate AccessRequest token config from registration: %w", err)
				}
				ar.Spec.OIDC, err = reg.AccessRequestOIDCConfig()
				if err != nil {
					return fmt.Errorf("unable to generate AccessRequest OIDC config from registration: %w", err)
				}
				return nil
			}); err != nil {
				return reconcile.Result{}, fmt.Errorf("unable to create or update AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
			}
			if !ar.Status.IsGranted() {
				rlog.Info("Waiting for AccessRequest to be granted", "arName", ar.Name, "arNamespace", ar.Namespace)
				if fc := r.fakingCallbacks[FakingCallback_WaitingForAccessRequestReadiness]; fc != nil {
					rlog.Info("Executing faking callback, this message should only appear in unit tests", "key", FakingCallback_WaitingForAccessRequestReadiness)
					if err := fc(ctx, r.platformClusterClient, FakingCallback_WaitingForAccessRequestReadiness, &request, cr, ar, nil, nil); err != nil {
						return reconcile.Result{}, fmt.Errorf("faking callback for key '%s' failed: %w", FakingCallback_WaitingForAccessRequestReadiness, err)
					}
				}
				return reconcile.Result{RequeueAfter: r.interval}, nil
			}
		}
	}
	log.Info("Successfully reconciled cluster access")

	return reconcile.Result{}, nil
}

// ReconcileDelete implements Reconciler.
func (r *reconcilerImpl) ReconcileDelete(ctx context.Context, request reconcile.Request, additionalData ...any) (reconcile.Result, error) {
	log := logging.FromContextOrDiscard(ctx).WithName(caControllerName)
	ctx = logging.NewContext(ctx, log)
	log.Info("Reconciling cluster access")

	// iterate over registrations and ensure that the corresponding resources are ready
	for _, rawReg := range r.registrations {
		rlog := log.WithValues("id", rawReg.ID())
		rlog.Debug("Processing registration")
		suffix := rawReg.Suffix()
		if suffix == "" {
			suffix = rawReg.ID()
		}
		reg := rawReg.Parameterize(request, additionalData...)
		managedBy, managedPurpose, additionalLabels := r.managedBy(r.controllerName, request, reg)
		expectedLabels := map[string]string{}
		expectedLabels[openmcpconst.ManagedByLabel] = managedBy
		expectedLabels[openmcpconst.ManagedPurposeLabel] = managedPurpose
		expectedLabels = maputils.Merge(additionalLabels, expectedLabels)

		namespace, err := reg.Namespace()
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("unable to generate name of platform cluster namespace for request '%s': %w", request.String(), err)
		}

		// if an AccessRequest is requested, ensure it is deleted
		if reg.AccessRequestAvailable() { //nolint:dupl
			rlog.Debug("Ensuring AccessRequest is deleted")
			ar := &clustersv1alpha1.AccessRequest{}
			ar.Name = StableRequestName(r.controllerName, request, suffix)
			ar.Namespace = namespace
			// check if AccessRequest exists
			if err := r.platformClusterClient.Get(ctx, client.ObjectKeyFromObject(ar), ar); err != nil {
				if !apierrors.IsNotFound(err) {
					return reconcile.Result{}, fmt.Errorf("unable to get AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
				}
				rlog.Debug("AccessRequest already deleted", "arName", ar.Name, "arNamespace", ar.Namespace)
			} else {
				managed := true
				for k, v := range expectedLabels {
					if v2, ok := ar.Labels[k]; !ok || v2 != v {
						managed = false
						break
					}
				}
				if !managed {
					rlog.Info("Skipping AccessRequest '%s/%s' deletion because it is not managed by this controller", ar.Namespace, ar.Name)
				} else {
					if ar.DeletionTimestamp.IsZero() {
						rlog.Info("Deleting AccessRequest", "arName", ar.Name, "arNamespace", ar.Namespace)
						if err := r.platformClusterClient.Delete(ctx, ar); err != nil {
							return reconcile.Result{}, fmt.Errorf("unable to delete AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
						}
					} else {
						rlog.Info("Waiting for AccessRequest to be deleted", "arName", ar.Name, "arNamespace", ar.Namespace)
					}
					if fc := r.fakingCallbacks[FakingCallback_WaitingForAccessRequestDeletion]; fc != nil {
						rlog.Info("Executing faking callback, this message should only appear in unit tests", "key", FakingCallback_WaitingForAccessRequestDeletion)
						if err := fc(ctx, r.platformClusterClient, FakingCallback_WaitingForAccessRequestDeletion, &request, nil, ar, nil, nil); err != nil {
							return reconcile.Result{}, fmt.Errorf("faking callback for key '%s' failed: %w", FakingCallback_WaitingForAccessRequestDeletion, err)
						}
					}
					return reconcile.Result{RequeueAfter: r.interval}, nil
				}
			}
		}

		// if a ClusterRequest is requested, ensure it is deleted
		if reg.ClusterRequestAvailable() { //nolint:dupl
			rlog.Debug("Ensuring ClusterRequest is deleted")
			cr := &clustersv1alpha1.ClusterRequest{}
			cr.Name = StableRequestName(r.controllerName, request, suffix)
			cr.Namespace = namespace
			// check if ClusterRequest exists
			if err := r.platformClusterClient.Get(ctx, client.ObjectKeyFromObject(cr), cr); err != nil {
				if !apierrors.IsNotFound(err) {
					return reconcile.Result{}, fmt.Errorf("unable to get ClusterRequest '%s/%s': %w", cr.Namespace, cr.Name, err)
				}
				rlog.Debug("ClusterRequest already deleted", "crName", cr.Name, "crNamespace", cr.Namespace)
			} else {
				managed := true
				for k, v := range expectedLabels {
					if v2, ok := cr.Labels[k]; !ok || v2 != v {
						managed = false
						break
					}
				}
				if !managed {
					rlog.Info("Skipping ClusterRequest '%s/%s' deletion because it is not managed by this controller", cr.Namespace, cr.Name)
				} else {
					if cr.DeletionTimestamp.IsZero() {
						rlog.Info("Deleting ClusterRequest", "crName", cr.Name, "crNamespace", cr.Namespace)
						if err := r.platformClusterClient.Delete(ctx, cr); err != nil {
							return reconcile.Result{}, fmt.Errorf("unable to delete ClusterRequest '%s/%s': %w", cr.Namespace, cr.Name, err)
						}
					} else {
						rlog.Info("Waiting for ClusterRequest to be deleted", "crName", cr.Name, "crNamespace", cr.Namespace)
					}
					if fc := r.fakingCallbacks[FakingCallback_WaitingForClusterRequestDeletion]; fc != nil {
						rlog.Info("Executing faking callback, this message should only appear in unit tests", "key", FakingCallback_WaitingForClusterRequestDeletion)
						if err := fc(ctx, r.platformClusterClient, FakingCallback_WaitingForClusterRequestDeletion, &request, cr, nil, nil, nil); err != nil {
							return reconcile.Result{}, fmt.Errorf("faking callback for key '%s' failed: %w", FakingCallback_WaitingForClusterRequestDeletion, err)
						}
					}
					return reconcile.Result{RequeueAfter: r.interval}, nil
				}
			}
		}

		// don't delete the namespace, because it might contain other resources
	}
	log.Info("Successfully reconciled cluster access")

	return reconcile.Result{}, nil
}

// Register implements Reconciler.
func (r *reconcilerImpl) Register(reg ClusterRegistration) ClusterAccessReconciler {
	if reg == nil {
		return r
	}
	r.registrations[reg.ID()] = reg
	return r
}

// Unregister implements Reconciler.
func (r *reconcilerImpl) Unregister(id string) ClusterAccessReconciler {
	delete(r.registrations, id)
	return r
}

// WithManagedLabels implements Reconciler.
func (r *reconcilerImpl) WithManagedLabels(gen ManagedLabelGenerator) ClusterAccessReconciler {
	if gen == nil {
		gen = DefaultManagedLabelGenerator
	}
	r.managedBy = gen
	return r
}

// WithRetryInterval implements Reconciler.
func (r *reconcilerImpl) WithRetryInterval(interval time.Duration) ClusterAccessReconciler {
	r.interval = interval
	return r
}

// WithFakingCallback implements Reconciler.
func (r *reconcilerImpl) WithFakingCallback(key string, callback FakingCallback) ClusterAccessReconciler {
	if r.fakingCallbacks == nil {
		r.fakingCallbacks = map[string]FakingCallback{}
	}
	r.fakingCallbacks[key] = callback
	return r
}

const (
	// FakingCallback_WaitingForAccessRequestReadiness is a key for a faking callback that is called when the reconciler is waiting for the AccessRequest to be granted.
	// Note that the execution happens directly before the return of the reconcile function (with a requeue). This means that the reconciliation needs to run a second time to pick up the changes made in the callback.
	FakingCallback_WaitingForAccessRequestReadiness = "WaitingForAccessRequestReadiness"
	// FakingCallback_WaitingForClusterRequestReadiness is a key for a faking callback that is called when the reconciler is waiting for the ClusterRequest to be granted.
	// Note that the execution happens directly before the return of the reconcile function (with a requeue). This means that the reconciliation needs to run a second time to pick up the changes made in the callback.
	FakingCallback_WaitingForClusterRequestReadiness = "WaitingForClusterRequestReadiness"
	// FakingCallback_WaitingForAccessRequestDeletion is a key for a faking callback that is called when the reconciler is waiting for the AccessRequest to be deleted.
	// Note that the execution happens directly before the return of the reconcile function (with a requeue). This means that the reconciliation needs to run a second time to pick up the changes made in the callback.
	FakingCallback_WaitingForAccessRequestDeletion = "WaitingForAccessRequestDeletion"
	// FakingCallback_WaitingForClusterRequestDeletion is a key for a faking callback that is called when the reconciler is waiting for the ClusterRequest to be deleted.
	// Note that the execution happens directly before the return of the reconcile function (with a requeue). This means that the reconciliation needs to run a second time to pick up the changes made in the callback.
	FakingCallback_WaitingForClusterRequestDeletion = "WaitingForClusterRequestDeletion"
)

///////////////////////////
/// AUXILIARY FUNCTIONS ///
///////////////////////////

// StableRequestName generates a stable name for a Cluster- or AccessRequest related to an MCP.
// This basically results in '<lowercase_controller_name>--<request_name>--<lowercase_suffix>'.
// If the resulting string exceeds the Kubernetes name length limit of 63 characters, it will be truncated with the last characters (excluding the suffix) replaced by a hash of what was removed.
// If the suffix is empty, it will be omitted (and the preceding hyphen as well).
func StableRequestName(controllerName string, request reconcile.Request, suffix string) string {
	return StableRequestNameFromLocalName(controllerName, request.Name, suffix)
}

// StableRequestNameFromLocalName works like StableRequestName but takes a local name directly instead of a reconcile.Request.
// localName is converted to lowercase before processing.
func StableRequestNameFromLocalName(controllerName, localName, suffix string) string {
	controllerName = strings.ToLower(controllerName)
	localName = strings.ToLower(localName)
	raw := fmt.Sprintf("%s--%s", controllerName, localName)
	if suffix != "" {
		suffix = strings.ToLower(suffix)
		return fmt.Sprintf("%s--%s", ctrlutils.ShortenToXCharactersUnsafe(raw, ctrlutils.K8sMaxNameLength-len(suffix)-2), suffix)
	}
	return ctrlutils.ShortenToXCharactersUnsafe(raw, ctrlutils.K8sMaxNameLength)
}

// AccessFromAccessRequest provides access to a k8s cluster based on the given AccessRequest.
func AccessFromAccessRequest(ctx context.Context, platformClusterClient client.Client, id string, scheme *runtime.Scheme, ar *clustersv1alpha1.AccessRequest) (*clusters.Cluster, error) {
	if ar.Status.SecretRef == nil {
		return nil, fmt.Errorf("AccessRequest '%s/%s' has no secret reference in status", ar.Namespace, ar.Name)
	}

	s := &corev1.Secret{}
	s.Name = ar.Status.SecretRef.Name
	s.Namespace = ar.Status.SecretRef.Namespace

	if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(s), s); err != nil {
		return nil, fmt.Errorf("unable to get secret '%s/%s' for AccessRequest '%s/%s': %w", s.Namespace, s.Name, ar.Namespace, ar.Name, err)
	}

	kubeconfigBytes, ok := s.Data[clustersv1alpha1.SecretKeyKubeconfig]
	if !ok {
		return nil, fmt.Errorf("kubeconfig key '%s' not found in AccessRequest secret '%s/%s'", clustersv1alpha1.SecretKeyKubeconfig, s.Namespace, s.Name)
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create rest config from kubeconfig bytes: %w", err)
	}

	c := clusters.New(id).WithRESTConfig(config)

	if err = c.InitializeClient(scheme); err != nil {
		return nil, fmt.Errorf("failed to initialize client: %w", err)
	}

	return c, nil
}

// StaticNamespaceGenerator returns a namespace generator that always returns the same namespace.
func StaticNamespaceGenerator(namespace string) func(reconcile.Request, ...any) (string, error) {
	return func(_ reconcile.Request, _ ...any) (string, error) {
		return namespace, nil
	}
}

// RequestNamespaceGenerator is a namespace generator that returns the namespace of the request.
func RequestNamespaceGenerator(req reconcile.Request, _ ...any) (string, error) {
	return req.Namespace, nil
}

// DefaultNamespaceGenerator is a default implementation of a namespace generator.
// It computes a UUID-style hash from the given request.
func DefaultNamespaceGenerator(req reconcile.Request, _ ...any) (string, error) {
	return ctrlutils.K8sNameUUID(req.Namespace, req.Name)
}

// DefaultNamespaceGeneratorForMCP is a default implementation of a namespace generator for MCPs.
// It computes a UUID-style hash from the given request and prefixes it with "mcp--".
func DefaultNamespaceGeneratorForMCP(req reconcile.Request, _ ...any) (string, error) {
	return libutils.StableMCPNamespace(req.Name, req.Namespace)
}

// StaticClusterRequestSpecGenerator is a helper function that returns a ClusterRequestSpec generator which just returns deep copies of the given spec.
func StaticClusterRequestSpecGenerator(spec *clustersv1alpha1.ClusterRequestSpec) func(reconcile.Request, ...any) (*clustersv1alpha1.ClusterRequestSpec, error) {
	return func(_ reconcile.Request, _ ...any) (*clustersv1alpha1.ClusterRequestSpec, error) {
		return spec.DeepCopy(), nil
	}
}

// StaticReferenceGenerator is a helper function that returns an ObjectReference generator which just returns a deep copy of the given reference.
func StaticReferenceGenerator(ref *commonapi.ObjectReference) func(reconcile.Request, ...any) (*commonapi.ObjectReference, error) {
	return func(_ reconcile.Request, _ ...any) (*commonapi.ObjectReference, error) {
		return ref.DeepCopy(), nil
	}
}

// IdentityReferenceGenerator is an ObjectReference generator that returns a reference that is identical to the request (name and namespace).
func IdentityReferenceGenerator(req reconcile.Request, _ ...any) (*commonapi.ObjectReference, error) {
	return &commonapi.ObjectReference{
		Name:      req.Name,
		Namespace: req.Namespace,
	}, nil
}

////////////////////////////////
/// FAKE AUXILIARY FUNCTIONS ///
////////////////////////////////

// FakeClusterRequestReadiness returns a faking callback that sets the ClusterRequest to 'Granted'.
// If the given ClusterSpec is not nil, it creates a corresponding Cluster next to the ClusterRequest, if it doesn't exist yet.
// If during the callback, the Cluster is non-nil, with a non-empty name and namespace, but doesn't exist yet, it will be created with the data from the Cluster, ignoring the given ClusterSpec.
// Otherwise, only the ClusterRequest's status is modified.
// The callback is a no-op if the ClusterRequest is already granted (Cluster reference and existence are not checked in this case).
// It returns an error if the ClusterRequest is nil.
//
// The returned callback is meant to be used with the key stored in FakingCallback_WaitingForClusterRequestReadiness.
func FakeClusterRequestReadiness(clusterSpec *clustersv1alpha1.ClusterSpec) FakingCallback {
	return func(ctx context.Context, platformClusterClient client.Client, key string, _ *reconcile.Request, cr *clustersv1alpha1.ClusterRequest, _ *clustersv1alpha1.AccessRequest, c *clustersv1alpha1.Cluster, _ *clusters.Cluster) error {
		if cr == nil {
			return fmt.Errorf("ClusterRequest is nil")
		}
		if cr.Status.IsGranted() {
			// already granted, nothing to do
			return nil
		}

		// create cluster, if desired
		if c != nil && c.Name != "" && c.Namespace != "" {
			// check if cluster exists
			if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(c), c); err != nil {
				if !apierrors.IsNotFound(err) {
					return fmt.Errorf("unable to get Cluster '%s/%s': %w", c.Namespace, c.Name, err)
				}
				// create cluster
				if err := platformClusterClient.Create(ctx, c); err != nil {
					return fmt.Errorf("unable to create Cluster '%s/%s': %w", c.Namespace, c.Name, err)
				}
			}
		} else {
			// cluster not known, create fake one, if spec is given
			c = &clustersv1alpha1.Cluster{}
			c.Name = cr.Name
			c.Namespace = cr.Namespace
			if clusterSpec != nil {
				c.Spec = *clusterSpec.DeepCopy()
				if err := platformClusterClient.Create(ctx, c); err != nil {
					return fmt.Errorf("unable to create Cluster '%s/%s': %w", c.Namespace, c.Name, err)
				}
			}
		}

		// mock ClusterRequest status
		old := cr.DeepCopy()
		cr.Status.Cluster = &commonapi.ObjectReference{
			Name:      c.Name,
			Namespace: c.Namespace,
		}
		cr.Status.Phase = clustersv1alpha1.REQUEST_GRANTED
		cr.Status.ObservedGeneration = cr.Generation
		if err := platformClusterClient.Status().Patch(ctx, cr, client.MergeFrom(old)); err != nil {
			return fmt.Errorf("unable to update status of ClusterRequest '%s/%s': %w", cr.Namespace, cr.Name, err)
		}
		return nil
	}
}

// FakeAccessRequestReadiness returns a faking callback that sets the AccessRequest to 'Granted'.
// If kcfgData is not nil or empty, it will be used as 'kubeconfig' data in the secret referenced by the AccessRequest.
// Otherwise, the content of the kubeconfig key will just be 'fake'.
// The callback is a no-op if the AccessRequest is already granted (Secret reference and existence are not checked in this case).
// It returns an error if the AccessRequest is nil.
//
// The returned callback is meant to be used with the key stored in FakingCallback_WaitingForAccessRequestReadiness.
func FakeAccessRequestReadiness(kcfgData []byte) FakingCallback {
	return func(ctx context.Context, platformClusterClient client.Client, key string, req *reconcile.Request, cr *clustersv1alpha1.ClusterRequest, ar *clustersv1alpha1.AccessRequest, c *clustersv1alpha1.Cluster, access *clusters.Cluster) error {
		if ar == nil {
			return fmt.Errorf("AccessRequest is nil")
		}
		if ar.Status.IsGranted() {
			// already granted, nothing to do
			return nil
		}

		// if a ClusterRequest is referenced, but no Cluster, try to identify the Cluster
		if ar.Spec.ClusterRef == nil {
			old := ar.DeepCopy()
			if c != nil {
				ar.Spec.ClusterRef = &commonapi.ObjectReference{
					Name:      c.Name,
					Namespace: c.Namespace,
				}
				if err := platformClusterClient.Patch(ctx, ar, client.MergeFrom(old)); err != nil {
					return fmt.Errorf("unable to update spec of AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
				}
			} else if cr != nil && cr.Status.Cluster != nil {
				ar.Spec.ClusterRef = cr.Status.Cluster.DeepCopy()
				if err := platformClusterClient.Patch(ctx, ar, client.MergeFrom(old)); err != nil {
					return fmt.Errorf("unable to update spec of AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
				}
			} else if ar.Spec.RequestRef != nil {
				cr2 := &clustersv1alpha1.ClusterRequest{}
				cr2.Name = ar.Spec.RequestRef.Name
				cr2.Namespace = ar.Spec.RequestRef.Namespace
				if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(cr2), cr2); err == nil {
					if cr2.Status.Cluster != nil {
						old := ar.DeepCopy()
						ar.Spec.ClusterRef = cr2.Status.Cluster.DeepCopy()
						if err := platformClusterClient.Patch(ctx, ar, client.MergeFrom(old)); err != nil {
							return fmt.Errorf("unable to update spec of AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
						}
					}
				}
			}
		}

		// create secret
		if len(kcfgData) == 0 {
			kcfgData = []byte("fake")
		}
		s := &corev1.Secret{}
		s.Name = ar.Name
		s.Namespace = ar.Namespace
		s.Data = map[string][]byte{
			clustersv1alpha1.SecretKeyKubeconfig: kcfgData,
		}
		if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(s), s); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("unable to get secret '%s/%s': %w", s.Namespace, s.Name, err)
			}
			// create secret
			if err := platformClusterClient.Create(ctx, s); err != nil {
				return fmt.Errorf("unable to create secret '%s/%s': %w", s.Namespace, s.Name, err)
			}
		}

		// mock AccessRequest status
		old := ar.DeepCopy()
		ar.Status.SecretRef = &commonapi.ObjectReference{
			Name:      s.Name,
			Namespace: s.Namespace,
		}
		ar.Status.Phase = clustersv1alpha1.REQUEST_GRANTED
		ar.Status.ObservedGeneration = ar.Generation
		if err := platformClusterClient.Status().Patch(ctx, ar, client.MergeFrom(old)); err != nil {
			return fmt.Errorf("unable to update status of AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
		}
		return nil
	}
}

// FakeClusterRequestDeletion returns a faking callback that removes finalizers from the given ClusterRequest and potentially deletes the referenced Cluster.
// The callback is a no-op if the ClusterRequest is already nil (not found).
// If deleteCluster is true, the Cluster referenced by the ClusterRequest will be deleted, if it exists.
// All finalizers from the finalizersToRemove* slices will be removed from the ClusterRequest and/or Cluster before deletion.
// The ClusterRequest itself is not deleted by this callback, only finalizers are removed.
//
// The returned callback is meant to be used with the key stored in FakingCallback_WaitingForClusterRequestDeletion.
//
//nolint:dupl
func FakeClusterRequestDeletion(deleteCluster bool, finalizersToRemoveFromClusterRequest, finalizersToRemoveFromCluster []string) FakingCallback {
	return func(ctx context.Context, platformClusterClient client.Client, key string, _ *reconcile.Request, cr *clustersv1alpha1.ClusterRequest, _ *clustersv1alpha1.AccessRequest, _ *clustersv1alpha1.Cluster, _ *clusters.Cluster) error {
		if cr == nil {
			// already deleted, nothing to do
			return nil
		}

		// delete cluster, if desired
		if deleteCluster && cr.Status.Cluster != nil {
			c := &clustersv1alpha1.Cluster{}
			c.Name = cr.Status.Cluster.Name
			c.Namespace = cr.Status.Cluster.Namespace

			if len(finalizersToRemoveFromCluster) > 0 {
				// fetch cluster to remove finalizers
				if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(c), c); err != nil {
					if !apierrors.IsNotFound(err) {
						return fmt.Errorf("unable to get Cluster '%s/%s': %w", c.Namespace, c.Name, err)
					}
					// cluster doesn't exist, nothing to do
					c = nil
				} else {
					cOld := c.DeepCopy()
					if removeFinalizers(c, finalizersToRemoveFromCluster...) {
						if err := platformClusterClient.Patch(ctx, c, client.MergeFrom(cOld)); err != nil {
							return fmt.Errorf("unable to remove finalizers from Cluster '%s/%s': %w", c.Namespace, c.Name, err)
						}
					}
				}
			}

			if c != nil {
				if err := platformClusterClient.Delete(ctx, c); client.IgnoreNotFound(err) != nil {
					return fmt.Errorf("unable to delete Cluster '%s/%s': %w", c.Namespace, c.Name, err)
				}
			}
		}

		// remove finalizers from ClusterRequest, if any
		if len(finalizersToRemoveFromClusterRequest) > 0 {
			crOld := cr.DeepCopy()
			if removeFinalizers(cr, finalizersToRemoveFromClusterRequest...) {
				if err := platformClusterClient.Patch(ctx, cr, client.MergeFrom(crOld)); err != nil {
					return fmt.Errorf("unable to remove finalizers from ClusterRequest '%s/%s': %w", cr.Namespace, cr.Name, err)
				}
			}
		}

		return nil
	}
}

// FakeAccessRequestDeletion returns a faking callback that removes finalizers from the given AccessRequest and deletes the referenced Secret, if it exists.
// The callback is a no-op if the AccessRequest is already nil (not found).
// It returns an error if the AccessRequest is non-nil but cannot be deleted.
// All finalizers from the finalizersToRemove* slices will be removed from the AccessRequest and/or Secret before deletion.
// The AccessRequest itself is not deleted by this callback, only finalizers are removed.
//
// The returned callback is meant to be used with the key stored in FakingCallback_WaitingForAccessRequestDeletion.
//
//nolint:dupl
func FakeAccessRequestDeletion(finalizersToRemoveFromAccessRequest, finalizersToRemoveFromSecret []string) FakingCallback {
	return func(ctx context.Context, platformClusterClient client.Client, key string, _ *reconcile.Request, _ *clustersv1alpha1.ClusterRequest, ar *clustersv1alpha1.AccessRequest, _ *clustersv1alpha1.Cluster, _ *clusters.Cluster) error {
		if ar == nil {
			// already deleted, nothing to do
			return nil
		}

		// delete secret
		if ar.Status.SecretRef != nil {
			s := &corev1.Secret{}
			s.Name = ar.Status.SecretRef.Name
			s.Namespace = ar.Status.SecretRef.Namespace

			if len(finalizersToRemoveFromSecret) > 0 {
				// fetch secret to remove finalizers
				if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(s), s); err != nil {
					if !apierrors.IsNotFound(err) {
						return fmt.Errorf("unable to get secret '%s/%s': %w", s.Namespace, s.Name, err)
					}
					// secret doesn't exist, nothing to do
					s = nil
				} else {
					sOld := s.DeepCopy()
					if removeFinalizers(s, finalizersToRemoveFromSecret...) {
						if err := platformClusterClient.Patch(ctx, s, client.MergeFrom(sOld)); err != nil {
							return fmt.Errorf("unable to remove finalizers from secret '%s/%s': %w", s.Namespace, s.Name, err)
						}
					}
				}
			}

			if s != nil {
				if err := platformClusterClient.Delete(ctx, s); client.IgnoreNotFound(err) != nil {
					return fmt.Errorf("unable to delete secret '%s/%s': %w", s.Namespace, s.Name, err)
				}
			}
		}

		// remove finalizers from AccessRequest, if any
		if len(finalizersToRemoveFromAccessRequest) > 0 {
			arOld := ar.DeepCopy()
			if removeFinalizers(ar, finalizersToRemoveFromAccessRequest...) {
				if err := platformClusterClient.Patch(ctx, ar, client.MergeFrom(arOld)); err != nil {
					return fmt.Errorf("unable to remove finalizers from AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
				}
			}
		}

		return nil
	}
}

// removeFinalizers takes an object and a list of finalizers to remove from that object.
// If the list contains "*", all finalizers will be removed.
// It updates the object in-place and returns an indicator whether any finalizers were removed.
func removeFinalizers(obj client.Object, finalizersToRemove ...string) bool {
	if obj == nil || len(obj.GetFinalizers()) == 0 || len(finalizersToRemove) == 0 {
		return false
	}

	changed := false
	if slices.Contains(finalizersToRemove, "*") {
		obj.SetFinalizers(nil)
		changed = true
	} else {
		for _, ftr := range finalizersToRemove {
			if controllerutil.RemoveFinalizer(obj, ftr) {
				changed = true
			}
		}
	}
	return changed
}
