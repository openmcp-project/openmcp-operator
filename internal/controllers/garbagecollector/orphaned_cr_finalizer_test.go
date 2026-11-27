package garbagecollector_test

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	testutils "github.com/openmcp-project/controller-utils/pkg/testing"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/api/install"
	"github.com/openmcp-project/openmcp-operator/internal/config"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/garbagecollector"
)

var orphanedFinalizerScheme = install.InstallOperatorAPIsPlatform(runtime.NewScheme())

func runOrphanedFinalizerGC(c client.Client, cfg *config.GarbageCollectorConfig) *testPeriodic {
	p := &testPeriodic{}
	fn := garbagecollector.GenerateGarbageCollectOrphanedClusterRequestFinalizer(
		clusters.NewTestClusterFromClient("platform", c),
	)
	fn(context.Background(), p, cfg)
	return p
}

func enabledOrphanedFinalizerCfg() *config.GarbageCollectorConfig {
	return &config.GarbageCollectorConfig{
		Interval: metav1.Duration{Duration: time.Hour},
		OrphanedClusterRequestFinalizer: config.OrphanedClusterRequestFinalizer{
			Enabled: true,
		},
	}
}

var _ = Describe("GarbageCollector: OrphanedClusterRequestFinalizer", func() {

	It("should not remove any finalizers when cfg is nil", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(orphanedFinalizerScheme).WithUIDs().WithInitObjectPath("testdata", "test-06").Build()
		runOrphanedFinalizerGC(env.Client(), nil)
		cl := &clustersv1alpha1.Cluster{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("cluster-with-orphaned-cr-finalizer", "foo"), cl)).To(Succeed())
		Expect(cl.Finalizers).To(ContainElement("request.clusters.openmcp.cloud/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
	})

	It("should not remove any finalizers when the module is disabled", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(orphanedFinalizerScheme).WithUIDs().WithInitObjectPath("testdata", "test-06").Build()
		cfg := enabledOrphanedFinalizerCfg()
		cfg.OrphanedClusterRequestFinalizer.Enabled = false
		runOrphanedFinalizerGC(env.Client(), cfg)
		cl := &clustersv1alpha1.Cluster{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("cluster-with-orphaned-cr-finalizer", "foo"), cl)).To(Succeed())
		Expect(cl.Finalizers).To(ContainElement("request.clusters.openmcp.cloud/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
	})

	It("should use the module-specific interval when set", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(orphanedFinalizerScheme).WithUIDs().WithInitObjectPath("testdata", "test-06").Build()
		cfg := enabledOrphanedFinalizerCfg()
		cfg.OrphanedClusterRequestFinalizer.Interval = metav1.Duration{Duration: 30 * time.Minute}
		p := runOrphanedFinalizerGC(env.Client(), cfg)
		Expect(p.interval).To(Equal(30 * time.Minute))
	})

	It("should fall back to the global interval when the module interval is not set", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(orphanedFinalizerScheme).WithUIDs().WithInitObjectPath("testdata", "test-06").Build()
		p := runOrphanedFinalizerGC(env.Client(), enabledOrphanedFinalizerCfg())
		Expect(p.interval).To(Equal(time.Hour))
	})

	Context("ClusterRequest Finalizer", func() {

		It("should not remove a finalizer from a Cluster whose ClusterRequest still exists", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(orphanedFinalizerScheme).WithUIDs().WithInitObjectPath("testdata", "test-06").Build()
			runOrphanedFinalizerGC(env.Client(), enabledOrphanedFinalizerCfg())
			cl := &clustersv1alpha1.Cluster{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("cluster-with-existing-cr", "foo"), cl)).To(Succeed())
			Expect(cl.Finalizers).To(ContainElement("request.clusters.openmcp.cloud/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
		})

		It("should remove a finalizer from a Cluster whose ClusterRequest is gone", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(orphanedFinalizerScheme).WithUIDs().WithInitObjectPath("testdata", "test-06").Build()
			runOrphanedFinalizerGC(env.Client(), enabledOrphanedFinalizerCfg())
			cl := &clustersv1alpha1.Cluster{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("cluster-with-orphaned-cr-finalizer", "foo"), cl)).To(Succeed())
			Expect(cl.Finalizers).NotTo(ContainElement("request.clusters.openmcp.cloud/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
		})

		It("should remove only the orphaned finalizers when a Cluster has multiple ClusterRequest finalizers and only some ClusterRequests are gone", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(orphanedFinalizerScheme).WithUIDs().WithInitObjectPath("testdata", "test-06").Build()
			runOrphanedFinalizerGC(env.Client(), enabledOrphanedFinalizerCfg())
			cl := &clustersv1alpha1.Cluster{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("cluster-with-mixed-cr-finalizers", "foo"), cl)).To(Succeed())
			Expect(cl.Finalizers).To(ContainElement("request.clusters.openmcp.cloud/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
			Expect(cl.Finalizers).NotTo(ContainElement("request.clusters.openmcp.cloud/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
		})

		It("should only remove the orphaned request finalizer and keep other finalizers", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(orphanedFinalizerScheme).WithUIDs().WithInitObjectPath("testdata", "test-07").Build()
			runOrphanedFinalizerGC(env.Client(), enabledOrphanedFinalizerCfg())
			cl := &clustersv1alpha1.Cluster{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("cluster-mixed-finalizers", "foo"), cl)).To(Succeed())
			Expect(cl.Finalizers).NotTo(ContainElement("request.clusters.openmcp.cloud/cccccccc-cccc-cccc-cccc-cccccccccccc"))
			Expect(cl.Finalizers).To(ContainElement("some-other-finalizer"))
		})

		It("should not remove any finalizers from a Cluster with no request finalizers", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(orphanedFinalizerScheme).WithUIDs().WithInitObjectPath("testdata", "test-09").Build()
			runOrphanedFinalizerGC(env.Client(), enabledOrphanedFinalizerCfg())
			cl := &clustersv1alpha1.Cluster{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("cluster-no-request-finalizers", "foo"), cl)).To(Succeed())
			Expect(cl.Finalizers).To(ContainElement("some-other-finalizer"))
		})

	})

	Context("Garbage Collector Annotation", func() {

		It("should not remove finalizers from a Cluster with the ignore annotation", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(orphanedFinalizerScheme).WithUIDs().WithInitObjectPath("testdata", "test-08").Build()
			runOrphanedFinalizerGC(env.Client(), enabledOrphanedFinalizerCfg())
			cl := &clustersv1alpha1.Cluster{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("cluster-ignore-annotation", "foo"), cl)).To(Succeed())
			Expect(cl.Finalizers).To(ContainElement("request.clusters.openmcp.cloud/dddddddd-dddd-dddd-dddd-dddddddddddd"))
		})

		It("should not remove finalizers from a Cluster with a future GC timestamp annotation", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(orphanedFinalizerScheme).WithUIDs().WithInitObjectPath("testdata", "test-08").Build()
			runOrphanedFinalizerGC(env.Client(), enabledOrphanedFinalizerCfg())
			cl := &clustersv1alpha1.Cluster{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("cluster-future-timestamp", "foo"), cl)).To(Succeed())
			Expect(cl.Finalizers).To(ContainElement("request.clusters.openmcp.cloud/eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"))
		})

		It("should remove finalizers from a Cluster with a past GC timestamp annotation", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(orphanedFinalizerScheme).WithUIDs().WithInitObjectPath("testdata", "test-08").Build()
			runOrphanedFinalizerGC(env.Client(), enabledOrphanedFinalizerCfg())
			cl := &clustersv1alpha1.Cluster{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("cluster-past-timestamp", "foo"), cl)).To(Succeed())
			Expect(cl.Finalizers).NotTo(ContainElement("request.clusters.openmcp.cloud/ffffffff-ffff-ffff-ffff-ffffffffffff"))
		})

		It("should not remove finalizers from a Cluster with an invalid annotation value", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(orphanedFinalizerScheme).WithUIDs().WithInitObjectPath("testdata", "test-08").Build()
			runOrphanedFinalizerGC(env.Client(), enabledOrphanedFinalizerCfg())
			cl := &clustersv1alpha1.Cluster{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("cluster-invalid-annotation", "foo"), cl)).To(Succeed())
			Expect(cl.Finalizers).To(ContainElement("request.clusters.openmcp.cloud/11111111-1111-1111-1111-111111111111"))
		})

	})

})
