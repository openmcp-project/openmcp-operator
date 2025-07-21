package scheduler

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openmcp-project/controller-utils/pkg/collections"
	"github.com/openmcp-project/controller-utils/pkg/collections/filters"
	"github.com/openmcp-project/controller-utils/pkg/collections/maps"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/controller-utils/pkg/pairs"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/internal/config"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/scheduler/strategy"
)

type internalSchedulingResult struct {
	*SchedulingResult
	scheduledRegularRequests    *scheduledRequests // for internal use, to track scheduled regular requests
	scheduledPreemptiveRequests *scheduledRequests // for internal use, to track scheduled preemptive requests
}

type scheduledRequests struct {
	clustersToRequests map[*clustersv1alpha1.Cluster]map[string]int   // maps clusters to their request finalizers with their respective counts
	requestsToClusters map[string]sets.Set[*clustersv1alpha1.Cluster] // maps request UIDs to clusters that have at least one finalizer for this request
	preemptive         bool
}

func newScheduledRequests(preemptive bool) *scheduledRequests {
	return &scheduledRequests{
		clustersToRequests: map[*clustersv1alpha1.Cluster]map[string]int{},
		requestsToClusters: map[string]sets.Set[*clustersv1alpha1.Cluster]{},
		preemptive:         preemptive,
	}
}

func (sr *scheduledRequests) setRequestsForCluster(c *clustersv1alpha1.Cluster) {
	if c == nil {
		return
	}
	prefix := clustersv1alpha1.RequestFinalizerOnClusterPrefix
	if sr.preemptive {
		prefix = clustersv1alpha1.PreemptiveRequestFinalizerOnClusterPrefix
	}

	// fill clustersToRequests map
	curClusterMapping := map[string]int{}
	for _, fin := range c.Finalizers {
		if strings.HasPrefix(fin, prefix) {
			uid := strings.TrimPrefix(fin, prefix)
			curClusterMapping[uid]++
		}
	}
	sr.clustersToRequests[c] = curClusterMapping

	// fill requestsToClusters map
	// ensure that the cluster is present in all sets for all uids of request finalizers it has
	for uid := range curClusterMapping {
		clusterSet := sr.requestsToClusters[uid]
		if clusterSet == nil {
			clusterSet = sets.New[*clustersv1alpha1.Cluster]()
		}
		clusterSet.Insert(c)
		sr.requestsToClusters[uid] = clusterSet
	}
	// ensure that the cluster is not present in the sets of any uid that it does not have a finalizer for
	for uid, clusterSet := range sr.requestsToClusters {
		if _, ok := curClusterMapping[uid]; ok || clusterSet == nil {
			continue // skip sets where it should be present or where the set is nil
		}
		clusterSet.Delete(c)
		sr.requestsToClusters[uid] = clusterSet
	}
}

// getClustersForRequest returns a set of clusters that have at least one finalizer for the given request UID.
func (sr *scheduledRequests) getClustersForRequest(uid string) sets.Set[*clustersv1alpha1.Cluster] {
	return sr.requestsToClusters[uid]
}

// getRequestCountForCluster returns the number of finalizers the given cluster has for the given request UID.
func (sr *scheduledRequests) getRequestCountForCluster(c *clustersv1alpha1.Cluster, uid string) int {
	if requests := sr.clustersToRequests[c]; requests != nil {
		return requests[uid]
	}
	return 0
}

// getRequestCounts returns a mapping from clusters to the number of finalizers they have for the given request UID.
func (sr *scheduledRequests) getRequestCounts(uid string) map[*clustersv1alpha1.Cluster]int {
	res := map[*clustersv1alpha1.Cluster]int{}
	for c := range sr.getClustersForRequest(uid) {
		res[c] = sr.getRequestCountForCluster(c, uid)
	}
	return res
}

// getTotalRequestCount returns the total number of finalizers for the given request UID across all known clusters.
func (sr *scheduledRequests) getTotalRequestCount(uid string) int {
	return collections.AggregateMap(sr.getRequestCounts(uid), func(_ *clustersv1alpha1.Cluster, count int, current int) int {
		return count + current
	}, 0)
}

