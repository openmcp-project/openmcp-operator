//nolint:revive
package advanced

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

//////////////////
/// INTERFACES ///
//////////////////

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

// ClusterRequestSpecGenerator is a function that takes the reconcile.Request and arbitrary additional arguments and generates a ClusterRequestSpec.
// Request and additional arguments depend on the arguments the ClusterAccessReconciler's Reconcile method is called with.
type ClusterRequestSpecGenerator func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.ClusterRequestSpec, error)

// NewClusterRequest instructs the Reconciler to create and manage a new ClusterRequest.
func NewClusterRequest(id, suffix string, generateClusterRequestSpec ClusterRequestSpecGenerator) ClusterRegistrationBuilder {
	return &clusterRegistrationBuilderImpl{
		clusterRegistrationImpl: clusterRegistrationImpl{
			id:                         id,
			suffix:                     suffix,
			generateClusterRequestSpec: generateClusterRequestSpec,
			generateNamespace:          DefaultNamespaceGenerator,
		},
	}
}

// ObjectReferenceGenerator is a function that takes the reconcile.Request and arbitrary additional arguments and generates an ObjectReference.
// Request and additional arguments depend on the arguments the ClusterAccessReconciler's Reconcile method is called with.
// The kind of the object the reference refers to depends on the method the function is passed into.
type ObjectReferenceGenerator func(req reconcile.Request, additionalData ...any) (*commonapi.ObjectReference, error)

// ExistingCluster instructs the Reconciler to use an existing Cluster resource.
// The given generateClusterRef function is used to generate the reference to the Cluster resource.
func ExistingCluster(id, suffix string, generateClusterRef ObjectReferenceGenerator) ClusterRegistrationBuilder {
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
// The given generateClusterRequestRef function is used to generate the reference to the ClusterRequest resource.
func ExistingClusterRequest(id, suffix string, generateClusterRequestRef ObjectReferenceGenerator) ClusterRegistrationBuilder {
	return &clusterRegistrationBuilderImpl{
		clusterRegistrationImpl: clusterRegistrationImpl{
			id:                        id,
			suffix:                    suffix,
			generateClusterRequestRef: generateClusterRequestRef,
			generateNamespace:         DefaultNamespaceGenerator,
		},
	}
}
