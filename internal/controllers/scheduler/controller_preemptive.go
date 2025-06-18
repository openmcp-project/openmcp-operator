package scheduler

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

const PreemptiveControllerName = "PreemptiveScheduler"

type PreemptiveScheduler struct {
	PlatformCluster *clusters.Cluster
	Config          *config.SchedulerConfig
}

var _ reconcile.Reconciler = &PreemptiveScheduler{}

type PreemptiveReconcileResult = ctrlutils.ReconcileResult[*clustersv1alpha1.PreemptiveClusterRequest, clustersv1alpha1.ConditionStatus]

func (r *PreemptiveScheduler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(PreemptiveControllerName)
	ctx = logging.NewContext(ctx, log)
	log.Info("Starting reconcile")
	rr := r.reconcile(ctx, req)
	// status update
	return ctrlutils.NewStatusUpdaterBuilder[*clustersv1alpha1.PreemptiveClusterRequest, clustersv1alpha1.RequestPhase, clustersv1alpha1.ConditionStatus]().
		WithNestedStruct("CommonStatus").
		WithFieldOverride(ctrlutils.STATUS_FIELD_PHASE, "Phase").
		WithoutFields(ctrlutils.STATUS_FIELD_CONDITIONS).
		WithPhaseUpdateFunc(func(obj *clustersv1alpha1.PreemptiveClusterRequest, rr PreemptiveReconcileResult) (clustersv1alpha1.RequestPhase, error) {
			if rr.ReconcileError != nil || rr.Object == nil {
				return clustersv1alpha1.REQUEST_PENDING, nil
			}
			return clustersv1alpha1.REQUEST_GRANTED, nil
		}).
		Build().
		UpdateStatus(ctx, r.PlatformCluster.Client(), rr)
}

func (r *PreemptiveScheduler) reconcile(ctx context.Context, req reconcile.Request) PreemptiveReconcileResult {
	log := logging.FromContextOrPanic(ctx)
	// get ClusterRequest resource
	pcr := &clustersv1alpha1.PreemptiveClusterRequest{}
	if err := r.PlatformCluster.Client().Get(ctx, req.NamespacedName, pcr); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Resource not found")
			return PreemptiveReconcileResult{}
		}
		return PreemptiveReconcileResult{ReconcileError: errutils.WithReason(fmt.Errorf("unable to get resource '%s' from cluster: %w", req.String(), err), cconst.ReasonPlatformClusterInteractionProblem)}
	}

	// handle operation annotation
	if pcr.GetAnnotations() != nil {
		op, ok := pcr.GetAnnotations()[apiconst.OperationAnnotation]
		if ok {
			switch op {
			case apiconst.OperationAnnotationValueIgnore:
				log.Info("Ignoring resource due to ignore operation annotation")
				return PreemptiveReconcileResult{}
			case apiconst.OperationAnnotationValueReconcile:
				log.Debug("Removing reconcile operation annotation from resource")
				if err := ctrlutils.EnsureAnnotation(ctx, r.PlatformCluster.Client(), pcr, apiconst.OperationAnnotation, "", true, ctrlutils.DELETE); err != nil {
					return PreemptiveReconcileResult{ReconcileError: errutils.WithReason(fmt.Errorf("error removing operation annotation: %w", err), cconst.ReasonPlatformClusterInteractionProblem)}
				}
			}
		}
	}

	inDeletion := !pcr.DeletionTimestamp.IsZero()
	var rr PreemptiveReconcileResult
	if !inDeletion {
		rr = r.handleCreateOrUpdate(ctx, req, pcr)
	} else {
		rr = r.handleDelete(ctx, req, pcr)
	}

	return rr
}

func (r *PreemptiveScheduler) handleCreateOrUpdate(ctx context.Context, req reconcile.Request, pcr *clustersv1alpha1.PreemptiveClusterRequest) PreemptiveReconcileResult {
	log := logging.FromContextOrPanic(ctx)
	rr := PreemptiveReconcileResult{
		Object:    pcr,
		OldObject: pcr.DeepCopy(),
	}

	log.Info("Creating/updating resource")

	// ensure finalizer
	if AddFinalizer(pcr, clustersv1alpha1.ClusterRequestFinalizer, false) {
		log.Info("Adding finalizer")
		if err := r.PlatformCluster.Client().Patch(ctx, pcr, client.MergeFrom(rr.OldObject)); err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error patching finalizer on resource '%s': %w", req.String(), err), cconst.ReasonPlatformClusterInteractionProblem)
			return rr
		}
	}

	// fetch cluster definition
	purpose := pcr.Spec.Purpose
	cDef, ok := r.Config.PurposeMappings[purpose]
	if !ok {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("no cluster definition found for purpose '%s'", purpose), cconst.ReasonConfigurationProblem)
		return rr
	}

	clusters, rerr := fetchRelevantClusters(ctx, r.PlatformCluster.Client(), r.Config.Scope, cDef, pcr.Spec.Purpose, pcr.Namespace)
	if rerr != nil {
		rr.ReconcileError = rerr
		return rr
	}

	sr, err := Schedule(ctx, clusters, cDef, r.Config.Strategy, NewSchedulingRequest(string(pcr.UID), pcr.Spec.Workload, false, pcr.Namespace, pcr.Spec.Purpose))
	if err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("error scheduling request '%s': %w", req.String(), err), cconst.ReasonSchedulingFailed)
		return rr
	}

	// apply required changes to the cluster
	_, err = sr.Apply(ctx, r.PlatformCluster.Client())
	if err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("error applying scheduling result for request '%s': %w", req.String(), err), cconst.ReasonPlatformClusterInteractionProblem)
		return rr
	}

	return rr
}

