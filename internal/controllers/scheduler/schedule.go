package scheduler

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/openmcp-project/controller-utils/pkg/collections/maps"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	"github.com/openmcp-project/openmcp-operator/internal/config"
)

// SchedulingResult describes the result of a scheduling operation.
// The Backup field contains deep copies of the original clusters, in a map structure for easy lookup. This can be used for patch operations.
//
//	Use SRKey to get the key for a Cluster.
//
// The Patch field contains clusters that need to be patched. The changes are already in the Cluster objects, but not in the Original map, which can therefore be used for diffing.
// The Create field contains clusters that need to be created.
// The Delete field contains clusters that need to be deleted.
// The Reschedule field contains UIDs of preemptive requests for which a reconciliation must be triggered, because some or all of their finalizers have been removed.
// The Clusters field holds the current clusters. The references are the same that were passed in originally and they differ from the ones in the Backup field.
//
//	It overlaps with the Patch and Create fields and lacks the clusters from the Delete field.
//	This is basically what the state should look like after applying the changes described in the Patch, Create, and Delete fields.
type SchedulingResult struct {
	Backup     map[string]*clustersv1alpha1.Cluster
	Clusters   []*clustersv1alpha1.Cluster
	Patch      map[string]*clustersv1alpha1.Cluster
	Create     []*clustersv1alpha1.Cluster // cannot be a map, because we use generateName for new clusters, which means they don't have a unique identifier until created
	Delete     map[string]*clustersv1alpha1.Cluster
	Reschedule sets.Set[string]
	id         string // to detect clusters that were newly created by this scheduling operation
}

// Apply applies the changes described in the SchedulingResult to the platform.
// It returns an updated list of clusters, including unchanged, patched, and newly created ones.
func (sr *SchedulingResult) Apply(ctx context.Context, platformClient client.Client) ([]*clustersv1alpha1.Cluster, errutils.ReasonableError) {
	errs := errutils.NewReasonableErrorList()
	res := make([]*clustersv1alpha1.Cluster, 0, len(sr.Backup)-len(sr.Delete)+len(sr.Create))

	// patch clusters
	for k, cOld := range sr.Backup {
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

// ID returns the unique identifier of this scheduling operation.
func (sr *SchedulingResult) ID() string {
	return sr.id
}

// HasCreated returns true if the given cluster was newly created by this scheduling operation.
// This basically checks whether the cluster has the SchedulerCreationIDLabel label set to the ID of this scheduling operation.
func (sr *SchedulingResult) HasCreated(c *clustersv1alpha1.Cluster) bool {
	return ctrlutils.HasLabelWithValue(c, clustersv1alpha1.SchedulerCreationIDLabel, sr.ID())
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
	id := controller.ReconcileIDFromContext(ctx)
	if id == "" {
		id = uuid.NewUUID()
	}
	isr := &internalSchedulingResult{
		SchedulingResult: &SchedulingResult{
			Backup:     make(map[string]*clustersv1alpha1.Cluster, len(clusters)),
			Clusters:   make([]*clustersv1alpha1.Cluster, 0, len(clusters)),
			Patch:      map[string]*clustersv1alpha1.Cluster{},
			Create:     []*clustersv1alpha1.Cluster{},
			Delete:     map[string]*clustersv1alpha1.Cluster{},
			Reschedule: sets.New[string](),
			id:         string(id),
		},
		scheduledRegularRequests:    newScheduledRequests(false),
		scheduledPreemptiveRequests: newScheduledRequests(true),
	}
	for _, c := range clusters {
		if c == nil {
			continue
		}
		isr.Clusters = append(isr.Clusters, c)
		isr.scheduledRegularRequests.setRequestsForCluster(c)
		isr.scheduledPreemptiveRequests.setRequestsForCluster(c)
		isr.Backup[SRKey(c)] = c.DeepCopy()
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
				if len(isr.scheduledPreemptiveRequests.getClustersForRequest(req.UID)) > 0 {
					removePreemptive = append(removePreemptive, req)
				}
			case false:
				addPreemptive = append(addPreemptive, req)
			}
		case false:
			switch req.Remove {
			case true:
				if len(isr.scheduledRegularRequests.getClustersForRequest(req.UID)) > 0 {
					removeRegular = append(removeRegular, req)
				}
			case false:
				if len(isr.scheduledRegularRequests.getClustersForRequest(req.UID)) == 0 {
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
		isr.freeClusters(ctx, req, cDef) // try to free clusters from this request's finalizers, if possible
		if err := isr.addRequest(ctx, req, cDef, strategy); err != nil {
			return nil, fmt.Errorf("error adding preemptive request '%s': %w", req.UID, err)
		}
	}
	// identify preemptive requests that need to be rescheduled because their cluster exceeded its tenancy limit
	isr.identifyToBeRescheduledRequests(ctx, cDef, len(removeRegular) > 0)

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
		c = sr.initializeNewCluster(ctx, req, cDef)
		sr.Create = append(sr.Create, c)
		sr.Clusters = append(sr.Clusters, c)
		log.Debug("Issuing creation of a new cluster to satisfy request", "clusterName", c.Name, "clusterNamespace", c.Namespace)
	}

	if req.IsPreemptive() {
		sr.scheduledPreemptiveRequests.setRequestsForCluster(c)
	} else {
		sr.scheduledRegularRequests.setRequestsForCluster(c)
	}
	return c
}