func RequestFinalizer(uid string, preemptive bool) string {
	prefix := clustersv1alpha1.RequestFinalizerOnClusterPrefix
	if preemptive {
		prefix = clustersv1alpha1.PreemptiveRequestFinalizerOnClusterPrefix
	}
	return fmt.Sprintf("%s%s", prefix, uid)
}

// Finalizer returns the finalizer that corresponds to this SchedulingRequest.
func (sr *SchedulingRequest) Finalizer() string {
	return RequestFinalizer(sr.UID, sr.IsPreemptive())
}

func (sr *internalSchedulingResult) addRequest(ctx context.Context, req *SchedulingRequest, cDef *config.ClusterDefinition, stratKey config.Strategy) error {
	logName := "AddRegular"
	var currentCount int
	if req.IsPreemptive() {
		logName = "RemovePreemptive"
		currentCount = sr.scheduledPreemptiveRequests.getTotalRequestCount(req.UID)
	} else {
		currentCount = sr.scheduledRegularRequests.getTotalRequestCount(req.UID)
	}
	log := logging.FromContextOrPanic(ctx).WithName(logName).WithValues("requestUID", req.UID)
	ctx = logging.NewContext(ctx, log)

	// map new and existing clusters to their remaining capacity
	clustersCap := capacityMapping(sr.Clusters, req, cDef, false, false)
	existingClustersCap := make([]*pairs.Pair[*clustersv1alpha1.Cluster, int], 0, len(clustersCap))
	newClustersCap := make([]*pairs.Pair[*clustersv1alpha1.Cluster, int], 0, len(clustersCap))
	for _, p := range clustersCap {
		if sr.HasCreated(p.Key) {
			existingClustersCap = append(existingClustersCap, p)
		} else {
			newClustersCap = append(newClustersCap, p)
		}
	}

	for range max(0, req.WorkloadCount()-currentCount) {
		// check existing clusters first
		availableClustersCap := existingClustersCap
		if !req.IsPreemptive() {
			existingReadyClusters := filters.FilterSlice(availableClustersCap, func(args ...any) bool {
				p := args[0].(*pairs.Pair[*clustersv1alpha1.Cluster, int])
				return p.Key.Status.Phase == clustersv1alpha1.CLUSTER_PHASE_READY
			})
			if len(existingReadyClusters) > 0 {
				log.Debug("Excluding some non-ready clusters from scheduling", "existingClusters", len(availableClustersCap), "existingReadyClusters", len(existingReadyClusters))
				availableClustersCap = existingReadyClusters
			}
		}

		if len(availableClustersCap) == 0 {
			availableClustersCap = newClustersCap
		}
		var chosenPair *pairs.Pair[*clustersv1alpha1.Cluster, int]
		if len(availableClustersCap) > 0 {
			// choose a cluster using the configured strategy
			strat := strategy.FromConfig[*pairs.Pair[*clustersv1alpha1.Cluster, int]](stratKey)                                                                   //nolint:misspell
			log.Debug("Fitting clusters available, choosing one according to strategy", "availableClusters", len(availableClustersCap), "strategy", strat.Name()) //nolint:misspell
			var err error
			chosenPair, err = strat.Choose(ctx, availableClustersCap, func(p *pairs.Pair[*clustersv1alpha1.Cluster, int]) *clustersv1alpha1.Cluster { //nolint:misspell
				return p.Key
			}, cDef, req.IsPreemptive())
			if err != nil {
				return fmt.Errorf("error choosing cluster for request '%s': %w", req.UID, err)
			}
		}
		if chosenPair != nil {
			// found a fitting cluster
			// add request finalizer to the cluster
			if AddFinalizer(chosenPair.Key, req.Finalizer(), req.IsPreemptive()) && !sr.HasCreated(chosenPair.Key) {
				// if the cluster is not new, it needs to be patched
				sr.Patch[SRKey(chosenPair.Key)] = chosenPair.Key
			}
			// re-compute cluster capacity
			chosenPair.Value = strategy.Capacity(chosenPair.Key, cDef, req.IsPreemptive())
		} else {
			// no cluster could be chosen
			log.Debug("Either no fitting clusters were available or the strategy did not choose a cluster, creating a new one", "availableClusters", len(availableClustersCap), "strategy", stratKey)
			c := sr.newOrRecoveredCluster(ctx, req, cDef)
			chosenPair = ptr.To(pairs.New(c, strategy.Capacity(c, cDef, req.IsPreemptive())))
			newClustersCap = append(newClustersCap, chosenPair)
		}
		log.Debug("Scheduling request on cluster", "clusterName", chosenPair.Key.Name, "clusterGenerateName", chosenPair.Key.GenerateName, "clusterNamespace", chosenPair.Key.Namespace, "isNewCluster", sr.HasCreated(chosenPair.Key))
		if req.IsPreemptive() {
			sr.scheduledPreemptiveRequests.setRequestsForCluster(chosenPair.Key)
		} else {
			sr.scheduledRegularRequests.setRequestsForCluster(chosenPair.Key)
		}

		// remove clusters with capacity 0 from the list of available clusters
		hasCapacity := func(args ...any) bool {
			p := args[0].(*pairs.Pair[*clustersv1alpha1.Cluster, int])
			return p.Value > 0
		}
		existingClustersCap = filters.FilterSlice(existingClustersCap, hasCapacity)
		newClustersCap = filters.FilterSlice(newClustersCap, hasCapacity)
	}

	return nil
}

