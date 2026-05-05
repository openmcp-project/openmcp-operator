package helm

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/collections"
	"github.com/openmcp-project/controller-utils/pkg/conditions"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/controller/smartrequeue"
	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	testutils "github.com/openmcp-project/controller-utils/pkg/testing"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"
	helmv1alpha1 "github.com/openmcp-project/openmcp-operator/api/helm/v1alpha1"
)

const (
	ControllerName = "HelmDeployment"

	EventActionReconcile = "Reconcile"
)

type HelmDeploymentController struct {
	PlatformCluster   *clusters.Cluster
	eventRecorder     events.EventRecorder
	ProviderName      string
	ProviderNamespace string
	Environment       string
	sr                *smartrequeue.Store
	cr                *HelmDeploymentClusterController
	enqueueCluster    func(*clustersv1alpha1.Cluster)
}

func NewHelmDeploymentController(platformCluster *clusters.Cluster, recorder events.EventRecorder, providerName, providerNamespace, environment string) *HelmDeploymentController {
	cr, enqueueCluster := newHelmDeploymentClusterController(platformCluster, providerName)
	return &HelmDeploymentController{
		PlatformCluster:   platformCluster,
		eventRecorder:     recorder,
		ProviderName:      providerName,
		ProviderNamespace: providerNamespace,
		Environment:       environment,
		sr:                smartrequeue.NewStore(30*time.Second, 24*time.Hour, 1.2),
		cr:                cr,
		enqueueCluster:    enqueueCluster,
	}
}

// TestHelmDeploymentController creates a new instance of the HelmDeploymentController.
// THIS CONSTRUCTOR IS MEANT FOR UNIT TESTS AND SHOULD NEVER BE CALLED OUTSIDE OF TESTS.
// It uses a queue instead of a channel for triggering cluster reconciliation, since this is easier to handle in unit tests.
func TestHelmDeploymentController(platformCluster *clusters.Cluster, providerName, providerNamespace, environment string, clusterReconciler *HelmDeploymentClusterController, clusterReconcileQueue collections.Queue[*clustersv1alpha1.Cluster]) *HelmDeploymentController {
	res := NewHelmDeploymentController(platformCluster, nil, providerName, providerNamespace, environment)
	res.cr = clusterReconciler
	res.enqueueCluster = func(cluster *clustersv1alpha1.Cluster) {
		if err := clusterReconcileQueue.Push(cluster); err != nil {
			panic(fmt.Errorf("queue ran out of space, this is not supposed to happen"))
		}
	}
	return res
}

type ReconcileResult struct {
	ctrlutils.ReconcileResult[*helmv1alpha1.HelmDeployment]

	// SourceKind indicates which kind of Flux source was reconciled for this HelmDeployment, if any.
	SourceKind string
	// SelectorDefinition is the selector definition of this HelmDeployment.
	SelectorDefinition helmv1alpha1.SelectorDefinition
	// Config is the provider configuration.
	Config *helmv1alpha1.HelmDeployerConfig
}

func (c *HelmDeploymentController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(ControllerName)
	ctx = logging.NewContext(ctx, log)
	log.Info("Starting reconcile")

	rr := c.reconcile(ctx, req)

	return ctrlutils.NewOpenMCPStatusUpdaterBuilder[*helmv1alpha1.HelmDeployment]().
		WithNestedStruct("Status").
		WithConditionUpdater(false).
		WithConditionEvents(c.eventRecorder, conditions.EventPerNewStatus).
		WithSmartRequeue(c.sr, func(rr ctrlutils.ReconcileResult[*helmv1alpha1.HelmDeployment]) ctrlutils.SmartRequeueAction {
			if rr.SmartRequeue == ctrlutils.SR_RESET {
				return ctrlutils.SR_RESET
			}
			for _, con := range rr.Object.Status.Conditions {
				if con.Status != metav1.ConditionTrue {
					return ctrlutils.SR_BACKOFF
				}
			}
			if rr.SmartRequeue != "" {
				return rr.SmartRequeue
			}
			return ctrlutils.SR_NO_REQUEUE
		}).
		WithPhaseUpdateFunc(func(obj *helmv1alpha1.HelmDeployment, rr ctrlutils.ReconcileResult[*helmv1alpha1.HelmDeployment]) (string, error) {
			if !rr.Object.DeletionTimestamp.IsZero() {
				return commonapi.StatusPhaseTerminating, nil
			}
			for _, con := range rr.Object.Status.Conditions {
				if con.Status != metav1.ConditionTrue {
					return commonapi.StatusPhaseProgressing, nil
				}
			}
			return commonapi.StatusPhaseReady, nil
		}).
		Build().
		UpdateStatus(ctx, c.PlatformCluster.Client(), rr.ReconcileResult)
}

