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

	"github.com/openmcp-project/controller-utils/pkg/collections/filters"
	"github.com/openmcp-project/controller-utils/pkg/collections/maps"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/controller-utils/pkg/pairs"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	"github.com/openmcp-project/openmcp-operator/internal/config"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/scheduler/strategy"
)

// SchedulingResult describes the result of a scheduling operation.
// The Original field contains the original clusters, in a map structure for easy lookup. This can be used for patch operations.
//
//	Use SRKey to get the key for a Cluster.
//
// The Patch field contains clusters that need to be patched. The changes are already in the Cluster objects, but not in the Original map, which can therefore be used for diffing.
// The Create field contains clusters that need to be created.
// The Delete field contains clusters that need to be deleted.
// The Reschedule field contains UIDs of preemptive requests for which a reconciliation must be triggered, because some or all of their finalizers have been removed.
type SchedulingResult struct {
	Original   map[string]*clustersv1alpha1.Cluster
	Patch      map[string]*clustersv1alpha1.Cluster
	Create     []*clustersv1alpha1.Cluster // cannot be a map, because we use generateName for new clusters, which means they don't have a unique identifier until created
	Delete     map[string]*clustersv1alpha1.Cluster
	Reschedule sets.Set[string]
}

// Apply applies the changes described in the SchedulingResult to the platform.
// It returns an updated list of clusters, including unchanged, patched, and newly created ones.
func (sr *SchedulingResult) Apply(ctx context.Context, platformClient client.Client) ([]*clustersv1alpha1.Cluster, errutils.ReasonableError) {
	errs := errutils.NewReasonableErrorList()
	res := make([]*clustersv1alpha1.Cluster, 0, len(sr.Original)-len(sr.Delete)+len(sr.Create))

	// patch clusters
	for k, cOld := range sr.Original {
		updated := cOld
		if c, ok := sr.Patch[k]; ok {
			if err := platformClient.Patch(ctx, c, client.MergeFrom(cOld)); err != nil {
				errs.Append(errutils.WithReason(fmt.Errorf("error patching cluster '%s': %w", k, err), cconst.ReasonPlatformClusterInteractionProblem))
			}
			updated = c
		}
		res = append(res, updated)
	}

	// create new clusters
	for _, c := range sr.Create {
		if err := platformClient.Create(ctx, c); err != nil {
			errs.Append(errutils.WithReason(fmt.Errorf("error creating cluster: %w", err), cconst.ReasonPlatformClusterInteractionProblem))
		}
		res = append(res, c)
	}

	// delete clusters
	for k, c := range sr.Delete {
		if err := platformClient.Delete(ctx, c); client.IgnoreNotFound(err) != nil {
			errs.Append(errutils.WithReason(fmt.Errorf("error deleting cluster '%s': %w", k, err), cconst.ReasonPlatformClusterInteractionProblem))
		}
	}

	// patch reconcile annotation on preemptive requests that need to be rescheduled
	for uid := range sr.Reschedule {
		errs.Append(ReschedulePreemptiveRequest(ctx, platformClient, uid))
	}

	return res, errs.Aggregate()
}

type internalSchedulingResult struct {
	*SchedulingResult
	scheduledRegularRequests    map[string]*clustersv1alpha1.Cluster   // for internal use, to track scheduled regular requests
	scheduledPreemptiveRequests map[string][]*clustersv1alpha1.Cluster // for internal use, to track scheduled preemptive requests
}

