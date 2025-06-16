package strategy

import (
	"context"
	"math/rand/v2"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/internal/config"
)

// Strategy defines how a cluster for scheduling is chosen if there are multiple fitting clusters.
type Strategy interface {
	// Name returns the strategy's name. For logging and debugging purposes only.
	Name() string
	// Choose selects a cluster from the provided list based on the strategy's logic.
	// It is assumed that all clusters in the list are fitting for the given ClusterDefinition and have enough capacity to host the request.
	// Might return nil if it cannot choose a cluster (e.g., if the list is empty).
	Choose(ctx context.Context, clusters []*clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, preemptive bool) (*clustersv1alpha1.Cluster, error)
}

type strategyImpl struct {
	name   string
	choose func(ctx context.Context, clusters []*clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, preemptive bool) (*clustersv1alpha1.Cluster, error)
}

func (s *strategyImpl) Name() string {
	return s.name
}

func (s *strategyImpl) Choose(ctx context.Context, clusters []*clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, preemptive bool) (*clustersv1alpha1.Cluster, error) {
	return s.choose(ctx, clusters, cDef, preemptive)
}

// Implement returns a Strategy implementation that uses the provided choose function to select a cluster.
func Implement(name string, chooseFunc func(ctx context.Context, clusters []*clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, preemptive bool) (*clustersv1alpha1.Cluster, error)) Strategy {
	return &strategyImpl{name: name, choose: chooseFunc}
}

// FromConfig returns a Strategy implementation based on the provided config.Strategy value.
// If the value is not recognized, it defaults to Balanced.
func FromConfig(s config.Strategy) Strategy {
	switch s {
	case config.STRATEGY_RANDOM:
		return Random
	case config.STRATEGY_SIMPLE:
		return Simple
	case config.STRATEGY_BALANCED:
		return Balanced
	case config.STRATEGY_BALANCED_IGNORE_EMPTY:
		return BalancedIgnoreEmpty
	default:
		return Balanced
	}
}

var (
	// Random chooses a cluster at random.
	Random = Implement("Random", func(ctx context.Context, clusters []*clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, preemptive bool) (*clustersv1alpha1.Cluster, error) {
		if len(clusters) == 0 {
			return nil, nil
		}
		return clusters[rand.IntN(len(clusters))], nil
	})

	// Simple chooses the first cluster in the list.
	Simple = Implement("Simple", func(ctx context.Context, clusters []*clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, preemptive bool) (*clustersv1alpha1.Cluster, error) {
		if len(clusters) == 0 {
			return nil, nil
		}
		return clusters[0], nil
	})

	// Balanced chooses the cluster with the least number of requests, or the first one in case of a tie (preemptive requests are ignored).
	// For preemptive requests, the logic works the other way around and tries to find the cluster with the most requests (including preemptive ones).
	Balanced = &balanced{
		ignoreEmpty: false,
	}

	// BalancedIgnoreEmpty works like Balanced, but it favors clusters that have at least one request over empty clusters.
	// This means that it only really starts balancing after some requests have been deleted and just 'fills up' at the beginning,
	// but it can prevent unnecessary cluster creation when preemptive requests are used.
	// The logic for preemptive requests is the same as for Balanced.
	BalancedIgnoreEmpty = &balanced{
		ignoreEmpty: true,
	}
)

type balanced struct {
	ignoreEmpty bool
}

func (s *balanced) Name() string {
	if s.ignoreEmpty {
		return "BalancedIgnoreEmpty"
	}
	return "Balanced"
}

func (s *balanced) Choose(ctx context.Context, clusters []*clustersv1alpha1.Cluster, cDef *config.ClusterDefinition, preemptive bool) (*clustersv1alpha1.Cluster, error) {
	if len(clusters) == 0 {
		return nil, nil
	}
	cluster := clusters[0]
	if preemptive {
		count := cluster.GetTenancyCount() + cluster.GetPreemptiveTenancyCount()
		for _, c := range clusters[1:] {
			tmp := c.GetTenancyCount() + c.GetPreemptiveTenancyCount()
			if tmp > count {
				count = tmp
				cluster = c
			}
		}
	} else {
		count := cluster.GetTenancyCount()
		for _, c := range clusters[1:] {
			tmp := c.GetTenancyCount()
			if s.ignoreEmpty {
				if tmp != 0 && (tmp < count || count == 0) {
					count = tmp
					cluster = c
				}
			} else {
				if tmp < count {
					count = tmp
					cluster = c
				}
			}
		}
	}
	return cluster, nil
}
