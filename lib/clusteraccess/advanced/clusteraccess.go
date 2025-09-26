//nolint:revive
package advanced

import (
	"context"
	"fmt"
	"maps"
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

//////////////////
/// INTERFACES ///
//////////////////

// Reconciler is an interface for reconciling access k8s clusters based on the openMCP 'Cluster' API.
// It can create ClusterRequests and/or AccessRequests for an amount of clusters.
type Reconciler interface {
	// Register registers a cluster to be managed by the reconciler.
	Register(reg ClusterRegistration) Reconciler
	// WithRetryInterval sets the retry interval.
	WithRetryInterval(interval time.Duration) Reconciler
	// WithManagedLabels allows to overwrite the managed-by and managed-purpose labels that are set on the created resources.
	// Note that the implementation might depend on these labels to identify the resources it created,
	// so changing them might lead to unexpected behavior. They also must be unique within the context of this reconciler.
	// Use with caution.
	WithManagedLabels(managedBy, managedPurpose string) Reconciler

	// Access returns an internal Cluster object granting access to the cluster for the specified request with the specified id.
	// Will fail if the cluster is not registered or no AccessRequest is registered for the cluster, or if some other error occurs.
	Access(ctx context.Context, request reconcile.Request, id string) (*clusters.Cluster, error)
	// AccessRequest fetches the AccessRequest object for the cluster for the specified request with the specified id.
	// Will fail if the cluster is not registered or no AccessRequest is registered for the cluster, or if some other error occurs.
	AccessRequest(ctx context.Context, request reconcile.Request, id string) (*clustersv1alpha1.AccessRequest, error)
	// ClusterRequest fetches the ClusterRequest object for the cluster for the specified request with the specified id.
	// Will fail if the cluster is not registered or no ClusterRequest is registered for the cluster, or if some other error occurs.
	ClusterRequest(ctx context.Context, request reconcile.Request, id string) (*clustersv1alpha1.ClusterRequest, error)
	// Cluster fetches the external Cluster object for the cluster for the specified request with the specified id.
	// Will fail if the cluster is not registered or no Cluster can be determined, or if some other error occurs.
	Cluster(ctx context.Context, request reconcile.Request, id string) (*clustersv1alpha1.Cluster, error)

	// Reconcile creates the ClusterRequests and/or AccessRequests for the registered clusters.
	// This function should be called during all reconciliations of the reconciled object.
	// ctx is the context for the reconciliation.
	// request is the object that is being reconciled
	// It returns a reconcile.Result and an error if the reconciliation failed.
	// The reconcile.Result may contain a RequeueAfter value to indicate that the reconciliation should be retried after a certain duration.
	// The duration is set by the WithRetryInterval method.
	Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error)
	// ReconcileDelete deletes the ClusterRequests and/or AccessRequests for the registered clusters.
	// This function should be called during the deletion of the reconciled object.
	// ctx is the context for the reconciliation.
	// request is the object that is being reconciled
	// It returns a reconcile.Result and an error if the reconciliation failed.
	// The reconcile.Result may contain a RequeueAfter value to indicate that the reconciliation should be retried after a certain duration.
	// The duration is set by the WithRetryInterval method.
	ReconcileDelete(ctx context.Context, request reconcile.Request) (reconcile.Result, error)
}

