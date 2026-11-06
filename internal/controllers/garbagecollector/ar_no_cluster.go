package garbagecollector

import (
	"context"
	"time"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/controller-utils/pkg/periodic"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	apiconst "github.com/openmcp-project/openmcp-operator/api/constants"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	"github.com/openmcp-project/openmcp-operator/internal/config"
)

const (
	ARNoClusterGarbageCollectorName = "AccessRequestNoCluster"
)

func NewAccessRequestNoClusterGarbageCollector(platformCluster *clusters.Cluster, log logging.Logger, getConfig config.GarbageCollectorConfigGetter) (periodic.Periodic[*config.GarbageCollectorConfig], error) {
	return periodic.NewPeriodicBuilder(GenerateGarbageCollectAccessRequestsWithoutCluster(platformCluster)).
		WithName(ARNoClusterGarbageCollectorName).
		WithConfigFuncs(func(ctx context.Context, cfg *config.GarbageCollectorConfig) (*config.GarbageCollectorConfig, error) {
			return getConfig(ctx)
		}).
		WithLogger(log.WithName(GarbageCollectorName).WithName(ARNoClusterGarbageCollectorName), false).
		Build()
}

func GenerateGarbageCollectAccessRequestsWithoutCluster(platformCluster *clusters.Cluster) func(ctx context.Context, p periodic.Periodic[*config.GarbageCollectorConfig], cfg *config.GarbageCollectorConfig) {
	return func(ctx context.Context, p periodic.Periodic[*config.GarbageCollectorConfig], cfg *config.GarbageCollectorConfig) {
		log := p.Log()
		// First, check whether this specific garbage collector module is enabled.
		if cfg == nil || !cfg.AccessRequestNoCluster.Enabled {
			log.Debug("Garbage collector module is disabled, skipping garbage collection")
			return
			// We cannot call Disable() on the periodic, because we would need an external trigger to re-enable it, which we don't have at the moment.
		}
		// update interval
		if cfg.AccessRequestNoCluster.Interval.Duration > 0 {
			p.SetInterval(cfg.AccessRequestNoCluster.Interval.Duration)
		} else {
			p.SetInterval(cfg.Interval.Duration)
		}

		log.Info("Starting garbage collection")

		log.Debug("Listing AccessRequests")
		ars := &clustersv1alpha1.AccessRequestList{}
		if err := platformCluster.Client().List(ctx, ars); err != nil {
			log.Error(err, "Failed to list AccessRequests")
			return
		}

		log.Debug("Listing Clusters")
		cls := &clustersv1alpha1.ClusterList{}
		if err := platformCluster.Client().List(ctx, cls); err != nil {
			log.Error(err, "Failed to list Clusters")
			return
		}
		clusterLookup := sets.New[commonapi.ObjectReference]()
		for _, c := range cls.Items {
			clusterLookup.Insert(commonapi.ReferenceFromObject(&c))
		}

		log.Debug("Listing ClusterRequests")
		crs := &clustersv1alpha1.ClusterRequestList{}
		if err := platformCluster.Client().List(ctx, crs); err != nil {
			log.Error(err, "Failed to list ClusterRequests")
			return
		}
		crLookup := sets.New[commonapi.ObjectReference]()
		for _, cr := range crs.Items {
			crLookup.Insert(commonapi.ReferenceFromObject(&cr))
		}

		for _, ar := range ars.Items {
			alog := log.WithValues("accessRequestName", ar.Name, "accessRequestNamespace", ar.Namespace)
			val, ok := ctrlutils.GetAnnotation(&ar, apiconst.GarbageCollectorAnnotation)
			if ok {
				// check value of garbage collector annotation
				if val == apiconst.GarbageCollectorAnnotationValueIgnore {
					alog.Debug("Skipping AccessRequest due to ignore annotation")
					continue
				}
				// try to parse value as a timestamp
				gcTime, err := time.Parse(time.RFC3339, val)
				if err == nil {
					if gcTime.After(time.Now()) {
						alog.Debug("Postponing garbage collection due to garbage collection timestamp in the future", "timestamp", val)
						continue
					}
				} else {
					alog.Error(err, "Unknown value for garbage collector annotation, skipping garbage collection", "value", val)
					continue
				}
			}
			if ar.Spec.ClusterRef != nil {
				if clusterLookup.Has(*ar.Spec.ClusterRef) {
					alog.Debug("Cluster referenced in AccessRequest still exists, skipping garbage collection")
					continue
				}
				alog.Debug("Cluster referenced by AccessRequest is gone")
			} else if ar.Spec.RequestRef != nil {
				if crLookup.Has(*ar.Spec.RequestRef) {
					alog.Debug("ClusterRequest referenced in AccessRequest still exists, skipping garbage collection")
					continue
				}
				alog.Debug("ClusterRequest referenced by AccessRequest is gone")
			} else {
				alog.Debug("AccessRequest has neither ClusterRef nor RequestRef")
			}

			alog.Info("Force-deleting AccessRequest")
			if err := platformCluster.Client().Delete(ctx, &ar); client.IgnoreNotFound(err) != nil {
				alog.Error(err, "Failed to delete AccessRequest")
			}
			if len(ar.Finalizers) > 0 {
				alog.Debug("Removing finalizers from AccessRequest")
				arOld := ar.DeepCopy()
				ar.Finalizers = nil
				if err := platformCluster.Client().Patch(ctx, &ar, client.MergeFrom(arOld)); err != nil {
					alog.Error(err, "Failed to remove finalizers from AccessRequest")
				}
			}
		}

		log.Info("Finished garbage collection")
	}
}
