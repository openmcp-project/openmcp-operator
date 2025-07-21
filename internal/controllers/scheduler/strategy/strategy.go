package strategy

import (
	"context"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/internal/config"
)

const MaxInt = int(^uint(0) >> 1)

// Strategy defines how a cluster for scheduling is chosen if there are multiple fitting clusters.
type Strategy[T any] interface {
	// Name returns the strategy's name. For logging and debugging purposes only.
	Name() string
	// Choose selects a cluster from the provided clusterData slice based on the strategy's logic.
	// The getCluster function must be able to extract the cluster from any element of clusterData.
	// Returns the zero value for T if the strategy cannot choose a cluster (e.g., if clusterData is empty).
	Choose(ctx context.Context, clusterData []T, getCluster func(T) *clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, preemptive bool) (T, error)
}

type strategyImpl[T any] struct {
	name   string
	choose func(ctx context.Context, clusterData []T, getCluster func(T) *clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, preemptive bool) (T, error)
}

func (s *strategyImpl[T]) Name() string {
	return s.name
}

func (s *strategyImpl[T]) Choose(ctx context.Context, clusterData []T, getCluster func(T) *clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, preemptive bool) (T, error) {
	return s.choose(ctx, clusterData, getCluster, cDef, preemptive)
}

// Implement returns a Strategy implementation that uses the provided choose function to select a cluster.
func Implement[T any](name string, chooseFunc func(ctx context.Context, clusterData []T, getCluster func(T) *clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, preemptive bool) (T, error)) Strategy[T] {
	return &strategyImpl[T]{name: name, choose: chooseFunc}
}

// FromConfig returns a Strategy implementation based on the provided config.Strategy value.
// If the value is not recognized, it defaults to Balanced.
func FromConfig[T any](s config.Strategy) Strategy[T] {
	switch s {
	case config.STRATEGY_BALANCED:
		return Balanced[T]()
	case config.STRATEGY_BALANCED_IGNORE_EMPTY:
		return BalancedIgnoreEmpty[T]()
	default:
		return BalancedIgnoreEmpty[T]()
	}
}

// Balanced chooses the cluster with the least number of requests, or the first one in case of a tie (preemptive requests are ignored).
// For preemptive requests, the logic works the other way around and tries to find the cluster with the most requests (including preemptive ones).
func Balanced[T any]() Strategy[T] {
	return &balanced[T]{
		ignoreEmpty: false,
	}
}

// BalancedIgnoreEmpty works like Balanced, but it favors clusters that have at least one request over empty clusters.
// This means that it only really starts balancing after some requests have been deleted and just 'fills up' at the beginning,
// but it can prevent unnecessary cluster creation when preemptive requests are used.
// The logic for preemptive requests is the same as for Balanced.
func BalancedIgnoreEmpty[T any]() Strategy[T] {
	return &balanced[T]{
		ignoreEmpty: true,
	}
}

type balanced[T any] struct {
	ignoreEmpty bool
}

func (s *balanced[T]) Name() string {
	if s.ignoreEmpty {
		return "BalancedIgnoreEmpty"
	}
	return "Balanced"
}

func (s *balanced[T]) Choose(ctx context.Context, clusterData []T, getCluster func(T) *clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, preemptive bool) (T, error) {
	var zero T
	if len(clusterData) == 0 {
		return zero, nil
	}
	chosen := clusterData[0]
	count := WorkloadCount(getCluster(chosen), preemptive)
	if preemptive {
		for _, cd := range clusterData[1:] {
			tmp := WorkloadCount(getCluster(cd), preemptive)
			if tmp > count {
				count = tmp
				chosen = cd
			}
		}
	} else {
		for _, cd := range clusterData[1:] {
			tmp := WorkloadCount(getCluster(cd), preemptive)
			if s.ignoreEmpty {
				if tmp != 0 && (tmp < count || count == 0) {
					count = tmp
					chosen = cd
				}
			} else {
				if tmp < count {
					count = tmp
					chosen = cd
				}
			}
		}
	}
	return chosen, nil
}

// Capacity returns the remaining capacity of a cluster based on its tenancy count and the ClusterDefinition.
// If the cluster is shared unlimitedly, it returns maxInt (unlimited capacity).
func Capacity(cluster *clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, preemptive bool) int {
	if cDef.IsSharedUnlimitedly() {
		return MaxInt // unlimited capacity
	}
	return cDef.TenancyCount - WorkloadCount(cluster, preemptive)
}

// WorkloadCount returns the number of workloads currently scheduled on the cluster.
// If preemptive is true, preemptive requests are also counted.
func WorkloadCount(cluster *clustersv1alpha1.Cluster, preemptive bool) int {
	count := cluster.GetTenancyCount()
	if preemptive {
		count += cluster.GetPreemptiveTenancyCount()
	}
	return count
}
