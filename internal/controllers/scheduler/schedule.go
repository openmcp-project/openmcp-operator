package scheduler

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openmcp-project/controller-utils/pkg/collections/filters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/logging"

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
	scheduledRegularRequests    map[string]*clustersv1alpha1.Cluster // for internal use, to track scheduled regular requests
	scheduledPreemptiveRequests map[string]*clustersv1alpha1.Cluster // for internal use, to track scheduled preemptive requests
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
	// Preemptive indicates whether this request is preemptive or not.
	Preemptive bool
	// Remove indicates whether this request is to be removed (true) or added (false).
	Remove bool
	// Namespace is the namespace of the k8s resource this request originated from.
	Namespace string
	// Purpose is the purpose of the request.
	Purpose string
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
	return RequestFinalizer(sr.UID, sr.Preemptive)
}

func NewSchedulingRequest(uid string, preemptive bool, remove bool, namespace, purpose string) *SchedulingRequest {
	return &SchedulingRequest{
		UID:        uid,
		Preemptive: preemptive,
		Remove:     remove,
		Namespace:  namespace,
		Purpose:    purpose,
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
func Schedule(ctx context.Context, clusters []*clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, strategy strategy.Strategy, requests ...*SchedulingRequest) (*SchedulingResult, error) {
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
		scheduledPreemptiveRequests: map[string]*clustersv1alpha1.Cluster{},
	}
	for _, c := range clusters {
		if c == nil {
			continue
		}
		for _, fin := range c.Finalizers {
			if strings.HasPrefix(fin, clustersv1alpha1.RequestFinalizerOnClusterPrefix) {
				isr.scheduledRegularRequests[strings.TrimPrefix(fin, clustersv1alpha1.RequestFinalizerOnClusterPrefix)] = c.DeepCopy()
			} else if strings.HasPrefix(fin, clustersv1alpha1.PreemptiveRequestFinalizerOnClusterPrefix) {
				isr.scheduledPreemptiveRequests[strings.TrimPrefix(fin, clustersv1alpha1.PreemptiveRequestFinalizerOnClusterPrefix)] = c.DeepCopy()
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
		switch req.Preemptive {
		case true:
			switch req.Remove {
			case true:
				if _, ok := isr.scheduledPreemptiveRequests[req.UID]; ok {
					removePreemptive = append(removePreemptive, req)
				}
			case false:
				if _, ok := isr.scheduledPreemptiveRequests[req.UID]; !ok {
					addPreemptive = append(addPreemptive, req)
				}
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
		c := GetAny(sr.Delete).Value
		if controllerutil.AddFinalizer(c, req.Finalizer()) {
			sr.Patch[SRKey(c)] = c
		}
		delete(sr.Delete, SRKey(c))
		log.Debug("Recovering to-be-deleted cluster to satisfy request", "clusterName", c.Name, "clusterNamespace")
	} else {
		c = initializeNewCluster(ctx, req, cDef)
		sr.Create = append(sr.Create, c)
		log.Debug("Issuing creation of a new cluster to satisfy request", "clusterName", c.Name, "clusterNamespace", c.Namespace)
	}

	if req.Preemptive {
		sr.scheduledPreemptiveRequests[req.UID] = c
	} else {
		sr.scheduledRegularRequests[req.UID] = c
	}
	return c
}

func (sr *internalSchedulingResult) removeRequest(ctx context.Context, req *SchedulingRequest) {
	logName := "RemoveRegular"
	uidMapping := sr.scheduledRegularRequests
	if req.Preemptive {
		logName = "RemovePreemptive"
		uidMapping = sr.scheduledPreemptiveRequests
	}
	log := logging.FromContextOrPanic(ctx).WithName(logName).WithValues("requestUID", req.UID)
	c, ok := uidMapping[req.UID]
	if !ok {
		log.Debug("Request not scheduled on any cluster, nothing to do")
		return
	}
	if controllerutil.RemoveFinalizer(c, req.Finalizer()) {
		log.Debug("Unscheduling request from cluster", "clusterName", c.Name, "clusterNamespace", c.Namespace)
		sr.Patch[SRKey(c)] = c
	}
	delete(uidMapping, req.UID)
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

func (sr *internalSchedulingResult) addRequest(ctx context.Context, req *SchedulingRequest, cDef *config.ClusterDefinition, strategy strategy.Strategy) error {
	logName := "AddRegular"
	uidMapping := sr.scheduledRegularRequests
	if req.Preemptive {
		logName = "AddPreemptive"
		uidMapping = sr.scheduledPreemptiveRequests
	}
	log := logging.FromContextOrPanic(ctx).WithName(logName).WithValues("requestUID", req.UID)
	ctx = logging.NewContext(ctx, log)
	isNewCluster := false
	existingClusters, newClusters := sr.Current()
	// check existing clusters first
	availableClusters := existingClusters
	// filter out clusters that are in deletion
	availableClusters = filters.FilterSlice(availableClusters, func(args ...any) bool {
		c := args[0].(*clustersv1alpha1.Cluster)
		return c.DeletionTimestamp.IsZero()
	})
	// filter out clusters that are already at their tenancy limit
	if !cDef.IsSharedUnlimitedly() {
		availableClusters = filters.FilterSlice(availableClusters, func(args ...any) bool {
			c := args[0].(*clustersv1alpha1.Cluster)
			tenCount := c.GetTenancyCount()
			if req.Preemptive {
				tenCount += c.GetPreemptiveTenancyCount()
			}
			return cDef.IsSharedUnlimitedly() || (tenCount < cDef.TenancyCount)
		})
	}
	if !req.Preemptive {
		// avoid choosing non-ready clusters if ready ones are available
		readyClusters := filters.FilterSlice(availableClusters, func(args ...any) bool {
			c := args[0].(*clustersv1alpha1.Cluster)
			return c.Status.Phase == clustersv1alpha1.CLUSTER_PHASE_READY
		})
		if len(readyClusters) > 0 {
			log.Debug("Excluding some non-ready clusters from scheduling", "availableClusters", len(availableClusters), "readyClusters", len(readyClusters))
			availableClusters = readyClusters
		}
	}
	if len(availableClusters) == 0 {
		// check new clusters if no fitting one exists
		availableClusters = newClusters
		isNewCluster = true
		// filter out clusters that are in deletion
		availableClusters = filters.FilterSlice(availableClusters, func(args ...any) bool {
			c := args[0].(*clustersv1alpha1.Cluster)
			return c.DeletionTimestamp.IsZero()
		})
		// filter out clusters that are already at their tenancy limit
		if !cDef.IsSharedUnlimitedly() {
			availableClusters = filters.FilterSlice(availableClusters, func(args ...any) bool {
				c := args[0].(*clustersv1alpha1.Cluster)
				tenCount := c.GetTenancyCount()
				if req.Preemptive {
					tenCount += c.GetPreemptiveTenancyCount()
				}
				return cDef.IsSharedUnlimitedly() || (tenCount < cDef.TenancyCount)
			})
		}
	}
	if len(availableClusters) == 0 {
		log.Debug("No clusters available for scheduling regular request")
		c := sr.newOrRecoveredCluster(ctx, req, cDef)
		uidMapping[req.UID] = c
		return nil
	}
	// choose a cluster using the configured strategy
	log.Debug("Fitting clusters available, choosing one according to strategy", "availableClusters", len(availableClusters), "strategy", strategy.Name())
	chosenCluster, err := strategy.Choose(ctx, availableClusters, cDef, req.Preemptive)
	if err != nil {
		return fmt.Errorf("error choosing cluster for request '%s': %w", req.UID, err)
	}
	if chosenCluster == nil {
		log.Debug("Strategy did not choose a cluster", "strategy", strategy.Name())
		c := sr.newOrRecoveredCluster(ctx, req, cDef)
		uidMapping[req.UID] = c
		return nil
	}
	if controllerutil.AddFinalizer(chosenCluster, req.Finalizer()) {
		if !isNewCluster {
			sr.Patch[SRKey(chosenCluster)] = chosenCluster
		}
	}
	log.Debug("Scheduling request on cluster", "clusterName", chosenCluster.Name, "clusterGenerateName", chosenCluster.GenerateName, "clusterNamespace", chosenCluster.Namespace, "isNewCluster", isNewCluster)
	uidMapping[req.UID] = chosenCluster
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
			p := GetAny(pUIDs).Key
			sr.Reschedule.Insert(p)
			controllerutil.RemoveFinalizer(c, RequestFinalizer(p, true))
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