func (c *HelmDeploymentController) reconcile(ctx context.Context, req ctrl.Request) ReconcileResult {
	log := logging.FromContextOrPanic(ctx)

	rr := ReconcileResult{}
	rr.SmartRequeue = ctrlutils.SR_NO_REQUEUE // we don't need to requeue by default

	// get HelmDeployment resource
	hd := &helmv1alpha1.HelmDeployment{}
	if err := c.PlatformCluster.Client().Get(ctx, req.NamespacedName, hd); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Resource not found")
			return ReconcileResult{}
		}
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to get resource '%s' from cluster: %w", req.String(), err), cconst.ReasonPlatformClusterInteractionProblem)
		return rr
	}

	// handle operation annotation
	if hd.GetAnnotations() != nil {
		op, ok := hd.GetAnnotations()[openmcpconst.OperationAnnotation]
		if ok {
			switch op {
			case openmcpconst.OperationAnnotationValueIgnore:
				log.Info("Ignoring resource due to ignore operation annotation")
				return ReconcileResult{}
			case openmcpconst.OperationAnnotationValueReconcile:
				log.Debug("Removing reconcile operation annotation from resource")
				if err := ctrlutils.EnsureAnnotation(ctx, c.PlatformCluster.Client(), hd, openmcpconst.OperationAnnotation, "", true, ctrlutils.DELETE); err != nil {
					rr.ReconcileError = errutils.WithReason(fmt.Errorf("error removing operation annotation: %w", err), cconst.ReasonPlatformClusterInteractionProblem)
					return rr
				}
			}
		}
	}
	rr.Object = hd
	rr.Conditions = []metav1.Condition{}

	// load provider config
	rr.Config = &helmv1alpha1.HelmDeployerConfig{}
	if err := c.PlatformCluster.Client().Get(ctx, types.NamespacedName{Name: c.ProviderName}, rr.Config); err != nil {
		if !apierrors.IsNotFound(err) {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to get HelmDeployerConfig '%s' from cluster: %w", c.ProviderName, err), cconst.ReasonPlatformClusterInteractionProblem)
			return rr
		}
		log.Info("Config not found, assuming default configuration", "config", c.ProviderName)
	}

	// determine selector definition for this HelmDeployment
	selectorDef, err := getSelectorDefinition(hd, rr.Config)
	if err != nil {
		rr.ReconcileError = err
		return rr
	}
	rr.SelectorDefinition = selectorDef

	expectedLabels := map[string]string{
		openmcpconst.ManagedByLabel:      ctrlutils.ShortenToXCharactersUnsafe(fmt.Sprintf("%s.%s", c.ProviderName, ControllerName), ctrlutils.K8sMaxNameLength),
		openmcpconst.ManagedPurposeLabel: ctrlutils.ShortenToXCharactersUnsafe(fmt.Sprintf("%s.%s", rr.Object.Namespace, rr.Object.Name), ctrlutils.K8sMaxNameLength),
	}

	if hd.DeletionTimestamp.IsZero() {
		rr = c.handleCreateOrUpdate(ctx, rr, expectedLabels)
	} else {
		rr = c.handleDelete(ctx, rr, expectedLabels)
	}

	return rr
}

