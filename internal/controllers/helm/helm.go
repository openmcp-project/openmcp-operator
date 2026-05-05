package helm

import (
	"context"
	"fmt"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fluxhelmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	fluxsourcev1 "github.com/fluxcd/source-controller/api/v1"

	"github.com/openmcp-project/controller-utils/pkg/collections"
	maputils "github.com/openmcp-project/controller-utils/pkg/collections/maps"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	clusterconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	helmv1alpha1 "github.com/openmcp-project/openmcp-operator/api/helm/v1alpha1"
)

// deployHelmChartSource deploys the configured Flux source (HelmRepository, GitRepository, OCIRepository) into the Cluster namespace.
// The first return value specifies whether a requeue is required (because the source is not ready yet).
func (c *HelmDeploymentController) deployHelmChartSource(ctx context.Context, cluster *clustersv1alpha1.Cluster, expectedLabels map[string]string, rr ReconcileResult, createConForCluster func(status metav1.ConditionStatus, reason, message string)) (bool, errutils.ReasonableError) {
	log := logging.FromContextOrPanic(ctx)

	// deploy Flux Source
	var fluxSource client.Object
	var setSpec func(obj client.Object) error
	sourceName := FluxResourceName(cluster.Name, rr.Object.Name, c.ProviderName)
	// list existing Flux sources to detect obsolete ones
	existingHelm := &fluxsourcev1.HelmRepositoryList{}
	if err := c.PlatformCluster.Client().List(ctx, existingHelm, client.InNamespace(cluster.Namespace), client.MatchingLabels(expectedLabels)); err != nil {
		createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonHelmChartSourceDeploymentFailed, fmt.Sprintf("Error listing existing HelmRepository resources in target namespace '%s': %s", cluster.Namespace, err.Error()))
		return false, errutils.WithReason(fmt.Errorf("error listing existing HelmRepository resources in target namespace '%s': %w", cluster.Namespace, err), clusterconst.ReasonPlatformClusterInteractionProblem)
	}
	existingGit := &fluxsourcev1.GitRepositoryList{}
	if err := c.PlatformCluster.Client().List(ctx, existingGit, client.InNamespace(cluster.Namespace), client.MatchingLabels(expectedLabels)); err != nil {
		createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonHelmChartSourceDeploymentFailed, fmt.Sprintf("Error listing existing GitRepository resources in target namespace '%s': %s", cluster.Namespace, err.Error()))
		return false, errutils.WithReason(fmt.Errorf("error listing existing GitRepository resources in target namespace '%s': %w", cluster.Namespace, err), clusterconst.ReasonPlatformClusterInteractionProblem)
	}
	existingOCI := &fluxsourcev1.OCIRepositoryList{}
	if err := c.PlatformCluster.Client().List(ctx, existingOCI, client.InNamespace(cluster.Namespace), client.MatchingLabels(expectedLabels)); err != nil {
		createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonHelmChartSourceDeploymentFailed, fmt.Sprintf("Error listing existing OCIRepository resources in target namespace '%s': %s", cluster.Namespace, err.Error()))
		return false, errutils.WithReason(fmt.Errorf("error listing existing OCIRepository resources in target namespace '%s': %w", cluster.Namespace, err), clusterconst.ReasonPlatformClusterInteractionProblem)
	}
	toBeDeleted := []client.Object{}
	var sourceKind string
	// determine which type of source to create and which existing sources to delete
	if rr.Object.Spec.ChartSource.Helm != nil {
		fluxSource = &fluxsourcev1.HelmRepository{}
		setSpec = func(obj client.Object) error {
			helmRepo, ok := obj.(*fluxsourcev1.HelmRepository)
			if !ok {
				return fmt.Errorf("expected HelmRepository object, got %T", obj)
			}
			helmRepo.Spec = *rr.Object.Spec.ChartSource.Helm.DeepCopy()
			return nil
		}
		sourceKind = helmv1alpha1.SourceKindHelmRepository
		for i := range existingHelm.Items {
			obj := &existingHelm.Items[i]
			if obj.GetName() != sourceName {
				toBeDeleted = append(toBeDeleted, obj)
			}
		}
		toBeDeleted = append(toBeDeleted, collections.ProjectSliceToSlice(existingGit.Items, func(obj fluxsourcev1.GitRepository) client.Object { return &obj })...)
		toBeDeleted = append(toBeDeleted, collections.ProjectSliceToSlice(existingOCI.Items, func(obj fluxsourcev1.OCIRepository) client.Object { return &obj })...)
	} else if rr.Object.Spec.ChartSource.Git != nil {
		fluxSource = &fluxsourcev1.GitRepository{}
		setSpec = func(obj client.Object) error {
			gitRepo, ok := obj.(*fluxsourcev1.GitRepository)
			if !ok {
				return fmt.Errorf("expected GitRepository object, got %T", obj)
			}
			gitRepo.Spec = *rr.Object.Spec.ChartSource.Git.DeepCopy()

			return nil
		}
		sourceKind = helmv1alpha1.SourceKindGitRepository
		for i := range existingGit.Items {
			obj := &existingGit.Items[i]
			if obj.GetName() != sourceName {
				toBeDeleted = append(toBeDeleted, obj)
			}
		}
		toBeDeleted = append(toBeDeleted, collections.ProjectSliceToSlice(existingHelm.Items, func(obj fluxsourcev1.HelmRepository) client.Object { return &obj })...)
		toBeDeleted = append(toBeDeleted, collections.ProjectSliceToSlice(existingOCI.Items, func(obj fluxsourcev1.OCIRepository) client.Object { return &obj })...)
	} else if rr.Object.Spec.ChartSource.OCI != nil {
		fluxSource = &fluxsourcev1.OCIRepository{}
		setSpec = func(obj client.Object) error {
			ociRepo, ok := obj.(*fluxsourcev1.OCIRepository)
			if !ok {
				return fmt.Errorf("expected OCIRepository object, got %T", obj)
			}
			ociRepo.Spec = *rr.Object.Spec.ChartSource.OCI.DeepCopy()
			return nil
		}
		sourceKind = helmv1alpha1.SourceKindOCIRepository
		for i := range existingOCI.Items {
			obj := &existingOCI.Items[i]
			if obj.GetName() != sourceName {
				toBeDeleted = append(toBeDeleted, obj)
			}
		}
		toBeDeleted = append(toBeDeleted, collections.ProjectSliceToSlice(existingHelm.Items, func(obj fluxsourcev1.HelmRepository) client.Object { return &obj })...)
		toBeDeleted = append(toBeDeleted, collections.ProjectSliceToSlice(existingGit.Items, func(obj fluxsourcev1.GitRepository) client.Object { return &obj })...)
	} else {
		return false, errutils.WithReason(fmt.Errorf("no flux source configured"), clusterconst.ReasonConfigurationProblem)
	}
	fluxSource.SetName(sourceName)
	fluxSource.SetNamespace(cluster.Namespace)
	log.Info("Creating or updating Flux source", "kind", sourceKind, "sourceName", fluxSource.GetName(), "sourceNamespace", fluxSource.GetNamespace())
	if _, err := controllerutil.CreateOrUpdate(ctx, c.PlatformCluster.Client(), fluxSource, func() error {
		if err := controllerutil.SetOwnerReference(cluster, fluxSource, c.PlatformCluster.Scheme()); err != nil {
			createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonHelmChartSourceDeploymentFailed, fmt.Sprintf("Error setting owner reference on %s '%s/%s': %s", sourceKind, fluxSource.GetNamespace(), fluxSource.GetName(), err.Error()))
			return fmt.Errorf("error setting owner reference on %s '%s/%s': %w", sourceKind, fluxSource.GetNamespace(), fluxSource.GetName(), err)
		}
		fluxSource.SetLabels(maputils.Merge(fluxSource.GetLabels(), expectedLabels))
		return setSpec(fluxSource)
	}); err != nil {
		createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonHelmChartSourceDeploymentFailed, fmt.Sprintf("Error creating or updating %s '%s/%s': %s", sourceKind, fluxSource.GetNamespace(), fluxSource.GetName(), err.Error()))
		return false, errutils.WithReason(fmt.Errorf("error creating or updating %s '%s/%s': %w", sourceKind, fluxSource.GetNamespace(), fluxSource.GetName(), err), clusterconst.ReasonPlatformClusterInteractionProblem)
	}
	// delete obsolete sources
	for _, obj := range toBeDeleted {
		log.Info("Deleting obsolete Flux source", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName(), "namespace", obj.GetNamespace())
		if err := c.PlatformCluster.Client().Delete(ctx, obj); err != nil {
			if !apierrors.IsNotFound(err) {
				createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonHelmChartSourceDeploymentFailed, fmt.Sprintf("Error deleting obsolete %s '%s/%s': %s", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName(), err.Error()))
				return false, errutils.WithReason(fmt.Errorf("error deleting obsolete %s '%s/%s': %w", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName(), err), clusterconst.ReasonPlatformClusterInteractionProblem)
			}
		}
	}

	if err := c.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(fluxSource), fluxSource); err != nil {
		return false, errutils.WithReason(fmt.Errorf("error getting %s '%s/%s' to check for readiness: %w", sourceKind, fluxSource.GetNamespace(), fluxSource.GetName(), err), clusterconst.ReasonPlatformClusterInteractionProblem)
	}
	readyCon := getReadyCondition(fluxSource)
	if readyCon == nil || readyCon.Status != metav1.ConditionTrue || readyCon.ObservedGeneration != fluxSource.GetGeneration() {
		createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonWaitingForHelmChartSourceHealthy, fmt.Sprintf("%s '%s/%s' is not ready yet", sourceKind, fluxSource.GetNamespace(), fluxSource.GetName()))
		return true, nil
	}

	// no need to set the condition here, because deployment of the HelmRelease will start immediately and overwrite it anyway
	return false, nil
}

