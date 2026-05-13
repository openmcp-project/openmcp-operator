package helm

import (
	"context"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openmcp-project/controller-utils/pkg/collections"
	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/controller-utils/pkg/pairs"

	cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	helmv1alpha1 "github.com/openmcp-project/openmcp-operator/api/helm/v1alpha1"
)

// ManageSecrets manages the secret copying.
// It copies all specified secrets to the specified clusters and removes all secrets (within the corresponding namespaces) which have the desired labels but were not specified to copy.
// It returns errors if
// - a secret cannot be created or deleted due to some issue with the cluster (reason is 'PlatformClusterInteractionProblem' or 'TargetClusterInteractionProblem')
// - a secret should be updated, but the existing secret does not have the expected labels (reason is 'ConfigurationProblem')
// - a SecretCopy is invalid (reason is 'ConfigurationProblem')
func (c *HelmDeploymentController) ManageSecrets(ctx context.Context, targetClusterClient client.Client, expectedLabels map[string]string, secretsToCopy ...SecretCopy) *errutils.ReasonableErrorList {
	log := logging.FromContextOrDiscard(ctx)
	log.Debug("Starting secret management")
	errs := errutils.NewReasonableErrorList()

	// create namespace(s), if missing
	targetNamespacesList := &corev1.NamespaceList{}
	if err := targetClusterClient.List(ctx, targetNamespacesList); err != nil {
		errs.Append(errutils.WithReason(fmt.Errorf("unable to list target cluster namespaces: %w", err), helmv1alpha1.ReasonTargetClusterInteractionProblem))
		return errs
	}
	targetNamespaces := sets.New(collections.ProjectSliceToSlice(targetNamespacesList.Items, func(ns corev1.Namespace) string {
		return ns.Name
	})...)

	alreadyCopied := sets.New[string]()
	for _, sc := range secretsToCopy {
		var cl client.Client
		var clusterFailReason string
		switch sc.Cluster {
		case ToPlatformCluster:
			cl = c.PlatformCluster.Client()
			clusterFailReason = cconst.ReasonPlatformClusterInteractionProblem
		case ToTargetCluster:
			cl = targetClusterClient
			clusterFailReason = helmv1alpha1.ReasonTargetClusterInteractionProblem
		default:
			errs.Append(errutils.WithReason(fmt.Errorf("invalid target cluster '%s' for secret copy instruction (copying from '%s' to '%s')", sc.Cluster, sc.Source.NamespacedName().String(), sc.Target.NamespacedName().String()), cconst.ReasonConfigurationProblem))
			continue
		}
		clog := log.WithValues("source", sc.Source.NamespacedName().String(), "target", sc.Target.NamespacedName().String(), "secretCopyTarget", string(sc.Cluster))
		if sc.Cluster == ToPlatformCluster && sc.Source.Name == sc.Target.Name && sc.Source.Namespace == sc.Target.Namespace {
			clog.Debug("Skipping secret copying, because source and target are identical")
			continue
		}
		alreadyCopiedID := fmt.Sprintf("%s/%s/%s", string(sc.Cluster), sc.Source.NamespacedName().String(), sc.Target.NamespacedName().String())
		if alreadyCopied.Has(alreadyCopiedID) {
			clog.Debug("Skipping secret copying, because this secret was already copied")
			continue
		}
		if sc.Cluster == ToTargetCluster && !targetNamespaces.Has(sc.Target.Namespace) {
			clog.Debug("Creating missing namespace on target cluster")
			if err := cl.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: sc.Target.Namespace,
				},
			}); err != nil {
				errs.Append(errutils.WithReason(fmt.Errorf("unable to create namespace '%s' on target cluster: %w", sc.Target.Namespace, err), helmv1alpha1.ReasonTargetClusterInteractionProblem))
				continue
			}
			targetNamespaces.Insert(sc.Target.Namespace)
		}
		alreadyCopied.Insert(alreadyCopiedID)
		expectedSecretLabels := maps.Clone(expectedLabels)
		expectedSecretLabels[helmv1alpha1.GroupName+"/source-name"] = sc.Source.Name
		expectedSecretLabels[helmv1alpha1.GroupName+"/source-namespace"] = sc.Source.Namespace
		clog.Debug("Copying secret")
		// fetch source secret
		srcSec := &corev1.Secret{}
		if err := c.PlatformCluster.Client().Get(ctx, sc.Source.NamespacedName(), srcSec); err != nil {
			errs.Append(errutils.WithReason(fmt.Errorf("error fetching source secret '%s': %w", sc.Source.NamespacedName().String(), err), cconst.ReasonPlatformClusterInteractionProblem))
			continue
		}
		// check if target secret exists
		tarSec := &corev1.Secret{}
		tarSec.Name = sc.Target.Name
		tarSec.Namespace = sc.Target.Namespace
		exists := true
		if err := cl.Get(ctx, client.ObjectKeyFromObject(tarSec), tarSec); err != nil {
			if !apierrors.IsNotFound(err) {
				errs.Append(errutils.WithReason(fmt.Errorf("error checking for target secret '%s' on %s cluster: %w", sc.Target.NamespacedName().String(), string(sc.Cluster), err), clusterFailReason))
				continue
			}
			exists = false
		}
		// if target secret exists, validate that it has the correct labels, otherwise we are not allowed to overwrite it
		if exists {
			for key, eVal := range expectedSecretLabels {
				aVal := tarSec.Labels[key]
				if aVal != eVal {
					errs.Append(errutils.WithReason(fmt.Errorf("unable to update secret '%s' on %s cluster: label '%s' has value '%s', expected '%s'", sc.Target.NamespacedName().String(), string(sc.Cluster), key, aVal, eVal), cconst.ReasonConfigurationProblem))
					continue
				}
			}
		}
		// update secret
		if _, err := controllerutil.CreateOrUpdate(ctx, cl, tarSec, func() error {
			tarSec.Labels = maps.Clone(srcSec.Labels)
			if tarSec.Labels == nil {
				tarSec.Labels = make(map[string]string, 2)
			}
			maps.Copy(tarSec.Labels, expectedSecretLabels)
			tarSec.Annotations = maps.Clone(srcSec.Annotations)
			tarSec.Type = srcSec.Type
			tarSec.Data = srcSec.Data // shallow copy, but doesn't matter, as neither srcSec nor tarSec are used afterwards
			return nil
		}); err != nil {
			errs.Append(errutils.WithReason(fmt.Errorf("error creating/updating target secret '%s' on %s cluster: %w", sc.Target.NamespacedName().String(), string(sc.Cluster), err), clusterFailReason))
			continue
		}
	}

	log.Debug("Finished secret management")
	return errs
}