func (sr *internalSchedulingResult) removeRequest(ctx context.Context, req *SchedulingRequest) {
	logName := "RemoveRegular"
	var clusters sets.Set[*clustersv1alpha1.Cluster]
	if req.IsPreemptive() {
		logName = "RemovePreemptive"
		clusters = sets.New(sr.scheduledPreemptiveRequests.getClustersForRequest(req.UID).UnsortedList()...)
	} else {
		clusters = sets.New(sr.scheduledRegularRequests.getClustersForRequest(req.UID).UnsortedList()...)
	}
	log := logging.FromContextOrPanic(ctx).WithName(logName).WithValues("requestUID", req.UID)
	if len(clusters) == 0 {
		log.Debug("Request not scheduled on any cluster, nothing to do")
		return
	}
	for c := range clusters {
		if RemoveFinalizer(c, req.Finalizer(), true) {
			log.Debug("Unscheduling request from cluster", "clusterName", c.Name, "clusterNamespace", c.Namespace)
		}
		sr.deleteIfPossible(ctx, c)
		sr.Patch[SRKey(c)] = c
		if req.IsPreemptive() {
			sr.scheduledPreemptiveRequests.setRequestsForCluster(c)
		} else {
			sr.scheduledRegularRequests.setRequestsForCluster(c)
		}
	}
}

// deleteIfPossible marks a cluster for deletion if it has no more requests scheduled on it and the corresponding label set.
func (sr *internalSchedulingResult) deleteIfPossible(ctx context.Context, c *clustersv1alpha1.Cluster) {
	log := logging.FromContextOrPanic(ctx)
	// if this was the last request on the cluster and it has the DeleteWithoutRequestsLabel, mark it for deletion
	if c.GetTenancyCount() == 0 && c.GetPreemptiveTenancyCount() == 0 {
		val, ok := ctrlutils.GetLabel(c, clustersv1alpha1.DeleteWithoutRequestsLabel)
		if ok && val == "true" {
			log.Debug("Cluster has no more requests, marking for deletion", "clusterName", c.Name, "clusterNamespace", c.Namespace)
			sr.Delete[SRKey(c)] = c
		} else {
			log.Debug("Cluster has no more requests, but the label is missing or not set to 'true', not marking it for deletion", "clusterName", c.Name, "clusterNamespace", c.Namespace, "label", clustersv1alpha1.DeleteWithoutRequestsLabel, "labelValue", val)
		}
	}
}