// Current returns what the clusters would look like after applying the changes in the Patch, Create, and Delete fields.
// The first list contains the existing clusters, while the second one contains the to-be-created ones.
// This distinction is important because clusters which are yet to be created might have 'generateName' set instead of 'name', which breaks logic that depends on the cluster name.
// It returns deep copies of the clusters.
func (sr *SchedulingResult) Current() ([]*clustersv1alpha1.Cluster, []*clustersv1alpha1.Cluster) {
	res1 := make([]*clustersv1alpha1.Cluster, 0, len(sr.Original)-len(sr.Delete))
	for k, c := range sr.Original {
		if _, ok := sr.Delete[k]; ok {
			continue
		}
		if patch, ok := sr.Patch[k]; ok {
			c = patch
		}
		res1 = append(res1, c.DeepCopy())
	}
	res2 := make([]*clustersv1alpha1.Cluster, 0, len(sr.Create))
	for _, c := range sr.Create {
		res2 = append(res2, c.DeepCopy())
	}
	return res1, res2
}

// SchedulingRequest represents something that needs to be scheduled.
// If Remove is true, the corresponding request is to be removed, otherwise it should be added.
type SchedulingRequest struct {
	// UID is the unique identifier of the request.
	UID string
	// workloadCount specifies how many workloads this request accounts for.
	// A value of 0 indicates a regular request, which always accounts for 1 workload.
	// A value greater than 0 indicates a preemptive request, which accounts for the specified number of workloads.
	// The actual value is only relevant if remove is false.
	workloadCount int
	// Remove indicates whether this request is to be removed (true) or added (false).
	Remove bool
	// Namespace is the namespace of the k8s resource this request originated from.
	Namespace string
	// Purpose is the purpose of the request.
	Purpose string
}

func (sr *SchedulingRequest) IsPreemptive() bool {
	return sr.workloadCount > 0
}

