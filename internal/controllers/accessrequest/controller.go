package accessrequest

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
)

const ControllerName = "AccessRequest"

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

type ReconcileResult = ctrlutils.ReconcileResult[*clustersv1alpha1.AccessRequest, clustersv1alpha1.ConditionStatus]

func (r *AccessRequestReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(ControllerName)
	ctx = logging.NewContext(ctx, log)
	log.Info("Starting reconcile")
	rr := r.reconcile(ctx, req)
	// status update
	return ctrlutils.NewStatusUpdaterBuilder[*clustersv1alpha1.AccessRequest, clustersv1alpha1.RequestPhase, clustersv1alpha1.ConditionStatus]().
		WithNestedStruct("CommonStatus").
		WithFieldOverride(ctrlutils.STATUS_FIELD_PHASE, "Phase").
		WithoutFields(ctrlutils.STATUS_FIELD_CONDITIONS).
		WithPhaseUpdateFunc(func(obj *clustersv1alpha1.AccessRequest, rr ReconcileResult) (clustersv1alpha1.RequestPhase, error) {
			if rr.Reason == cconst.ReasonPreemptiveRequest {
				return clustersv1alpha1.REQUEST_DENIED, nil
			}
			return clustersv1alpha1.REQUEST_PENDING, nil
		}).
		Build().
		UpdateStatus(ctx, r.PlatformCluster.Client(), rr)
}

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

	// handle operation annotation
	if ar.GetAnnotations() != nil {
		op, ok := ar.GetAnnotations()[apiconst.OperationAnnotation]
		if ok {
			switch op {
			case apiconst.OperationAnnotationValueIgnore:
				log.Info("Ignoring resource due to ignore operation annotation")
				return ReconcileResult{}
			case apiconst.OperationAnnotationValueReconcile:
				log.Debug("Removing reconcile operation annotation from resource")
				if err := ctrlutils.EnsureAnnotation(ctx, r.PlatformCluster.Client(), ar, apiconst.OperationAnnotation, "", true, ctrlutils.DELETE); err != nil {
					return ReconcileResult{ReconcileError: errutils.WithReason(fmt.Errorf("error removing operation annotation: %w", err), cconst.ReasonPlatformClusterInteractionProblem)}
				}
			}
		}
	}

	rr := ReconcileResult{
		Object: ar,
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
		if cr.Spec.Preemptive {
			rr.Reason = cconst.ReasonPreemptiveRequest
			rr.Message = "The referenced ClusterRequest is preemptive and access cannot be granted."
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
		ar.Spec.ClusterRef = &clustersv1alpha1.NamespacedObjectReference{}
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
			predicate.Not(predicate.And(
				ctrlutils.HasLabelPredicate(clustersv1alpha1.ProviderLabel, ""),
				ctrlutils.HasLabelPredicate(clustersv1alpha1.ProfileLabel, ""),
			)),
			ctrlutils.LabelSelectorPredicate(r.Config.Selector.Completed()),
			predicate.Or(
				ctrlutils.GotAnnotationPredicate(apiconst.OperationAnnotation, apiconst.OperationAnnotationValueReconcile),
				ctrlutils.LostAnnotationPredicate(apiconst.OperationAnnotation, apiconst.OperationAnnotationValueIgnore),
			),
			predicate.Not(ctrlutils.HasAnnotationPredicate(apiconst.OperationAnnotation, apiconst.OperationAnnotationValueIgnore)),
		)).
		Complete(r)
}