// deployHelmRelease deploys the HelmRelease to install external-dns onto the Cluster.
// It expects 'Config', 'AccessRequest', and 'SourceKind' to be set in the given ReconcileResult.
// The first return value specifies whether a requeue is required (because the release is not ready yet).
func (c *HelmDeploymentController) deployHelmRelease(ctx context.Context, cluster *clustersv1alpha1.Cluster, ar *clustersv1alpha1.AccessRequest, expectedLabels map[string]string, rr ReconcileResult, createConForCluster func(status metav1.ConditionStatus, reason, message string)) (bool, errutils.ReasonableError) {
	log := logging.FromContextOrPanic(ctx)

	hr := &fluxhelmv2.HelmRelease{}
	hr.Name = FluxResourceName(cluster.Name, rr.Object.Name, c.ProviderName)
	hr.Namespace = cluster.Namespace

	log.Info("Creating or updating HelmRelease", "resourceName", hr.Name, "resourceNamespace", hr.Namespace)
	if _, err := controllerutil.CreateOrUpdate(ctx, c.PlatformCluster.Client(), hr, func() error {
		// owner reference
		if err := controllerutil.SetOwnerReference(cluster, hr, c.PlatformCluster.Scheme()); err != nil {
			createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonHelmReleaseDeploymentFailed, fmt.Sprintf("Error setting owner reference on HelmRelease '%s/%s': %s", hr.Namespace, hr.Name, err.Error()))
			return fmt.Errorf("error setting owner reference on HelmRelease '%s/%s': %w", hr.Namespace, hr.Name, err)
		}
		// labels
		hr.Labels = maputils.Merge(hr.Labels, expectedLabels)
		// chart
		if rr.SourceKind == helmv1alpha1.SourceKindOCIRepository {
			hr.Spec.Chart = nil
			hr.Spec.ChartRef = &fluxhelmv2.CrossNamespaceSourceReference{
				APIVersion: fluxsourcev1.SchemeBuilder.GroupVersion.String(),
				Kind:       rr.SourceKind,
				Name:       hr.Name,
				Namespace:  hr.Namespace,
			}
		} else {
			hr.Spec.ChartRef = nil
			hr.Spec.Chart = &fluxhelmv2.HelmChartTemplate{
				Spec: fluxhelmv2.HelmChartTemplateSpec{
					SourceRef: fluxhelmv2.CrossNamespaceObjectReference{
						APIVersion: fluxsourcev1.SchemeBuilder.GroupVersion.String(),
						Kind:       rr.SourceKind,
						Name:       hr.Name,
						Namespace:  hr.Namespace,
					},
				},
			}
			chartNameVersion := strings.Split(rr.Object.Spec.ChartSource.ChartName, "@")
			hr.Spec.Chart.Spec.Chart = chartNameVersion[0]
			if len(chartNameVersion) > 1 {
				hr.Spec.Chart.Spec.Version = chartNameVersion[1]
			}
		}
		// release information
		hr.Spec.ReleaseName = fmt.Sprintf("%s/%s", rr.Object.Namespace, rr.Object.Name)
		hr.Spec.TargetNamespace = rr.Object.Spec.Namespace
		hr.Spec.StorageNamespace = rr.Object.Spec.Namespace
		// values
		if rr.Object.Spec.HelmValues != nil {
			values := string(rr.Object.Spec.HelmValues.Raw)
			// at some point '<' and '>' get escaped and we have to match the escaped version here
			values = strings.ReplaceAll(values, fmt.Sprintf("%sprovider.namespace%s", "\\u003c", "\\u003e"), c.ProviderNamespace)
			values = strings.ReplaceAll(values, fmt.Sprintf("%sprovider.name%s", "\\u003c", "\\u003e"), c.ProviderName)
			values = strings.ReplaceAll(values, fmt.Sprintf("%senvironment%s", "\\u003c", "\\u003e"), c.Environment)
			values = strings.ReplaceAll(values, fmt.Sprintf("%shelm.name%s", "\\u003c", "\\u003e"), rr.Object.Name)
			values = strings.ReplaceAll(values, fmt.Sprintf("%shelm.namespace%s", "\\u003c", "\\u003e"), rr.Object.Namespace)
			values = strings.ReplaceAll(values, fmt.Sprintf("%scluster.namespace%s", "\\u003c", "\\u003e"), cluster.Namespace)
			values = strings.ReplaceAll(values, fmt.Sprintf("%scluster.name%s", "\\u003c", "\\u003e"), cluster.Name)
			hr.Spec.Values = &apiextensionsv1.JSON{Raw: []byte(values)}
		}
		// install configuration
		if hr.Spec.Install == nil {
			hr.Spec.Install = &fluxhelmv2.Install{
				CreateNamespace: true,
			}
		}
		hr.Spec.Install.CRDs = fluxhelmv2.CreateReplace
		if hr.Spec.Install.Remediation == nil {
			hr.Spec.Install.Remediation = &fluxhelmv2.InstallRemediation{}
		}
		hr.Spec.Install.Remediation.Retries = 3
		// upgrade configuration
		if hr.Spec.Upgrade == nil {
			hr.Spec.Upgrade = &fluxhelmv2.Upgrade{}
		}
		hr.Spec.Upgrade.CRDs = fluxhelmv2.CreateReplace
		if hr.Spec.Upgrade.Remediation == nil {
			hr.Spec.Upgrade.Remediation = &fluxhelmv2.UpgradeRemediation{}
		}
		hr.Spec.Upgrade.Remediation.Retries = 3
		// reference Cluster kubeconfig
		hr.Spec.KubeConfig = &fluxmeta.KubeConfigReference{
			SecretRef: &fluxmeta.SecretKeyReference{
				Name: ar.Status.SecretRef.Name,
				Key:  clustersv1alpha1.SecretKeyKubeconfig,
			},
		}
		// deploy interval
		hr.Spec.Interval = rr.Config.Spec.HelmReleaseReconciliationIntervals.IntervalForSourceKind(rr.SourceKind)
		return nil
	}); err != nil {
		createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonHelmReleaseDeploymentFailed, fmt.Sprintf("Error creating or updating HelmRelease '%s/%s': %s", hr.Namespace, hr.Name, err.Error()))
		return false, errutils.WithReason(fmt.Errorf("error creating or updating HelmRelease '%s/%s': %w", hr.Namespace, hr.Name, err), clusterconst.ReasonPlatformClusterInteractionProblem)
	}

	if err := c.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(hr), hr); err != nil {
		createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonHelmReleaseDeploymentFailed, fmt.Sprintf("Error getting HelmRelease '%s/%s' to check for readiness: %s", hr.Namespace, hr.Name, err.Error()))
		return false, errutils.WithReason(fmt.Errorf("error getting HelmRelease '%s/%s' to check for readiness: %w", hr.Namespace, hr.Name, err), clusterconst.ReasonPlatformClusterInteractionProblem)
	}
	readyCon := getReadyCondition(hr)
	if readyCon == nil || readyCon.Status != metav1.ConditionTrue || readyCon.ObservedGeneration != hr.GetGeneration() {
		createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonWaitingForHelmReleaseHealthy, fmt.Sprintf("HelmRelease '%s/%s' is not ready yet", hr.Namespace, hr.Name))
		return true, nil
	}

	createConForCluster(metav1.ConditionTrue, helmv1alpha1.ReasonFluxResourcesDeployedAndHealthy, fmt.Sprintf("HelmRelease '%s/%s' is ready", hr.Namespace, hr.Name))
	return false, nil
}

