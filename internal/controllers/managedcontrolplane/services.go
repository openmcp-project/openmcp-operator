package managedcontrolplane

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	corev2alpha1 "github.com/openmcp-project/openmcp-operator/api/core/v2alpha1"
	providerv1alpha1 "github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
)

// deleteDependingServices deletes service resources that belong to service providers which have a 'services.openmcp.cloud/<name>' finalizer on the ManagedControlPlane.
// It returns a set of service provider names for which still resources exist (should be in deletion by the time this function returns) and the total number of resources that are still left.
// Deletion of the MCP should wait until the set is empty and the count is zero.
func (r *ManagedControlPlaneReconciler) deleteDependingServices(ctx context.Context, mcp *corev2alpha1.ManagedControlPlaneV2) (map[string][]*unstructured.Unstructured, errutils.ReasonableError) {
	log := logging.FromContextOrPanic(ctx)

	// delete depending service resources, if any
	serviceProviderNames := sets.New[string]()

	if mcp == nil {
		log.Debug("MCP is nil, no need to check for services")
		return nil, nil
	}

	// identify service finalizers
	for _, fin := range mcp.Finalizers {
		if service, ok := strings.CutPrefix(fin, corev2alpha1.ServiceDependencyFinalizerPrefix); ok {
			serviceProviderNames.Insert(service)
		}
	}

	if serviceProviderNames.Len() == 0 {
		log.Debug("No service finalizers found on MCP")
		return nil, nil
	}

	// fetch service resources, if any exist
	resources := map[string][]*unstructured.Unstructured{}
	errs := errutils.NewReasonableErrorList()
	for providerName := range serviceProviderNames {
		sp := &providerv1alpha1.ServiceProvider{}
		sp.SetName(providerName)
		if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(sp), sp); err != nil {
			errs.Append(errutils.WithReason(fmt.Errorf("failed to get ServiceProvider %s: %w", providerName, err), cconst.ReasonPlatformClusterInteractionProblem))
			continue
		}

		if len(sp.Status.Resources) == 0 {
			errs.Append(errutils.WithReason(fmt.Errorf("a dependency finalizer for ServiceProvider '%s' exist on MCP, but the provider does not expose any service resources", providerName), cconst.ReasonInternalError))
		}
		serviceResources := []*unstructured.Unstructured{}
		for _, resourceType := range sp.Status.Resources {
			res := &unstructured.Unstructured{}
			res.SetAPIVersion(resourceType.Group + "/" + resourceType.Version)
			res.SetKind(resourceType.Kind)
			res.SetName(mcp.Name)
			res.SetNamespace(mcp.Namespace)
			if err := r.OnboardingCluster.Client().Get(ctx, client.ObjectKeyFromObject(res), res); err != nil {
				if !apierrors.IsNotFound(err) {
					errs.Append(errutils.WithReason(fmt.Errorf("error getting service resource [%s.%s] '%s/%s' for ServiceProvider '%s': %w", res.GetKind(), res.GetAPIVersion(), res.GetNamespace(), res.GetName(), providerName, err), cconst.ReasonOnboardingClusterInteractionProblem))
				}
				continue
			}
			serviceResources = append(serviceResources, res)
		}

		resources[providerName] = serviceResources
	}
	if rerr := errs.Aggregate(); rerr != nil {
		return nil, rerr
	}

	// delete service resources
	errs = errutils.NewReasonableErrorList()
	remainingResources := map[string][]*unstructured.Unstructured{}
	for providerName, serviceResources := range resources {
		if len(serviceResources) == 0 {
			log.Debug("No service resources found for ServiceProvider", "providerName", providerName)
			continue
		}
		remainingServiceResources := []*unstructured.Unstructured{}
		for _, res := range serviceResources {
			if !res.GetDeletionTimestamp().IsZero() {
				log.Debug("Service resource already marked for deletion", "resourceKind", res.GetKind(), "resourceAPIVersion", res.GetAPIVersion(), "provider", providerName)
				remainingServiceResources = append(remainingServiceResources, res)
				continue
			}
			log.Info("Deleting service resource", "resourceKind", res.GetKind(), "resourceAPIVersion", res.GetAPIVersion(), "provider", providerName)
			if err := r.OnboardingCluster.Client().Delete(ctx, res); err != nil {
				if !apierrors.IsNotFound(err) {
					errs.Append(errutils.WithReason(fmt.Errorf("error deleting service resource [%s.%s] '%s/%s' for ServiceProvider '%s': %w", res.GetKind(), res.GetAPIVersion(), res.GetNamespace(), res.GetName(), providerName, err), cconst.ReasonOnboardingClusterInteractionProblem))
				} else {
					log.Debug("Service resource not found during deletion", "resourceKind", res.GetKind(), "resourceAPIVersion", res.GetAPIVersion(), "provider", providerName)
				}
				continue
			}
			remainingServiceResources = append(remainingServiceResources, res)
		}
		if len(remainingServiceResources) > 0 {
			remainingResources[providerName] = remainingServiceResources
		}
	}
	if rerr := errs.Aggregate(); rerr != nil {
		return remainingResources, rerr
	}

	return remainingResources, nil
}
