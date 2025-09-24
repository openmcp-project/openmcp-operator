package managedcontrolplane

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openmcp-project/controller-utils/pkg/collections"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	apiconst "github.com/openmcp-project/openmcp-operator/api/constants"
	corev2alpha1 "github.com/openmcp-project/openmcp-operator/api/core/v2alpha1"
)

// manageAccessRequests aligns the existing AccessRequests for the MCP with the currently configured OIDC providers and tokens.
// It uses the given createCon function to create conditions for AccessRequests and returns a set of conditions that should be removed from the MCP status.
// The bool return value specifies whether everything related to MCP access is in the desired state or not. If 'false', it is recommended to requeue the MCP.
func (r *ManagedControlPlaneReconciler) manageAccessRequests(ctx context.Context, mcp *corev2alpha1.ManagedControlPlaneV2, platformNamespace string, cr *clustersv1alpha1.ClusterRequest, createCon func(conType string, status metav1.ConditionStatus, reason, message string)) (bool, sets.Set[string], errutils.ReasonableError) {
	updatedAccessRequests, rerr := r.createOrUpdateDesiredAccessRequests(ctx, mcp, platformNamespace, cr, createCon)
	if rerr != nil {
		return false, nil, rerr
	}

	accessRequestsInDeletion, rerr := r.deleteUndesiredAccessRequests(ctx, mcp, platformNamespace, updatedAccessRequests, createCon)
	if rerr != nil {
		return false, nil, rerr
	}

	allAccessReady, rerr := r.syncAccessSecrets(ctx, mcp, updatedAccessRequests, createCon)
	if rerr != nil {
		return false, nil, rerr
	}

	accessSecretsInDeletion, rerr := r.deleteUndesiredAccessSecrets(ctx, mcp, updatedAccessRequests, createCon)
	if rerr != nil {
		return false, nil, rerr
	}

	// remove conditions for AccessRequests that are neither required nor in deletion (= have been deleted already)
	// first, build a set of OIDC provider names that have a condition in the MCP status
	removeConditions := collections.AggregateSlice(mcp.Status.Conditions, func(con metav1.Condition, cur sets.Set[string]) sets.Set[string] {
		if providerName, found := strings.CutPrefix(con.Type, corev2alpha1.ConditionPrefixAccessReady); found {
			cur.Insert(providerName)
		}
		return cur
	}, sets.New[string]())
	// then, remove all conditions from it which belong to updated AccessRequests
	removeConditions = removeConditions.Difference(sets.KeySet(updatedAccessRequests))
	// and all conditions that are in deletion
	removeConditions = removeConditions.Difference(accessRequestsInDeletion)
	// now, add the condition prefix again
	removeConditions = collections.ProjectMapToMap(removeConditions, func(providerName string, _ sets.Empty) (string, sets.Empty) {
		return corev2alpha1.ConditionPrefixAccessReady + providerName, sets.Empty{}
	})

	everythingReady := accessRequestsInDeletion.Len() == 0 && accessSecretsInDeletion.Len() == 0 && allAccessReady
	if everythingReady {
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionTrue, "", "All accesses are ready")
	} else {
		reason := cconst.ReasonWaitingForAccessRequest
		if allAccessReady {
			reason = cconst.ReasonWaitingForAccessRequestDeletion
		}
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, reason, "Not all accesses are ready")
	}

	return everythingReady, removeConditions, nil
}