type ClusterRegistration interface {
	// ID is the unique identifier for the cluster.
	ID() string
	// Suffix is the suffix to be used for the names of the created resources.
	// It must be unique within the context of the reconciler.
	// If empty, the ID will be used as suffix.
	Suffix() string
	// AccessRequestTokenConfig is the token configuration for the AccessRequest to be created for the cluster.
	// Might be nil if no AccessRequest should be created or if OIDC access is used.
	// Only one of AccessRequestTokenConfig and AccessRequestOIDCConfig should be non-nil.
	AccessRequestTokenConfig() *clustersv1alpha1.TokenConfig
	// AccessRequestOIDCConfig is the OIDC configuration for the AccessRequest to be created for the cluster.
	// Might be nil if no AccessRequest should be created or if token-based access is used.
	// Only one of AccessRequestTokenConfig and AccessRequestOIDCConfig should be non-nil.
	AccessRequestOIDCConfig() *clustersv1alpha1.OIDCConfig
	// ClusterRequestSpec is the spec for the ClusterRequest to be created for the cluster.
	// It is nil if no ClusterRequest should be created.
	// Only one of ClusterRequestSpec, ClusterReference and ClusterRequestReference should be non-nil.
	ClusterRequestSpec() *clustersv1alpha1.ClusterRequestSpec
	// ClusterReference returns name and namespace of an existing Cluster resource.
	// It is nil if a new ClusterRequest should be created or if the Cluster is referenced by an existing ClusterRequest.
	// Only one of ClusterRequestSpec, ClusterReference and ClusterRequestReference should be non-nil.
	ClusterReference() *commonapi.ObjectReference
	// ClusterRequestReference returns name and namespace of the ClusterRequest resource.
	// It is nil if a new ClusterRequest should be created or if the Cluster is referenced directly.
	// Only one of ClusterRequestSpec, ClusterReference and ClusterRequestReference should be non-nil.
	ClusterRequestReference() *commonapi.ObjectReference
	// Scheme is the scheme for the Kubernetes client of the cluster.
	// If nil, the default scheme will be used.
	Scheme() *runtime.Scheme
	// NamespaceGenerator returns a function that generates the namespace on the Platform cluster to use for the created requests.
	// The function must take the name and namespace of the reconciled object as parameters and return the namespace or an error.
	// The generated namespace must be unique within the context of the reconciler.
	// If nil, the StableMCPNamespace function will be used.
	NamespaceGenerator() func(requestName, requestNamespace string) (string, error)
}

type ClusterRegistrationBuilder interface {
	// WithTokenAccess enables an AccessRequest for token-based access to be created for the cluster.
	// It is mutually exclusive to WithOIDCAccess, calling this method after WithOIDCAccess will override the previous call.
	// Passing in a nil cfg will disable AccessRequest creation, if it was token-based before, and have no effect if it was OIDC-based before.
	WithTokenAccess(cfg *clustersv1alpha1.TokenConfig) ClusterRegistrationBuilder
	// WithOIDCAccess enables an AccessRequest for OIDC-based access to be created for the cluster.
	// It is mutually exclusive to WithTokenAccess, calling this method after WithTokenAccess will override the previous call.
	// Passing in a nil cfg will disable AccessRequest creation, if it was OIDC-based before, and have no effect if it was token-based before.
	WithOIDCAccess(cfg *clustersv1alpha1.OIDCConfig) ClusterRegistrationBuilder
	// WithScheme sets the scheme for the Kubernetes client of the cluster.
	// If not set or set to nil, the default scheme will be used.
	WithScheme(scheme *runtime.Scheme) ClusterRegistrationBuilder
	// WithNamespaceGenerator sets the function that generates the namespace on the Platform cluster to use for the created requests.
	// If set to nil, the StableMCPNamespace function will be used.
	WithNamespaceGenerator(f func(requestName, requestNamespace string) (string, error)) ClusterRegistrationBuilder
	// Build builds the ClusterRegistration object.
	Build() ClusterRegistration
}

///////////////////////////////////////////
/// IMPLEMENTATION: ClusterRegistration ///
///////////////////////////////////////////

type clusterRegistrationImpl struct {
	id                 string
	suffix             string
	scheme             *runtime.Scheme
	accessTokenConfig  *clustersv1alpha1.TokenConfig
	accessOIDCConfig   *clustersv1alpha1.OIDCConfig
	clusterRequestSpec *clustersv1alpha1.ClusterRequestSpec
	clusterRef         *commonapi.ObjectReference
	clusterRequestRef  *commonapi.ObjectReference
	namespaceGenerator func(requestName, requestNamespace string) (string, error)
}

var _ ClusterRegistration = &clusterRegistrationImpl{}

// AccessRequestTokenConfig implements ClusterRegistration.
func (c *clusterRegistrationImpl) AccessRequestTokenConfig() *clustersv1alpha1.TokenConfig {
	return c.accessTokenConfig.DeepCopy()
}

// AccessRequestOIDCConfig implements ClusterRegistration.
func (c *clusterRegistrationImpl) AccessRequestOIDCConfig() *clustersv1alpha1.OIDCConfig {
	return c.accessOIDCConfig.DeepCopy()
}

// ClusterRequestSpec implements ClusterRegistration.
func (c *clusterRegistrationImpl) ClusterRequestSpec() *clustersv1alpha1.ClusterRequestSpec {
	return c.clusterRequestSpec.DeepCopy()
}

// ClusterReference implements ClusterRegistration.
func (c *clusterRegistrationImpl) ClusterReference() *commonapi.ObjectReference {
	return c.clusterRef.DeepCopy()
}