// freeClusters checks if there are any clusters that exist due to preemptive requests only
// if the current request has finalizers on such clusters which could be rescheduled to other clusters,
// they will be removed
// note that they won't be added again, this needs to happen in a call to addRequest
func (sr *internalSchedulingResult) freeClusters(ctx context.Context, req *SchedulingRequest, cDef *config.ClusterDefinition) {
	log := logging.FromContextOrPanic(ctx)
	if !req.IsPreemptive() {
		// regular requests cannot be rescheduled, so nothing to do here
		log.Debug("Skipping freeClusters for non-preemptive request")
		return
	}
	if cDef.IsSharedUnlimitedly() {
		// no need to try to redistribute finalizers from unlimitedly shared clusters
		log.Debug("Cluster definition specifies unlimited sharing, need to redistribute finalizers")
		return
	}
	// identify candidates which could potentially be deleted if freed from this preemptive request
	// these are candidates that
	// - have at least one finalizer from this request
	// - have only preemptive request finalizers, no regular ones
	// - have the DeleteWithoutRequestsLabel set to "true"
	// - are not newly created clusters (they would not have been created if it would not have been necessary)
	candidates := sr.scheduledPreemptiveRequests.getClustersForRequest(req.UID).UnsortedList()
	candidates = filters.FilterSlice(candidates, func(args ...any) bool {
		c := args[0].(*clustersv1alpha1.Cluster)
		return ctrlutils.HasLabelWithValue(c, clustersv1alpha1.DeleteWithoutRequestsLabel, "true") && c.GetTenancyCount() == 0 && !sr.HasCreated(c)
	})
	if len(candidates) == 0 {
		// no candidates for deletion found
		log.Debug("No cluster candidates found which could possibly be deleted by redistributing preemptive requests")
		return
	}
	log.Debug("Found cluster candidates which could possibly be deleted by redistributing preemptive requests", "count", len(candidates))
	// sort the clusters by their tenancy count, from lowest to highest
	slices.SortFunc(candidates, func(a, b *clustersv1alpha1.Cluster) int {
		return a.GetPreemptiveTenancyCount() - b.GetPreemptiveTenancyCount()
	})

	// avoid removing too many finalizers by estimating free capacity
	// we could simply remove all finalizers from the candidates and have them rescheduled later,
	// but this is inefficient and could lead to finalizers 'jumping' between clusters
	clustersCap := capacityMapping(sr.Clusters, req, cDef, false, false)
	filledClusters := sets.New[*clustersv1alpha1.Cluster]() // holds the clusters that have been filled with finalizers already

	for len(candidates) > 0 && len(clustersCap) > 0 {
		// pick the cluster with the lowest tenancy count
		can := candidates[0]
		candidates = candidates[1:]

		// remove candidates where we have planned to redistribute finalizers onto already
		if filledClusters.Has(can) {
			continue
		}

		// count the number of this request's finalizers on the cluster
		count := sr.scheduledPreemptiveRequests.getRequestCountForCluster(can, req.UID)
		// try to redistribute the finalizers to other clusters
		for count > 0 && len(clustersCap) > 0 {
			p := clustersCap[0]
			remainingCapacity := p.Value - count
			if remainingCapacity <= 0 {
				clustersCap = clustersCap[1:] // remove the cluster from the list, it has no more capacity
				filledClusters.Insert(p.Key)  // mark the cluster as filled
			}
			if remainingCapacity < 0 {
				count = -remainingCapacity // we can only remove as many finalizers as the cluster has capacity for
			} else {
				count = 0 // all finalizers can be removed
			}
		}

		if count > 0 {
			// not enough capacity to redistribute all finalizers, so we don't have to do anything
			// but there is no need to further try to redistribute finalizers from this cluster
			log.Debug("Not enough estimated capacity to redistribute this request's finalizers from cluster", "clusterName", can.Name, "clusterNamespace", can.Namespace)
			break
		}

		// there is enough capacity to redistribute all this cluster's finalizers
		// so let's remove them all
		if RemoveFinalizer(can, req.Finalizer(), true) {
			log.Debug("Removing finalizer from cluster for redistribution", "clusterName", can.Name, "clusterNamespace", can.Namespace)
			sr.Patch[SRKey(can)] = can
			sr.scheduledPreemptiveRequests.setRequestsForCluster(can)
			sr.deleteIfPossible(ctx, can)
		}
	}
}

