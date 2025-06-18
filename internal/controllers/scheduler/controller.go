package scheduler

import (
	"context"
	"fmt"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/collections/filters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	apiconst "github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/openmcp-project/openmcp-operator/internal/config"
)

const ControllerName = "Scheduler"

func NewClusterScheduler(setupLog *logging.Logger, platformCluster *clusters.Cluster, config *config.SchedulerConfig) (*ClusterScheduler, error) {
	if platformCluster == nil {
		return nil, fmt.Errorf("onboarding cluster must not be nil")
	}
	if config == nil {
		return nil, fmt.Errorf("scheduler config must not be nil")
	}
	if setupLog != nil {
		setupLog.WithName(ControllerName).Info("Initializing cluster scheduler", "scope", string(config.Scope), "strategy", string(config.Strategy), "knownPurposes", strings.Join(sets.List(sets.KeySet(config.PurposeMappings)), ","))
	}
	return &ClusterScheduler{
		Preemptive: PreemptiveScheduler{
			PlatformCluster: platformCluster,
			Config:          config,
		},
		PlatformCluster: platformCluster,
		Config:          config,
	}, nil
}

type ClusterScheduler struct {
	Preemptive      PreemptiveScheduler
	PlatformCluster *clusters.Cluster
	Config          *config.SchedulerConfig
}

var _ reconcile.Reconciler = &ClusterScheduler{}

type ReconcileResult = ctrlutils.ReconcileResult[*clustersv1alpha1.ClusterRequest, clustersv1alpha1.ConditionStatus]

func (r *ClusterScheduler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(ControllerName)
	ctx = logging.NewContext(ctx, log)
	log.Info("Starting reconcile")
	rr := r.reconcile(ctx, req)
	// status update
	return ctrlutils.NewStatusUpdaterBuilder[*clustersv1alpha1.ClusterRequest, clustersv1alpha1.RequestPhase, clustersv1alpha1.ConditionStatus]().
		WithNestedStruct("CommonStatus").
		WithFieldOverride(ctrlutils.STATUS_FIELD_PHASE, "Phase").
		WithoutFields(ctrlutils.STATUS_FIELD_CONDITIONS).
		WithPhaseUpdateFunc(func(obj *clustersv1alpha1.ClusterRequest, rr ReconcileResult) (clustersv1alpha1.RequestPhase, error) {
			if rr.ReconcileError != nil || rr.Object == nil || rr.Object.Status.Cluster == nil {
				return clustersv1alpha1.REQUEST_PENDING, nil
			}
			return clustersv1alpha1.REQUEST_GRANTED, nil
		}).
		Build().
		UpdateStatus(ctx, r.PlatformCluster.Client(), rr)
}

func (r *ClusterScheduler) reconcile(ctx context.Context, req reconcile.Request) ReconcileResult {
	log := logging.FromContextOrPanic(ctx)
	// get ClusterRequest resource
	cr := &clustersv1alpha1.ClusterRequest{}
	if err := r.PlatformCluster.Client().Get(ctx, req.NamespacedName, cr); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Resource not found")
			return ReconcileResult{}
		}
		return ReconcileResult{ReconcileError: errutils.WithReason(fmt.Errorf("unable to get resource '%s' from cluster: %w", req.String(), err), cconst.ReasonPlatformClusterInteractionProblem)}
	}

	// handle operation annotation
	if cr.GetAnnotations() != nil {
		op, ok := cr.GetAnnotations()[apiconst.OperationAnnotation]
		if ok {
			switch op {
			case apiconst.OperationAnnotationValueIgnore:
				log.Info("Ignoring resource due to ignore operation annotation")
				return ReconcileResult{}
			case apiconst.OperationAnnotationValueReconcile:
				log.Debug("Removing reconcile operation annotation from resource")
				if err := ctrlutils.EnsureAnnotation(ctx, r.PlatformCluster.Client(), cr, apiconst.OperationAnnotation, "", true, ctrlutils.DELETE); err != nil {
					return ReconcileResult{ReconcileError: errutils.WithReason(fmt.Errorf("error removing operation annotation: %w", err), cconst.ReasonPlatformClusterInteractionProblem)}
				}
			}
		}
	}

	inDeletion := !cr.DeletionTimestamp.IsZero()
	var rr ReconcileResult
	if !inDeletion {
		rr = r.handleCreateOrUpdate(ctx, req, cr)
	} else {
		rr = r.handleDelete(ctx, req, cr)
	}

	return rr
}