// ClusterRequestReference implements ClusterRegistration.
func (c *clusterRegistrationImpl) ClusterRequestReference() *commonapi.ObjectReference {
	return c.clusterRequestRef.DeepCopy()
}

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

// NamespaceGenerator implements ClusterRegistration.
func (c *clusterRegistrationImpl) NamespaceGenerator() func(requestName, requestNamespace string) (string, error) {
	if c.namespaceGenerator != nil {
		return c.namespaceGenerator
	}
	return libutils.StableMCPNamespace
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
	c.accessOIDCConfig = cfg.DeepCopy()
	return c
}

// WithTokenAccess implements ClusterRegistrationBuilder.
func (c *clusterRegistrationBuilderImpl) WithTokenAccess(cfg *clustersv1alpha1.TokenConfig) ClusterRegistrationBuilder {
	c.accessTokenConfig = cfg.DeepCopy()
	return c
}

// WithScheme implements ClusterRegistrationBuilder.
func (c *clusterRegistrationBuilderImpl) WithScheme(scheme *runtime.Scheme) ClusterRegistrationBuilder {
	c.scheme = scheme
	return c
}

// WithNamespaceGenerator implements ClusterRegistrationBuilder.
func (c *clusterRegistrationBuilderImpl) WithNamespaceGenerator(f func(requestName, requestNamespace string) (string, error)) ClusterRegistrationBuilder {
	c.namespaceGenerator = f
	return c
}

// NewCluster instructs the Reconciler to create and manage a new ClusterRequest.
func NewCluster(id, suffix string, purpose string) ClusterRegistrationBuilder {
	return &clusterRegistrationBuilderImpl{
		clusterRegistrationImpl: clusterRegistrationImpl{
			id:     id,
			suffix: suffix,
			clusterRequestSpec: &clustersv1alpha1.ClusterRequestSpec{
				Purpose: purpose,
			},
		},
	}
}

// ExistingCluster instructs the Reconciler to use an existing Cluster resource.
func ExistingCluster(id, suffix, name, namespace string) ClusterRegistrationBuilder {
	return &clusterRegistrationBuilderImpl{
		clusterRegistrationImpl: clusterRegistrationImpl{
			id:     id,
			suffix: suffix,
			clusterRef: &commonapi.ObjectReference{
				Name:      name,
				Namespace: namespace,
			},
		},
	}
}

// ExistingClusterRequest instructs the Reconciler to use an existing Cluster resource that is referenced by the given ClusterRequest.
func ExistingClusterRequest(id, suffix, name, namespace string) ClusterRegistrationBuilder {
	return &clusterRegistrationBuilderImpl{
		clusterRegistrationImpl: clusterRegistrationImpl{
			id:     id,
			suffix: suffix,
			clusterRequestRef: &commonapi.ObjectReference{
				Name:      name,
				Namespace: namespace,
			},
		},
	}
}

//////////////////////////////////
/// IMPLEMENTATION: Reconciler ///
//////////////////////////////////

type reconcilerImpl struct {
	controllerName        string
	platformClusterClient client.Client

	interval       time.Duration
	registrations  map[string]ClusterRegistration
	managedBy      string
	managedPurpose string
}

var _ Reconciler = &reconcilerImpl{}