func (c *HelmDeploymentController) handleCreateOrUpdate(ctx context.Context, rr ReconcileResult, expectedLabels map[string]string) ReconcileResult {
	log := logging.FromContextOrPanic(ctx)

	createCon := ctrlutils.GenerateCreateConditionFunc(&rr.ReconcileResult)

	// ensure finalizer on HelmDeployment
	old := rr.Object.DeepCopy()
	if controllerutil.AddFinalizer(rr.Object, helmv1alpha1.Finalizer) {
		log.Info("Adding finalizer to HelmDeployment")
		if err := c.PlatformCluster.Client().Patch(ctx, rr.Object, client.MergeFrom(old)); err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to add finalizer to HelmDeployment '%s/%s': %w", rr.Object.Namespace, rr.Object.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
			return rr
		}
	}

	// list all clusters
	cl := &clustersv1alpha1.ClusterList{}
	if err := c.PlatformCluster.Client().List(ctx, cl); err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to list Clusters from platform cluster: %w", err), cconst.ReasonPlatformClusterInteractionProblem)
		return rr
	}

	errs := errutils.NewReasonableErrorList()
	for _, cluster := range cl.Items {
		cID := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
		clog := log.WithValues("cluster", cID)
		cct := clusterConType(cluster.Namespace, cluster.Name)

		expectedLabelsForCluster := maps.Clone(expectedLabels)
		expectedLabelsForCluster[helmv1alpha1.ClusterNameLabel] = cluster.Name

		createConForCluster := func(status metav1.ConditionStatus, reason, message string) {
			createCon(cct, status, reason, message)
		}

		clusterMatches := rr.SelectorDefinition.Selector.Matches(&cluster) // whether the cluster matches the HelmDeployment's selector
		clusterInDeletion := !cluster.DeletionTimestamp.IsZero()           // whether the cluster is in deletion

		// If the cluster matches the selector and is not in deletion, we need to first ensure the finalizer and then create/update the HelmRelease on the cluster.
		if clusterMatches && !clusterInDeletion {
			// ensure finalizer on cluster for proper cleanup
			oldCluster := cluster.DeepCopy()
			if controllerutil.AddFinalizer(&cluster, rr.Object.Finalizer()) {
				clog.Debug("Adding finalizer to Cluster")
				if err := c.PlatformCluster.Client().Patch(ctx, &cluster, client.MergeFrom(oldCluster)); err != nil {
					errs.Append(errutils.WithReason(fmt.Errorf("unable to add finalizer to Cluster '%s/%s': %w", cluster.Namespace, cluster.Name, err), cconst.ReasonPlatformClusterInteractionProblem))
					continue
				}
			}

			ar, err := c.cr.GetAccessRequestForCluster(ctx, &cluster)
			if err != nil {
				errs.Append(errutils.WithReason(fmt.Errorf("error getting AccessRequest for Cluster '%s/%s': %w", cluster.Namespace, cluster.Name, err), cconst.ReasonInternalError))
				createCon(cct, metav1.ConditionFalse, helmv1alpha1.ReasonClusterAccessNotAvailable, fmt.Sprintf("Error getting AccessRequest for Cluster: %s", err.Error()))
				c.enqueueCluster(&cluster) // trigger reconciliation of the cluster with the cluster controller to fix the AccessRequest
				continue
			}
			if ar == nil {
				clog.Debug("Waiting for AccessRequest to become available")
				createCon(cct, metav1.ConditionFalse, helmv1alpha1.ReasonClusterAccessNotAvailable, "Waiting for AccessRequest to become available")
				c.enqueueCluster(&cluster) // trigger reconciliation of the cluster with the cluster controller to create the AccessRequest
				continue
			}

			// TODO: secret management
			requeueRequired, rerr := c.deployHelmChartSource(ctx, &cluster, expectedLabelsForCluster, rr, createConForCluster)
			if rerr != nil {
				errs.Append(errutils.Errorf("error creating/updating helm chart source for cluster '%s/%s': %w", rerr, cluster.Namespace, cluster.Name, rerr))
				continue
			}
			if requeueRequired {
				if rr.SmartRequeue != ctrlutils.SR_RESET {
					rr.SmartRequeue = ctrlutils.SR_BACKOFF
				}
				clog.Debug("Waiting for helm chart source to become healthy")
				continue
			}
			requeueRequired, rerr = c.deployHelmRelease(ctx, &cluster, ar, expectedLabelsForCluster, rr, createConForCluster)
			if rerr != nil {
				errs.Append(errutils.Errorf("error creating/updating HelmRelease for cluster '%s/%s': %w", rerr, cluster.Namespace, cluster.Name, rerr))
				continue
			}
			if requeueRequired {
				if rr.SmartRequeue != ctrlutils.SR_RESET {
					rr.SmartRequeue = ctrlutils.SR_BACKOFF
				}
				clog.Debug("Waiting for HelmRelease to become healthy")
				continue
			}
			clog.Debug("Successfully created/updated helm chart source and HelmRelease")
		} else {
			// TODO: secret management
			remainingHelmReleases, rerr := c.deleteMultipleHelmReleases(ctx, expectedLabelsForCluster, cluster.Namespace, createCon)
			if rerr != nil {
				errs.Append(errutils.Errorf("error deleting HelmRelease for Cluster '%s/%s': %w", rerr, cluster.Namespace, cluster.Name, rerr))
				continue
			}
			clog.Debug("Successfully deleted HelmReleases")
			remainingHelmChartSources, rerr := c.deleteMultipleHelmChartSources(ctx, expectedLabelsForCluster, cluster.Namespace, remainingHelmReleases, createCon)
			if rerr != nil {
				errs.Append(errutils.Errorf("error deleting HelmChartSources for Cluster '%s/%s': %w", rerr, cluster.Namespace, cluster.Name, rerr))
				continue
			}
			if len(remainingHelmReleases)+len(remainingHelmChartSources) > 0 {
				clog.Debug("Waiting for HelmReleases and/or HelmChartSources to be deleted", "remainingHelmReleases", len(remainingHelmReleases), "remainingHelmChartSources", len(remainingHelmChartSources))
				continue
			}
			rr.ConditionsToRemove = append(rr.ConditionsToRemove, cct)

			// At this point, we can safely remove this HelmDeployment's finalizer from the cluster.
			oldCluster := cluster.DeepCopy()
			if controllerutil.RemoveFinalizer(&cluster, rr.Object.Finalizer()) {
				clog.Debug("Removing finalizer from Cluster")
				if err := c.PlatformCluster.Client().Patch(ctx, &cluster, client.MergeFrom(oldCluster)); err != nil {
					errs.Append(errutils.WithReason(fmt.Errorf("unable to remove finalizer '%s' from Cluster '%s/%s': %w", rr.Object.Finalizer(), cluster.Namespace, cluster.Name, err), cconst.ReasonPlatformClusterInteractionProblem))
					continue
				}
			}
		}
	}

	return rr
}

