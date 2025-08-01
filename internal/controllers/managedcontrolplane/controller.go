package managedcontrolplane

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/conditions"
	"github.com/openmcp-project/controller-utils/pkg/controller"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/controller/smartrequeue"
	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/logging"
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
		rr = r.handleDelete(ctx, req, mcp)
	}

	// default error and never for the smart requeue store
	// doing this here means that only the 'interesting' cases of backoff and reset have to be handled in the reconcile logic
	if rr.Object != nil {
		if rr.ReconcileError != nil {
			r.sr.For(rr.Object).Error(rr.ReconcileError)
		} else if rr.Result.IsZero() {
			r.sr.For(rr.Object).Never()
		}
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
		}), controller.ToTypedPredicate[*corev2alpha1.ManagedControlPlane](predicate.And(
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
		Object:     mcp,
		OldObject:  mcp.DeepCopy(),
		Conditions: []metav1.Condition{},
	}
	createCon := controller.GenerateCreateConditionFunc(&rr)

	// ensure that the ClusterRequest exists
	// since ClusterRequests are basically immutable, updating it is not required
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
			Purpose: r.Config.MCPClusterPurpose,
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
		rr.Result, _ = r.sr.For(mcp).Backoff()
		log.Info("Waiting for ClusterRequest to become ready", "clusterRequestName", cr.Name, "clusterRequestNamespace", cr.Namespace, "phase", cr.Status.Phase, "requeueAfter", rr.Result.RequeueAfter)
		createCon(corev2alpha1.ConditionClusterRequestReady, metav1.ConditionFalse, cconst.ReasonWaitingForClusterRequest, "ClusterRequest is not ready yet")
		return rr
	}
	log.Debug("ClusterRequest is ready", "clusterRequestName", cr.Name, "clusterRequestNamespace", cr.Namespace)
	createCon(corev2alpha1.ConditionClusterRequestReady, metav1.ConditionTrue, "", "ClusterRequest is ready")

	// create or update AccessRequests for the ManagedControlPlane
	updatedAccessRequests := map[string]*clustersv1alpha1.AccessRequest{}

	oidcProviders := make([]*commonapi.OIDCProviderConfig, 0, len(mcp.Spec.IAM.OIDCProviders)+1)
	if r.Config.StandardOIDCProvider != nil && len(mcp.Spec.IAM.RoleBindings) > 0 {
		// add default OIDC provider, unless it has been disabled
		defaultOidc := r.Config.StandardOIDCProvider.DeepCopy()
		defaultOidc.Name = corev2alpha1.DefaultOIDCProviderName
		defaultOidc.RoleBindings = mcp.Spec.IAM.RoleBindings
		oidcProviders = append(oidcProviders, defaultOidc)
	}
	oidcProviders = append(oidcProviders, mcp.Spec.IAM.OIDCProviders...)
	for _, oidc := range mcp.Spec.IAM.OIDCProviders {
		log.Debug("Creating/updating AccessRequest for OIDC provider", "oidcProviderName", oidc.Name)
		arName := controller.K8sNameHash(mcp.Name, oidc.Name)
		ar := &clustersv1alpha1.AccessRequest{}
		ar.Name = arName
		ar.Namespace = namespace
		if _, err := controllerutil.CreateOrUpdate(ctx, r.PlatformCluster.Client(), ar, func() error {
			ar.Spec.RequestRef = &commonapi.ObjectReference{
				Name:      cr.Name,
				Namespace: cr.Namespace,
			}
			ar.Spec.OIDCProvider = oidc

			// set labels
			if ar.Labels == nil {
				ar.Labels = map[string]string{}
			}
			ar.Labels[corev2alpha1.MCPLabel] = mcp.Name
			ar.Labels[apiconst.ManagedByLabel] = ControllerName
			ar.Labels[corev2alpha1.OIDCProviderLabel] = corev2alpha1.DefaultOIDCProviderName

			return nil
		}); err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error creating/updating AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
			createCon(corev2alpha1.ConditionPrefixOIDCAccessReady+oidc.Name, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
			createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "Error creating/updating AccessRequest for OIDC provider "+oidc.Name)
			return rr
		}
		updatedAccessRequests[corev2alpha1.DefaultOIDCProviderName] = ar
	}

	// delete all AccessRequests that have previously been created for this ManagedControlPlane but are not needed anymore
	oidcARs := &clustersv1alpha1.AccessRequestList{}
	if err := r.PlatformCluster.Client().List(ctx, oidcARs, client.InNamespace(namespace), client.HasLabels{corev2alpha1.OIDCProviderLabel}, client.MatchingLabels{
		corev2alpha1.MCPLabel:   mcp.Name,
		apiconst.ManagedByLabel: ControllerName,
	}); err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("error listing AccessRequests for ManagedControlPlane '%s/%s': %w", mcp.Namespace, mcp.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
		return rr
	}
	accessRequestsInDeletion := sets.New[string]()
	errs := errutils.NewReasonableErrorList()
	for _, ar := range oidcARs.Items {
		if _, ok := updatedAccessRequests[ar.Spec.OIDCProvider.Name]; ok {
			continue
		}
		providerName := "<unknown>"
		if ar.Spec.OIDCProvider != nil {
			providerName = ar.Spec.OIDCProvider.Name
		}
		accessRequestsInDeletion.Insert(ar.Name)
		if !ar.DeletionTimestamp.IsZero() {
			log.Debug("Waiting for deletion of AccessRequest that is no longer required", "accessRequestName", ar.Name, "accessRequestNamespace", ar.Namespace, "oidcProviderName", providerName)
			createCon(corev2alpha1.ConditionPrefixOIDCAccessReady+providerName, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "AccessRequest is being deleted")
			continue
		}
		log.Debug("Deleting AccessRequest that is no longer needed", "accessRequestName", ar.Name, "accessRequestNamespace", ar.Namespace, "oidcProviderName", providerName)
		if err := r.PlatformCluster.Client().Delete(ctx, &ar); client.IgnoreNotFound(err) != nil {
			rerr := errutils.WithReason(fmt.Errorf("error deleting AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
			errs.Append(rerr)
			createCon(corev2alpha1.ConditionPrefixOIDCAccessReady+providerName, metav1.ConditionFalse, rerr.Reason(), rerr.Error())
		}
		createCon(corev2alpha1.ConditionPrefixOIDCAccessReady+providerName, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "AccessRequest is being deleted")
	}
	if err := errs.Aggregate(); err != nil {
		rr.ReconcileError = err
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "Error deleting AccessRequests that are no longer needed")
		return rr
	}

	// delete all AccessRequest secrets that have been copied to the Onboarding cluster and belong to AccessRequests that are no longer needed
	mcpSecrets := &corev1.SecretList{}
	if err := r.OnboardingCluster.Client().List(ctx, mcpSecrets, client.InNamespace(mcp.Namespace), client.HasLabels{corev2alpha1.OIDCProviderLabel}, client.MatchingLabels{
		corev2alpha1.MCPLabel:   mcp.Name,
		apiconst.ManagedByLabel: ControllerName,
	}); err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("error listing secrets for ManagedControlPlane '%s/%s': %w", mcp.Namespace, mcp.Name, err), cconst.ReasonOnboardingClusterInteractionProblem)
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
		return rr
	}
	for _, mcpSecret := range mcpSecrets.Items {
		providerName := mcpSecret.Labels[corev2alpha1.OIDCProviderLabel]
		if providerName == "" {
			log.Error(nil, "Secret for ManagedControlPlane has an empty OIDCProvider label, this should not happen", "secretName", mcpSecret.Name, "secretNamespace", mcpSecret.Namespace)
			continue
		}
		if _, ok := updatedAccessRequests[providerName]; ok {
			continue
		}
		accessRequestsInDeletion.Insert(providerName)
		if !mcpSecret.DeletionTimestamp.IsZero() {
			log.Debug("Waiting for deletion of AccessRequest secret that is no longer required", "secretName", mcpSecret.Name, "secretNamespace", mcpSecret.Namespace, "oidcProviderName", providerName)
			createCon(corev2alpha1.ConditionPrefixOIDCAccessReady+providerName, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "AccessRequest secret is being deleted")
			continue
		}
		log.Debug("Deleting AccessRequest secret that is no longer required", "secretName", mcpSecret.Name, "secretNamespace", mcpSecret.Namespace, "oidcProviderName", providerName)
		if err := r.OnboardingCluster.Client().Delete(ctx, &mcpSecret); client.IgnoreNotFound(err) != nil {
			rerr := errutils.WithReason(fmt.Errorf("error deleting AccessRequest secret '%s/%s': %w", mcpSecret.Namespace, mcpSecret.Name, err), cconst.ReasonOnboardingClusterInteractionProblem)
			errs.Append(rerr)
			createCon(corev2alpha1.ConditionPrefixOIDCAccessReady+providerName, metav1.ConditionFalse, rerr.Reason(), rerr.Error())
		}
		createCon(corev2alpha1.ConditionPrefixOIDCAccessReady+providerName, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "AccessRequest secret is being deleted")
	}

	// remove conditions for AccessRequests that are neither required nor in deletion (= have been deleted already)
	cu := conditions.ConditionUpdater(mcp.Status.Conditions, false).WithEventRecorder(r.eventRecorder, conditions.EventPerChange)
	for _, con := range mcp.Status.Conditions {
		if !strings.HasPrefix(con.Type, corev2alpha1.ConditionPrefixOIDCAccessReady) {
			continue
		}
		providerName := strings.TrimPrefix(con.Type, corev2alpha1.ConditionPrefixOIDCAccessReady)
		if _, ok := updatedAccessRequests[providerName]; !ok && !accessRequestsInDeletion.Has(providerName) {
			cu.RemoveCondition(corev2alpha1.ConditionPrefixOIDCAccessReady + providerName)
		}
	}
	rr.Object.Status.Conditions, _ = cu.Record(mcp).Conditions()

	// check if all required access requests are ready
	allAccessReady := true
	if rr.Object.Status.Access == nil {
		rr.Object.Status.Access = map[string]commonapi.LocalObjectReference{}
	}
	for providerName, ar := range updatedAccessRequests {
		if !ar.Status.IsGranted() || ar.Status.SecretRef == nil {
			log.Debug("AccessRequest is not ready yet", "accessRequestName", ar.Name, "accessRequestNamespace", ar.Namespace, "oidcProviderName", providerName)
			createCon(corev2alpha1.ConditionPrefixOIDCAccessReady+providerName, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "AccessRequest is not ready yet")
			allAccessReady = false
		} else {
			// copy access request secret and reference it in the ManagedControlPlane status
			arSecret := &corev1.Secret{}
			arSecret.Name = ar.Status.SecretRef.Name
			arSecret.Namespace = ar.Status.SecretRef.Namespace
			if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(arSecret), arSecret); err != nil {
				rr.ReconcileError = errutils.WithReason(fmt.Errorf("error getting AccessRequest secret '%s/%s': %w", arSecret.Namespace, arSecret.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
				createCon(corev2alpha1.ConditionPrefixOIDCAccessReady+providerName, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
				createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "Error getting AccessRequest secret for OIDC provider "+providerName)
				return rr
			}
			mcpSecret := &corev1.Secret{}
			mcpSecret.Name = controller.K8sNameHash(mcp.Name, providerName)
			mcpSecret.Namespace = mcp.Namespace
			if _, err := controllerutil.CreateOrUpdate(ctx, r.OnboardingCluster.Client(), mcpSecret, func() error {
				mcpSecret.Data = arSecret.Data
				if mcpSecret.Labels == nil {
					mcpSecret.Labels = map[string]string{}
				}
				mcpSecret.Labels[corev2alpha1.MCPLabel] = mcp.Name
				mcpSecret.Labels[corev2alpha1.OIDCProviderLabel] = providerName
				mcpSecret.Labels[apiconst.ManagedByLabel] = ControllerName

				if err := controllerutil.SetControllerReference(mcp, mcpSecret, r.OnboardingCluster.Scheme()); err != nil {
					return err
				}
				return nil
			}); err != nil {
				rr.ReconcileError = errutils.WithReason(fmt.Errorf("error creating/updating AccessRequest secret '%s/%s': %w", mcpSecret.Namespace, mcpSecret.Name, err), cconst.ReasonOnboardingClusterInteractionProblem)
				createCon(corev2alpha1.ConditionPrefixOIDCAccessReady+providerName, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
				createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "Error creating/updating AccessRequest secret for OIDC provider "+providerName)
				return rr
			}
			log.Debug("Access secret for ManagedControlPlane created/updated", "secretName", mcpSecret.Name, "oidcProviderName", providerName)
			rr.Object.Status.Access[providerName] = commonapi.LocalObjectReference{
				Name: mcpSecret.Name,
			}
			createCon(corev2alpha1.ConditionPrefixOIDCAccessReady+providerName, metav1.ConditionTrue, "", "")
		}
	}

	if allAccessReady {
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionTrue, "", "All accesses are ready")
		rr.Result, _ = r.sr.For(mcp).Never()
	} else {
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "Not all accesses are ready")
		rr.Result, _ = r.sr.For(mcp).Backoff()
	}

	return rr
}

func (r *ManagedControlPlaneReconciler) handleDelete(ctx context.Context, req reconcile.Request, mcp *corev2alpha1.ManagedControlPlane) ReconcileResult {
	log := logging.FromContextOrPanic(ctx)
	log.Info("Handling deletion of ManagedControlPlane resource")

	rr := ReconcileResult{
		Object:     mcp,
		OldObject:  mcp.DeepCopy(),
		Conditions: []metav1.Condition{},
	}

	return rr
}