func (r *PreemptiveScheduler) handleDelete(ctx context.Context, req reconcile.Request, pcr *clustersv1alpha1.PreemptiveClusterRequest) PreemptiveReconcileResult {
	log := logging.FromContextOrPanic(ctx)
	rr := PreemptiveReconcileResult{
		Object:    pcr,
		OldObject: pcr.DeepCopy(),
	}

	log.Info("Deleting resource")

	// fetch cluster definition
	purpose := pcr.Spec.Purpose
	cDef, ok := r.Config.PurposeMappings[purpose]
	if !ok {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("no cluster definition found for purpose '%s'", purpose), cconst.ReasonConfigurationProblem)
		return rr
	}

	// fetch relevant clusters
	clusters, rerr := fetchRelevantClusters(ctx, r.PlatformCluster.Client(), r.Config.Scope, cDef, pcr.Spec.Purpose, pcr.Namespace)
	if rerr != nil {
		rr.ReconcileError = rerr
		return rr
	}

	// run the scheduling algorithm to remove the finalizers from this request
	sr, err := Schedule(ctx, clusters, cDef, r.Config.Strategy, NewSchedulingRequest(string(pcr.UID), pcr.Spec.Workload, true, pcr.Namespace, pcr.Spec.Purpose))
	if err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("error unscheduling request '%s': %w", req.String(), err), cconst.ReasonSchedulingFailed)
		return rr
	}

	// apply required changes to the cluster
	_, err = sr.Apply(ctx, r.PlatformCluster.Client())
	if err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("error applying scheduling result for request '%s': %w", req.String(), err), cconst.ReasonPlatformClusterInteractionProblem)
		return rr
	}

	// remove finalizer
	if RemoveFinalizer(pcr, clustersv1alpha1.ClusterRequestFinalizer, true) {
		log.Info("Removing finalizer")
		if err := r.PlatformCluster.Client().Patch(ctx, pcr, client.MergeFrom(rr.OldObject)); err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error removing finalizer from resource '%s': %w", req.String(), err), cconst.ReasonPlatformClusterInteractionProblem)
			return rr
		}
	}
	rr.Object = nil // this prevents the controller from trying to update an already deleted resource

	return rr
}

// SetupWithManager sets up the controller with the Manager.
func (r *PreemptiveScheduler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// watch ClusterRequest resources
		For(&clustersv1alpha1.PreemptiveClusterRequest{}).
		WithEventFilter(predicate.And(
			ctrlutils.LabelSelectorPredicate(r.Config.Selectors.Requests.Completed()),
			predicate.Or(
				predicate.GenerationChangedPredicate{},
				ctrlutils.DeletionTimestampChangedPredicate{},
				ctrlutils.GotAnnotationPredicate(apiconst.OperationAnnotation, apiconst.OperationAnnotationValueReconcile),
				ctrlutils.LostAnnotationPredicate(apiconst.OperationAnnotation, apiconst.OperationAnnotationValueIgnore),
			),
			predicate.Not(ctrlutils.HasAnnotationPredicate(apiconst.OperationAnnotation, apiconst.OperationAnnotationValueIgnore)),
		)).
		Complete(r)
}

// ReschedulePreemptiveRequest fetches the ClusterRequest with the corresponding UID from the cluster and adds a reconcile annotation to it.
func ReschedulePreemptiveRequest(ctx context.Context, platformClient client.Client, uid string) errutils.ReasonableError {
	crl := &clustersv1alpha1.PreemptiveClusterRequestList{}
	if err := platformClient.List(ctx, crl); err != nil {
		return errutils.WithReason(fmt.Errorf("error listing PreemptiveClusterRequests: %w", err), cconst.ReasonPlatformClusterInteractionProblem)
	}
	var pcr *clustersv1alpha1.PreemptiveClusterRequest
	for _, elem := range crl.Items {
		if string(elem.UID) == uid {
			pcr = &elem
			break
		}
	}
	if pcr == nil {
		return errutils.WithReason(fmt.Errorf("unable to find PreemptiveClusterRequest with UID '%s'", uid), cconst.ReasonInternalError)
	}
	if err := ctrlutils.EnsureAnnotation(ctx, platformClient, pcr, apiconst.OperationAnnotation, apiconst.OperationAnnotationValueReconcile, true); err != nil {
		return errutils.WithReason(fmt.Errorf("error adding reconcile annotation to PreemptiveClusterRequest '%s': %w", pcr.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
	}
	return nil
}