// createOrUpdateDesiredAccessRequests creates/updates all AccessRequests that are desired according to the ManagedControlPlane's configured OIDC providers and tokens.
// It returns a mapping from OIDC provider names to the corresponding AccessRequests.
// If the ManagedControlPlane has a non-zero DeletionTimestamp, no AccessRequests will be created or updated and the returned map will be empty.
func (r *ManagedControlPlaneReconciler) createOrUpdateDesiredAccessRequests(ctx context.Context, mcp *corev2alpha1.ManagedControlPlaneV2, platformNamespace string, cr *clustersv1alpha1.ClusterRequest, createCon func(conType string, status metav1.ConditionStatus, reason, message string)) (map[string]*clustersv1alpha1.AccessRequest, errutils.ReasonableError) {
	log := logging.FromContextOrPanic(ctx)

	updatedAccessRequests := map[string]*clustersv1alpha1.AccessRequest{}
	var oidcProviders []commonapi.OIDCProviderConfig
	var tokenProviders []corev2alpha1.TokenConfig

	// create or update AccessRequests for the ManagedControlPlane
	if mcp.DeletionTimestamp.IsZero() {
		oidcProviders = make([]commonapi.OIDCProviderConfig, 0, len(mcp.Spec.IAM.OIDC.ExtraProviders)+1)
		if r.Config.DefaultOIDCProvider != nil && len(mcp.Spec.IAM.OIDC.DefaultProvider.RoleBindings) > 0 {
			// add default OIDC provider, unless it has been disabled
			defaultOidc := r.Config.DefaultOIDCProvider.DeepCopy()
			defaultOidc.Name = corev2alpha1.DefaultOIDCProviderName
			defaultOidc.RoleBindings = mcp.Spec.IAM.OIDC.DefaultProvider.RoleBindings
			oidcProviders = append(oidcProviders, *defaultOidc)
		}
		oidcProviders = append(oidcProviders, mcp.Spec.IAM.OIDC.ExtraProviders...)

		tokenProviders = mcp.Spec.IAM.Tokens
	}

	setArLabels := func(ar *clustersv1alpha1.AccessRequest) {
		if ar.Labels == nil {
			ar.Labels = map[string]string{}
		}
		ar.Labels[corev2alpha1.MCPNameLabel] = mcp.Name
		ar.Labels[corev2alpha1.MCPNamespaceLabel] = mcp.Namespace
		ar.Labels[apiconst.ManagedByLabel] = ControllerName
	}

	for _, oidc := range oidcProviders {
		log.Debug("Creating/updating AccessRequest for OIDC provider", "oidcProviderName", oidc.Name)
		arName := ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.OIDCNamePrefix+oidc.Name)
		ar := &clustersv1alpha1.AccessRequest{}
		ar.Name = arName
		ar.Namespace = platformNamespace
		if _, err := controllerutil.CreateOrUpdate(ctx, r.PlatformCluster.Client(), ar, func() error {
			ar.Spec.RequestRef = &commonapi.ObjectReference{
				Name:      cr.Name,
				Namespace: cr.Namespace,
			}
			ar.Spec.OIDC = &clustersv1alpha1.OIDCConfig{
				OIDCProviderConfig: oidc,
			}

			// set labels
			setArLabels(ar)
			ar.Labels[corev2alpha1.OIDCProviderLabel] = oidc.Name

			return nil
		}); err != nil {
			rerr := errutils.WithReason(fmt.Errorf("error creating/updating AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
			createCon(corev2alpha1.ConditionPrefixAccessReady+oidc.Name, metav1.ConditionFalse, rerr.Reason(), rerr.Error())
			createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "Error creating/updating AccessRequest for OIDC provider "+oidc.Name)
			return nil, rerr
		}
		updatedAccessRequests[corev2alpha1.OIDCNamePrefix+oidc.Name] = ar
	}

	for _, token := range tokenProviders {
		log.Debug("Creating/updating AccessRequest for token", "tokenName", token.Name)
		// add the "token:" prefix to avoid name clashes with OIDC providers
		arName := ctrlutils.NameHashSHAKE128Base32(mcp.Name, corev2alpha1.TokenNamePrefix+token.Name)
		ar := &clustersv1alpha1.AccessRequest{}
		ar.Name = arName
		ar.Namespace = platformNamespace
		if _, err := controllerutil.CreateOrUpdate(ctx, r.PlatformCluster.Client(), ar, func() error {
			ar.Spec.RequestRef = &commonapi.ObjectReference{
				Name:      cr.Name,
				Namespace: cr.Namespace,
			}
			ar.Spec.Token = &clustersv1alpha1.TokenConfig{
				Permissions: token.Permissions,
				RoleRefs:    token.RoleRefs,
			}

			setArLabels(ar)
			ar.Labels[corev2alpha1.TokenProviderLabel] = token.Name

			return nil
		}); err != nil {
			rerr := errutils.WithReason(fmt.Errorf("error creating/updating AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
			createCon(corev2alpha1.ConditionPrefixAccessReady+corev2alpha1.TokenNamePrefix+token.Name, metav1.ConditionFalse, rerr.Reason(), rerr.Error())
			createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "Error creating/updating AccessRequest for token "+token.Name)
			return nil, rerr
		}
		updatedAccessRequests[corev2alpha1.TokenNamePrefix+token.Name] = ar
	}

	return updatedAccessRequests, nil
}

// deleteUndesiredAccessRequests deletes all AccessRequests that belong to the given ManagedControlPlane, but are not in the updatedAccessRequests map.
// These are AccessRequests that have been created for a previous version of the ManagedControlPlane and are not needed anymore.
// It returns a set of OIDC provider names for which the AccessRequests are still in deletion. If the set is empty, all undesired AccessRequests have been deleted.
func (r *ManagedControlPlaneReconciler) deleteUndesiredAccessRequests(ctx context.Context, mcp *corev2alpha1.ManagedControlPlaneV2, platformNamespace string, updatedAccessRequests map[string]*clustersv1alpha1.AccessRequest, createCon func(conType string, status metav1.ConditionStatus, reason, message string)) (sets.Set[string], errutils.ReasonableError) {
	log := logging.FromContextOrPanic(ctx)

	accessRequestsInDeletion := sets.New[string]()

	matchingLabels := client.MatchingLabels{
		corev2alpha1.MCPNameLabel:      mcp.Name,
		corev2alpha1.MCPNamespaceLabel: mcp.Namespace,
		apiconst.ManagedByLabel:        ControllerName,
	}

	// delete all AccessRequests that have previously been created for this ManagedControlPlane but are not needed anymore
	oidcARs := &clustersv1alpha1.AccessRequestList{}
	tokenArs := &clustersv1alpha1.AccessRequestList{}

	if err := r.PlatformCluster.Client().List(ctx, oidcARs, client.InNamespace(platformNamespace), client.HasLabels{corev2alpha1.OIDCProviderLabel}, matchingLabels); err != nil {
		rerr := errutils.WithReason(fmt.Errorf("error listing AccessRequests for ManagedControlPlane '%s/%s': %w", mcp.Namespace, mcp.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, rerr.Reason(), rerr.Error())
		return accessRequestsInDeletion, rerr
	}

	if err := r.PlatformCluster.Client().List(ctx, tokenArs, client.InNamespace(platformNamespace), client.HasLabels{corev2alpha1.TokenProviderLabel}, matchingLabels); err != nil {
		rerr := errutils.WithReason(fmt.Errorf("error listing AccessRequests for ManagedControlPlane '%s/%s': %w", mcp.Namespace, mcp.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, rerr.Reason(), rerr.Error())
		return accessRequestsInDeletion, rerr
	}

	mcpARs := &clustersv1alpha1.AccessRequestList{}
	mcpARs.Items = append(mcpARs.Items, oidcARs.Items...)
	mcpARs.Items = append(mcpARs.Items, tokenArs.Items...)

	errs := errutils.NewReasonableErrorList()
	for _, ar := range mcpARs.Items {
		providerName := "<unknown>"
		if ar.Spec.OIDC != nil {
			providerName = corev2alpha1.OIDCNamePrefix + ar.Spec.OIDC.Name
		}
		if ar.Spec.Token != nil {
			providerName = corev2alpha1.TokenNamePrefix + ar.Labels[corev2alpha1.TokenProviderLabel]
		}
		if _, ok := updatedAccessRequests[providerName]; ok {
			continue
		}
		accessRequestsInDeletion.Insert(providerName)
		if !ar.DeletionTimestamp.IsZero() {
			log.Debug("Waiting for deletion of AccessRequest that is no longer required", "accessRequestName", ar.Name, "accessRequestNamespace", ar.Namespace, "oidcProviderName", providerName)
			createCon(corev2alpha1.ConditionPrefixAccessReady+providerName, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequestDeletion, "AccessRequest is being deleted")
			continue
		}
		log.Debug("Deleting AccessRequest that is no longer needed", "accessRequestName", ar.Name, "accessRequestNamespace", ar.Namespace, "oidcProviderName", providerName)
		if err := r.PlatformCluster.Client().Delete(ctx, &ar); client.IgnoreNotFound(err) != nil {
			rerr := errutils.WithReason(fmt.Errorf("error deleting AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
			errs.Append(rerr)
			createCon(corev2alpha1.ConditionPrefixAccessReady+providerName, metav1.ConditionFalse, rerr.Reason(), rerr.Error())
		}
		createCon(corev2alpha1.ConditionPrefixAccessReady+providerName, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequestDeletion, "AccessRequest is being deleted")
	}
	if rerr := errs.Aggregate(); rerr != nil {
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequestDeletion, "Error deleting AccessRequests that are no longer needed")
		return accessRequestsInDeletion, rerr
	}

	return accessRequestsInDeletion, nil
}

// deleteUndesiredAccessSecrets deletes all access secrets belonging to the ManagedControlPlane that are not copied from an up-to-date AccessRequest.
// It also deletes all mappings for which no secret exists from the ManagedControlPlane status.
// It returns a set of OIDC provider names for which the AccessRequest secrets are still in deletion.
func (r *ManagedControlPlaneReconciler) deleteUndesiredAccessSecrets(ctx context.Context, mcp *corev2alpha1.ManagedControlPlaneV2, updatedAccessRequests map[string]*clustersv1alpha1.AccessRequest, createCon func(conType string, status metav1.ConditionStatus, reason, message string)) (sets.Set[string], errutils.ReasonableError) {
	log := logging.FromContextOrPanic(ctx)

	accessSecretsInDeletion := sets.New[string]()

	matchingLabels := client.MatchingLabels{
		corev2alpha1.MCPNameLabel:      mcp.Name,
		corev2alpha1.MCPNamespaceLabel: mcp.Namespace,
		apiconst.ManagedByLabel:        ControllerName,
	}

	// delete all AccessRequest secrets that have been copied to the Onboarding cluster and belong to AccessRequests that are no longer needed
	oidcSecrets := &corev1.SecretList{}
	tokenSecrets := &corev1.SecretList{}

	if err := r.OnboardingCluster.Client().List(ctx, oidcSecrets, client.InNamespace(mcp.Namespace), client.HasLabels{corev2alpha1.OIDCProviderLabel}, matchingLabels); err != nil {
		rerr := errutils.WithReason(fmt.Errorf("error listing secrets for ManagedControlPlane '%s/%s': %w", mcp.Namespace, mcp.Name, err), cconst.ReasonOnboardingClusterInteractionProblem)
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, rerr.Reason(), rerr.Error())
		return accessSecretsInDeletion, rerr
	}

	if err := r.OnboardingCluster.Client().List(ctx, tokenSecrets, client.InNamespace(mcp.Namespace), client.HasLabels{corev2alpha1.TokenProviderLabel}, matchingLabels); err != nil {
		rerr := errutils.WithReason(fmt.Errorf("error listing secrets for ManagedControlPlane '%s/%s': %w", mcp.Namespace, mcp.Name, err), cconst.ReasonOnboardingClusterInteractionProblem)
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, rerr.Reason(), rerr.Error())
		return accessSecretsInDeletion, rerr
	}

	mcpSecrets := &corev1.SecretList{}
	mcpSecrets.Items = append(mcpSecrets.Items, oidcSecrets.Items...)
	mcpSecrets.Items = append(mcpSecrets.Items, tokenSecrets.Items...)

	errs := errutils.NewReasonableErrorList()
	for _, mcpSecret := range mcpSecrets.Items {
		oidcProviderName := mcpSecret.Labels[corev2alpha1.OIDCProviderLabel]
		tokeProviderName := mcpSecret.Labels[corev2alpha1.TokenProviderLabel]
		if oidcProviderName == "" && tokeProviderName == "" {
			log.Error(nil, "Secret for ManagedControlPlane has an empty OIDCProvider and empty TokenProvider label, this should not happen", "secretName", mcpSecret.Name, "secretNamespace", mcpSecret.Namespace)
			continue
		}
		if oidcProviderName != "" && tokeProviderName != "" {
			log.Error(nil, "Secret for ManagedControlPlane has both OIDCProvider and TokenProvider label set, this should not happen", "secretName", mcpSecret.Name, "secretNamespace", mcpSecret.Namespace, "oidcProviderName", oidcProviderName, "tokenProviderName", tokeProviderName)
			continue
		}
		providerName := corev2alpha1.OIDCNamePrefix + oidcProviderName
		if tokeProviderName != "" {
			providerName = corev2alpha1.TokenNamePrefix + tokeProviderName
		}
		if _, ok := updatedAccessRequests[providerName]; ok {
			continue
		}
		accessSecretsInDeletion.Insert(providerName)
		if !mcpSecret.DeletionTimestamp.IsZero() {
			log.Debug("Waiting for deletion of access secret that is no longer required", "secretName", mcpSecret.Name, "secretNamespace", mcpSecret.Namespace, "oidcProviderName", oidcProviderName)
			createCon(corev2alpha1.ConditionPrefixAccessReady+oidcProviderName, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequestDeletion, "AccessRequest secret is being deleted")
			continue
		}
		log.Debug("Deleting access secret that is no longer required", "secretName", mcpSecret.Name, "secretNamespace", mcpSecret.Namespace, "oidcProviderName", oidcProviderName)
		if err := r.OnboardingCluster.Client().Delete(ctx, &mcpSecret); client.IgnoreNotFound(err) != nil {
			rerr := errutils.WithReason(fmt.Errorf("error deleting access secret '%s/%s': %w", mcpSecret.Namespace, mcpSecret.Name, err), cconst.ReasonOnboardingClusterInteractionProblem)
			errs.Append(rerr)
			createCon(corev2alpha1.ConditionPrefixAccessReady+oidcProviderName, metav1.ConditionFalse, rerr.Reason(), rerr.Error())
		}
		createCon(corev2alpha1.ConditionPrefixAccessReady+oidcProviderName, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequestDeletion, "access secret is being deleted")
	}
	if rerr := errs.Aggregate(); rerr != nil {
		createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequestDeletion, "Error deleting access secrets that are no longer needed")
		return accessSecretsInDeletion, rerr
	}

	// delete all references to access secrets that are being deleted from the ManagedControlPlane status
	for providerName := range accessSecretsInDeletion {
		delete(mcp.Status.Access, providerName)
	}

	return accessSecretsInDeletion, nil
}

// syncAccessSecrets checks if all AccessRequests belonging to the ManagedControlPlane are ready and copies their secrets to the Onboarding cluster and references them in the ManagedControlPlane status.
// It returns a boolean indicating whether all AccessRequests are ready and their secrets have been copied successfully (true) or not (false).
func (r *ManagedControlPlaneReconciler) syncAccessSecrets(ctx context.Context, mcp *corev2alpha1.ManagedControlPlaneV2, updatedAccessRequests map[string]*clustersv1alpha1.AccessRequest, createCon func(conType string, status metav1.ConditionStatus, reason, message string)) (bool, errutils.ReasonableError) {
	log := logging.FromContextOrPanic(ctx)

	allAccessReady := true
	if mcp.Status.Access == nil {
		mcp.Status.Access = map[string]commonapi.LocalObjectReference{}
	}
	for providerName, ar := range updatedAccessRequests {
		if !ar.Status.IsGranted() || ar.Status.SecretRef == nil {
			log.Debug("AccessRequest is not ready yet", "accessRequestName", ar.Name, "accessRequestNamespace", ar.Namespace, "oidcProviderName", providerName)
			createCon(corev2alpha1.ConditionPrefixAccessReady+providerName, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "AccessRequest is not ready yet")
			allAccessReady = false
		} else {
			// copy access request secret and reference it in the ManagedControlPlane status
			arSecret := &corev1.Secret{}
			arSecret.Name = ar.Status.SecretRef.Name
			arSecret.Namespace = ar.Namespace
			if err := r.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(arSecret), arSecret); err != nil {
				rerr := errutils.WithReason(fmt.Errorf("error getting AccessRequest secret '%s/%s': %w", arSecret.Namespace, arSecret.Name, err), cconst.ReasonPlatformClusterInteractionProblem)
				createCon(corev2alpha1.ConditionPrefixAccessReady+providerName, metav1.ConditionFalse, rerr.Reason(), rerr.Error())
				createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "Error getting AccessRequest secret for OIDC provider "+providerName)
				return false, rerr
			}
			mcpSecret := &corev1.Secret{}
			mcpSecret.Name = ctrlutils.NameHashSHAKE128Base32(mcp.Name, providerName)
			mcpSecret.Namespace = mcp.Namespace
			if _, err := controllerutil.CreateOrUpdate(ctx, r.OnboardingCluster.Client(), mcpSecret, func() error {
				mcpSecret.Data = arSecret.Data
				if mcpSecret.Labels == nil {
					mcpSecret.Labels = map[string]string{}
				}
				mcpSecret.Labels[corev2alpha1.MCPNameLabel] = mcp.Name
				mcpSecret.Labels[corev2alpha1.MCPNamespaceLabel] = mcp.Namespace
				if ar.Spec.OIDC != nil {
					mcpSecret.Labels[corev2alpha1.OIDCProviderLabel] = ar.Spec.OIDC.Name
				}
				if ar.Spec.Token != nil {
					mcpSecret.Labels[corev2alpha1.TokenProviderLabel] = ar.Labels[corev2alpha1.TokenProviderLabel]
				}
				mcpSecret.Labels[apiconst.ManagedByLabel] = ControllerName

				if err := controllerutil.SetControllerReference(mcp, mcpSecret, r.OnboardingCluster.Scheme()); err != nil {
					return err
				}
				return nil
			}); err != nil {
				rerr := errutils.WithReason(fmt.Errorf("error creating/updating AccessRequest secret '%s/%s': %w", mcpSecret.Namespace, mcpSecret.Name, err), cconst.ReasonOnboardingClusterInteractionProblem)
				createCon(corev2alpha1.ConditionPrefixAccessReady+providerName, metav1.ConditionFalse, rerr.Reason(), rerr.Error())
				createCon(corev2alpha1.ConditionAllAccessReady, metav1.ConditionFalse, cconst.ReasonWaitingForAccessRequest, "Error creating/updating AccessRequest secret for OIDC provider "+providerName)
				return false, rerr
			}
			log.Debug("Access secret for ManagedControlPlane created/updated", "secretName", mcpSecret.Name, "oidcProviderName", providerName)
			mcp.Status.Access[providerName] = commonapi.LocalObjectReference{
				Name: mcpSecret.Name,
			}
			createCon(corev2alpha1.ConditionPrefixAccessReady+providerName, metav1.ConditionTrue, "", "")
		}
	}

	return allAccessReady, nil
}
