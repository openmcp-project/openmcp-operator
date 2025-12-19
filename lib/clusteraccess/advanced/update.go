//nolint:revive
package advanced

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
)

//////////////////
/// INTERFACES ///
//////////////////

type UpdatableClusterRegistration interface {
	ClusterRegistration

	// UpdateTokenAccess updates an AccessRequest to use token-based access.
	// Use this method, if the token configuration does not depend on the reconcile request, use UpdateTokenAccessGenerator otherwise.
	// Calling this method will override any previous calls to UpdateTokenAccess, UpdateTokenAccessGenerator, UpdateOIDCAccess, or UpdateOIDCAccessGenerator.
	// Passing in a nil cfg will disable AccessRequest creation, if it was token-based before, and have no effect if it was OIDC-based before.
	UpdateTokenAccess(cfg *clustersv1alpha1.TokenConfig) UpdatableClusterRegistration
	// UpdateTokenAccessGenerator is like UpdateTokenAccess, but takes a function that generates the TokenConfig based on the reconcile request.
	// Calling this method will override any previous calls to UpdateTokenAccess, UpdateTokenAccessGenerator, UpdateOIDCAccess, or UpdateOIDCAccessGenerator.
	// Passing in a nil function will disable AccessRequest creation, if it was token-based before, and have no effect if it was OIDC-based before.
	UpdateTokenAccessGenerator(f func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.TokenConfig, error)) UpdatableClusterRegistration
	// UpdateOIDCAccess updates an AccessRequest to use OIDC-based access.
	// Use this method, if the OIDC configuration does not depend on the reconcile request, use UpdateOIDCAccessGenerator otherwise.
	// Calling this method will override any previous calls to UpdateTokenAccess, UpdateTokenAccessGenerator, UpdateOIDCAccess, or UpdateOIDCAccessGenerator.
	// Passing in a nil cfg will disable AccessRequest creation, if it was OIDC-based before, and have no effect if it was token-based before.
	UpdateOIDCAccess(cfg *clustersv1alpha1.OIDCConfig) UpdatableClusterRegistration
	// UpdateOIDCAccessGenerator is like UpdateOIDCAccess, but takes a function that generates the OIDCConfig based on the reconcile request.
	// Calling this method will override any previous calls to UpdateTokenAccess, UpdateTokenAccessGenerator, UpdateOIDCAccess, or UpdateOIDCAccessGenerator.
	// Passing in a nil function will disable AccessRequest creation, if it was OIDC-based before, and have no effect if it was token-based before.
	UpdateOIDCAccessGenerator(f func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.OIDCConfig, error)) UpdatableClusterRegistration
	// UpdateScheme sets the scheme for the Kubernetes client of the cluster.
	// If not set or set to nil, the default scheme will be used.
	UpdateScheme(scheme *runtime.Scheme) UpdatableClusterRegistration
}

type ClusterRegistrationUpdate interface {
	// Apply applies the update to the given ClusterRegistration.
	// The update will only take effect after the next reconciliation.
	Apply(ureg UpdatableClusterRegistration) error
}

////////////////////////////////////////////////////
/// IMPLEMENTATION: UpdatableClusterRegistration ///
////////////////////////////////////////////////////

var _ UpdatableClusterRegistration = &clusterRegistrationImpl{}

// UpdateOIDCAccess implements UpdatableClusterRegistration.
func (c *clusterRegistrationImpl) UpdateOIDCAccess(cfg *clustersv1alpha1.OIDCConfig) UpdatableClusterRegistration {
	var f func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.OIDCConfig, error)
	if cfg != nil {
		f = func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.OIDCConfig, error) {
			return cfg.DeepCopy(), nil
		}
	}
	return c.UpdateOIDCAccessGenerator(f)
}

// UpdateOIDCAccessGenerator implements UpdatableClusterRegistration.
func (c *clusterRegistrationImpl) UpdateOIDCAccessGenerator(f func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.OIDCConfig, error)) UpdatableClusterRegistration {
	c.generateAccessOIDCConfig = f
	if f != nil && c.generateAccessTokenConfig != nil {
		// both access methods configured, disable token
		c.generateAccessTokenConfig = nil
	}
	return c
}

// UpdateTokenAccess implements UpdatableClusterRegistration.
func (c *clusterRegistrationImpl) UpdateTokenAccess(cfg *clustersv1alpha1.TokenConfig) UpdatableClusterRegistration {
	var f func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.TokenConfig, error)
	if cfg != nil {
		f = func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.TokenConfig, error) {
			return cfg.DeepCopy(), nil
		}
	}
	return c.UpdateTokenAccessGenerator(f)
}

// UpdateTokenAccessGenerator implements UpdatableClusterRegistration.
func (c *clusterRegistrationImpl) UpdateTokenAccessGenerator(f func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.TokenConfig, error)) UpdatableClusterRegistration {
	c.generateAccessTokenConfig = f
	if f != nil && c.generateAccessOIDCConfig != nil {
		// both access methods configured, disable OIDC
		c.generateAccessOIDCConfig = nil
	}
	return c
}

// UpdateScheme implements UpdatableClusterRegistration.
func (c *clusterRegistrationImpl) UpdateScheme(scheme *runtime.Scheme) UpdatableClusterRegistration {
	c.scheme = scheme
	return c
}

/////////////////////////////////////////////////
/// IMPLEMENTATION: ClusterRegistrationUpdate ///
/////////////////////////////////////////////////

type clusterRegistrationUpdateImpl struct {
	updateFunc func(ureg UpdatableClusterRegistration) error
}

var _ ClusterRegistrationUpdate = &clusterRegistrationUpdateImpl{}

// Apply implements ClusterRegistrationUpdate.
func (u *clusterRegistrationUpdateImpl) Apply(ureg UpdatableClusterRegistration) error {
	return u.updateFunc(ureg)
}

func GenericUpdate(updateFunc func(ureg UpdatableClusterRegistration) error) ClusterRegistrationUpdate {
	return &clusterRegistrationUpdateImpl{
		updateFunc: updateFunc,
	}
}

func UpdateOIDCAccess(cfg *clustersv1alpha1.OIDCConfig) ClusterRegistrationUpdate {
	return GenericUpdate(func(ureg UpdatableClusterRegistration) error {
		ureg.UpdateOIDCAccess(cfg)
		return nil
	})
}

func UpdateOIDCAccessGenerator(f func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.OIDCConfig, error)) ClusterRegistrationUpdate {
	return GenericUpdate(func(ureg UpdatableClusterRegistration) error {
		ureg.UpdateOIDCAccessGenerator(f)
		return nil
	})
}

func UpdateTokenAccess(cfg *clustersv1alpha1.TokenConfig) ClusterRegistrationUpdate {
	return GenericUpdate(func(ureg UpdatableClusterRegistration) error {
		ureg.UpdateTokenAccess(cfg)
		return nil
	})
}

func UpdateTokenAccessGenerator(f func(req reconcile.Request, additionalData ...any) (*clustersv1alpha1.TokenConfig, error)) ClusterRegistrationUpdate {
	return GenericUpdate(func(ureg UpdatableClusterRegistration) error {
		ureg.UpdateTokenAccessGenerator(f)
		return nil
	})
}
