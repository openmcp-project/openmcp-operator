package accessrequest

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	apiconst "github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/openmcp-project/openmcp-operator/internal/config"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

const (
	ControllerName      = "AccessRequest"
	allClusterProviders = ""
)

func NewAccessRequestReconciler(platformCluster *clusters.Cluster, cfg *config.AccessRequestConfig) *AccessRequestReconciler {
	if cfg == nil {
		cfg = &config.AccessRequestConfig{}
	}
	return &AccessRequestReconciler{
		PlatformCluster: platformCluster,
		Config:          cfg,
	}
}

type AccessRequestReconciler struct {
	PlatformCluster *clusters.Cluster
	Config          *config.AccessRequestConfig
}

var _ reconcile.Reconciler = &AccessRequestReconciler{}

type ReconcileResult = ctrlutils.ReconcileResult[*clustersv1alpha1.AccessRequest]

func (r *AccessRequestReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(ControllerName)
	ctx = logging.NewContext(ctx, log)
	log.Info("Starting reconcile")
	rr := r.reconcile(ctx, req)
	// status update
	return ctrlutils.NewOpenMCPStatusUpdaterBuilder[*clustersv1alpha1.AccessRequest]().
		WithNestedStruct("Status").
		WithoutFields(ctrlutils.STATUS_FIELD_CONDITIONS).
		WithPhaseUpdateFunc(func(obj *clustersv1alpha1.AccessRequest, rr ctrlutils.ReconcileResult[*clustersv1alpha1.AccessRequest]) (string, error) {
			return clustersv1alpha1.REQUEST_PENDING, nil
		}).
		Build().
		UpdateStatus(ctx, r.PlatformCluster.Client(), rr)
}