func (c *HelmDeploymentController) handleDelete(ctx context.Context, rr ReconcileResult, expectedLabels map[string]string) ReconcileResult {
	log := logging.FromContextOrPanic(ctx)

	createCon := ctrlutils.GenerateCreateConditionFunc(&rr.ReconcileResult)
	errs := errutils.NewReasonableErrorList()

	// delete all HelmReleases which belong to this HelmDeployment (based on the labels)
	remainingHelmReleases, rerr := c.deleteMultipleHelmReleases(ctx, expectedLabels, "", createCon)
	if rerr != nil {
		errs.Append(errutils.Errorf("error deleting HelmReleases: %w", rerr, rerr))
	}
	remainingHelmChartSources, rerr := c.deleteMultipleHelmChartSources(ctx, expectedLabels, "", remainingHelmReleases, createCon)
	if rerr != nil {
		errs.Append(errutils.Errorf("error deleting HelmChartSources: %w", rerr, rerr))
	} else if len(remainingHelmReleases)+len(remainingHelmChartSources) > 0 {
		log.Debug("Waiting for HelmReleases and/or HelmChartSources to be deleted", "remainingHelmReleases", len(remainingHelmReleases), "remainingHelmChartSources", len(remainingHelmChartSources))
		rr.SmartRequeue = ctrlutils.SR_BACKOFF
		return rr
	}

	// remove this helm deployment's finalizer from all clusters, unless that cluster's identity is still referenced
	clustersToKeep := sets.New[string]()
	for _, hr := range remainingHelmReleases {
		if cID := clusterIdentityFromLabels(hr); cID != "" {
			clustersToKeep.Insert(cID)
		}
	}
	for _, hcs := range remainingHelmChartSources {
		if cID := clusterIdentityFromLabels(hcs); cID != "" {
			clustersToKeep.Insert(cID)
		}
	}
	cl := &clustersv1alpha1.ClusterList{}
	if err := c.PlatformCluster.Client().List(ctx, cl); err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to list Clusters from platform cluster: %w", err), cconst.ReasonPlatformClusterInteractionProblem)
		return rr
	}
	for _, cluster := range cl.Items {
		if !slices.Contains(cluster.Finalizers, rr.Object.Finalizer()) {
			continue
		}
		cID := fmt.Sprintf("%s/%s", cluster.Namespace, cluster.Name)
		if clustersToKeep.Has(cID) {
			log.Debug("Cluster still has HelmRelease or HelmChartSource referencing it, keeping finalizer for now", "cluster", cID)
			continue
		}
		oldCluster := cluster.DeepCopy()
		if controllerutil.RemoveFinalizer(&cluster, rr.Object.Finalizer()) {
			log.Debug("Removing finalizer from cluster", "cluster", cID, "finalizer", rr.Object.Finalizer())
			if err := c.PlatformCluster.Client().Patch(ctx, &cluster, client.MergeFrom(oldCluster)); err != nil {
				errs.Append(errutils.WithReason(fmt.Errorf("unable to remove finalizer '%s' from Cluster '%s/%s': %w", rr.Object.Finalizer(), cluster.Namespace, cluster.Name, err), cconst.ReasonPlatformClusterInteractionProblem))
				continue
			}
		}
	}

	// remove the conditions for all clusters where the old condition was that it is waiting for something, but it is not contained in the remaining resources anymore and there is no new condition for it
	for _, oldCon := range rr.Object.Status.Conditions {
		if !strings.HasPrefix(oldCon.Type, helmv1alpha1.ConditionPrefixCluster) || oldCon.Status == metav1.ConditionTrue {
			continue
		}
		for _, uCon := range rr.Conditions {
			if uCon.Type == oldCon.Type {
				// condition will already be updated
				goto nextOldCon
			}
		}
		if oldCon.Reason != helmv1alpha1.ReasonHelmReleaseDeletionFailed && oldCon.Reason != helmv1alpha1.ReasonWaitingForHelmReleaseDeletion && oldCon.Reason != helmv1alpha1.ReasonHelmChartSourceDeletionFailed && oldCon.Reason != helmv1alpha1.ReasonWaitingForHelmChartSourceDeletion {
			// this should not happen, ignore the condition
			continue
		}
		rr.ConditionsToRemove = append(rr.ConditionsToRemove, oldCon.Type)
	nextOldCon:
	}

	rr.ReconcileError = errs.Aggregate()
	if rr.ReconcileError == nil {
		// finally, remove the finalizer from the HelmDeployment itself
		old := rr.Object.DeepCopy()
		if controllerutil.RemoveFinalizer(rr.Object, helmv1alpha1.Finalizer) {
			log.Info("Removing finalizer from HelmDeployment")
			if err := c.PlatformCluster.Client().Patch(ctx, rr.Object, client.MergeFrom(old)); err != nil {
				rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to remove finalizer from HelmDeployment '%s/%s': %w", rr.Object.Namespace, rr.Object.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
				return rr
			}
			rr.Object = nil // otherwise we will get an error trying to update the non-existing HelmDeployment's status
		}
	}
	return rr
}

