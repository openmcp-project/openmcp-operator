package utils

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
)

// ClusterSelectorPredicate returns a typed predicate that filters objects that implement the ObjectWithPurposes interface based on the given selector.
// Note that the predicate does not validate the integrity of the selector, this should be done beforehand.
// The untyped predicate can be obtained via ClusterSelectorPredicateUntyped.
func ClusterSelectorPredicate[Obj clustersv1alpha1.ObjectWithPurposes](s clustersv1alpha1.Selector) predicate.TypedPredicate[Obj] {
	return predicate.NewTypedPredicateFuncs(func(obj Obj) bool {
		return s.Matches(obj)
	})
}

// ClusterSelectorPredicateUntyped returns an untyped predicate that filters objects that implement the ObjectWithPurposes interface based on the given selector.
// Note that the predicate does not validate the integrity of the selector, this should be done beforehand.
// The typed predicate can be obtained via ClusterSelectorPredicate.
func ClusterSelectorPredicateUntyped(s clustersv1alpha1.Selector) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		owp, ok := obj.(clustersv1alpha1.ObjectWithPurposes)
		return ok && s.Matches(owp)
	})
}