// WorkloadCount returns the requested workload count.
// It returns 1 for regular requests and the specified amount for preemptive requests.
func (sr *SchedulingRequest) WorkloadCount() int {
	if sr.workloadCount == 0 {
		return 1
	}
	return sr.workloadCount
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

func NewSchedulingRequest(uid string, workloadCount int, remove bool, namespace, purpose string) *SchedulingRequest {
	return &SchedulingRequest{
		UID:           uid,
		workloadCount: workloadCount,
		Remove:        remove,
		Namespace:     namespace,
		Purpose:       purpose,
	}
}

// SRKey takes a Cluster and returns the key under which this Cluster can be found in the Original field of a SchedulingResult.
// The key is a string in the format "namespace/name".
func SRKey(c *clustersv1alpha1.Cluster) string {
	return fmt.Sprintf("%s/%s", c.Namespace, c.Name)
}

// Schedule takes any amount of SchedulingRequests and returns a SchedulingResult, which describes the changes that need to be made to the clusters.
// It applies (or removes) the finalizers corresponding to the requests.
// Finalizers for non-preemptive requests are never moved between clusters, finalizers for preemptive requests might be moved for efficiency.
// If requests is empty, the result could still indicate changes, if existing preemptive requests could be cleaned up.
// Note that the originally passed-in clusters might be modified. Their unmodified versions can be retrieved from the Original field of the SchedulingResult.
func Schedule(ctx context.Context, clusters []*clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, strategy config.Strategy, requests ...*SchedulingRequest) (*SchedulingResult, error) {
	log := logging.FromContextOrPanic(ctx).WithName("Schedule")
	ctx = logging.NewContext(ctx, log)
	isr := &internalSchedulingResult{
		SchedulingResult: &SchedulingResult{
			Original:   make(map[string]*clustersv1alpha1.Cluster, len(clusters)),
			Patch:      map[string]*clustersv1alpha1.Cluster{},
			Create:     []*clustersv1alpha1.Cluster{},
			Delete:     map[string]*clustersv1alpha1.Cluster{},
			Reschedule: sets.New[string](),
		},
		scheduledRegularRequests:    map[string]*clustersv1alpha1.Cluster{},
		scheduledPreemptiveRequests: map[string][]*clustersv1alpha1.Cluster{},
	}
	for _, c := range clusters {
		if c == nil {
			continue
		}
		for _, fin := range c.Finalizers {
			if strings.HasPrefix(fin, clustersv1alpha1.RequestFinalizerOnClusterPrefix) {
				isr.scheduledRegularRequests[strings.TrimPrefix(fin, clustersv1alpha1.RequestFinalizerOnClusterPrefix)] = c.DeepCopy()
			} else if strings.HasPrefix(fin, clustersv1alpha1.PreemptiveRequestFinalizerOnClusterPrefix) {
				uid := strings.TrimPrefix(fin, clustersv1alpha1.PreemptiveRequestFinalizerOnClusterPrefix)
				current := isr.scheduledPreemptiveRequests[uid]
				if current == nil {
					current = []*clustersv1alpha1.Cluster{}
				}
				current = append(current, c.DeepCopy())
				isr.scheduledPreemptiveRequests[uid] = current
			}
		}
		isr.Original[SRKey(c)] = c.DeepCopy()
	}

	// split requests and filter out irrelevant ones
	addPreemptive := []*SchedulingRequest{}
	removePreemptive := []*SchedulingRequest{}
	addRegular := []*SchedulingRequest{}
	removeRegular := []*SchedulingRequest{}
	for _, req := range requests {
		switch req.IsPreemptive() {
		case true:
			switch req.Remove {
			case true:
				if _, ok := isr.scheduledPreemptiveRequests[req.UID]; ok {
					removePreemptive = append(removePreemptive, req)
				}
			case false:
				addPreemptive = append(addPreemptive, req)
			}
		case false:
			switch req.Remove {
			case true:
				if _, ok := isr.scheduledRegularRequests[req.UID]; ok {
					removeRegular = append(removeRegular, req)
				}
			case false:
				if _, ok := isr.scheduledRegularRequests[req.UID]; !ok {
					addRegular = append(addRegular, req)
				}
			}
		}
	}

	// handle regular requests first, because they might replace preemptive requests
	// remove requests first, because they might free up clusters for new requests
	for _, req := range removeRegular {
		isr.removeRequest(ctx, req)
	}
	// add regular requests
	for _, req := range addRegular {
		if err := isr.addRequest(ctx, req, cDef, strategy); err != nil {
			return nil, fmt.Errorf("error adding regular request '%s': %w", req.UID, err)
		}
	}
	// remove preemptive requests
	for _, req := range removePreemptive {
		isr.removeRequest(ctx, req)
	}
	// add preemptive requests
	for _, req := range addPreemptive {
		if err := isr.addRequest(ctx, req, cDef, strategy); err != nil {
			return nil, fmt.Errorf("error adding preemptive request '%s': %w", req.UID, err)
		}
	}
	// identify preemptive requests that need to be rescheduled because their cluster exceeded its tenancy limit
	isr.identifyToBeRescheduledRequests(ctx, cDef)

	return isr.SchedulingResult, nil
}

// newOrRecoveredCluster tries to prevent the deletion of an existing cluster or creates a new one if none can be recovered.
// It adds the request's finalizer to the cluster.
func (sr *internalSchedulingResult) newOrRecoveredCluster(ctx context.Context, req *SchedulingRequest, cDef *config.ClusterDefinition) *clustersv1alpha1.Cluster {
	log := logging.FromContextOrPanic(ctx)
	var c *clustersv1alpha1.Cluster
	if len(sr.Delete) > 0 {
		c := maps.GetAny(sr.Delete).Value
		if AddFinalizer(c, req.Finalizer(), true) {
			sr.Patch[SRKey(c)] = c
		}
		delete(sr.Delete, SRKey(c))
		log.Debug("Recovering to-be-deleted cluster to satisfy request", "clusterName", c.Name, "clusterNamespace")
	} else {
		c = initializeNewCluster(ctx, req, cDef)
		sr.Create = append(sr.Create, c)
		log.Debug("Issuing creation of a new cluster to satisfy request", "clusterName", c.Name, "clusterNamespace", c.Namespace)
	}

	if req.IsPreemptive() {
		current := sr.scheduledPreemptiveRequests[req.UID]
		if current == nil {
			current = []*clustersv1alpha1.Cluster{}
		}
		current = append(current, c)
		sr.scheduledPreemptiveRequests[req.UID] = current
	} else {
		sr.scheduledRegularRequests[req.UID] = c
	}
	return c
}

func (sr *internalSchedulingResult) removeRequest(ctx context.Context, req *SchedulingRequest) {
	logName := "RemoveRegular"
	var clusters sets.Set[*clustersv1alpha1.Cluster]
	var ok bool
	if req.IsPreemptive() {
		logName = "RemovePreemptive"
		var tmp []*clustersv1alpha1.Cluster
		tmp, ok = sr.scheduledPreemptiveRequests[req.UID]
		if ok {
			clusters = sets.New[*clustersv1alpha1.Cluster](tmp...)
		}
	} else {
		var c *clustersv1alpha1.Cluster
		c, ok = sr.scheduledRegularRequests[req.UID]
		if ok {
			clusters = sets.New[*clustersv1alpha1.Cluster](c)
		}
	}
	log := logging.FromContextOrPanic(ctx).WithName(logName).WithValues("requestUID", req.UID)
	if !ok {
		log.Debug("Request not scheduled on any cluster, nothing to do")
		return
	}
	for c := range clusters {
		if RemoveFinalizer(c, req.Finalizer(), true) {
			log.Debug("Unscheduling request from cluster", "clusterName", c.Name, "clusterNamespace", c.Namespace)
		}
		// if this was the last request on the cluster and it has the DeleteWithoutRequestsLabel, mark it for deletion
		deleteIt := false
		if c.GetTenancyCount() == 0 && c.GetPreemptiveTenancyCount() == 0 {
			val, ok := ctrlutils.GetLabel(c, clustersv1alpha1.DeleteWithoutRequestsLabel)
			if ok && val == "true" {
				log.Debug("Cluster has no more requests, marking for deletion", "clusterName", c.Name, "clusterNamespace", c.Namespace)
				deleteIt = true
				sr.Delete[SRKey(c)] = c
			} else {
				log.Debug("Cluster has no more requests, but the label is missing or not set to 'true', not marking it for deletion", "clusterName", c.Name, "clusterNamespace", c.Namespace, "label", clustersv1alpha1.DeleteWithoutRequestsLabel, "labelValue", val)
			}
		}
		if !deleteIt {
			// if the cluster is not marked for deletion, it needs to be patched
			sr.Patch[SRKey(c)] = c
		}
	}
	if req.IsPreemptive() {
		delete(sr.scheduledPreemptiveRequests, req.UID)
	} else {
		delete(sr.scheduledRegularRequests, req.UID)
	}
}

func (sr *internalSchedulingResult) addRequest(ctx context.Context, req *SchedulingRequest, cDef *config.ClusterDefinition, stratKey config.Strategy) error {
	logName := "AddRegular"
	var clusters []*clustersv1alpha1.Cluster
	if req.IsPreemptive() {
		logName = "RemovePreemptive"
		clusters = sr.scheduledPreemptiveRequests[req.UID]
	} else {
		c, ok := sr.scheduledRegularRequests[req.UID]
		if ok {
			clusters = []*clustersv1alpha1.Cluster{c}
		}
	}
	log := logging.FromContextOrPanic(ctx).WithName(logName).WithValues("requestUID", req.UID)
	ctx = logging.NewContext(ctx, log)
	currentCount := len(clusters)

	// map new and existing clusters to their remaining capacity
	existingClusters, newClusters := sr.Current()
	existingClustersCap := capacityMapping(existingClusters, req, cDef, false, false)
	newClustersCap := capacityMapping(newClusters, req, cDef, false, false)

	// TODO: if the request is preemptive, check if we can re-assign some request finalizers to potentiall free existing clusters

	for range max(0, req.WorkloadCount()-currentCount) {
		isNewCluster := false
		// check existing clusters first
		availableClustersCap := existingClustersCap
		if !req.IsPreemptive() {
			existingReadyClusters := filters.FilterSlice(existingClustersCap, func(args ...any) bool {
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
			isNewCluster = true
		}
		var chosenPair *pairs.Pair[*clustersv1alpha1.Cluster, int]
		if len(availableClustersCap) > 0 {
			// choose a cluster using the configured strategy
			strat := strategy.FromConfig[*pairs.Pair[*clustersv1alpha1.Cluster, int]](stratKey)
			log.Debug("Fitting clusters available, choosing one according to strategy", "availableClusters", len(availableClustersCap), "strategy", strat.Name())
			var err error
			chosenPair, err = strat.Choose(ctx, availableClustersCap, func(p *pairs.Pair[*clustersv1alpha1.Cluster, int]) *clustersv1alpha1.Cluster {
				return p.Key
			}, cDef, req.IsPreemptive())
			if err != nil {
				return fmt.Errorf("error choosing cluster for request '%s': %w", req.UID, err)
			}
		}
		if chosenPair != nil {
			// found a fitting cluster
			// add request finalizer to the cluster
			if AddFinalizer(chosenPair.Key, req.Finalizer(), req.IsPreemptive()) && !isNewCluster {
				// if the cluster is not new, it needs to be patched
				sr.Patch[SRKey(chosenPair.Key)] = chosenPair.Key
			}
			// re-compute cluster capacity
			chosenPair.Value = strategy.Capacity(chosenPair.Key, cDef, req.IsPreemptive())
		} else {
			// no cluster could be chosen
			log.Debug("Either no fitting clusters were available or the strategy did not choose a cluster, creating a new one", "availableClusters", len(availableClustersCap), "strategy", stratKey)
			isNewCluster = true
			c := sr.newOrRecoveredCluster(ctx, req, cDef)
			chosenPair = ptr.To(pairs.New(c, strategy.Capacity(c, cDef, req.IsPreemptive())))
			newClustersCap = append(newClustersCap, chosenPair)
		}
		log.Debug("Scheduling request on cluster", "clusterName", chosenPair.Key.Name, "clusterGenerateName", chosenPair.Key.GenerateName, "clusterNamespace", chosenPair.Key.Namespace, "isNewCluster", isNewCluster)
		if req.IsPreemptive() {
			current := sr.scheduledPreemptiveRequests[req.UID]
			if current == nil {
				current = []*clustersv1alpha1.Cluster{}
			}
			current = append(current, chosenPair.Key)
			sr.scheduledPreemptiveRequests[req.UID] = current
		} else {
			sr.scheduledRegularRequests[req.UID] = chosenPair.Key
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

// identifyToBeRescheduledRequests returns a set of preemptive request UIDs that have been removed from clusters which exceeded their tenancy limit.
func (sr *internalSchedulingResult) identifyToBeRescheduledRequests(ctx context.Context, cDef *config.ClusterDefinition) {
	log := logging.FromContextOrPanic(ctx)
	if cDef.IsSharedUnlimitedly() {
		log.Debug("ClusterDefinition is shared unlimitedly, no preemptive requests to reschedule")
		return
	}
	currentClusters, _ := sr.Current() // we can ignore new clusters here, since regular requests are scheduled first, so there shouldn't be any replaced preemptive requests in new clusters
	for _, c := range currentClusters {
		pCount := c.GetPreemptiveTenancyCount()
		rCount := c.GetTenancyCount()
		exceed := (rCount + pCount) - cDef.TenancyCount
		// remove exceeding preemptive requests
		changed := false
		for exceed > 0 {
			pUIDs := c.GetPreemptiveRequestUIDs()
			if pUIDs.Len() == 0 {
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
		}
	}
}

// initializeNewCluster creates a new Cluster resource based on the given ClusterRequest and ClusterDefinition.
// It directly adds the request's finalizer to the Cluster.
func initializeNewCluster(ctx context.Context, req *SchedulingRequest, cDef *config.ClusterDefinition) *clustersv1alpha1.Cluster {
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