//nolint:gocyclo
func (r *AccessRequestReconciler) reconcile(ctx context.Context, req reconcile.Request) ReconcileResult {
	log := logging.FromContextOrPanic(ctx)
	// get AccessRequest resource
	ar := &clustersv1alpha1.AccessRequest{}
	if err := r.PlatformCluster.Client().Get(ctx, req.NamespacedName, ar); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Resource not found")
			return ReconcileResult{}
		}
		return ReconcileResult{ReconcileError: errutils.WithReason(fmt.Errorf("unable to get resource '%s' from cluster: %w", req.String(), err), cconst.ReasonPlatformClusterInteractionProblem)}
	}

	// don't act on AccessRequest if the ClusterProvider is responsible for it
	ttlCheckOnly := false

	if libutils.IsClusterProviderResponsibleForAccessRequest(ar, allClusterProviders) {
		if ar.Spec.TTL != nil {
			log.Debug("Controller is not responsible for reconciling this AccessRequest, just checking TTL expiration")
			ttlCheckOnly = true
		} else {
			log.Info("AccessRequest controller is not responsible for AccessRequest, skipping reconciliation")
			return ReconcileResult{}
		}
	}

	// handle operation annotation
	if ar.GetAnnotations() != nil {
		op, ok := ar.GetAnnotations()[apiconst.OperationAnnotation]
		if ok {
			switch op {
			case apiconst.OperationAnnotationValueIgnore:
				log.Info("Ignoring resource due to ignore operation annotation")
				return ReconcileResult{}
			case apiconst.OperationAnnotationValueReconcile:
				// ignore reconcile annotation if this is only a TTL check
				if ttlCheckOnly {
					break
				}
				log.Debug("Removing reconcile operation annotation from resource")
				if err := ctrlutils.EnsureAnnotation(ctx, r.PlatformCluster.Client(), ar, apiconst.OperationAnnotation, "", true, ctrlutils.DELETE); err != nil {
					return ReconcileResult{ReconcileError: errutils.WithReason(fmt.Errorf("error removing operation annotation: %w", err), cconst.ReasonPlatformClusterInteractionProblem)}
				}
			}
		}
	}

	rr := ReconcileResult{}
	if !ttlCheckOnly {
		// avoid updating the status if this reconciliation is only a TTL check
		rr.Object = ar
	}

	// ignore AccessRequests that are in deletion
	if !ar.GetDeletionTimestamp().IsZero() {
		log.Info("Ignoring AccessRequest, because it is in deletion")
		return ReconcileResult{}
	}

	// check if TTL is set and has expired
	if ar.Spec.TTL != nil {
		isExpired, expirationTime := hasExpiredTTL(ar)
		if isExpired {
			log.Info("AccessRequest TTL has expired, deleting AccessRequest", "expirationTime", expirationTime, "creationTime", ar.GetCreationTimestamp(), "ttl", ar.Spec.TTL.Duration)
			if err := r.PlatformCluster.Client().Delete(ctx, ar); err != nil {
				rr.ReconcileError = errutils.WithReason(fmt.Errorf("error deleting AccessRequest after TTL expiration: %w", err), cconst.ReasonPlatformClusterInteractionProblem)
				return rr
			}
			// don't update the status after deletion, could become a race condition
			rr.Object = nil
			return rr
		}
		log.Debug("AccessRequest TTL not yet expired", "expirationTime", expirationTime)
		// compute requeue after duration, which is the remaining time until expiration plus one second
		rr.Result.RequeueAfter = time.Until(expirationTime) + time.Second
	}

	// check if this reconciliation was just a TTL check and no further action is required
	if ttlCheckOnly {
		log.Debug("Finished TTL check reconciliation, skipping further processing")
		return rr
	}
	if ctrlutils.HasLabel(ar, clustersv1alpha1.ProviderLabel) && ctrlutils.HasLabel(ar, clustersv1alpha1.ProfileLabel) && ar.Spec.ClusterRef != nil {
		log.Info("AccessRequest already has provider and profile labels and a clusterRef, no further action required")
		return rr
	}

	// get Cluster that the request refers to
	c := &clustersv1alpha1.Cluster{}
	if ar.Spec.ClusterRef != nil {
		c.SetName(ar.Spec.ClusterRef.Name)
		c.SetNamespace(ar.Spec.ClusterRef.Namespace)
		log.Debug("Cluster is referenced in AccessRequest", "clusterName", c.Name, "clusterNamespace", c.Namespace)
	} else if ar.Spec.RequestRef != nil {
		// fetch request to lookup the cluster reference
		cr := &clustersv1alpha1.ClusterRequest{}
		cr.SetName(ar.Spec.RequestRef.Name)
		cr.SetNamespace(ar.Spec.RequestRef.Namespace)
		log.Debug("ClusterRequest is referenced in AccessRequest", "clusterRequestName", cr.Name, "clusterRequestNamespace", cr.Namespace)
		if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(cr), cr); err != nil {
			if apierrors.IsNotFound(err) {
				rr.ReconcileError = errutils.WithReason(fmt.Errorf("ClusterRequest '%s/%s' not found", cr.Namespace, cr.Name), cconst.ReasonInvalidReference)
				return rr
			}
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to get ClusterRequest '%s/%s': %w", cr.Namespace, cr.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
			return rr
		}
		if cr.Status.Phase != clustersv1alpha1.REQUEST_GRANTED {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("ClusterRequest '%s/%s' is not granted", cr.Namespace, cr.Name), cconst.ReasonInvalidReference)
			return rr
		}
		if cr.Status.Cluster == nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("ClusterRequest '%s/%s' is granted but does not reference a cluster", cr.Namespace, cr.Name), cconst.ReasonInternalError)
			return rr
		}
		c.SetName(cr.Status.Cluster.Name)
		c.SetNamespace(cr.Status.Cluster.Namespace)
		log.Debug("Retrieved Cluster reference from ClusterRequest", "clusterName", c.Name, "clusterNamespace", c.Namespace)
	} else {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("invalid AccessRequest resource '%s/%s': neither clusterRef nor requestRef is set", ar.Namespace, ar.Name), cconst.ReasonConfigurationProblem)
		return rr
	}

	// fetch Cluster resource
	if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(c), c); err != nil {
		if apierrors.IsNotFound(err) {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("referenced Cluster '%s/%s' not found", c.Namespace, c.Name), cconst.ReasonInvalidReference)
			return rr
		}
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to get Cluster '%s/%s': %w", c.Namespace, c.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
		return rr
	}

	// fetch ClusterProfile resource
	cp := &clustersv1alpha1.ClusterProfile{}
	cp.SetName(c.Spec.Profile)
	log.Debug("Fetching ClusterProfile", "profileName", cp.Name)
	if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(cp), cp); err != nil {
		if apierrors.IsNotFound(err) {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("ClusterProfile '%s' not found", cp.Name), cconst.ReasonInvalidReference)
			return rr
		}
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to get ClusterProfile '%s': %w", cp.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
		return rr
	}

	log.Info("Identified responsible ClusterProvider", "providerName", cp.Spec.ProviderRef.Name, "profileName", cp.Name)
	arOld := ar.DeepCopy()
	if err := ctrlutils.EnsureLabel(ctx, nil, ar, clustersv1alpha1.ProviderLabel, cp.Spec.ProviderRef.Name, false); err != nil {
		if e, ok := err.(*ctrlutils.MetadataEntryAlreadyExistsError); ok {
			log.Error(err, "label '%s' already set on resource '%s', but with value '%s' instead of the desired value '%s'", e.Key, req.String(), e.ActualValue, e.DesiredValue)
			return rr
		}
	}
	if err := ctrlutils.EnsureLabel(ctx, nil, ar, clustersv1alpha1.ProfileLabel, cp.Name, false); err != nil {
		if e, ok := err.(*ctrlutils.MetadataEntryAlreadyExistsError); ok {
			log.Error(err, "label '%s' already set on resource '%s', but with value '%s' instead of the desired value '%s'", e.Key, req.String(), e.ActualValue, e.DesiredValue)
			return rr
		}
	}

	// set cluster reference, if only the request reference is set
	if ar.Spec.ClusterRef == nil {
		log.Info("Setting cluster reference in AccessRequest", "clusterName", c.Name, "clusterNamespace", c.Namespace)
		ar.Spec.ClusterRef = &commonapi.ObjectReference{}
		ar.Spec.ClusterRef.Name = c.Name
		ar.Spec.ClusterRef.Namespace = c.Namespace
	}

	if err := r.PlatformCluster.Client().Patch(ctx, ar, client.MergeFrom(arOld)); err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("error patching resource '%s': %w", req.String(), err), cconst.ReasonPlatformClusterInteractionProblem)
		return rr
	}

	return rr
}