// If the first return value is true, a requeue is required to verify the deletion of the HelmRelease.
func (c *HelmDeploymentController) deleteHelmRelease(ctx context.Context, hr *fluxhelmv2.HelmRelease) (bool, errutils.ReasonableError) {
	log := logging.FromContextOrPanic(ctx)

	if err := c.PlatformCluster.Client().Get(ctx, client.ObjectKeyFromObject(hr), hr); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("HelmRelease not found, nothing to do")
			return false, nil
		} else {
			return false, errutils.WithReason(fmt.Errorf("error getting HelmRelease '%s/%s': %w", hr.Namespace, hr.Name, err), clusterconst.ReasonPlatformClusterInteractionProblem)
		}
	}

	// check if HelmRelease is marked for deletion
	if !hr.DeletionTimestamp.IsZero() {
		log.Info("HelmRelease already marked for deletion, waiting for its removal")
	} else {
		// delete HelmRelease
		log.Info("Deleting HelmRelease")
		if err := c.PlatformCluster.Client().Delete(ctx, hr); err != nil {
			if !apierrors.IsNotFound(err) {
				return false, errutils.WithReason(fmt.Errorf("error deleting HelmRelease '%s/%s': %w", hr.Namespace, hr.Name, err), clusterconst.ReasonPlatformClusterInteractionProblem)
			}
		}
	}

	// requeue to verify deletion
	return true, nil
}