func (r *ClusterScheduler) handleCreateOrUpdate(ctx context.Context, req reconcile.Request, cr *clustersv1alpha1.ClusterRequest) ReconcileResult {
	log := logging.FromContextOrPanic(ctx)
	rr := ReconcileResult{
		Object:    cr,
		OldObject: cr.DeepCopy(),
	}

	log.Info("Creating/updating resource")

	// ensure finalizer
	if AddFinalizer(cr, clustersv1alpha1.ClusterRequestFinalizer, false) {
		log.Info("Adding finalizer")
		if err := r.PlatformCluster.Client().Patch(ctx, cr, client.MergeFrom(rr.OldObject)); err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error patching finalizer on resource '%s': %w", req.String(), err), cconst.ReasonPlatformClusterInteractionProblem)
			return rr
		}
	}

	// check if request is already granted
	if cr.Status.Cluster != nil {
		log.Info("Request already contains a cluster reference, nothing to do", "clusterName", cr.Status.Cluster.Name, "clusterNamespace", cr.Status.Cluster.Namespace)
		return rr
	}
	log.Debug("Request status does not contain a cluster reference, checking for existing clusters with referencing finalizers")

	// fetch cluster definition
	purpose := cr.Spec.Purpose
	cDef, ok := r.Config.PurposeMappings[purpose]
	if !ok {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("no cluster definition found for purpose '%s'", purpose), cconst.ReasonConfigurationProblem)
		return rr
	}

	clusters, rerr := fetchRelevantClusters(ctx, r.PlatformCluster.Client(), r.Config.Scope, cDef, cr.Spec.Purpose, cr.Namespace)
	if rerr != nil {
		rr.ReconcileError = rerr
		return rr
	}

	// // check if status was lost, but there exists a cluster that was already assigned to this request
	// reqFin := cr.FinalizerForCluster()
	var cluster *clustersv1alpha1.Cluster
	// for _, c := range clusters {
	// 	if slices.Contains(c.Finalizers, reqFin) {
	// 		cluster = c
	// 		break
	// 	}
	// }
	// if cluster != nil {
	// 	log.Info("Recovered cluster from referencing finalizer", "clusterName", cluster.Name, "clusterNamespace", cluster.Namespace)
	// 	rr.Object.Status.Cluster = &clustersv1alpha1.NamespacedObjectReference{}
	// 	rr.Object.Status.Cluster.Name = cluster.Name
	// 	rr.Object.Status.Cluster.Namespace = cluster.Namespace
	// 	return rr
	// }

	sr, err := Schedule(ctx, clusters, cDef, r.Config.Strategy, NewSchedulingRequest(string(cr.UID), 0, false, cr.Namespace, cr.Spec.Purpose))
	if err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("error scheduling request '%s': %w", req.String(), err), cconst.ReasonSchedulingFailed)
		return rr
	}

	// apply required changes to the cluster
	updated, err := sr.Apply(ctx, r.PlatformCluster.Client())
	if err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("error applying scheduling result for request '%s': %w", req.String(), err), cconst.ReasonPlatformClusterInteractionProblem)
		return rr
	}
	// identify the cluster that was chosen for this request
	for _, c := range updated {
		if slices.Contains(c.Finalizers, cr.FinalizerForCluster()) {
			cluster = c
			log.Debug("Request has been scheduled", "clusterName", cluster.Name, "clusterNamespace", cluster.Namespace)
			break
		}
	}
	if cluster == nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to determine cluster for request '%s', this should not happen", req.String()), cconst.ReasonSchedulingFailed)
		return rr
	}

	// add cluster reference to request
	rr.Object.Status.Cluster = &clustersv1alpha1.NamespacedObjectReference{}
	rr.Object.Status.Cluster.Name = cluster.Name
	rr.Object.Status.Cluster.Namespace = cluster.Namespace

	return rr
}

