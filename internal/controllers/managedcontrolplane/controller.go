package managedcontrolplane

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/collections"
	"github.com/openmcp-project/controller-utils/pkg/collections/filters"
	"github.com/openmcp-project/controller-utils/pkg/conditions"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/controller/smartrequeue"
	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/controller-utils/pkg/pairs"
	testutils "github.com/openmcp-project/controller-utils/pkg/testing"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	apiconst "github.com/openmcp-project/openmcp-operator/api/constants"
	corev2alpha1 "github.com/openmcp-project/openmcp-operator/api/core/v2alpha1"
	"github.com/openmcp-project/openmcp-operator/internal/config"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"
)

const ControllerName = "ManagedControlPlane"

func NewManagedControlPlaneReconciler(platformCluster *clusters.Cluster, onboardingCluster *clusters.Cluster, eventRecorder record.EventRecorder, cfg *config.ManagedControlPlaneConfig) *ManagedControlPlaneReconciler {
	if cfg == nil {
		cfg = &config.ManagedControlPlaneConfig{}
	}
	return &ManagedControlPlaneReconciler{
		PlatformCluster:   platformCluster,
		OnboardingCluster: onboardingCluster,
		Config:            cfg,
		eventRecorder:     eventRecorder,
		sr:                smartrequeue.NewStore(5*time.Second, 24*time.Hour, 1.5),
	}
}

type ManagedControlPlaneReconciler struct {
	PlatformCluster   *clusters.Cluster
	OnboardingCluster *clusters.Cluster
	Config            *config.ManagedControlPlaneConfig
	eventRecorder     record.EventRecorder
	sr                *smartrequeue.Store
}

var _ reconcile.Reconciler = &ManagedControlPlaneReconciler{}

type ReconcileResult = ctrlutils.ReconcileResult[*corev2alpha1.ManagedControlPlane]

func (r *ManagedControlPlaneReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(ControllerName)
	ctx = logging.NewContext(ctx, log)
	log.Info("Starting reconcile")
	rr := r.reconcile(ctx, req)

	if rr.Result.IsZero() && r.Config.ReconcileMCPEveryXDays > 0 {
		// requeue the MCP for periodic reconciliation
		rr.Result.RequeueAfter = time.Duration(r.Config.ReconcileMCPEveryXDays) * 24 * time.Hour
	}

	// status update
	return ctrlutils.NewOpenMCPStatusUpdaterBuilder[*corev2alpha1.ManagedControlPlane]().
		WithNestedStruct("Status").
		WithPhaseUpdateFunc(func(obj *corev2alpha1.ManagedControlPlane, rr ctrlutils.ReconcileResult[*corev2alpha1.ManagedControlPlane]) (string, error) {
			if rr.Object != nil {
				if !rr.Object.DeletionTimestamp.IsZero() {
					return commonapi.StatusPhaseTerminating, nil
				}
				if conditions.AllConditionsHaveStatus(metav1.ConditionTrue, rr.Object.Status.Conditions...) {
					return commonapi.StatusPhaseReady, nil
				}
			}
			return commonapi.StatusPhaseProgressing, nil
		}).
		WithConditionUpdater(false).
		WithConditionEvents(r.eventRecorder, conditions.EventPerChange).
		WithSmartRequeue(r.sr).
		Build().
		UpdateStatus(ctx, r.OnboardingCluster.Client(), rr)
}

