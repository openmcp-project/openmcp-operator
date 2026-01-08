//nolint:revive
package advanced

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"
)

///////////////////////////
/// AUXILIARY FUNCTIONS ///
///////////////////////////

// StableRequestName generates a stable name for a Cluster- or AccessRequest related to an MCP.
// This basically results in '<lowercase_controller_name>--<request_name>--<lowercase_suffix>'.
// If the resulting string exceeds the Kubernetes name length limit of 63 characters, it will be truncated with the last characters (excluding the suffix) replaced by a hash of what was removed.
// If the suffix is empty, it will be omitted (and the preceding hyphen as well).
func StableRequestName(controllerName string, request reconcile.Request, suffix string) string {
	return StableRequestNameFromLocalName(controllerName, request.Name, suffix)
}

// StableRequestNameFromLocalName works like StableRequestName but takes a local name directly instead of a reconcile.Request.
// localName is converted to lowercase before processing.
func StableRequestNameFromLocalName(controllerName, localName, suffix string) string {
	controllerName = strings.ToLower(controllerName)
	localName = strings.ToLower(localName)
	raw := fmt.Sprintf("%s--%s", controllerName, localName)
	if suffix != "" {
		suffix = strings.ToLower(suffix)
		return fmt.Sprintf("%s--%s", ctrlutils.ShortenToXCharactersUnsafe(raw, ctrlutils.K8sMaxNameLength-len(suffix)-2), suffix)
	}
	return ctrlutils.ShortenToXCharactersUnsafe(raw, ctrlutils.K8sMaxNameLength)
}

// AccessFromAccessRequest provides access to a k8s cluster based on the given AccessRequest.
func AccessFromAccessRequest(ctx context.Context, platformClusterClient client.Client, id string, scheme *runtime.Scheme, ar *clustersv1alpha1.AccessRequest) (*clusters.Cluster, error) {
	return accessFromAccessRequest(ctx, platformClusterClient, id, scheme, ar, nil)
}

func accessFromAccessRequest(ctx context.Context, platformClusterClient client.Client, id string, scheme *runtime.Scheme, ar *clustersv1alpha1.AccessRequest, generateFakeClient FakeClientGenerator, additionalData ...any) (*clusters.Cluster, error) {
	if ar.Status.SecretRef == nil {
		return nil, fmt.Errorf("AccessRequest '%s/%s' has no secret reference in status", ar.Namespace, ar.Name)
	}

	s := &corev1.Secret{}
	s.Name = ar.Status.SecretRef.Name
	s.Namespace = ar.Namespace

	if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(s), s); err != nil {
		return nil, fmt.Errorf("unable to get secret '%s/%s' for AccessRequest '%s/%s': %w", s.Namespace, s.Name, ar.Namespace, ar.Name, err)
	}

	kubeconfigBytes, ok := s.Data[clustersv1alpha1.SecretKeyKubeconfig]
	if !ok {
		return nil, fmt.Errorf("kubeconfig key '%s' not found in AccessRequest secret '%s/%s'", clustersv1alpha1.SecretKeyKubeconfig, s.Namespace, s.Name)
	}

	var c *clusters.Cluster
	if generateFakeClient != nil {
		fc, err := generateFakeClient(ctx, kubeconfigBytes, scheme, additionalData...)
		if err != nil {
			return nil, fmt.Errorf("error creating fake client: %w", err)
		}
		c = clusters.NewTestClusterFromClient(id, fc)
	} else {
		config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to create rest config from kubeconfig bytes: %w", err)
		}

		c = clusters.New(id).WithRESTConfig(config)

		if err = c.InitializeClient(scheme); err != nil {
			return nil, fmt.Errorf("failed to initialize client: %w", err)
		}
	}

	return c, nil
}

// StaticNamespaceGenerator returns a namespace generator that always returns the same namespace.
func StaticNamespaceGenerator(namespace string) func(reconcile.Request, ...any) (string, error) {
	return func(_ reconcile.Request, _ ...any) (string, error) {
		return namespace, nil
	}
}

// RequestNamespaceGenerator is a namespace generator that returns the namespace of the request.
func RequestNamespaceGenerator(req reconcile.Request, _ ...any) (string, error) {
	return req.Namespace, nil
}