func (r *ClusterScheduler) handleDelete(ctx context.Context, req reconcile.Request, cr *clustersv1alpha1.ClusterRequest) ReconcileResult {
	log := logging.FromContextOrPanic(ctx)
	rr := ReconcileResult{
		Object:    cr,
		OldObject: cr.DeepCopy(),
	}

	log.Info("Deleting resource")

	// fetch cluster definition
	purpose := cr.Spec.Purpose
	cDef, ok := r.Config.PurposeMappings[purpose]
	if !ok {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("no cluster definition found for purpose '%s'", purpose), cconst.ReasonConfigurationProblem)
		return rr
	}

	// fetch relevant clusters
	clusters, rerr := fetchRelevantClusters(ctx, r.PlatformCluster.Client(), r.Config.Scope, cDef, cr.Spec.Purpose, cr.Namespace)
	if rerr != nil {
		rr.ReconcileError = rerr
		return rr
	}

	// run the scheduling algorithm to remove the finalizers from this request
	sr, err := Schedule(ctx, clusters, cDef, r.Config.Strategy, NewSchedulingRequest(string(cr.UID), 0, true, cr.Namespace, cr.Spec.Purpose))
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
	if RemoveFinalizer(cr, clustersv1alpha1.ClusterRequestFinalizer, true) {
		log.Info("Removing finalizer")
		if err := r.PlatformCluster.Client().Patch(ctx, cr, client.MergeFrom(rr.OldObject)); err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error removing finalizer from resource '%s': %w", req.String(), err), cconst.ReasonPlatformClusterInteractionProblem)
			return rr
		}
	}
	rr.Object = nil // this prevents the controller from trying to update an already deleted resource

	return rr
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterScheduler) SetupWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewControllerManagedBy(mgr).
		// watch ClusterRequest resources
		For(&clustersv1alpha1.ClusterRequest{}).
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
	if err != nil {
		return fmt.Errorf("error setting up scheduler: %w", err)
	}
	err = r.Preemptive.SetupWithManager(mgr)
	if err != nil {
		return fmt.Errorf("error setting up preemptive scheduler: %w", err)
	}
	return nil
}

// fetchRelevantClusters fetches all Cluster resources that could qualify for the given ClusterRequest.
// These are all clusters in the expected namespace (for namespaced mode) that match the definition's label selector and have the requested purpose.
// They are sorted by age, with the oldest cluster first. This reduces the chance of picking a cluster that is not ready yet because it was just created.
func fetchRelevantClusters(ctx context.Context, platformClient client.Client, scope config.SchedulerScope, cDef *config.ClusterDefinition, purpose, requestNamespace string) ([]*clustersv1alpha1.Cluster, errutils.ReasonableError) {
	// fetch clusters
	namespace := requestNamespace
	if scope == config.SCOPE_CLUSTER {
		// in cluster scope, search all namespaces
		namespace = ""
	} else if cDef.Template.Namespace != "" {
		// in namespaced scope, use template namespace if set, and request namespace otherwise
		namespace = cDef.Template.Namespace
	}
	clusterList := &clustersv1alpha1.ClusterList{}
	if err := platformClient.List(ctx, clusterList, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: cDef.Selector.Completed()}); err != nil {
		return nil, errutils.WithReason(fmt.Errorf("error listing Clusters: %w", err), cconst.ReasonPlatformClusterInteractionProblem)
	}
	clusters := make([]*clustersv1alpha1.Cluster, len(clusterList.Items))
	for i := range clusterList.Items {
		clusters[i] = &clusterList.Items[i]
	}

	// filter clusters by the desired purpose
	clusters = filters.FilterSlice(clusters, func(args ...any) bool {
		c, ok := args[0].(*clustersv1alpha1.Cluster)
		if !ok {
			return false
		}
		return slices.Contains(c.Spec.Purposes, purpose)
	})

	slices.SortStableFunc(clusters, func(a, b *clustersv1alpha1.Cluster) int {
		if a == nil || b == nil {
			return 0 // cannot compare nil clusters, should not happen
		}
		return a.CreationTimestamp.Compare(b.CreationTimestamp.Time)
	})

	return clusters, nil
}
