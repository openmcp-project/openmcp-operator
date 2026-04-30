package helm

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	testutils "github.com/openmcp-project/controller-utils/pkg/testing"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"
	helmv1alpha1 "github.com/openmcp-project/openmcp-operator/api/helm/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/lib/clusteraccess/advanced"
)

const (
	SubControllerName = "Cluster"
	clusterAccessID   = "cluster"
)

// newHelmDeploymentClusterController creates a new cluster controller which is used internally by the HelmDeployment controller.
// It returns the controller and a function to trigger a reconciliation of a specific cluster.
func newHelmDeploymentClusterController(platformCluster *clusters.Cluster, providerName string) (*helmDeploymentClusterController, func(*clustersv1alpha1.Cluster)) {
	c := &helmDeploymentClusterController{
		PlatformCluster:    platformCluster,
		ProviderName:       providerName,
		clustersWithAccess: make(map[string]accessStatus),
		lock:               &sync.RWMutex{},
		car: advanced.NewClusterAccessReconciler(platformCluster.Client(), fmt.Sprintf("%s/%s", ControllerName, SubControllerName)).
			WithManagedLabels(func(controllerName string, req ctrl.Request, _ advanced.ClusterRegistration) (string, string, map[string]string) {
				return fmt.Sprintf("%s.%s", providerName, controllerName), req.Name, nil
			}).
			Register(
				advanced.ExistingCluster(clusterAccessID, "", advanced.IdentityReferenceGenerator).
					WithScheme(platformCluster.Scheme()).
					WithNamespaceGenerator(advanced.RequestNamespaceGenerator).
					WithTokenAccess(&clustersv1alpha1.TokenConfig{
						RoleRefs: []commonapi.RoleRef{
							{
								Kind: "ClusterRole",
								Name: "cluster-admin",
							},
						},
					}).
					Build()),
		trigger: make(chan event.TypedGenericEvent[*clustersv1alpha1.Cluster], 1024),
	}

	return c, func(cluster *clustersv1alpha1.Cluster) {
		if cluster != nil {
			c.trigger <- event.TypedGenericEvent[*clustersv1alpha1.Cluster]{Object: cluster}
		}
	}
}

type helmDeploymentClusterController struct {
	PlatformCluster    *clusters.Cluster
	ProviderName       string
	car                advanced.ClusterAccessReconciler
	lock               *sync.RWMutex
	clustersWithAccess map[string]accessStatus
	trigger            chan event.TypedGenericEvent[*clustersv1alpha1.Cluster]
}

type accessStatus int

const (
	accessStatusWaiting accessStatus = iota
	accessStatusReady
	accessStatusInDeletion
	accessStatusDeletingButRequired // this means that deletion of the access was triggered, but then something changed and now access is required again
)

func (c *helmDeploymentClusterController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(ControllerName).WithName(SubControllerName)
	ctx = logging.NewContext(ctx, log)
	log.Info("Starting reconcile")

	return c.reconcile(ctx, req)
}