func (r *ManagedControlPlaneReconciler) reconcile(ctx context.Context, req reconcile.Request) ReconcileResult {
	log := logging.FromContextOrPanic(ctx)
	// get ManagedControlPlane resource
	mcp := &corev2alpha1.ManagedControlPlane{}
	if err := r.OnboardingCluster.Client().Get(ctx, req.NamespacedName, mcp); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Resource not found")
			return ReconcileResult{}
		}
		return ReconcileResult{ReconcileError: errutils.WithReason(fmt.Errorf("unable to get resource '%s' from cluster: %w", req.String(), err), cconst.ReasonOnboardingClusterInteractionProblem)}
	}

	// handle operation annotation
	if mcp.GetAnnotations() != nil {
		op, ok := mcp.GetAnnotations()[apiconst.OperationAnnotation]
		if ok {
			switch op {
			case apiconst.OperationAnnotationValueIgnore:
				log.Info("Ignoring resource due to ignore operation annotation")
				return ReconcileResult{}
			case apiconst.OperationAnnotationValueReconcile:
				log.Debug("Removing reconcile operation annotation from resource")
				if err := ctrlutils.EnsureAnnotation(ctx, r.OnboardingCluster.Client(), mcp, apiconst.OperationAnnotation, "", true, ctrlutils.DELETE); err != nil {
					return ReconcileResult{ReconcileError: errutils.WithReason(fmt.Errorf("error removing operation annotation: %w", err), cconst.ReasonOnboardingClusterInteractionProblem)}
				}
			}
		}
	}

	var rr ReconcileResult
	if mcp.DeletionTimestamp.IsZero() {
		rr = r.handleCreateOrUpdate(ctx, mcp)
	} else {
		rr = r.handleDelete(ctx, mcp)
	}

	return rr
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(strings.ToLower(ControllerName)).
		// watch ManagedControlPlane resources on the Onboarding cluster
		WatchesRawSource(source.Kind(r.OnboardingCluster.Cluster().GetCache(), &corev2alpha1.ManagedControlPlane{}, handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, obj *corev2alpha1.ManagedControlPlane) []ctrl.Request {
			if obj == nil {
				return nil
			}
			return []ctrl.Request{testutils.RequestFromObject(obj)}
		}), ctrlutils.ToTypedPredicate[*corev2alpha1.ManagedControlPlane](predicate.And(
			predicate.Or(
				predicate.GenerationChangedPredicate{},
				ctrlutils.DeletionTimestampChangedPredicate{},
				ctrlutils.GotAnnotationPredicate(apiconst.OperationAnnotation, apiconst.OperationAnnotationValueReconcile),
				ctrlutils.LostAnnotationPredicate(apiconst.OperationAnnotation, apiconst.OperationAnnotationValueIgnore),
			),
			predicate.Not(
				ctrlutils.HasAnnotationPredicate(apiconst.OperationAnnotation, apiconst.OperationAnnotationValueIgnore),
			),
		)))).
		Complete(r)
}