// DefaultNamespaceGenerator is a default implementation of a namespace generator.
// It computes a UUID-style hash from the given request.
func DefaultNamespaceGenerator(req reconcile.Request, _ ...any) (string, error) {
	return ctrlutils.K8sNameUUID(req.Namespace, req.Name)
}

// DefaultNamespaceGeneratorForMCP is a default implementation of a namespace generator for MCPs.
// It computes a UUID-style hash from the given request and prefixes it with "mcp--".
func DefaultNamespaceGeneratorForMCP(req reconcile.Request, _ ...any) (string, error) {
	return libutils.StableMCPNamespace(req.Name, req.Namespace)
}

// StaticClusterRequestSpecGenerator is a helper function that returns a ClusterRequestSpec generator which just returns deep copies of the given spec.
func StaticClusterRequestSpecGenerator(spec *clustersv1alpha1.ClusterRequestSpec) func(reconcile.Request, ...any) (*clustersv1alpha1.ClusterRequestSpec, error) {
	return func(_ reconcile.Request, _ ...any) (*clustersv1alpha1.ClusterRequestSpec, error) {
		return spec.DeepCopy(), nil
	}
}

// StaticReferenceGenerator is a helper function that returns an ObjectReference generator which just returns a deep copy of the given reference.
func StaticReferenceGenerator(ref *commonapi.ObjectReference) func(reconcile.Request, ...any) (*commonapi.ObjectReference, error) {
	return func(_ reconcile.Request, _ ...any) (*commonapi.ObjectReference, error) {
		return ref.DeepCopy(), nil
	}
}

// IdentityReferenceGenerator is an ObjectReference generator that returns a reference that is identical to the request (name and namespace).
func IdentityReferenceGenerator(req reconcile.Request, _ ...any) (*commonapi.ObjectReference, error) {
	return &commonapi.ObjectReference{
		Name:      req.Name,
		Namespace: req.Namespace,
	}, nil
}

////////////////////////////////
/// FAKE AUXILIARY FUNCTIONS ///
////////////////////////////////

// FakeClusterRequestReadiness returns a faking callback that sets the ClusterRequest to 'Granted'.
// Adds a 'clusterprovider' finalizer to the ClusterRequest, if not already present.
// If the given ClusterSpec is not nil, it creates a corresponding Cluster next to the ClusterRequest, if it doesn't exist yet.
// If during the callback, the Cluster is non-nil, with a non-empty name and namespace, but doesn't exist yet, it will be created with the data from the Cluster, ignoring the given ClusterSpec.
// Otherwise, only the ClusterRequest's status is modified.
// The callback is a no-op if the ClusterRequest is already granted (Cluster reference and existence are not checked in this case).
// It returns an error if the ClusterRequest is nil.
//
// The returned callback is meant to be used with the key stored in FakingCallback_WaitingForClusterRequestReadiness.
func FakeClusterRequestReadiness(clusterSpec *clustersv1alpha1.ClusterSpec) FakingCallback {
	return func(ctx context.Context, platformClusterClient client.Client, key string, _ *reconcile.Request, cr *clustersv1alpha1.ClusterRequest, _ *clustersv1alpha1.AccessRequest, c *clustersv1alpha1.Cluster, _ *clusters.Cluster) error {
		if cr == nil {
			return fmt.Errorf("ClusterRequest is nil")
		}
		if cr.Status.IsGranted() {
			// already granted, nothing to do
			return nil
		}

		// add finalizer, if not present
		old := cr.DeepCopy()
		if controllerutil.AddFinalizer(cr, "clusterprovider") {
			if err := platformClusterClient.Patch(ctx, cr, client.MergeFrom(old)); err != nil {
				return fmt.Errorf("unable to patch ClusterRequest '%s/%s': %w", cr.Namespace, cr.Name, err)
			}
		}

		// create cluster, if desired
		if c != nil && c.Name != "" && c.Namespace != "" {
			// check if cluster exists
			if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(c), c); err != nil {
				if !apierrors.IsNotFound(err) {
					return fmt.Errorf("unable to get Cluster '%s/%s': %w", c.Namespace, c.Name, err)
				}
				// create cluster
				if err := platformClusterClient.Create(ctx, c); err != nil {
					return fmt.Errorf("unable to create Cluster '%s/%s': %w", c.Namespace, c.Name, err)
				}
			}
		} else {
			// cluster not known, create fake one, if spec is given
			c = &clustersv1alpha1.Cluster{}
			c.Name = cr.Name
			c.Namespace = cr.Namespace
			if clusterSpec != nil {
				c.Spec = *clusterSpec.DeepCopy()
				if err := platformClusterClient.Create(ctx, c); err != nil {
					return fmt.Errorf("unable to create Cluster '%s/%s': %w", c.Namespace, c.Name, err)
				}
			}
		}

		// mock ClusterRequest status
		old = cr.DeepCopy()
		cr.Status.Cluster = &commonapi.ObjectReference{
			Name:      c.Name,
			Namespace: c.Namespace,
		}
		cr.Status.Phase = clustersv1alpha1.REQUEST_GRANTED
		cr.Status.ObservedGeneration = cr.Generation
		if err := platformClusterClient.Status().Patch(ctx, cr, client.MergeFrom(old)); err != nil {
			return fmt.Errorf("unable to update status of ClusterRequest '%s/%s': %w", cr.Namespace, cr.Name, err)
		}
		return nil
	}
}

