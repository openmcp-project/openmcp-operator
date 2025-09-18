package clusteraccess

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/resources"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"
)

func NewTestClusterAccessManager(platformClusterClient client.Client, controllerName, controllerNamespace string, fakeClientMapping map[string]client.Client) Manager {
	return &testManagerImpl{
		managerImpl: managerImpl{
			platformClusterClient: platformClusterClient,
			controllerName:        controllerName,
			controllerNamespace:   controllerNamespace,
			timeout:               5 * time.Minute,
			interval:              10 * time.Second,
			log:                   nil, // Default to no logging
		},
		fakeClientMapping: fakeClientMapping,
	}
}

type testManagerImpl struct {
	managerImpl
	fakeClientMapping map[string]client.Client
}

var _ Manager = &testManagerImpl{}

// CreateAndWaitForCluster implements Manager.
func (m *testManagerImpl) CreateAndWaitForCluster(ctx context.Context, localName, purpose string, scheme *runtime.Scheme, permissions []clustersv1alpha1.PermissionsRequest) (*clusters.Cluster, error) {
	cr := &clustersv1alpha1.ClusterRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      StableRequestNameFromLocalName(m.controllerName, localName),
			Namespace: m.controllerNamespace,
		},
	}

	clusterRequestMutator := newClusterRequestMutator(cr.Name, cr.Namespace, purpose)
	clusterRequestMutator.WithMetadata(resources.NewMetadataMutator().WithLabels(map[string]string{
		openmcpconst.ManagedByLabel: m.controllerName,
	}))

	if err := resources.CreateOrUpdateResource(ctx, m.platformClusterClient, clusterRequestMutator); err != nil {
		return nil, fmt.Errorf("failed to create/update ClusterRequest: %w", err)
	}

	// TESTING MODIFICATIONS
	// fake ClusterRequest readiness
	old := cr.DeepCopy()
	cr.Status.Phase = clustersv1alpha1.REQUEST_GRANTED
	if err := m.platformClusterClient.Status().Patch(ctx, cr, client.MergeFrom(old)); err != nil {
		return nil, fmt.Errorf("failed to update ClusterRequest status: %w", err)
	}

	cl, _, err := m.WaitForClusterAccess(ctx, localName, scheme, &commonapi.ObjectReference{
		Name:      cr.Name,
		Namespace: cr.Namespace,
	}, ReferenceToClusterRequest, permissions)
	return cl, err
}

// WaitForClusterAccess implements Manager.
func (m *testManagerImpl) WaitForClusterAccess(ctx context.Context, localName string, scheme *runtime.Scheme, ref *commonapi.ObjectReference, refType ClusterReferenceType, permissions []clustersv1alpha1.PermissionsRequest) (*clusters.Cluster, *clustersv1alpha1.AccessRequest, error) {
	ar := &clustersv1alpha1.AccessRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      StableRequestNameFromLocalName(m.controllerName, localName),
			Namespace: m.controllerNamespace,
		},
	}

	accessRequestMutator := newAccessRequestMutator(ar.Name, ar.Namespace)
	switch refType {
	case ReferenceToCluster:
		accessRequestMutator.WithClusterRef(ref)
	case ReferenceToClusterRequest:
		accessRequestMutator.WithRequestRef(ref)
	default:
		return nil, nil, fmt.Errorf("invalid ClusterReferenceType: %s", refType)
	}
	accessRequestMutator.WithTokenPermissions(permissions)
	accessRequestMutator.WithMetadata(resources.NewMetadataMutator().WithLabels(map[string]string{
		openmcpconst.ManagedByLabel: m.controllerName,
	}))

	if err := resources.CreateOrUpdateResource(ctx, m.platformClusterClient, accessRequestMutator); err != nil {
		return nil, nil, fmt.Errorf("failed to create/update AccessRequest: %w", err)
	}

	// TESTING MODIFICATIONS
	// fake AccessRequest readiness and return cluster from fake client mapping
	fakeClient, ok := m.fakeClientMapping[localName]
	if !ok {
		return nil, nil, fmt.Errorf("no fake client found for local cluster name %q", localName) // simulate AccessRequest not being granted
	}
	old := ar.DeepCopy()
	ar.Status.Phase = clustersv1alpha1.REQUEST_GRANTED
	ar.Status.SecretRef = &commonapi.ObjectReference{
		Name:      ar.Name,
		Namespace: ar.Namespace,
	}
	if err := m.platformClusterClient.Status().Patch(ctx, ar, client.MergeFrom(old)); err != nil {
		return nil, nil, fmt.Errorf("failed to update AccessRequest status: %w", err)
	}
	sec := &corev1.Secret{}
	sec.Name = ar.Status.SecretRef.Name
	sec.Namespace = ar.Status.SecretRef.Namespace
	if _, err := controllerutil.CreateOrUpdate(ctx, m.platformClusterClient, sec, func() error {
		sec.Data = map[string][]byte{
			clustersv1alpha1.SecretKeyKubeconfig: []byte("fake:" + localName),
		}
		return nil
	}); err != nil {
		return nil, nil, fmt.Errorf("failed to create/update fake kubeconfig secret for AccessRequest: %w", err)
	}
	if fakeClient == nil {
		// create new default fake client if none was provided
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
	}
	return clusters.NewTestClusterFromClient(localName, fakeClient), ar, nil
}