func (r *ManagedControlPlaneReconciler) handleCreateOrUpdate(ctx context.Context, mcp *corev2alpha1.ManagedControlPlane) ReconcileResult {
	log := logging.FromContextOrPanic(ctx)
	log.Info("Handling creation or update of ManagedControlPlane resource")

	rr := ReconcileResult{
		Result: ctrl.Result{
			RequeueAfter: time.Duration(r.Config.ReconcileMCPEveryXDays) * 24 * time.Hour,
		},
		Object:     mcp,
		OldObject:  mcp.DeepCopy(),
		Conditions: []metav1.Condition{},
	}
	createCon := ctrlutils.GenerateCreateConditionFunc(&rr)

	// ensure MCP and ClusterRequest finalizers on the MCP
	changed := controllerutil.AddFinalizer(mcp, corev2alpha1.MCPFinalizer)
	changed = controllerutil.AddFinalizer(mcp, corev2alpha1.ClusterRequestFinalizerPrefix+mcp.Name) || changed
	if changed {
		log.Debug("Adding finalizers to MCP")
		if err := r.OnboardingCluster.Client().Patch(ctx, mcp, client.MergeFrom(rr.OldObject)); err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error adding finalizers to MCP: %w", err), cconst.ReasonOnboardingClusterInteractionProblem)
			createCon(corev2alpha1.ConditionMeta, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
			return rr
		}
	}
	createCon(corev2alpha1.ConditionMeta, metav1.ConditionTrue, "", "")

	// ensure that the ClusterRequest exists
	// since ClusterRequests are basically immutable, updating them is not required
	namespace := libutils.StableRequestNamespace(mcp.Namespace)
	cr := &clustersv1alpha1.ClusterRequest{}
	cr.Name = mcp.Name
	cr.Namespace = namespace
	if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(cr), cr); err != nil {
		if !apierrors.IsNotFound(err) {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to get ClusterRequest '%s/%s': %w", cr.Namespace, cr.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
			createCon(corev2alpha1.ConditionClusterRequestReady, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
			return rr
		}

		log.Info("ClusterRequest not found, creating it", "clusterRequestName", cr.Name, "clusterRequestNamespace", cr.Namespace, "purpose", r.Config.MCPClusterPurpose)
		cr.Spec = clustersv1alpha1.ClusterRequestSpec{
			Purpose:                r.Config.MCPClusterPurpose,
			WaitForClusterDeletion: ptr.To(true),
		}
		if err := r.PlatformCluster.Client().Create(ctx, cr); err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error creating ClusterRequest '%s/%s': %w", cr.Namespace, cr.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
			createCon(corev2alpha1.ConditionClusterRequestReady, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
			return rr
		}
	} else {
		log.Debug("ClusterRequest found", "clusterRequestName", cr.Name, "clusterRequestNamespace", cr.Namespace, "purposeInConfig", r.Config.MCPClusterPurpose, "purposeInClusterRequest", cr.Spec.Purpose)
	}

	// check if the ClusterRequest is ready
	if cr.Status.Phase != commonapi.StatusPhaseReady {
		log.Info("Waiting for ClusterRequest to become ready", "clusterRequestName", cr.Name, "clusterRequestNamespace", cr.Namespace, "phase", cr.Status.Phase)
		createCon(corev2alpha1.ConditionClusterRequestReady, metav1.ConditionFalse, cconst.ReasonWaitingForClusterRequest, "ClusterRequest is not ready yet")
		rr.SmartRequeue = ctrlutils.SR_BACKOFF
		return rr
	}
	log.Debug("ClusterRequest is ready", "clusterRequestName", cr.Name, "clusterRequestNamespace", cr.Namespace)
	createCon(corev2alpha1.ConditionClusterRequestReady, metav1.ConditionTrue, "", "ClusterRequest is ready")

	// manage AccessRequests
	allAccessReady, removeConditions, rerr := r.manageAccessRequests(ctx, mcp, cr, createCon)
	rr.ConditionsToRemove = removeConditions.UnsortedList()
	if rerr != nil {
		rr.ReconcileError = rerr
		return rr
	}

	if allAccessReady {
		rr.SmartRequeue = ctrlutils.SR_NO_REQUEUE
	} else {
		rr.SmartRequeue = ctrlutils.SR_BACKOFF
	}

	return rr
}

