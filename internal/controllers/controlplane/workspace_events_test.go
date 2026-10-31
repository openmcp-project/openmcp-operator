package controlplane_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/recorder"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	corev2alpha1 "github.com/openmcp-project/openmcp-operator/api/core/v2alpha1"
	"github.com/openmcp-project/openmcp-operator/api/install"
	"github.com/openmcp-project/openmcp-operator/internal/config"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/controlplane"
)

type eventTestCluster struct {
	cluster.Cluster
	client   client.Client
	recorder *events.FakeRecorder
}

func (c eventTestCluster) GetClient() client.Client                       { return c.client }
func (c eventTestCluster) GetScheme() *runtime.Scheme                     { return c.client.Scheme() }
func (c eventTestCluster) GetEventRecorder(string) recorder.EventRecorder { return c.recorder }

func TestControlPlaneEventsUseWorkspaceRecorder(t *testing.T) {
	scheme := install.InstallOperatorAPIsPlatform(runtime.NewScheme())
	install.InstallOperatorAPIsOnboarding(scheme)
	log, err := logging.New(&logging.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := logging.NewContext(context.Background(), log)
	newCluster := func() eventTestCluster {
		cp := &corev2alpha1.ControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "sample-control-plane", Namespace: "tenant"}}
		return eventTestCluster{client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(cp).WithObjects(cp).Build(), recorder: events.NewFakeRecorder(20)}
	}
	physical := newCluster()
	platform := fake.NewClientBuilder().WithScheme(scheme).Build()
	cfg := &config.ManagedControlPlaneConfig{}
	if err := cfg.Default(nil); err != nil {
		t.Fatal(err)
	}
	base, err := controlplane.NewManagedControlPlaneReconciler(clusters.NewTestClusterFromClient("platform", platform), clusters.NewTestClusterFromClient("physical", physical.client), physical.recorder, func(context.Context) (*config.ManagedControlPlaneConfig, error) { return cfg, nil }, "")
	if err != nil {
		t.Fatal(err)
	}
	tenants := []eventTestCluster{newCluster(), newCluster()}
	req := reconcile.Request{NamespacedName: client.ObjectKey{Namespace: "tenant", Name: "sample-control-plane"}}
	for i, tenant := range tenants {
		rc := base.ForCluster(tenant)
		if _, err := rc.Reconcile(ctx, req); err != nil {
			t.Fatal(err)
		}
		select {
		case <-tenant.recorder.Events:
		default:
			t.Fatalf("workspace %d received no event", i)
		}
		for len(tenant.recorder.Events) > 0 {
			<-tenant.recorder.Events
		}
		select {
		case <-physical.recorder.Events:
			t.Fatal("workspace event reached physical cluster")
		default:
		}
		other := tenants[1-i]
		select {
		case <-other.recorder.Events:
			t.Fatal("event crossed workspace boundary")
		default:
		}
	}
	if _, err := base.Reconcile(ctx, req); err != nil {
		t.Fatal(err)
	}
	select {
	case <-physical.recorder.Events:
	default:
		t.Fatal("classic reconciler lost its recorder")
	}
}