// identifyToBeRescheduledRequests returns a set of preemptive request UIDs should be reconciled.
// This can happen if a regular request has been scheduled and 'replaced' a preemptive request's finalizer,
// or if a cluster could potentially be deleted by redistributing preemptive requests.
func (sr *internalSchedulingResult) identifyToBeRescheduledRequests(ctx context.Context, cDef *config.ClusterDefinition, lostRegularRequests bool) {
	log := logging.FromContextOrPanic(ctx)
	if cDef.IsSharedUnlimitedly() {
		log.Debug("ClusterDefinition is shared unlimitedly, no preemptive requests to reschedule")
		return
	}
	for _, c := range sr.Clusters {
		if sr.HasCreated(c) {
			// ignore newly created clusters, they should not have any exceeding preemptive requests because the regular requests were scheduled first
			continue
		}
		pCount := c.GetPreemptiveTenancyCount()
		rCount := c.GetTenancyCount()
		exceed := (rCount + pCount) - cDef.TenancyCount
		// remove exceeding preemptive requests
		changed := false
		for exceed > 0 {
			pUIDs := c.GetPreemptiveRequestUIDs()
			if len(pUIDs) == 0 {
				log.Error(nil, "Tenancy limit is exceeded and cannot be reached by rescheduling preemptive requests", "clusterName", c.Name, "clusterNamespace", c.Namespace, "tenancyLimit", cDef.TenancyCount, "tenancy", rCount, "preemptiveTenancy", pCount, "exceed", exceed)
				break
			}
			p := maps.GetAny(pUIDs).Key
			sr.Reschedule.Insert(p)
			RemoveFinalizer(c, RequestFinalizer(p, true), false)
			changed = true
			exceed--
			pCount--
		}
		if changed {
			sr.Patch[SRKey(c)] = c
			sr.scheduledPreemptiveRequests.setRequestsForCluster(c)
		}

		// if regular requests have been removed, reconcile preemptive requests from clusters that
		// - have no regular requests scheduled on them
		// - have at max 50% of their tenancy count in preemptive requests
		// - have the DeleteWithoutRequestsLabel set to "true"
		// note that the tenancy count metric might be wrong for multi-purpose clusters, but it should be fine for most cases (hopefully)
		if lostRegularRequests && rCount == 0 && pCount > 0 && pCount <= cDef.TenancyCount/2 && ctrlutils.HasLabelWithValue(c, clustersv1alpha1.DeleteWithoutRequestsLabel, "true") {
			log.Debug("Cluster marked for efficiency optimization", "clusterName", c.Name, "clusterNamespace", c.Namespace, "preemptiveTenancy", pCount)
			for pUID := range c.GetPreemptiveRequestUIDs() {
				sr.Reschedule.Insert(pUID)
			}
		}
	}
}