func (r *ManagedControlPlaneReconciler) handleDelete(ctx context.Context, mcp *corev2alpha1.ManagedControlPlane) ReconcileResult {
	log := logging.FromContextOrPanic(ctx)
	log.Info("Handling deletion of ManagedControlPlane resource")

	rr := ReconcileResult{
		Result: ctrl.Result{
			RequeueAfter: time.Duration(r.Config.ReconcileMCPEveryXDays) * 24 * time.Hour,
		},
		Object:     mcp,
		OldObject:  mcp.DeepCopy(),
		Conditions: []metav1.Condition{},
	}
	createCon := ctrlutils.GenerateCreateConditionFunc(&rr)

	// delete services
	remainingResources, rerr := r.deleteDependingServices(ctx, mcp)
	if rerr != nil {
		rr.ReconcileError = rerr
		createCon(corev2alpha1.ConditionAllServicesDeleted, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
		return rr
	}
	if len(remainingResources) > 0 {
		serviceResourceCount := collections.AggregateMap(remainingResources, func(service string, resources []*unstructured.Unstructured, agg pairs.Pair[*[]string, int]) pairs.Pair[*[]string, int] {
			*agg.Key = append(*agg.Key, service)
			agg.Value += len(resources)
			return agg
		}, pairs.New(ptr.To([]string{}), 0))
		log.Info("Waiting for service resources to be deleted", "services", strings.Join(*serviceResourceCount.Key, ", "), "remainingResourcesCount", serviceResourceCount.Value)
		msg := strings.Builder{}
		msg.WriteString("Waiting for the following service resources to be deleted: ")
		for providerName, resources := range remainingResources {
			for _, res := range resources {
				msg.WriteString(fmt.Sprintf("[%s]%s.%s, ", providerName, res.GetKind(), res.GetAPIVersion()))
			}
		}
		createCon(corev2alpha1.ConditionAllServicesDeleted, metav1.ConditionFalse, cconst.ReasonWaitingForServices, strings.TrimSuffix(msg.String(), ", "))
		rr.SmartRequeue = ctrlutils.SR_BACKOFF
		return rr
	}
	createCon(corev2alpha1.ConditionAllServicesDeleted, metav1.ConditionTrue, "", "All service resources have been deleted")
	log.Debug("All service resources deleted")

	// delete AccessRequests and related secrets
	accessReady, removeConditions, rerr := r.manageAccessRequests(ctx, mcp, nil, createCon)
	rr.ConditionsToRemove = removeConditions.UnsortedList()
	if rerr != nil {
		rr.ReconcileError = rerr
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
		return rr
	}
	if !accessReady {
		log.Info("Waiting for AccessRequests to be deleted")
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "Waiting for AccessRequests to be deleted")
		rr.SmartRequeue = ctrlutils.SR_BACKOFF
		return rr
	}
	createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionTrue, "", "All AccessRequests have been deleted")
	log.Debug("All AccessRequests deleted")

	// delete cluster requests related to this MCP
	remainingCRs, rerr := r.deleteRelatedClusterRequests(ctx, mcp)
	if rerr != nil {
		rr.ReconcileError = rerr
		createCon(corev2alpha1.ConditionAllClusterRequestsDeleted, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
		return rr
	}
	finalizersToRemove := sets.New(filters.FilterSlice(mcp.Finalizers, func(args ...any) bool {
		fin, ok := args[0].(string)
		if !ok {
			return false
		}
		return strings.HasPrefix(fin, corev2alpha1.ClusterRequestFinalizerPrefix) && !remainingCRs.Has(strings.TrimPrefix(fin, corev2alpha1.ClusterRequestFinalizerPrefix))
	})...)
	if len(finalizersToRemove) > 0 {
		log.Debug("Removing ClusterRequest finalizers for deleted ClusterRequests from MCP", "finalizers", strings.Join(sets.List(finalizersToRemove), ", "))
		old := mcp.DeepCopy()
		newFinalizers := []string{}
		for _, fin := range mcp.Finalizers {
			if !finalizersToRemove.Has(fin) {
				newFinalizers = append(newFinalizers, fin)
			}
		}
		mcp.Finalizers = newFinalizers
		if err := r.OnboardingCluster.Client().Patch(ctx, mcp, client.MergeFrom(old)); err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error removing ClusterRequest finalizers from MCP: %w", err), cconst.ReasonOnboardingClusterInteractionProblem)
			createCon(corev2alpha1.ConditionAllClusterRequestsDeleted, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
			return rr
		}
		rr.OldObject = mcp.DeepCopy()
	}
	if remainingCRs.Len() > 0 {
		tmp := strings.Join(sets.List(remainingCRs), ", ")
		log.Info("Waiting for ClusterRequests to be deleted", "remainingClusterRequests", tmp)
		createCon(corev2alpha1.ConditionAllClusterRequestsDeleted, metav1.ConditionFalse, cconst.ReasonWaitingForClusterRequest, fmt.Sprintf("Waiting for the following ClusterRequests to be deleted: %s", tmp))
		rr.SmartRequeue = ctrlutils.SR_BACKOFF
		return rr
	}
	createCon(corev2alpha1.ConditionAllClusterRequestsDeleted, metav1.ConditionTrue, "", "All ClusterRequests have been deleted")
	log.Debug("All ClusterRequests deleted")

	// remove MCP finalizer
	if controllerutil.RemoveFinalizer(mcp, corev2alpha1.MCPFinalizer) {
		log.Debug("Removing MCP finalizer")
		if err := r.OnboardingCluster.Client().Patch(ctx, mcp, client.MergeFrom(rr.OldObject)); err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error removing MCP finalizer: %w", err), cconst.ReasonOnboardingClusterInteractionProblem)
			createCon(corev2alpha1.ConditionMeta, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
			return rr
		}
	}
	createCon(corev2alpha1.ConditionMeta, metav1.ConditionTrue, "", "MCP finalizer removed")
	if len(mcp.Finalizers) == 0 {
		// if we just removed the last finalizer on the MCP
		// (which should usually be the case, unless something external added one)
		// the MCP is now gone and updating the status will fail
		rr.Object = nil
	}

	return rr
}
