package garbagecollector

import (
	"context"
	"strings"
	"time"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/controller-utils/pkg/periodic"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	apiconst "github.com/openmcp-project/openmcp-operator/api/constants"

	"github.com/openmcp-project/openmcp-operator/internal/config"
)

const (
	OrphanedClusterRequestFinalizer = "OrphanedClusterRequestFinalizer"
)

func NewOrphanedClusterRequestFinalizerGarbageCollector(platformCluster *clusters.Cluster, log logging.Logger, getConfig config.GarbageCollectorConfigGetter) (periodic.Periodic[*config.GarbageCollectorConfig], error) {
	return periodic.NewPeriodicBuilder(GenerateGarbageCollectOrphanedClusterRequestFinalizer(platformCluster)).
		WithName(OrphanedClusterRequestFinalizer).
		WithConfigFuncs(func(ctx context.Context, cfg *config.GarbageCollectorConfig) (*config.GarbageCollectorConfig, error) {
			return getConfig(ctx)
		}).
		WithLogger(log.WithName(GarbageCollectorName).WithName(OrphanedClusterRequestFinalizer), false).
		Build()
}

func GenerateGarbageCollectOrphanedClusterRequestFinalizer(platformCluster *clusters.Cluster) func(ctx context.Context, p periodic.Periodic[*config.GarbageCollectorConfig], cfg *config.GarbageCollectorConfig) {
	return func(ctx context.Context, p periodic.Periodic[*config.GarbageCollectorConfig], cfg *config.GarbageCollectorConfig) {
		log := p.Log()
		// First, check whether this specific garbage collector module is enabled.
		if cfg == nil || !cfg.OrphanedClusterRequestFinalizer.Enabled {
			log.Debug("Garbage collector module is disabled, skipping garbage collection")
			return
			// We cannot call Disable() on the periodic, because we would need an external trigger to re-enable it, which we don't have at the moment.
		}
		// update interval
		if cfg.OrphanedClusterRequestFinalizer.Interval.Duration > 0 {
			p.SetInterval(cfg.OrphanedClusterRequestFinalizer.Interval.Duration)
		} else {
			p.SetInterval(cfg.Interval.Duration)
		}

		log.Info("Starting garbage collection")

		log.Debug("Listing Clusters")
		cls := &clustersv1alpha1.ClusterList{}
		if err := platformCluster.Client().List(ctx, cls); err != nil {
			log.Error(err, "Failed to list Clusters")
			return
		}

		log.Debug("Listing ClusterRequests")
		crs := &clustersv1alpha1.ClusterRequestList{}
		if err := platformCluster.Client().List(ctx, crs); err != nil {
			log.Error(err, "Failed to list ClusterRequests")
			return
		}
		crUIDs := sets.New[string]()
		for _, cr := range crs.Items {
			crUIDs.Insert(string(cr.UID))
		}

		log.Debug("Identifying and removing orphaned ClusterRequest finalizers")
		for _, cl := range cls.Items {
			clog := log.WithValues("clusterName", cl.Name, "clusterNamespace", cl.Namespace)

			// check garbage collector annotation
			val, ok := ctrlutils.GetAnnotation(&cl, apiconst.GarbageCollectorAnnotation)
			if ok {
				// check value of garbage collector annotation
				if val == apiconst.GarbageCollectorAnnotationValueIgnore {
					clog.Debug("Skipping Cluster due to ignore annotation")
					continue
				}
				// try to parse value as a timestamp
				gcTime, err := time.Parse(time.RFC3339, val)
				if err == nil {
					if gcTime.After(time.Now()) {
						clog.Debug("Postponing garbage collection due to garbage collection timestamp in the future", "timestamp", val)
						continue
					}
				} else {
					clog.Error(err, "Unknown value for garbage collector annotation, skipping garbage collection", "value", val)
					continue
				}
			}

			finsToRemove := sets.New[string]()
			for _, fin := range cl.Finalizers {
				uid, ok := strings.CutPrefix(fin, clustersv1alpha1.RequestFinalizerOnClusterPrefix)
				if !ok {
					continue
				}
				if !crUIDs.Has(uid) {
					finsToRemove.Insert(fin)
				}
			}
			if finsToRemove.Len() > 0 {
				newFins := []string{}
				for _, fin := range cl.Finalizers {
					if !finsToRemove.Has(fin) {
						newFins = append(newFins, fin)
					}
				}
				clOld := cl.DeepCopy()
				cl.Finalizers = newFins
				if err := platformCluster.Retry().Patch(ctx, &cl, client.MergeFrom(clOld)); err != nil {
					clog.Error(err, "Failed to patch Cluster with removed finalizers", "finalizersToRemove", sets.List(finsToRemove))
					continue
				}
				clog.Info("Successfully removed orphaned ClusterRequest finalizers from Cluster", "removedFinalizers", sets.List(finsToRemove), "remainingFinalizers", newFins)
			} else {
				clog.Debug("No orphaned ClusterRequest finalizers to remove for Cluster")
			}
		}

		log.Info("Finished garbage collection")
	}
}
