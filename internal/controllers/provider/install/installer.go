package install

import (
	"context"
	"fmt"
	"strconv"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/controller-utils/pkg/readiness"
	"github.com/openmcp-project/controller-utils/pkg/resources"
	v1 "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	PlatformClient  client.Client
	Provider        *unstructured.Unstructured
	DeploymentSpec  *v1alpha1.DeploymentSpec
	Environment     string
	SystemNamespace string
}

// InstallInitJob installs the init job of a provider.
// Does nothing if the generation has not changed.
// Otherwise, it deletes the old job before it creates the new job.
// Adds provider generation as annotation to the job.
func (a *Installer) InstallInitJob(ctx context.Context) (completed bool, err error) {

	values := NewValues(a.Provider, a.DeploymentSpec, a.Environment, a.SystemNamespace)

	if err := resources.CreateOrUpdateResource(ctx, a.PlatformClient, resources.NewNamespaceMutator(values.Namespace())); err != nil {
		return false, err
	}

	if err = resources.CreateOrUpdateResource(ctx, a.PlatformClient, newInitServiceAccountMutator(values)); err != nil {
		return false, err
	}

	if err = resources.CreateOrUpdateResource(ctx, a.PlatformClient, newInitClusterRoleMutator(values)); err != nil {
		return false, err
	}

	if err = resources.CreateOrUpdateResource(ctx, a.PlatformClient, newInitClusterRoleBindingMutator(values)); err != nil {
		return false, err
	}

	if err = resources.CreateOrUpdateResource(ctx, a.PlatformClient, newInitRoleMutator(values)); err != nil {
		return false, err
	}

	if err = resources.CreateOrUpdateResource(ctx, a.PlatformClient, newInitRoleBindingMutator(values)); err != nil {
		return false, err
	}

	j := NewJobMutator(values, a.DeploymentSpec, map[string]string{
		ProviderKindLabel:       a.Provider.GetKind(),
		ProviderNameLabel:       a.Provider.GetName(),
		ProviderGenerationLabel: fmt.Sprintf("%d", a.Provider.GetGeneration()),
	})
	var job *v1.Job
	found := true
	job, err = resources.GetResource(ctx, a.PlatformClient, j)
	if err != nil {
		if apierrors.IsNotFound(err) {
			found = false
		} else {
			return false, fmt.Errorf("failed to get job %s/%s: %w", values.Namespace(), a.Provider.GetName(), err)
		}
	}

	if !found {
		// Job does not exist, create it
		if err := resources.CreateOrUpdateResource(ctx, a.PlatformClient, j); err != nil {
			return false, fmt.Errorf("failed to create job %s/%s: %w", values.Namespace(), a.Provider.GetName(), err)
		}
		return false, nil

	} else if !a.isGenerationUpToDate(ctx, job) || a.isJobFailed(job) {
		// Job exists, but needs to be deleted and re-created
		// Delete the job with propagation policy "Background" to also delete the pods.
		if err := resources.DeleteResource(ctx, a.PlatformClient, j, client.PropagationPolicy(meta.DeletePropagationBackground)); err != nil {
			return false, fmt.Errorf("failed to delete job %s/%s: %w", values.Namespace(), a.Provider.GetName(), err)
		}
		if err := resources.CreateOrUpdateResource(ctx, a.PlatformClient, j); err != nil {
			return false, fmt.Errorf("failed to re-create job %s/%s: %w", values.Namespace(), a.Provider.GetName(), err)
		}
		return false, nil

	} else {
		// Job exists, check completion
		return job.Status.Succeeded > 0, nil
	}
}

func (a *Installer) InstallProvider(ctx context.Context) error {

	values := NewValues(a.Provider, a.DeploymentSpec, a.Environment, a.SystemNamespace)

	if err := resources.CreateOrUpdateResource(ctx, a.PlatformClient, newProviderServiceAccountMutator(values)); err != nil {
		return err
	}

	if err := resources.CreateOrUpdateResource(ctx, a.PlatformClient, newProviderClusterRoleBindingMutator(values)); err != nil {
		return err
	}

	if err := resources.CreateOrUpdateResource(ctx, a.PlatformClient, NewDeploymentMutator(values)); err != nil {
		return err
	}

	return nil
}

func (a *Installer) CheckProviderReadiness(ctx context.Context) readiness.CheckResult {
	values := NewValues(a.Provider, a.DeploymentSpec, a.Environment, a.SystemNamespace)

	depl, err := resources.GetResource(ctx, a.PlatformClient, NewDeploymentMutator(values))
	if err != nil {
		return readiness.NewFailedResult(err)
	}

	return readiness.CheckDeployment(depl)
}

func (a *Installer) UninstallProvider(ctx context.Context) (deleted bool, err error) {

	values := NewValues(a.Provider, a.DeploymentSpec, a.Environment, a.SystemNamespace)

	if err := resources.DeleteResource(ctx, a.PlatformClient, NewDeploymentMutator(values)); err != nil {
		return false, err
	}

	if err := resources.DeleteResource(ctx, a.PlatformClient, newProviderClusterRoleBindingMutator(values)); err != nil {
		return false, err
	}

	if err := resources.DeleteResource(ctx, a.PlatformClient, newProviderServiceAccountMutator(values)); err != nil {
		return false, err
	}

	// Delete the job with propagation policy "Background" to also delete the pods.
	if err := resources.DeleteResource(ctx, a.PlatformClient, NewJobMutator(values, a.DeploymentSpec, nil), client.PropagationPolicy(meta.DeletePropagationBackground)); err != nil {
		return false, err
	}

	if err := resources.DeleteResource(ctx, a.PlatformClient, newInitClusterRoleBindingMutator(values)); err != nil {
		return false, err
	}

	if err := resources.DeleteResource(ctx, a.PlatformClient, newInitClusterRoleMutator(values)); err != nil {
		return false, err
	}

	if err := resources.DeleteResource(ctx, a.PlatformClient, newInitServiceAccountMutator(values)); err != nil {
		return false, err
	}

	return true, nil
}

func (a *Installer) isJobFailed(job *v1.Job) bool {
	if job != nil {
		for _, condition := range job.Status.Conditions {
			if condition.Type == v1.JobFailed && condition.Status == core.ConditionTrue {
				return true
			}
		}
	}
	return false
}

func (a *Installer) isGenerationUpToDate(ctx context.Context, job *v1.Job) bool {
	genJob := job.Annotations[ProviderGenerationLabel]
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