// FakeAccessRequestReadiness returns a faking callback that sets the AccessRequest to 'Granted'.
// The content of the secret's 'kubeconfig' key will one of the following:
// - 'fake:cluster:<cluster-namespace>/<cluster-name>' if the Cluster could be determined (should be the case most of the time)
// - 'fake:request:<request-namespace>/<request-name>' if no Cluster could be determined
// This function also adds a 'clusterprovider' finalizer to the AccessRequest, if not already present.
// The callback is a no-op if the AccessRequest is already granted and its generation has not changed (Secret reference and existence are not checked in this case).
// It returns an error if the AccessRequest is nil.
//
// The returned callback is meant to be used with the key stored in FakingCallback_WaitingForAccessRequestReadiness.
func FakeAccessRequestReadiness() FakingCallback {
	return func(ctx context.Context, platformClusterClient client.Client, key string, req *reconcile.Request, cr *clustersv1alpha1.ClusterRequest, ar *clustersv1alpha1.AccessRequest, c *clustersv1alpha1.Cluster, access *clusters.Cluster) error {
		if ar == nil {
			return fmt.Errorf("AccessRequest is nil")
		}
		if ar.Status.IsGranted() && ar.Status.ObservedGeneration == ar.Generation {
			// already granted, nothing to do
			return nil
		}

		old := ar.DeepCopy()
		applied := false
		// add finalizer, if not present
		changed := controllerutil.AddFinalizer(ar, "clusterprovider")

		// if a ClusterRequest is referenced, but no Cluster, try to identify the Cluster
		if ar.Spec.ClusterRef == nil {
			if c != nil {
				ar.Spec.ClusterRef = &commonapi.ObjectReference{
					Name:      c.Name,
					Namespace: c.Namespace,
				}
				if err := platformClusterClient.Patch(ctx, ar, client.MergeFrom(old)); err != nil {
					return fmt.Errorf("unable to patch AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
				}
				applied = true
			} else if cr != nil && cr.Status.Cluster != nil {
				ar.Spec.ClusterRef = cr.Status.Cluster.DeepCopy()
				if err := platformClusterClient.Patch(ctx, ar, client.MergeFrom(old)); err != nil {
					return fmt.Errorf("unable to patch AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
				}
				applied = true
			} else if ar.Spec.RequestRef != nil {
				cr2 := &clustersv1alpha1.ClusterRequest{}
				cr2.Name = ar.Spec.RequestRef.Name
				cr2.Namespace = ar.Spec.RequestRef.Namespace
				if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(cr2), cr2); err == nil {
					if cr2.Status.Cluster != nil {
						old := ar.DeepCopy()
						ar.Spec.ClusterRef = cr2.Status.Cluster.DeepCopy()
						if err := platformClusterClient.Patch(ctx, ar, client.MergeFrom(old)); err != nil {
							return fmt.Errorf("unable to patch AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
						}
						applied = true
					}
				}
			}
		}

		if changed && !applied {
			if err := platformClusterClient.Patch(ctx, ar, client.MergeFrom(old)); err != nil {
				return fmt.Errorf("unable to patch AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
			}
		}

		// create secret
		s := &corev1.Secret{}
		s.Name = ar.Name
		s.Namespace = ar.Namespace
		var kcfgBytes []byte
		if ar.Spec.ClusterRef != nil {
			kcfgBytes = fmt.Appendf(nil, "fake:cluster:%s/%s", ar.Spec.ClusterRef.Namespace, ar.Spec.ClusterRef.Name)
		} else {
			kcfgBytes = fmt.Appendf(nil, "fake:request:%s/%s", req.Namespace, req.Name)
		}
		s.Data = map[string][]byte{
			clustersv1alpha1.SecretKeyKubeconfig: kcfgBytes,
		}
		if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(s), s); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("unable to get secret '%s/%s': %w", s.Namespace, s.Name, err)
			}
			// create secret
			if err := platformClusterClient.Create(ctx, s); err != nil {
				return fmt.Errorf("unable to create secret '%s/%s': %w", s.Namespace, s.Name, err)
			}
		}

		// mock AccessRequest status
		old = ar.DeepCopy()
		ar.Status.SecretRef = &commonapi.LocalObjectReference{
			Name: s.Name,
		}
		ar.Status.Phase = clustersv1alpha1.REQUEST_GRANTED
		ar.Status.ObservedGeneration = ar.Generation
		if err := platformClusterClient.Status().Patch(ctx, ar, client.MergeFrom(old)); err != nil {
			return fmt.Errorf("unable to update status of AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
		}
		return nil
	}
}

// FakeClusterRequestDeletion returns a faking callback that removes finalizers from the given ClusterRequest and potentially deletes the referenced Cluster.
// The callback is a no-op if the ClusterRequest is already nil (not found).
// If deleteCluster is true, the Cluster referenced by the ClusterRequest will be deleted, if it exists.
// All finalizers from the finalizersToRemove* slices will be removed from the ClusterRequest and/or Cluster before deletion.
// The ClusterRequest itself is not deleted by this callback, only finalizers are removed.
//
// The returned callback is meant to be used with the key stored in FakingCallback_WaitingForClusterRequestDeletion.
//
//nolint:dupl
func FakeClusterRequestDeletion(deleteCluster bool, finalizersToRemoveFromClusterRequest, finalizersToRemoveFromCluster []string) FakingCallback {
	return func(ctx context.Context, platformClusterClient client.Client, key string, _ *reconcile.Request, cr *clustersv1alpha1.ClusterRequest, _ *clustersv1alpha1.AccessRequest, _ *clustersv1alpha1.Cluster, _ *clusters.Cluster) error {
		if cr == nil {
			// already deleted, nothing to do
			return nil
		}

		// delete cluster, if desired
		if deleteCluster && cr.Status.Cluster != nil {
			c := &clustersv1alpha1.Cluster{}
			c.Name = cr.Status.Cluster.Name
			c.Namespace = cr.Status.Cluster.Namespace

			if len(finalizersToRemoveFromCluster) > 0 {
				// fetch cluster to remove finalizers
				if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(c), c); err != nil {
					if !apierrors.IsNotFound(err) {
						return fmt.Errorf("unable to get Cluster '%s/%s': %w", c.Namespace, c.Name, err)
					}
					// cluster doesn't exist, nothing to do
					c = nil
				} else {
					cOld := c.DeepCopy()
					if removeFinalizers(c, finalizersToRemoveFromCluster...) {
						if err := platformClusterClient.Patch(ctx, c, client.MergeFrom(cOld)); err != nil {
							return fmt.Errorf("unable to remove finalizers from Cluster '%s/%s': %w", c.Namespace, c.Name, err)
						}
					}
				}
			}

			if c != nil {
				if err := platformClusterClient.Delete(ctx, c); client.IgnoreNotFound(err) != nil {
					return fmt.Errorf("unable to delete Cluster '%s/%s': %w", c.Namespace, c.Name, err)
				}
			}
		}

		// remove finalizers from ClusterRequest, if any
		if len(finalizersToRemoveFromClusterRequest) > 0 {
			crOld := cr.DeepCopy()
			if removeFinalizers(cr, finalizersToRemoveFromClusterRequest...) {
				if err := platformClusterClient.Patch(ctx, cr, client.MergeFrom(crOld)); err != nil {
					return fmt.Errorf("unable to remove finalizers from ClusterRequest '%s/%s': %w", cr.Namespace, cr.Name, err)
				}
			}
		}

		return nil
	}
}