// expected arguments: either (<clusterNamespace>, <clusterName>) or ("<clusterNamespace>/<clusterName>")
func clusterConType(values ...string) string {
	var namespace, name string
	if len(values) == 1 {
		parts := strings.SplitN(values[0], "/", 2)
		if len(parts) != 2 {
			panic(fmt.Sprintf("invalid cluster identity '%s', expected format 'namespace/name'", values[0]))
		}
		namespace, name = parts[0], parts[1]
	} else if len(values) == 2 {
		namespace, name = values[0], values[1]
	} else {
		panic(fmt.Sprintf("invalid number of arguments, expected 1 or 2, got %d", len(values)))
	}
	return fmt.Sprintf("%s%s_%s", helmv1alpha1.ConditionPrefixCluster, namespace, name)
}

func getSelectorDefinition(hd *helmv1alpha1.HelmDeployment, cfg *helmv1alpha1.HelmDeployerConfig) (helmv1alpha1.SelectorDefinition, errutils.ReasonableError) {
	selectorDef := helmv1alpha1.SelectorDefinition{}
	if hd.Spec.Selector != nil {
		selectorDef.Selector = hd.Spec.Selector
	} else if hd.Spec.SelectorName != nil {
		var ok bool
		selectorDef, ok = cfg.Spec.SelectorDefinitions[*hd.Spec.SelectorName]
		if !ok {
			return helmv1alpha1.SelectorDefinition{}, errutils.WithReason(fmt.Errorf("selector definition '%s' not found in HelmDeployerConfig", *hd.Spec.SelectorName), cconst.ReasonConfigurationProblem)
		}
	}
	return selectorDef, nil
}