func (c *helmDeploymentClusterController) reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logging.FromContextOrPanic(ctx)

	// fetch cluster
	cluster := &clustersv1alpha1.Cluster{}
	if err := c.PlatformCluster.Client().Get(ctx, req.NamespacedName, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Resource not found")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("unable to fetch resource from cluster: %w", err)
	}

	// handle operation annotation
	if cluster.GetAnnotations() != nil {
		op, ok := cluster.GetAnnotations()[openmcpconst.OperationAnnotation]
		if ok {
			switch op {
			case openmcpconst.OperationAnnotationValueIgnore:
				log.Info("Ignoring resource due to ignore operation annotation")
				return ctrl.Result{}, nil
			case openmcpconst.OperationAnnotationValueReconcile:
				log.Debug("Removing reconcile operation annotation from resource")
				if err := ctrlutils.EnsureAnnotation(ctx, c.PlatformCluster.Client(), cluster, openmcpconst.OperationAnnotation, "", true, ctrlutils.DELETE); err != nil {
					return ctrl.Result{}, fmt.Errorf("error removing operation annotation: %w", err)
				}
			}
		}
	}

	hdoc := helmDeploymentsOnCluster(cluster)
	cID := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
	c.lock.Lock()
	defer c.lock.Unlock()
	as, isKnown := c.clustersWithAccess[cID]
	if hdoc.Len() > 0 && isKnown && as == accessStatusInDeletion {
		c.clustersWithAccess[cID] = accessStatusDeletingButRequired
		as = accessStatusDeletingButRequired
		log.Info("Access is required again, but deletion has already been triggered, cluster will be requeued after deletion has finished")
	}
	if (cluster.DeletionTimestamp.IsZero() || hdoc.Len() > 0) && (!isKnown || (as != accessStatusInDeletion && as != accessStatusDeletingButRequired)) {
		// Access is required as long as the cluster is not being deleted and there are matching HelmDeployments.
		// In addition, even when the cluster is in deletion, access has to be kept ready until all HelmDeployments have been removed from the cluster.
		// The only exception is if deletion of the AccessRequest has already been triggered, then it needs to be removed completely (and potentially re-created afterwards).
		v := logging.DEBUG
		if !isKnown {
			v = logging.INFO
			c.clustersWithAccess[cID] = accessStatusWaiting
		}
		log.Log(v, "Ensuring cluster access", "requiredByCount", hdoc.Len(), "requiredByUIDs", sets.List(hdoc))

		old := cluster.DeepCopy()
		if controllerutil.AddFinalizer(cluster, helmv1alpha1.AccessFinalizer) {
			if err := c.PlatformCluster.Client().Patch(ctx, cluster, client.MergeFrom(old)); err != nil {
				return ctrl.Result{}, fmt.Errorf("error adding access finalizer to cluster: %w", err)
			}
		}

		rr, err := c.car.Reconcile(ctx, req)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error reconciling cluster access: %w", err)
		}
		if rr.RequeueAfter > 0 {
			log.Debug("Waiting for cluster access to become ready", "requeueAfter", rr.RequeueAfter)
			return rr, nil
		}

		v = logging.DEBUG
		if c.clustersWithAccess[cID] != accessStatusReady {
			v = logging.INFO
		}
		log.Log(v, "Cluster access is ready")
		c.clustersWithAccess[cID] = accessStatusReady
		return ctrl.Result{}, nil
	} else {
		v := logging.DEBUG
		if isKnown && as != accessStatusInDeletion && as != accessStatusDeletingButRequired {
			v = logging.INFO
		}
		log.Log(v, "Access to cluster no longer required, cleaning up resources")

		if !slices.Contains(cluster.Finalizers, helmv1alpha1.AccessFinalizer) {
			log.Log(v, "Finalizer already removed, skipping cleanup")
			delete(c.clustersWithAccess, cID)
			return ctrl.Result{}, nil
		}

		c.clustersWithAccess[cID] = accessStatusInDeletion
		rr, err := c.car.ReconcileDelete(ctx, req)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error reconciling cluster access deletion: %w", err)
		}
		if rr.RequeueAfter > 0 {
			log.Debug("Waiting for cluster access to be deleted", "requeueAfter", rr.RequeueAfter)
			return rr, nil
		}

		log.Info("Cluster access is deleted")
		old := cluster.DeepCopy()
		if controllerutil.RemoveFinalizer(cluster, helmv1alpha1.AccessFinalizer) {
			if err := c.PlatformCluster.Client().Patch(ctx, cluster, client.MergeFrom(old)); err != nil {
				return ctrl.Result{}, fmt.Errorf("error removing access finalizer from cluster: %w", err)
			}
		}
		delete(c.clustersWithAccess, cID)
		if as == accessStatusDeletingButRequired {
			// If access became required again while deletion was in progress,
			// we need to requeue so that the access will be re-created.
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, nil
	}
}

// helmDeploymentsOnCluster returns the UIDs of HelmDeployments that currently have their finalizer on the cluster.
// Technically, this returns all finalizers starting with 'helm.open-control-plane.io/', with that prefix removed.
// The access finalizer, which is managed by the cluster controller, is ignored.
func helmDeploymentsOnCluster(cluster *clustersv1alpha1.Cluster) sets.Set[string] {
	res := sets.New[string]()
	if cluster == nil {
		return res
	}
	prefix := fmt.Sprintf("%s/", helmv1alpha1.GroupName)
	for _, f := range cluster.Finalizers {
		if f == helmv1alpha1.AccessFinalizer {
			continue
		}
		if fval, ok := strings.CutPrefix(f, prefix); ok {
			res.Insert(fval)
		}
	}

	return res
}

func (c *helmDeploymentClusterController) GetAccessRequestForCluster(ctx context.Context, cluster *clustersv1alpha1.Cluster) (*clustersv1alpha1.AccessRequest, error) {
	if cluster == nil {
		return nil, nil
	}
	c.lock.RLock()
	defer c.lock.RUnlock()
	cID := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
	if as, ok := c.clustersWithAccess[cID]; !ok || as != accessStatusReady {
		// There is currently no (ready) access for the cluster.
		return nil, nil
	}
	return c.car.AccessRequest(ctx, testutils.RequestFromObject(cluster), clusterAccessID)
}

// This controller is somewhat special, as it only reconciles if triggered manually.
func (c *helmDeploymentClusterController) setupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("cluster").
		WatchesRawSource(source.Channel(c.trigger, &handler.TypedEnqueueRequestForObject[*clustersv1alpha1.Cluster]{})).
		Complete(c)
}
