package install

import (
	"context"
	"fmt"
	"strconv"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/controller-utils/pkg/readiness"
	"github.com/openmcp-project/controller-utils/pkg/resources"
	v1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
)

const (
	ProviderNameLabel       = "openmcp.cloud/provider-name"
	ProviderKindLabel       = "openmcp.cloud/provider-kind"
	ProviderGenerationLabel = "openmcp.cloud/provider-generation"
)

type Installer struct {
	PlatformClient client.Client
	Provider       *unstructured.Unstructured
	DeploymentSpec *v1alpha1.DeploymentSpec
	Namespace      string
}

// InstallInitJob installs the init job of a provider.
// Does nothing if the generation has not changed.
// Otherwise, it deletes the old job before it creates the new job.
// Adds provider generation as annotation to the job.
func (a *Installer) InstallInitJob(ctx context.Context) (completed bool, err error) {

	// Create namespace if it does not exist
	n := resources.NewNamespaceMutator(a.Namespace, nil, nil)
	if err := resources.CreateOrUpdateResource(ctx, a.PlatformClient, n); err != nil {
		return false, fmt.Errorf("failed to create namespace %s: %w", a.Namespace, err)
	}

	j := newJobMutator(a.Provider.GetName(), a.Namespace, a.DeploymentSpec, nil, map[string]string{
		ProviderKindLabel:       a.Provider.GetKind(),
		ProviderNameLabel:       a.Provider.GetName(),
		ProviderGenerationLabel: fmt.Sprintf("%d", a.Provider.GetGeneration()),
	})
	job := j.Empty()
	found := true
	if err := a.PlatformClient.Get(ctx, client.ObjectKeyFromObject(job), job); err != nil {
		if apierrors.IsNotFound(err) {
			found = false
		} else {
			return false, fmt.Errorf("failed to get job %s/%s: %w", a.Namespace, a.Provider.GetName(), err)
		}
	}

	if found {
		// Delete and re-create job if it has an old provider generation or is failed.
		if !a.isGenerationUpToDate(ctx, job) || a.isJobFailed(job) {
			if err := resources.DeleteResource(ctx, a.PlatformClient, j); err != nil {
				return false, fmt.Errorf("failed to delete job %s/%s: %w", a.Namespace, a.Provider.GetName(), err)
			}
			if err := resources.CreateOrUpdateResource(ctx, a.PlatformClient, j); err != nil {
				return false, fmt.Errorf("failed to re-create job %s/%s: %w", a.Namespace, a.Provider.GetName(), err)
			}
		}
	} else {
		if err := resources.CreateOrUpdateResource(ctx, a.PlatformClient, j); err != nil {
			return false, fmt.Errorf("failed to create job %s/%s: %w", a.Namespace, a.Provider.GetName(), err)
		}
	}

	return true, nil
}

func (a *Installer) CheckInitJobReadiness(ctx context.Context) readiness.CheckResult {
	return nil
}

func (a *Installer) InstallProvider(ctx context.Context) error {
	return nil
}

func (a *Installer) CheckProviderReadiness(ctx context.Context) readiness.CheckResult {
	return nil
}

func (a *Installer) UninstallProvider(ctx context.Context) error {
	return nil
}

func (a *Installer) isJobFailed(job *v1.Job) bool {
	return job.Status.Failed > 0
}

func (a *Installer) isGenerationUpToDate(ctx context.Context, job *v1.Job) bool {
	genJob := job.ObjectMeta.Annotations[ProviderGenerationLabel]
	if genJob == "" {
		return false
	}
	genJobInt, err := strconv.ParseInt(genJob, 10, 64)
	if err != nil {
		log := logging.FromContextOrPanic(ctx)
		log.Info(fmt.Errorf("failed to parse job generation %s: %w", genJob, err).Error())
		return false
	}
	genProvider := a.Provider.GetGeneration()
	return genJobInt == genProvider
}
