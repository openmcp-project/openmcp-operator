package managedcontrolplane

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	corev2alpha1 "github.com/openmcp-project/openmcp-operator/api/core/v2alpha1"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"
)

func (r *ManagedControlPlaneReconciler) deleteRelatedClusterRequests(ctx context.Context, mcp *corev2alpha1.ManagedControlPlane) (sets.Set[string], errutils.ReasonableError) {
	log := logging.FromContextOrPanic(ctx)

	// delete depending cluster requests, if any
	crNames := sets.New[string]()

	if mcp == nil {
		log.Debug("MCP is nil, no need to check for cluster requests")
		return crNames, nil
	}

	// identify cluster request finalizers
	for _, fin := range mcp.Finalizers {
		if crName, ok := strings.CutPrefix(fin, corev2alpha1.ClusterRequestFinalizerPrefix); ok {
			crNames.Insert(crName)
		}
	}

	if crNames.Len() == 0 {
		log.Debug("No cluster request finalizers found on MCP")
		return crNames, nil
	}

	// fetch cluster requests, if any exist
	namespace := libutils.StableRequestNamespace(mcp.Namespace)
	resources := map[string]*clustersv1alpha1.ClusterRequest{}
	errs := errutils.NewReasonableErrorList()
	for crName := range crNames {
		cr := &clustersv1alpha1.ClusterRequest{}
		cr.SetName(crName)
		cr.SetNamespace(namespace)
		if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(cr), cr); err != nil {
			if !apierrors.IsNotFound(err) {
				errs.Append(errutils.WithReason(fmt.Errorf("error getting ClusterRequest '%s/%s': %w", namespace, crName, err), cconst.ReasonPlatformClusterInteractionProblem))
			}
			continue
		}
		resources[crName] = cr
	}
	if rerr := errs.Aggregate(); rerr != nil {
		return sets.KeySet(resources), rerr
	}

	// delete cluster requests
	errs = errutils.NewReasonableErrorList()
	for crName, cr := range resources {
		if crName == mcp.Name && len(resources) > 1 {
			// skip the MCP's main ClusterRequest for now
			// we want to make sure that all other ClusterRequests are deleted first
			// in case the corresponding clusters are hosting resources that depend on the MCP cluster
			log.Debug("Skipping deletion of MCP's primary ClusterRequest, because there are other ClusterRequests to delete first", "crName", crName, "namespace", cr.GetNamespace())
			continue
		}
		if !cr.GetDeletionTimestamp().IsZero() {
			log.Debug("ClusterRequest resource already marked for deletion", "crName", crName, "namespace", cr.GetNamespace())
			continue
		}
		log.Info("Deleting ClusterRequest", "crName", crName, "namespace", cr.GetNamespace())
		if err := r.PlatformCluster.Client().Delete(ctx, cr); err != nil {
			if !apierrors.IsNotFound(err) {
				errs.Append(errutils.WithReason(fmt.Errorf("error deleting ClusterRequest '%s/%s': %w", namespace, crName, err), cconst.ReasonPlatformClusterInteractionProblem))
			} else {
				log.Debug("ClusterRequest not found during deletion", "crName", crName, "namespace", cr.GetNamespace())
				delete(resources, crName) // remove from resources if not found
			}
		}
	}
	if rerr := errs.Aggregate(); rerr != nil {
		return sets.KeySet(resources), rerr
	}

	return sets.KeySet(resources), nil
}
