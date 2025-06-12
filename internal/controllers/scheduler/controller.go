package scheduler

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
		PlatformCluster: platformCluster,
		Config:          config,
	}, nil
}

type ClusterScheduler struct {
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
		WithCustomUpdateFunc(func(obj *clustersv1alpha1.ClusterRequest, rr ctrlutils.ReconcileResult[*clustersv1alpha1.ClusterRequest, clustersv1alpha1.ConditionStatus]) error {
			// make sure to never write a cluster reference into the status for preemptive requests
			if rr.Object != nil && rr.Object.Spec.Preemptive {
				rr.Object.Status.Cluster = nil
			}
			return nil
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
	if controllerutil.AddFinalizer(cr, clustersv1alpha1.ClusterRequestFinalizer) {
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

	clusters, rerr := r.fetchRelevantClusters(ctx, cr, cDef)
	if rerr != nil {
		rr.ReconcileError = rerr
		return rr
	}

	// check if status was lost, but there exists a cluster that was already assigned to this request
	reqFin := cr.FinalizerForCluster()
	var cluster *clustersv1alpha1.Cluster
	for _, c := range clusters {
		if slices.Contains(c.Finalizers, reqFin) {
			cluster = c
			break
		}
	}
	if cluster != nil {
		log.Info("Recovered cluster from referencing finalizer", "clusterName", cluster.Name, "clusterNamespace", cluster.Namespace)
		rr.Object.Status.Cluster = &clustersv1alpha1.NamespacedObjectReference{}
		rr.Object.Status.Cluster.Name = cluster.Name
		rr.Object.Status.Cluster.Namespace = cluster.Namespace
		return rr
	}

	// if no cluster was found, check if there is an existing cluster that qualifies for the request
	// skip this check for preemptive requests with purposes with exclusive tenancy, as they will always result in a new cluster
	if !(cDef.IsExclusive() && cr.Spec.Preemptive) {
		cluster, rerr = r.pickFittingCluster(ctx, cr, clusters, cDef)
		if rerr != nil {
			rr.ReconcileError = rerr
			return rr
		}
	}

	if cluster != nil {
		log.Info("Existing cluster qualifies for request, using it", "clusterName", cluster.Name, "clusterNamespace", cluster.Namespace)

		// patch finalizer into Cluster
		oldCluster := cluster.DeepCopy()
		fin := cr.FinalizerForCluster()
		changed := controllerutil.AddFinalizer(cluster, fin) // should always be true at this point
		replacedPreemptiveUID := ""
		if !cr.Spec.Preemptive && !cDef.IsSharedUnlimitedly() {
			// if the request is not preemptive, it can potentially replace a preemptive request that was already assigned to this cluster
			// this check can be skipped for clusters which are shared unlimitedly, as the preemptive request would point to the same cluster again
			suffix, removed := RemoveFinalizerWithPrefix(cluster, clustersv1alpha1.PreemptiveRequestFinalizerOnClusterPrefix)
			replacedPreemptiveUID = suffix
			if removed {
				log.Info("Replacing preemptive request", "uid", replacedPreemptiveUID)
				if rerr := r.ReschedulePreemptiveRequest(ctx, replacedPreemptiveUID); rerr != nil {
					if rerr.Reason() == cconst.ReasonInternalError {
						// if the problem is that the preemptive request was not found, only log an error
						log.Error(rerr, "Error rescheduling preemptive request")
					} else {
						rr.ReconcileError = rerr
						return rr
					}
				}
			}
		}
		if changed {
			log.Debug("Adding finalizer to cluster", "clusterName", cluster.Name, "clusterNamespace", cluster.Namespace, "finalizer", fin, "replacedPreemptiveUID", replacedPreemptiveUID)
			if err := r.PlatformCluster.Client().Patch(ctx, cluster, client.MergeFrom(oldCluster)); err != nil {
				rr.ReconcileError = errutils.WithReason(fmt.Errorf("error patching finalizer '%s' on cluster '%s/%s': %w", fin, cluster.Namespace, cluster.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
				return rr
			}
		}
	} else {
		cluster = r.initializeNewCluster(ctx, cr, cDef)

		// create Cluster resource
		if err := r.PlatformCluster.Client().Create(ctx, cluster); err != nil {
			if apierrors.IsAlreadyExists(err) {
				rr.ReconcileError = errutils.WithReason(fmt.Errorf("cluster '%s/%s' already exists, this is not supposed to happen", cluster.Namespace, cluster.Name), cconst.ReasonInternalError)
				return rr
			}
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error creating cluster '%s/%s': %w", cluster.Namespace, cluster.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
			return rr
		}
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

	// fetch all clusters and filter for the ones that have a finalizer from this request
	fin := cr.FinalizerForCluster()
	clusterList := &clustersv1alpha1.ClusterList{}
	if err := r.PlatformCluster.Client().List(ctx, clusterList, client.MatchingLabelsSelector{Selector: r.Config.Selectors.Clusters.Completed()}); err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("error listing Clusters: %w", err), cconst.ReasonPlatformClusterInteractionProblem)
		return rr
	}
	clusters := make([]*clustersv1alpha1.Cluster, len(clusterList.Items))
	for i := range clusterList.Items {
		clusters[i] = &clusterList.Items[i]
	}
	clusters = filters.FilterSlice(clusters, func(args ...any) bool {
		c, ok := args[0].(*clustersv1alpha1.Cluster)
		if !ok {
			return false
		}
		return slices.Contains(c.Finalizers, fin)
	})

	// remove finalizer from all clusters
	errs := errutils.NewReasonableErrorList()
	for _, c := range clusters {
		log.Debug("Removing finalizer from cluster", "clusterName", c.Name, "clusterNamespace", c.Namespace, "finalizer", fin)
		oldCluster := c.DeepCopy()
		if controllerutil.RemoveFinalizer(c, fin) {
			if err := r.PlatformCluster.Client().Patch(ctx, c, client.MergeFrom(oldCluster)); err != nil {
				errs.Append(errutils.WithReason(fmt.Errorf("error patching finalizer '%s' on cluster '%s/%s': %w", fin, c.Namespace, c.Name, err), cconst.ReasonPlatformClusterInteractionProblem))
			}
		}
		if c.GetTenancyCount() == 0 && c.GetPreemptiveTenancyCount() == 0 && ctrlutils.HasLabelWithValue(c, clustersv1alpha1.DeleteWithoutRequestsLabel, "true") {
			log.Info("Deleting cluster without requests", "clusterName", c.Name, "clusterNamespace", c.Namespace)
			if err := r.PlatformCluster.Client().Delete(ctx, c); err != nil {
				if apierrors.IsNotFound(err) {
					log.Info("Cluster already deleted", "clusterName", c.Name, "clusterNamespace", c.Namespace)
				} else {
					errs.Append(errutils.WithReason(fmt.Errorf("error deleting cluster '%s/%s': %w", c.Namespace, c.Name, err), cconst.ReasonPlatformClusterInteractionProblem))
				}
			}
		}
	}
	rr.ReconcileError = errs.Aggregate()
	if rr.ReconcileError != nil {
		return rr
	}

	// remove finalizer
	if controllerutil.RemoveFinalizer(cr, clustersv1alpha1.ClusterRequestFinalizer) {
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
	return ctrl.NewControllerManagedBy(mgr).
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
}

// fetchRelevantClusters fetches all Cluster resources that could qualify for the given ClusterRequest.
// These are all clusters in the expected namespace (for namespaced mode) that match the definition's label selector and have the requested purpose.
// They are sorted by age, with the oldest cluster first. This reduces the chance of picking a cluster that is not ready yet because it was just created.
func (r *ClusterScheduler) fetchRelevantClusters(ctx context.Context, cr *clustersv1alpha1.ClusterRequest, cDef *config.ClusterDefinition) ([]*clustersv1alpha1.Cluster, errutils.ReasonableError) {
	// fetch clusters
	purpose := cr.Spec.Purpose
	namespace := cr.Namespace
	if r.Config.Scope == config.SCOPE_CLUSTER {
		// in cluster scope, search all namespaces
		namespace = ""
	} else if cDef.Template.Namespace != "" {
		// in namespaced scope, use template namespace if set, and request namespace otherwise
		namespace = cDef.Template.Namespace
	}
	clusterList := &clustersv1alpha1.ClusterList{}
	if err := r.PlatformCluster.Client().List(ctx, clusterList, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: cDef.Selector.Completed()}); err != nil {
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
		return a.CreationTimestamp.Time.Compare(b.CreationTimestamp.Time)
	})

	return clusters, nil
}

// initializeNewCluster creates a new Cluster resource based on the given ClusterRequest and ClusterDefinition.
func (r *ClusterScheduler) initializeNewCluster(ctx context.Context, cr *clustersv1alpha1.ClusterRequest, cDef *config.ClusterDefinition) *clustersv1alpha1.Cluster {
	log := logging.FromContextOrPanic(ctx)
	purpose := cr.Spec.Purpose
	cluster := &clustersv1alpha1.Cluster{}
	// choose a name for the cluster
	// priority as follows:
	// - for singleton clusters (shared unlimited):
	//  1. generateName of template
	//  2. name of template
	//  3. purpose
	// - for exclusive clusters or shared limited:
	//  1. generateName of template
	//  2. purpose used as generateName
	if cDef.IsSharedUnlimitedly() {
		// there will only be one instance of this cluster
		if cDef.Template.GenerateName != "" {
			cluster.SetGenerateName(cDef.Template.GenerateName)
		} else if cDef.Template.Name != "" {
			cluster.SetName(cDef.Template.Name)
		} else {
			cluster.SetName(purpose)
		}
	} else {
		// there might be multiple instances of this cluster
		if cDef.Template.GenerateName != "" {
			cluster.SetGenerateName(cDef.Template.GenerateName)
		} else {
			cluster.SetGenerateName(purpose + "-")
		}
	}
	// choose a namespace for the cluster
	// priority as follows:
	//  1. namespace of template
	//  2. namespace of request
	if cDef.Template.Namespace != "" {
		cluster.SetNamespace(cDef.Template.Namespace)
	} else {
		cluster.SetNamespace(cr.Namespace)
	}
	log.Info("Creating new cluster", "clusterName", cluster.Name, "clusterNamespace", cluster.Namespace, "preemptive", cr.Spec.Preemptive)

	// set finalizer
	cluster.SetFinalizers([]string{cr.FinalizerForCluster()})
	// take over labels, annotations, and spec from the template
	cluster.SetLabels(cDef.Template.Labels)
	if err := ctrlutils.EnsureLabel(ctx, nil, cluster, clustersv1alpha1.DeleteWithoutRequestsLabel, "true", false); err != nil {
		if !ctrlutils.IsMetadataEntryAlreadyExistsError(err) {
			log.Error(err, "error setting label", "label", clustersv1alpha1.DeleteWithoutRequestsLabel, "value", "true")
		}
	}
	cluster.SetAnnotations(cDef.Template.Annotations)
	cluster.Spec = cDef.Template.Spec

	// set purpose, if not set
	if len(cluster.Spec.Purposes) == 0 {
		cluster.Spec.Purposes = []string{purpose}
	} else {
		if !slices.Contains(cluster.Spec.Purposes, purpose) {
			cluster.Spec.Purposes = append(cluster.Spec.Purposes, purpose)
		}
	}

	return cluster
}

// TODO: move to controller-utils
// RemoveFinalizerWithPrefix removes the first finalizer with the given prefix from the object.
// The bool return value indicates whether a finalizer was removed.
// If it is true, the string return value holds the suffix of the removed finalizer.
// The logic is based on the controller-runtime's RemoveFinalizer function.
func RemoveFinalizerWithPrefix(obj client.Object, prefix string) (string, bool) {
	fins := obj.GetFinalizers()
	length := len(fins)
	suffix := ""
	found := false

	index := 0
	for i := range length {
		if !found && strings.HasPrefix(fins[i], prefix) {
			suffix = strings.TrimPrefix(fins[i], prefix)
			found = true
			continue
		}
		fins[index] = fins[i]
		index++
	}
	obj.SetFinalizers(fins[:index])
	return suffix, length != index
}

// ReschedulePreemptiveRequest fetches the ClusterRequest with the corresponding UID from the cluster and adds a reconcile annotation to it.
func (r *ClusterScheduler) ReschedulePreemptiveRequest(ctx context.Context, uid string) errutils.ReasonableError {
	crl := &clustersv1alpha1.ClusterRequestList{}
	if err := r.PlatformCluster.Client().List(ctx, crl, client.MatchingLabelsSelector{Selector: r.Config.Selectors.Requests.Completed()}, client.MatchingFields{"spec.preemptive": "true"}); err != nil {
		return errutils.WithReason(fmt.Errorf("error listing ClusterRequests: %w", err), cconst.ReasonPlatformClusterInteractionProblem)
	}
	var cr *clustersv1alpha1.ClusterRequest
	for _, elem := range crl.Items {
		if string(elem.UID) == uid {
			cr = &elem
			break
		}
	}
	if cr == nil {
		return errutils.WithReason(fmt.Errorf("unable to find preemptive ClusterRequest with UID '%s'", uid), cconst.ReasonInternalError)
	}
	if err := ctrlutils.EnsureAnnotation(ctx, r.PlatformCluster.Client(), cr, apiconst.OperationAnnotation, apiconst.OperationAnnotationValueReconcile, true); err != nil {
		return errutils.WithReason(fmt.Errorf("error adding reconcile annotation to preemptive ClusterRequest '%s': %w", cr.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
	}
	return nil
}

// pickFittingCluster gets a list of existing clusters that match the request's purpose and tries to pick one for the request.
// If the returned cluster is nil, no fitting cluster was found. This can e.g. happen if all clusters are already at their tenancy limit.
func (r *ClusterScheduler) pickFittingCluster(ctx context.Context, cr *clustersv1alpha1.ClusterRequest, clusters []*clustersv1alpha1.Cluster, cDef *config.ClusterDefinition) (*clustersv1alpha1.Cluster, errutils.ReasonableError) {
	log := logging.FromContextOrPanic(ctx)
	log.Debug("Checking for fitting clusters", "purpose", cr.Spec.Purpose, "tenancyCount", cDef.TenancyCount, "preemptive", cr.Spec.Preemptive)
	// remove all clusters with a non-zero deletion timestamp from the list of candidates
	clusters = filters.FilterSlice(clusters, func(args ...any) bool {
		c, ok := args[0].(*clustersv1alpha1.Cluster)
		if !ok {
			return false
		}
		return c.DeletionTimestamp.IsZero()
	})
	// avoid picking clusters which are not ready if ready ones are available
	readyClusters := filters.FilterSlice(clusters, func(args ...any) bool {
		c, ok := args[0].(*clustersv1alpha1.Cluster)
		if !ok {
			return false
		}
		return c.Status.Phase == clustersv1alpha1.CLUSTER_PHASE_READY
	})
	if len(readyClusters) > 0 {
		log.Debug("Excluding non-ready clusters from scheduling", "readyCount", len(readyClusters), "totalCount", len(clusters))
		clusters = readyClusters
	}
	// unless the cluster template for the requested purpose allows unlimited sharing, filter out all clusters that are already at their tenancy limit
	// for preemptive requests, other preemptive requests count towards the tenancy limit, for regular requests, preemptive requests are ignored
	if cDef.IsExclusive() || cDef.TenancyCount > 0 {
		clusters = filters.FilterSlice(clusters, func(args ...any) bool {
			c, ok := args[0].(*clustersv1alpha1.Cluster)
			if !ok {
				return false
			}
			preTenCount := c.GetPreemptiveTenancyCount()
			if cDef.IsExclusive() && preTenCount == 0 {
				// for exclusive tenancy, take only clusters into account that have been preemptively requested
				return false
			}
			tenCount := c.GetTenancyCount()
			if cr.Spec.Preemptive {
				// for preemptive requests, also take preemptive "workloads" into account
				tenCount += preTenCount
			}
			return tenCount < cDef.TenancyCount
		})
	}
	var cluster *clustersv1alpha1.Cluster
	if len(clusters) == 1 {
		cluster = clusters[0]
		log.Debug("One existing cluster qualifies for request", "clusterName", cluster.Name, "clusterNamespace", cluster.Namespace, "preemptive", cr.Spec.Preemptive)
	} else if len(clusters) > 0 {
		log.Debug("Multiple existing clusters qualify for request, choosing one according to strategy", "strategy", string(r.Config.Strategy), "count", len(clusters), "preemptive", cr.Spec.Preemptive)
		switch r.Config.Strategy {
		case config.STRATEGY_SIMPLE:
			cluster = clusters[0]
		case config.STRATEGY_RANDOM:
			cluster = clusters[rand.IntN(len(clusters))]
		case "":
			// default to balanced, if empty
			fallthrough
		case config.STRATEGY_BALANCED:
			// find cluster with least number of requests
			cluster = clusters[0]
			count := cluster.GetTenancyCount()
			for _, c := range clusters[1:] {
				tmp := c.GetTenancyCount()
				if tmp < count {
					count = tmp
					cluster = c
				}
			}
		default:
			return nil, errutils.WithReason(fmt.Errorf("unknown strategy '%s'", r.Config.Strategy), cconst.ReasonConfigurationProblem)
		}
	}
	return cluster, nil
}