// initializeNewCluster creates a new Cluster resource based on the given ClusterRequest and ClusterDefinition.
// It directly adds the request's finalizer to the Cluster.
func (sr *internalSchedulingResult) initializeNewCluster(ctx context.Context, req *SchedulingRequest, cDef *config.ClusterDefinition) *clustersv1alpha1.Cluster {
	log := logging.FromContextOrPanic(ctx)
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
			cluster.SetName(req.Purpose)
		}
	} else {
		// there might be multiple instances of this cluster
		if cDef.Template.GenerateName != "" {
			cluster.SetGenerateName(cDef.Template.GenerateName)
		} else {
			cluster.SetGenerateName(req.Purpose + "-")
		}
	}
	// choose a namespace for the cluster
	// priority as follows:
	//  1. namespace of template
	//  2. namespace of request
	if cDef.Template.Namespace != "" {
		cluster.SetNamespace(cDef.Template.Namespace)
	} else {
		cluster.SetNamespace(req.Namespace)
	}

	// set finalizer
	cluster.SetFinalizers([]string{req.Finalizer()})
	// take over labels, annotations, and spec from the template
	cluster.SetLabels(cDef.Template.Labels)
	if err := ctrlutils.EnsureLabel(ctx, nil, cluster, clustersv1alpha1.DeleteWithoutRequestsLabel, "true", false); err != nil {
		if !ctrlutils.IsMetadataEntryAlreadyExistsError(err) {
			log.Error(err, "error setting label", "label", clustersv1alpha1.DeleteWithoutRequestsLabel, "value", "true")
		}
	}
	if err := ctrlutils.EnsureLabel(ctx, nil, cluster, clustersv1alpha1.SchedulerCreationIDLabel, sr.ID(), false, ctrlutils.OVERWRITE); err != nil {
		log.Error(err, "error setting label", "label", clustersv1alpha1.DeleteWithoutRequestsLabel, "value", sr.ID())
	}
	cluster.SetAnnotations(cDef.Template.Annotations)
	cluster.Spec = cDef.Template.Spec

	// set purpose, if not set
	if len(cluster.Spec.Purposes) == 0 {
		cluster.Spec.Purposes = []string{req.Purpose}
	} else {
		if !slices.Contains(cluster.Spec.Purposes, req.Purpose) {
			cluster.Spec.Purposes = append(cluster.Spec.Purposes, req.Purpose)
		}
	}

	return cluster
}

// AddFinalizer adds a finalizer to the given object.
// Returns true if the object's finalizers were actually modified.
// If multi is true, the finalizer is always added, otherwise it is only added if it is not already present.
func AddFinalizer(obj client.Object, fin string, multi bool) bool {
	if obj == nil {
		return false
	}
	if !multi {
		return controllerutil.AddFinalizer(obj, fin)
	}
	fins := obj.GetFinalizers()
	if fins == nil {
		fins = []string{}
	}
	obj.SetFinalizers(append(fins, fin))
	return true
}

// RemoveFinalizer removes a finalizer from the given object.
// If all is true, all occurrences of the finalizer are removed, otherwise only the first occurrence.
// Returns true if the object's finalizers were actually modified.
func RemoveFinalizer(obj client.Object, fin string, all bool) bool {
	if obj == nil {
		return false
	}
	if all {
		return controllerutil.RemoveFinalizer(obj, fin)
	}
	fins := obj.GetFinalizers()
	for i, f := range fins {
		if f == fin {
			obj.SetFinalizers(append(fins[:i], fins[i+1:]...))
			return true
		}
	}
	return false
}

// capacityMapping returns a list of pairs of clusters and their remaining capacity.
// The list is sorted by capacity, from lowest to highest.
// If includeFull is true, clusters with zero or exceeded capacity are included as well, otherwise only ones with positive capacity are included.
// If includeDeleting is true, clusters that are in deletion are included as well, otherwise they are filtered out.
func capacityMapping(clusters []*clustersv1alpha1.Cluster, req *SchedulingRequest, cDef *config.ClusterDefinition, includeFull, includeDeleting bool) []*pairs.Pair[*clustersv1alpha1.Cluster, int] {
	clustersCap := make([]*pairs.Pair[*clustersv1alpha1.Cluster, int], 0, len(clusters))
	for _, c := range clusters {
		cap := strategy.Capacity(c, cDef, req.IsPreemptive())
		if (includeFull || cap > 0) && (includeDeleting || c.DeletionTimestamp.IsZero()) {
			clustersCap = append(clustersCap, ptr.To(pairs.New(c, cap)))
		}
	}
	// sort by capacity, from lowest to highest
	slices.SortFunc(clustersCap, func(a, b *pairs.Pair[*clustersv1alpha1.Cluster, int]) int {
		return a.Value - b.Value
	})
	return clustersCap
}