// FakeAccessRequestDeletion returns a faking callback that removes finalizers from the given AccessRequest and deletes the referenced Secret, if it exists.
// The callback is a no-op if the AccessRequest is already nil (not found).
// It returns an error if the AccessRequest is non-nil but cannot be deleted.
// All finalizers from the finalizersToRemove* slices will be removed from the AccessRequest and/or Secret before deletion.
// The AccessRequest itself is not deleted by this callback, only finalizers are removed.
//
// The returned callback is meant to be used with the key stored in FakingCallback_WaitingForAccessRequestDeletion.
//
//nolint:dupl
func FakeAccessRequestDeletion(finalizersToRemoveFromAccessRequest, finalizersToRemoveFromSecret []string) FakingCallback {
	return func(ctx context.Context, platformClusterClient client.Client, key string, _ *reconcile.Request, _ *clustersv1alpha1.ClusterRequest, ar *clustersv1alpha1.AccessRequest, _ *clustersv1alpha1.Cluster, _ *clusters.Cluster) error {
		if ar == nil {
			// already deleted, nothing to do
			return nil
		}

		// delete secret
		if ar.Status.SecretRef != nil {
			s := &corev1.Secret{}
			s.Name = ar.Status.SecretRef.Name
			s.Namespace = ar.Namespace

			if len(finalizersToRemoveFromSecret) > 0 {
				// fetch secret to remove finalizers
				if err := platformClusterClient.Get(ctx, client.ObjectKeyFromObject(s), s); err != nil {
					if !apierrors.IsNotFound(err) {
						return fmt.Errorf("unable to get secret '%s/%s': %w", s.Namespace, s.Name, err)
					}
					// secret doesn't exist, nothing to do
					s = nil
				} else {
					sOld := s.DeepCopy()
					if removeFinalizers(s, finalizersToRemoveFromSecret...) {
						if err := platformClusterClient.Patch(ctx, s, client.MergeFrom(sOld)); err != nil {
							return fmt.Errorf("unable to remove finalizers from secret '%s/%s': %w", s.Namespace, s.Name, err)
						}
					}
				}
			}

			if s != nil {
				if err := platformClusterClient.Delete(ctx, s); client.IgnoreNotFound(err) != nil {
					return fmt.Errorf("unable to delete secret '%s/%s': %w", s.Namespace, s.Name, err)
				}
			}
		}

		// remove finalizers from AccessRequest, if any
		if len(finalizersToRemoveFromAccessRequest) > 0 {
			arOld := ar.DeepCopy()
			if removeFinalizers(ar, finalizersToRemoveFromAccessRequest...) {
				if err := platformClusterClient.Patch(ctx, ar, client.MergeFrom(arOld)); err != nil {
					return fmt.Errorf("unable to remove finalizers from AccessRequest '%s/%s': %w", ar.Namespace, ar.Name, err)
				}
			}
		}

		return nil
	}
}

// removeFinalizers takes an object and a list of finalizers to remove from that object.
// The special finalizer defined in 'Finalizer' will never be removed.
// If the list contains "*", all finalizers will be removed.
// It updates the object in-place and returns an indicator whether any finalizers were removed.
func removeFinalizers(obj client.Object, finalizersToRemove ...string) bool {
	if obj == nil || len(obj.GetFinalizers()) == 0 || len(finalizersToRemove) == 0 {
		return false
	}

	changed := false
	finsToRemove := sets.New(finalizersToRemove...)
	fins := obj.GetFinalizers()
	for i := 0; i < len(fins); i++ {
		// keep the special finalizer
		if fins[i] == Finalizer {
			continue
		}
		if finsToRemove.Has("*") || finsToRemove.Has(fins[i]) {
			fins = append(fins[:i], fins[i+1:]...)
			changed = true
			i--
		}
	}
	obj.SetFinalizers(fins)
	return changed
}