// deleteMultipleHelmReleases deletes all HelmRelease resources matching the expected labels.
// If the given namespace is non-empty, only resources in that namespace will be considered.
// It returns a list of HelmRelease resources that still exist after the deletion attempt.
func (c *HelmDeploymentController) deleteMultipleHelmReleases(ctx context.Context, expectedLabels map[string]string, namespace string, createCon func(conType string, status metav1.ConditionStatus, reason, message string)) ([]*fluxhelmv2.HelmRelease, errutils.ReasonableError) {
	log := logging.FromContextOrPanic(ctx)

	hrs := &fluxhelmv2.HelmReleaseList{}
	listOptions := []client.ListOption{client.MatchingLabels(expectedLabels)}
	if namespace != "" {
		listOptions = append(listOptions, client.InNamespace(namespace))
	}
	if err := c.PlatformCluster.Client().List(ctx, hrs, listOptions...); err != nil {
		return nil, errutils.WithReason(fmt.Errorf("error listing HelmReleases: %w", err), clusterconst.ReasonPlatformClusterInteractionProblem)
	}

	errs := errutils.NewReasonableErrorList()
	remainingResources := []*fluxhelmv2.HelmRelease{}
	for _, hr := range hrs.Items {
		hrIdentity := fmt.Sprintf("%s/%s", hr.Namespace, hr.Name)
		clog := log.WithValues("helmRelease", hrIdentity)
		createConForCluster := func(status metav1.ConditionStatus, reason, message string) {}
		cID := clusterIdentityFromLabels(&hr)
		if cID == "" {
			clog.Error(nil, "HelmRelease is missing the cluster labels, this should not happen")
		} else {
			clog = clog.WithValues("cluster", cID)
			createConForCluster = func(status metav1.ConditionStatus, reason, message string) {
				createCon(clusterConType(cID), status, reason, message)
			}
		}
		requeueRequired, err := c.deleteHelmRelease(logging.NewContext(ctx, clog), &hr)
		if err != nil {
			errs.Append(err)
			clog.Error(err, "Error deleting HelmRelease")
			remainingResources = append(remainingResources, &hr)
			createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonHelmReleaseDeletionFailed, fmt.Sprintf("Error deleting HelmRelease '%s/%s': [%s] %s", hr.Namespace, hr.Name, err.Reason(), err.Error()))
			continue
		}
		if requeueRequired {
			remainingResources = append(remainingResources, &hr)
			createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonWaitingForHelmReleaseDeletion, fmt.Sprintf("Waiting for HelmRelease '%s/%s' to be deleted", hr.Namespace, hr.Name))
		}
	}
	return remainingResources, errs.Aggregate()
}