// clusterIdentityFromLabels extracts the cluster namespace and name from the given object's labels and returns <namespace>/<name>
// Empty strings are returned if any of the labels is missing.
func clusterIdentityFromLabels(obj client.Object) string {
	labels := obj.GetLabels()
	name := labels[helmv1alpha1.ClusterNameLabel]
	if name == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s", obj.GetNamespace(), name)
}

func (c *HelmDeploymentController) SetupWithManager(mgr ctrl.Manager) error {
	if err := c.cr.setupWithManager(mgr); err != nil {
		return fmt.Errorf("error setting up internal cluster controller with manager: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&helmv1alpha1.HelmDeployment{}).
		WithEventFilter(predicate.And(
			predicate.Or(
				predicate.GenerationChangedPredicate{},
				ctrlutils.DeletionTimestampChangedPredicate{},
				ctrlutils.GotAnnotationPredicate(openmcpconst.OperationAnnotation, openmcpconst.OperationAnnotationValueReconcile),
				ctrlutils.LostAnnotationPredicate(openmcpconst.OperationAnnotation, openmcpconst.OperationAnnotationValueIgnore),
			),
			predicate.Not(
				ctrlutils.HasAnnotationPredicate(openmcpconst.OperationAnnotation, openmcpconst.OperationAnnotationValueIgnore),
			),
		)).
		Watches(&helmv1alpha1.HelmDeployerConfig{}, handler.EnqueueRequestsFromMapFunc(c.enqueueRequestsForAllHelmDeployments), builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&clustersv1alpha1.Cluster{}, handler.EnqueueRequestsFromMapFunc(c.enqueueRequestsForAllHelmDeployments), builder.WithPredicates(predicate.And(
			predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicate.LabelChangedPredicate{},
				ctrlutils.DeletionTimestampChangedPredicate{},
				ctrlutils.GotAnnotationPredicate(openmcpconst.OperationAnnotation, openmcpconst.OperationAnnotationValueReconcile),
				ctrlutils.LostAnnotationPredicate(openmcpconst.OperationAnnotation, openmcpconst.OperationAnnotationValueIgnore),
			),
			predicate.Not(
				ctrlutils.HasAnnotationPredicate(openmcpconst.OperationAnnotation, openmcpconst.OperationAnnotationValueIgnore),
			),
		))).
		Complete(c)
}

func (c *HelmDeploymentController) enqueueRequestsForAllHelmDeployments(ctx context.Context, _ client.Object) []ctrl.Request {
	hdList := &helmv1alpha1.HelmDeploymentList{}
	if err := c.PlatformCluster.Client().List(ctx, hdList); err != nil {
		logging.FromContextOrDiscard(ctx).Error(err, "Error listing HelmDeployments for HelmDeployerConfig change")
		return nil
	}
	res := make([]ctrl.Request, 0, len(hdList.Items))
	for _, hd := range hdList.Items {
		res = append(res, testutils.RequestFromObject(&hd))
	}
	return res
}
