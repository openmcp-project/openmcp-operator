package utils

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	providerv1alpha1 "github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
)

// GetServiceProviderResource retrieves the ServiceProvider resource with the given name using the provided platform client.
func GetServiceProviderResource(ctx context.Context, platformClient client.Client, providerName string) (*providerv1alpha1.ServiceProvider, error) {
	serviceProvider := &providerv1alpha1.ServiceProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name: providerName,
		},
	}
	if err := platformClient.Get(ctx, client.ObjectKeyFromObject(serviceProvider), serviceProvider); err != nil {
		return nil, fmt.Errorf("failed to get service provider resource %s: %w", providerName, err)
	}
	return serviceProvider, nil
}

// RegisterGVKsAtServiceProvider updates the status.resources field of the ServiceProvider resource with the provided GroupVersionKinds.
// This can be used by service providers, for example in their init job, to register the kinds of resources they manage.
func RegisterGVKsAtServiceProvider(ctx context.Context, platformClient client.Client, providerName string, gvks ...metav1.GroupVersionKind) error {
	providerResource, err := GetServiceProviderResource(ctx, platformClient, providerName)
	if err != nil {
		return err
	}

	providerResourceOld := providerResource.DeepCopy()
	providerResource.Status.Resources = gvks
	if err := platformClient.Status().Patch(ctx, providerResource, client.MergeFrom(providerResourceOld)); err != nil {
		return fmt.Errorf("failed to patch platform service provider status: %w", err)
	}

	return nil
}

// IsClusterProviderResponsibleForAccessRequest checks whether the given AccessRequest should be handled by the ClusterProvider with the given name.
// True means that the ClusterProvider should reconcile the AccessRequest, false means that the generic AccessRequest controller has to do something first and the ClusterProvider must not act on it yet.
// It always returns false for nil AccessRequests and if the referenced Cluster is handled by a different ClusterProvider (unless the provider name is empty, in which case it is ignored).
// Otherwise, it depends on the state of the AccessRequest:
// - If the AccessRequest is missing either the 'clusters.openmcp.cloud/provider' or the 'clusters.openmcp.cloud/profile' label, it returns false.
// - If the AccessRequest's phase is neither 'Granted' nor 'Pending', it returns false.
// - If the AccessRequest's phase is 'Granted', but its observed generation differs from its current generation, it returns false.
// - In all other cases, it returns true.
func IsClusterProviderResponsibleForAccessRequest(ar *clustersv1alpha1.AccessRequest, providerName string) bool {
	if ar == nil {
		return false
	}
	// check labels
	provider, ok := ar.Labels[clustersv1alpha1.ProviderLabel]
	if !ok || (providerName != "" && provider != providerName) {
		return false
	}
	_, ok = ar.Labels[clustersv1alpha1.ProfileLabel]
	if !ok {
		return false
	}
	// phase check
	if ar.Status.Phase != clustersv1alpha1.REQUEST_GRANTED && ar.Status.Phase != clustersv1alpha1.REQUEST_PENDING {
		return false
	}
	// generation check
	if ar.Status.Phase == clustersv1alpha1.REQUEST_GRANTED && ar.Status.ObservedGeneration != ar.Generation {
		return false
	}
	return true
}

// AccessRequestPhasePredicate returns a predicate that filters AccessRequests by their phase.
// It is the typed version of AccessRequestPhasePredicateUntyped.
func AccessRequestPhasePredicate(phase string) predicate.TypedPredicate[*clustersv1alpha1.AccessRequest] {
	return &accessRequestPhasePredicate[*clustersv1alpha1.AccessRequest]{
		phase: phase,
		cast: func(obj *clustersv1alpha1.AccessRequest) *clustersv1alpha1.AccessRequest {
			return obj
		},
	}
}

// AccessRequestPhasePredicateUntyped returns a predicate that filters AccessRequests by their phase.
// It is the untyped version of AccessRequestPhasePredicate.
func AccessRequestPhasePredicateUntyped(phase string) predicate.Predicate {
	return &accessRequestPhasePredicate[client.Object]{
		phase: phase,
		cast: func(obj client.Object) *clustersv1alpha1.AccessRequest {
			ar, _ := obj.(*clustersv1alpha1.AccessRequest)
			return ar
		},
	}
}

type accessRequestPhasePredicate[Obj client.Object] struct {
	phase string
	cast  func(Obj) *clustersv1alpha1.AccessRequest
}

func (a *accessRequestPhasePredicate[Obj]) resolve(obj Obj) bool {
	ar := a.cast(obj)
	return ar != nil && ar.Status.Phase == a.phase
}

// Create implements predicate.TypedPredicate.
func (a *accessRequestPhasePredicate[Obj]) Create(e event.TypedCreateEvent[Obj]) bool {
	return a.resolve(e.Object)
}

// Delete implements predicate.TypedPredicate.
func (a *accessRequestPhasePredicate[Obj]) Delete(e event.TypedDeleteEvent[Obj]) bool {
	return a.resolve(e.Object)
}

// Generic implements predicate.TypedPredicate.
func (a *accessRequestPhasePredicate[Obj]) Generic(e event.TypedGenericEvent[Obj]) bool {
	return a.resolve(e.Object)
}

// Update implements predicate.TypedPredicate.
func (a *accessRequestPhasePredicate[Obj]) Update(e event.TypedUpdateEvent[Obj]) bool {
	return a.resolve(e.ObjectNew)
}