// deleteMultipleHelmChartSources deletes all Flux sources with the specified labels.
// Resources that share a cluster label with any of the remaining HelmReleases will be skipped.
// If the given namespace is non-empty, only resources in that namespace will be considered.
// It returns a list of resources that still exist after the deletion attempt (including the skipped ones), which can be used to verify their deletion in subsequent reconciliations.
func (c *HelmDeploymentController) deleteMultipleHelmChartSources(ctx context.Context, expectedLabels map[string]string, namespace string, remainingHelmReleases []*fluxhelmv2.HelmRelease, createCon func(conType string, status metav1.ConditionStatus, reason, message string)) ([]client.Object, errutils.ReasonableError) {
	log := logging.FromContextOrPanic(ctx)

	listOptions := []client.ListOption{client.MatchingLabels(expectedLabels)}
	if namespace != "" {
		listOptions = append(listOptions, client.InNamespace(namespace))
	}
	existingHelm := &fluxsourcev1.HelmRepositoryList{}
	if err := c.PlatformCluster.Client().List(ctx, existingHelm, listOptions...); err != nil {
		return nil, errutils.WithReason(fmt.Errorf("error listing existing HelmRepository resources: %w", err), clusterconst.ReasonPlatformClusterInteractionProblem)
	}
	existingGit := &fluxsourcev1.GitRepositoryList{}
	if err := c.PlatformCluster.Client().List(ctx, existingGit, listOptions...); err != nil {
		return nil, errutils.WithReason(fmt.Errorf("error listing existing GitRepository resources: %w", err), clusterconst.ReasonPlatformClusterInteractionProblem)
	}
	existingOCI := &fluxsourcev1.OCIRepositoryList{}
	if err := c.PlatformCluster.Client().List(ctx, existingOCI, listOptions...); err != nil {
		return nil, errutils.WithReason(fmt.Errorf("error listing existing OCIRepository resources: %w", err), clusterconst.ReasonPlatformClusterInteractionProblem)
	}
	toBeDeleted := []client.Object{}
	toBeDeleted = append(toBeDeleted, collections.ProjectSliceToSlice(existingHelm.Items, func(obj fluxsourcev1.HelmRepository) client.Object {
		if obj.GetObjectKind().GroupVersionKind().Kind == "" {
			obj.SetGroupVersionKind(schema.GroupVersionKind{Group: fluxsourcev1.GroupVersion.Group, Version: fluxsourcev1.GroupVersion.Version, Kind: helmv1alpha1.SourceKindHelmRepository})
		}
		return &obj
	})...)
	toBeDeleted = append(toBeDeleted, collections.ProjectSliceToSlice(existingGit.Items, func(obj fluxsourcev1.GitRepository) client.Object {
		if obj.GetObjectKind().GroupVersionKind().Kind == "" {
			obj.SetGroupVersionKind(schema.GroupVersionKind{Group: fluxsourcev1.GroupVersion.Group, Version: fluxsourcev1.GroupVersion.Version, Kind: helmv1alpha1.SourceKindGitRepository})
		}
		return &obj
	})...)
	toBeDeleted = append(toBeDeleted, collections.ProjectSliceToSlice(existingOCI.Items, func(obj fluxsourcev1.OCIRepository) client.Object {
		if obj.GetObjectKind().GroupVersionKind().Kind == "" {
			obj.SetGroupVersionKind(schema.GroupVersionKind{Group: fluxsourcev1.GroupVersion.Group, Version: fluxsourcev1.GroupVersion.Version, Kind: helmv1alpha1.SourceKindOCIRepository})
		}
		return &obj
	})...)

	remainingHRClusters := sets.New[string]()
	for _, hr := range remainingHelmReleases {
		cID := clusterIdentityFromLabels(hr)
		remainingHRClusters.Insert(cID)
	}

	errs := errutils.NewReasonableErrorList()
	remainingResources := []client.Object{}
	for _, tbd := range toBeDeleted {
		clog := log.WithValues("kind", tbd.GetObjectKind().GroupVersionKind().Kind, "resource", fmt.Sprintf("%s/%s", tbd.GetNamespace(), tbd.GetName()))
		createConForCluster := func(status metav1.ConditionStatus, reason, message string) {}
		cID := clusterIdentityFromLabels(tbd)
		if cID == "" {
			clog.Error(nil, "Flux source is missing the cluster labels, this should not happen")
		} else {
			clog = clog.WithValues("cluster", cID)
			createConForCluster = func(status metav1.ConditionStatus, reason, message string) {
				createCon(clusterConType(cID), status, reason, message)
			}
		}
		if remainingHRClusters.Has(cID) {
			clog.Debug("Skipping deletion of flux source because there are still HelmReleases referencing it")
			// no need to set condition, should have been set by the respective HelmRelease deletion logic already
			remainingResources = append(remainingResources, tbd)
			continue
		}

		if !tbd.GetDeletionTimestamp().IsZero() {
			log.Info("Flux source already marked for deletion, waiting for its removal")
			remainingResources = append(remainingResources, tbd)
			createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonWaitingForHelmChartSourceDeletion, fmt.Sprintf("Waiting for %s '%s/%s' to be deleted", tbd.GetObjectKind().GroupVersionKind().Kind, tbd.GetNamespace(), tbd.GetName()))
		} else {
			log.Info("Deleting Flux source")
			if err := c.PlatformCluster.Client().Delete(ctx, tbd); err != nil {
				if !apierrors.IsNotFound(err) {
					rerr := errutils.WithReason(fmt.Errorf("error deleting Flux source %s '%s/%s': %w", tbd.GetObjectKind().GroupVersionKind().Kind, tbd.GetNamespace(), tbd.GetName(), err), clusterconst.ReasonPlatformClusterInteractionProblem)
					errs.Append(rerr)
					remainingResources = append(remainingResources, tbd)
					createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonHelmChartSourceDeletionFailed, fmt.Sprintf("Error deleting %s '%s/%s': [%s] %s", tbd.GetObjectKind().GroupVersionKind().Kind, tbd.GetNamespace(), tbd.GetName(), rerr.Reason(), rerr.Error()))
					continue
				}
			} else {
				remainingResources = append(remainingResources, tbd)
				createConForCluster(metav1.ConditionFalse, helmv1alpha1.ReasonWaitingForHelmChartSourceDeletion, fmt.Sprintf("Waiting for %s '%s/%s' to be deleted", tbd.GetObjectKind().GroupVersionKind().Kind, tbd.GetNamespace(), tbd.GetName()))
			}
		}
	}

	return remainingResources, errs.Aggregate()
}

// FluxResourceName generates a name for the flux resources related to a specific combination of Cluster and HelmDeployment.
func FluxResourceName(clusterName, helmDeploymentName, providerName string) string {
	return ctrlutils.ShortenToXCharactersUnsafe(fmt.Sprintf("%s--%s--%s", clusterName, helmDeploymentName, providerName), ctrlutils.K8sMaxNameLength)
}

// getReadyCondition returns the condition with type 'Ready' from the given object's status conditions.
// This is meant to be used on flux resources, which usually have a 'status.conditions' field containing a list of conditions.
// This function will panic if the object does not have a 'status.conditions' field or if it is not a list of metav1.Conditions.
func getReadyCondition(obj client.Object) *metav1.Condition {
	rawCons := ctrlutils.GetField(obj, "Status.Conditions", false)
	if rawCons == nil {
		return nil
	}
	cons, ok := rawCons.([]metav1.Condition)
	if !ok {
		panic(fmt.Sprintf("expected status.conditions to be of type []metav1.Condition, got %T", rawCons))
	}
	for i := range cons {
		if cons[i].Type == "Ready" {
			return &cons[i]
		}
	}
	return nil
}