// Access implements Reconciler.
func (r *reconcilerImpl) Access(ctx context.Context, request reconcile.Request, id string) (*clusters.Cluster, error) {
	reg, ok := r.registrations[id]
	if !ok {
		return nil, fmt.Errorf("no registration found for request '%s' with id '%s'", request.String(), id)
	}
	ar, err := r.AccessRequest(ctx, request, id)
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
func (r *reconcilerImpl) AccessRequest(ctx context.Context, request reconcile.Request, id string) (*clustersv1alpha1.AccessRequest, error) {
	reg, ok := r.registrations[id]
	if !ok {
		return nil, fmt.Errorf("no registration found for request '%s' with id '%s'", request.String(), id)
	}
	if reg.AccessRequestOIDCConfig() == nil && reg.AccessRequestTokenConfig() == nil {
		return nil, fmt.Errorf("no AccessRequest configured for request '%s' with id '%s'", request.String(), id)
	}
	suffix := reg.Suffix()
	if suffix == "" {
		suffix = reg.ID()
	}
	namespace, err := reg.NamespaceGenerator()(request.Name, request.Namespace)
	if err != nil {
		return nil, fmt.Errorf("unable to generate name of platform cluster namespace for request '%s/%s': %w", request.Namespace, request.Name, err)
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
func (r *reconcilerImpl) Cluster(ctx context.Context, request reconcile.Request, id string) (*clustersv1alpha1.Cluster, error) {
	reg, ok := r.registrations[id]
	if !ok {
		return nil, fmt.Errorf("no registration found for request '%s' with id '%s'", request.String(), id)
	}
	clusterRef := reg.ClusterReference()
	if clusterRef == nil {
		// if no ClusterReference is specified, try to get it from the ClusterRequest
		if reg.ClusterRequestReference() != nil {
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
func (r *reconcilerImpl) ClusterRequest(ctx context.Context, request reconcile.Request, id string) (*clustersv1alpha1.ClusterRequest, error) {
	reg, ok := r.registrations[id]
	if !ok {
		return nil, fmt.Errorf("no registration found for request '%s' with id '%s'", request.String(), id)
	}
	crRef := reg.ClusterRequestReference()
	if crRef == nil {
		// if the ClusterRequest is not referenced directly, check if was created or can be retrieved via an AccessRequest
		if reg.ClusterRequestSpec() == nil {
			// the ClusterRequest was created by this library
			suffix := reg.Suffix()
			if suffix == "" {
				suffix = reg.ID()
			}
			namespace, err := reg.NamespaceGenerator()(request.Name, request.Namespace)
			if err != nil {
				return nil, fmt.Errorf("unable to generate name of platform cluster namespace for request '%s/%s': %w", request.Namespace, request.Name, err)
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
func (r *reconcilerImpl) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrDiscard(ctx).WithName("ClusterAccess")
	ctx = logging.NewContext(ctx, log)
	log.Info("Reconciling cluster access")

	// iterate over registrations and ensure that the corresponding resources are ready
	for _, reg := range r.registrations {
		rlog := log.WithValues("id", reg.ID())
		rlog.Debug("Processing registration")
		suffix := reg.Suffix()
		if suffix == "" {
			suffix = reg.ID()
		}
		expectedLabels := map[string]string{}
		if r.managedBy != "" {
			expectedLabels[openmcpconst.ManagedByLabel] = r.managedBy
		} else {
			expectedLabels[openmcpconst.ManagedByLabel] = r.controllerName
		}
		if r.managedPurpose != "" {
			expectedLabels[openmcpconst.ManagedPurposeLabel] = r.managedPurpose
		} else {
			expectedLabels[openmcpconst.ManagedPurposeLabel] = fmt.Sprintf("%s.%s.%s", request.Namespace, request.Name, reg.ID())
		}

		// ensure namespace exists
		namespace, err := reg.NamespaceGenerator()(request.Name, request.Namespace)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("unable to generate name of platform cluster namespace for request '%s/%s': %w", request.Namespace, request.Name, err)
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
		if reg.ClusterRequestSpec() != nil {
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
				cr.Spec = *reg.ClusterRequestSpec().DeepCopy()
				if err := r.platformClusterClient.Create(ctx, cr); err != nil {
					return reconcile.Result{}, fmt.Errorf("unable to create ClusterRequest '%s/%s': %w", cr.Namespace, cr.Name, err)
				}
			}
			if !cr.Status.IsGranted() {
				rlog.Info("Waiting for ClusterRequest to be granted", "crName", cr.Name, "crNamespace", cr.Namespace)
				return reconcile.Result{RequeueAfter: r.interval}, nil
			}
			rlog.Debug("ClusterRequest is granted", "crName", cr.Name, "crNamespace", cr.Namespace)
		}

		// if an AccessRequest is requested, ensure it exists and is ready
		var ar *clustersv1alpha1.AccessRequest
		if reg.AccessRequestOIDCConfig() != nil || reg.AccessRequestTokenConfig() != nil {
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
				if cr != nil {
					ar.Spec.RequestRef = &commonapi.ObjectReference{
						Name:      cr.Name,
						Namespace: cr.Namespace,
					}
				} else if reg.ClusterReference() != nil {
					ar.Spec.ClusterRef = reg.ClusterReference().DeepCopy()
				} else if reg.ClusterRequestReference() != nil {
					ar.Spec.RequestRef = reg.ClusterRequestReference().DeepCopy()
				} else {
					return reconcile.Result{}, fmt.Errorf("no ClusterRequestSpec, ClusterReference or ClusterRequestReference specified for registration with id '%s'", reg.ID())
				}
			} else {
				rlog.Debug("Updating AccessRequest", "arName", ar.Name, "arNamespace", ar.Namespace)
			}
			if _, err := controllerutil.CreateOrUpdate(ctx, r.platformClusterClient, ar, func() error {
				ar.Labels = maputils.Merge(ar.Labels, expectedLabels)
				ar.Spec.Token = reg.AccessRequestTokenConfig()
				ar.Spec.OIDC = reg.AccessRequestOIDCConfig()
				return nil
			}); err != nil {
				return reconcile.Result{}, fmt.Errorf("unable to create or update AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
			}
			if !ar.Status.IsGranted() {
				rlog.Info("Waiting for AccessRequest to be granted", "arName", ar.Name, "arNamespace", ar.Namespace)
				return reconcile.Result{RequeueAfter: r.interval}, nil
			}
		}
	}
	log.Info("Successfully reconciled cluster access")

	return reconcile.Result{}, nil
}

// ReconcileDelete implements Reconciler.
func (r *reconcilerImpl) ReconcileDelete(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrDiscard(ctx).WithName("ClusterAccess")
	ctx = logging.NewContext(ctx, log)
	log.Info("Reconciling cluster access")

	// iterate over registrations and ensure that the corresponding resources are ready
	for _, reg := range r.registrations {
		rlog := log.WithValues("id", reg.ID())
		rlog.Debug("Processing registration")
		suffix := reg.Suffix()
		if suffix == "" {
			suffix = reg.ID()
		}
		expectedLabels := map[string]string{}
		if r.managedBy != "" {
			expectedLabels[openmcpconst.ManagedByLabel] = r.managedBy
		} else {
			expectedLabels[openmcpconst.ManagedByLabel] = r.controllerName
		}
		if r.managedPurpose != "" {
			expectedLabels[openmcpconst.ManagedPurposeLabel] = r.managedPurpose
		} else {
			expectedLabels[openmcpconst.ManagedPurposeLabel] = fmt.Sprintf("%s.%s.%s", request.Namespace, request.Name, reg.ID())
		}

		namespace, err := reg.NamespaceGenerator()(request.Name, request.Namespace)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("unable to generate name of platform cluster namespace for request '%s/%s': %w", request.Namespace, request.Name, err)
		}

		// if an AccessRequest is requested, ensure it is deleted
		if reg.AccessRequestOIDCConfig() != nil || reg.AccessRequestTokenConfig() != nil { //nolint:dupl
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
				if ar.DeletionTimestamp.IsZero() {
					rlog.Info("Deleting AccessRequest", "arName", ar.Name, "arNamespace", ar.Namespace)
					if err := r.platformClusterClient.Delete(ctx, ar); err != nil {
						return reconcile.Result{}, fmt.Errorf("unable to delete AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
					}
				} else {
					rlog.Info("Waiting for AccessRequest to be deleted", "arName", ar.Name, "arNamespace", ar.Namespace)
				}
				return reconcile.Result{RequeueAfter: r.interval}, nil
			}
		}

		// if a ClusterRequest is requested, ensure it is deleted
		if reg.ClusterRequestSpec() != nil { //nolint:dupl
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
				if cr.DeletionTimestamp.IsZero() {
					rlog.Info("Deleting ClusterRequest", "crName", cr.Name, "crNamespace", cr.Namespace)
					if err := r.platformClusterClient.Delete(ctx, cr); err != nil {
						return reconcile.Result{}, fmt.Errorf("unable to delete ClusterRequest '%s/%s': %w", cr.Namespace, cr.Name, err)
					}
				} else {
					rlog.Info("Waiting for ClusterRequest to be deleted", "crName", cr.Name, "crNamespace", cr.Namespace)
				}
				return reconcile.Result{RequeueAfter: r.interval}, nil
			}
		}

		// don't delete the namespace, because it might contain other resources
	}
	log.Info("Successfully reconciled cluster access")

	return reconcile.Result{}, nil
}

// Register implements Reconciler.
func (r *reconcilerImpl) Register(reg ClusterRegistration) Reconciler {
	r.registrations[reg.ID()] = reg
	return r
}

// WithManagedLabels implements Reconciler.
func (r *reconcilerImpl) WithManagedLabels(managedBy string, managedPurpose string) Reconciler {
	r.managedBy = managedBy
	r.managedPurpose = managedPurpose
	return r
}

// WithRetryInterval implements Reconciler.
func (r *reconcilerImpl) WithRetryInterval(interval time.Duration) Reconciler {
	r.interval = interval
	return r
}

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