func NewSecretsToCopy() *SecretsToCopy {
	return &SecretsToCopy{
		toPlatformCluster: map[string]commonapi.ObjectReference{},
		toTargetCluster:   map[string]commonapi.ObjectReference{},
	}
}

// SecretsToCopy is basically a list of secret copying instructions,
// but it is internally represented as maps for a more efficient detection of conflicts.
type SecretsToCopy struct {
	toPlatformCluster map[string]commonapi.ObjectReference // target name => source secret
	toTargetCluster   map[string]commonapi.ObjectReference // target name => source secret
}

// CopyToPlatformCluster adds secrets which should be copied to the Cluster's namespace on the platform cluster.
// It is an alias for CopySecrets(ToPlatformCluster, ....).
func (stc *SecretsToCopy) CopyToPlatformCluster(sourceNamespace string, sourceAndTargetNames ...pairs.Pair[string, string]) *SecretsToCopy {
	return stc.CopySecrets(ToPlatformCluster, sourceNamespace, sourceAndTargetNames...)
}

// CopyToTargetCluster adds secrets which should be copied to the designated namespace on the target cluster.
// It is an alias for CopySecrets(ToTargetCluster, ....).
func (stc *SecretsToCopy) CopyToTargetCluster(sourceNamespace string, sourceAndTargetNames ...pairs.Pair[string, string]) *SecretsToCopy {
	return stc.CopySecrets(ToTargetCluster, sourceNamespace, sourceAndTargetNames...)
}

// CopySecrets adds secrets which should be copied.
// Multiple secrets can be specified, but they all need to go to the same target (either platform cluster or target cluster).
// Source and target names are specified as pairs with the source name being the pair's key and the target name being its value.
// In case of a conflict (identical target secret), newly appended instructions overwrite previous ones.
// Modifies the struct in-place, but returns it for chaining.
func (stc *SecretsToCopy) CopySecrets(target SecretCopyTarget, sourceNamespace string, sourceAndTargetNames ...pairs.Pair[string, string]) *SecretsToCopy {
	var m map[string]commonapi.ObjectReference
	switch target {
	case ToPlatformCluster:
		m = stc.toPlatformCluster
	case ToTargetCluster:
		m = stc.toTargetCluster
	default:
		panic(fmt.Errorf("unknown secret copy target '%s'", target))
	}
	for _, sat := range sourceAndTargetNames {
		m[sat.Value] = commonapi.ObjectReference{
			Name:      sat.Key,
			Namespace: sourceNamespace,
		}
	}
	return stc
}

type SecretCopyTarget string

const (
	ToPlatformCluster SecretCopyTarget = "platform"
	ToTargetCluster   SecretCopyTarget = "target"
)

// List returns the secret copy instructions as a list.
// All information necessary for copying is contained in the list (except for the cluster clients).
func (stc *SecretsToCopy) List(platformClusterNamespace, targetClusterNamespace string) []SecretCopy {
	if stc == nil {
		return nil
	}
	res := []SecretCopy{}

	for tName, src := range stc.toPlatformCluster {
		res = append(res, SecretCopy{
			Source: src,
			Target: commonapi.ObjectReference{
				Name:      tName,
				Namespace: platformClusterNamespace,
			},
			Cluster: ToPlatformCluster,
		})
	}
	for tName, src := range stc.toTargetCluster {
		res = append(res, SecretCopy{
			Source: src,
			Target: commonapi.ObjectReference{
				Name:      tName,
				Namespace: targetClusterNamespace,
			},
			Cluster: ToTargetCluster,
		})
	}

	return res
}

type SecretCopy struct {
	// Source is the source secret.
	// Always refers to the platform cluster.
	Source commonapi.ObjectReference `json:"source"`
	// Target is the target secret.
	// Cluster specifies which cluster it refers to.
	Target commonapi.ObjectReference `json:"target"`
	// Cluster specifies which Cluster the secret should be copied to.
	Cluster SecretCopyTarget `json:"cluster"`
}