// SetupWithManager sets up the controller with the Manager.
func (r *AccessRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// watch AccessRequest resources
		For(&clustersv1alpha1.AccessRequest{}).
		WithEventFilter(predicate.And(
			// ignore resources that already have the provider AND profile label set
			// unless the TTL is set, in which case we need to check for expiration
			predicate.Or(
				predicate.NewPredicateFuncs(func(obj client.Object) bool {
					return !libutils.IsClusterProviderResponsibleForAccessRequest(obj.(*clustersv1alpha1.AccessRequest), "")
				}),
				hasTTLPredicate(),
			),
			ctrlutils.LabelSelectorPredicate(r.Config.Selector.Completed()),
			predicate.Or(
				ctrlutils.GotAnnotationPredicate(apiconst.OperationAnnotation, apiconst.OperationAnnotationValueReconcile),
				ctrlutils.LostAnnotationPredicate(apiconst.OperationAnnotation, apiconst.OperationAnnotationValueIgnore),
			),
			predicate.Not(ctrlutils.HasAnnotationPredicate(apiconst.OperationAnnotation, apiconst.OperationAnnotationValueIgnore)),
		)).
		Complete(r)
}

// hasTTLPredicate returns true if the AccessRequest is not in deletion and has a TTL set.
func hasTTLPredicate() predicate.Predicate {
	hasTTL := func(obj client.Object) bool {
		ar, ok := obj.(*clustersv1alpha1.AccessRequest)
		if !ok {
			return false
		}
		if !ar.DeletionTimestamp.IsZero() {
			return false // no need to reconcile already deleted objects here
		}
		return ar.Spec.TTL != nil
	}
	return predicate.Funcs{
		UpdateFunc: func(tue event.TypedUpdateEvent[client.Object]) bool {
			return hasTTL(tue.ObjectNew)
		},
		CreateFunc: func(tce event.TypedCreateEvent[client.Object]) bool {
			return hasTTL(tce.Object)
		},
		DeleteFunc: func(tde event.TypedDeleteEvent[client.Object]) bool {
			return false
		},
		GenericFunc: func(tge event.TypedGenericEvent[client.Object]) bool {
			return hasTTL(tge.Object)
		},
	}
}

// hasExpiredTTL returns true if the AccessRequest has a TTL set and the TTL has expired.
// If the AccessRequest has a TTL set, the second return value is the expiration time, otherwise it is the zero time.
func hasExpiredTTL(ar *clustersv1alpha1.AccessRequest) (bool, time.Time) {
	if ar == nil {
		return false, time.Time{}
	}
	if ar.Spec.TTL == nil {
		return false, time.Time{}
	}
	expirationTime := ar.GetCreationTimestamp().Add(ar.Spec.TTL.Duration)
	return time.Now().After(expirationTime), expirationTime
}
