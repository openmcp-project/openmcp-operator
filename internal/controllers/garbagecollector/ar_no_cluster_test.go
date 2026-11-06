package garbagecollector_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

var scheme = install.InstallOperatorAPIsPlatform(runtime.NewScheme())

func runGC(c client.Client, cfg *config.GarbageCollectorConfig) *testPeriodic {
	p := &testPeriodic{}
	fn := garbagecollector.GenerateGarbageCollectAccessRequestsWithoutCluster(
		clusters.NewTestClusterFromClient("platform", c),
	)
	fn(context.Background(), p, cfg)
	return p
}

func enabledCfg() *config.GarbageCollectorConfig {
	return &config.GarbageCollectorConfig{
		Interval: time.Hour,
		AccessRequestNoCluster: config.AccessRequestNoClusterConfig{
			Enabled: true,
		},
	}
}

var _ = Describe("GarbageCollector: AccessRequestNoCluster", func() {

	It("should not delete anything when cfg is nil", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-01").Build()
		runGC(env.Client(), nil)
		ar := &clustersv1alpha1.AccessRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-with-cluster", "bar"), ar)).To(Succeed())
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-without-cluster", "bar"), ar)).To(Succeed())
	})

	It("should not delete anything when the module is disabled", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-01").Build()
		cfg := enabledCfg()
		cfg.AccessRequestNoCluster.Enabled = false
		runGC(env.Client(), cfg)
		ar := &clustersv1alpha1.AccessRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-with-cluster", "bar"), ar)).To(Succeed())
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-without-cluster", "bar"), ar)).To(Succeed())
	})

	It("should use the module-specific interval when set", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-01").Build()
		cfg := enabledCfg()
		cfg.AccessRequestNoCluster.Interval = 30 * time.Minute
		p := runGC(env.Client(), cfg)
		Expect(p.interval).To(Equal(30 * time.Minute))
	})

	It("should fall back to the global interval when the module interval is not set", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-01").Build()
		p := runGC(env.Client(), enabledCfg())
		Expect(p.interval).To(Equal(time.Hour))
	})

	Context("Cluster Reference", func() {

		It("should not delete an AccessRequest whose referenced Cluster still exists", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-01").Build()
			runGC(env.Client(), enabledCfg())
			ar := &clustersv1alpha1.AccessRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-with-cluster", "bar"), ar)).To(Succeed())
		})

		It("should delete an AccessRequest whose referenced Cluster is gone", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-01").Build()
			runGC(env.Client(), enabledCfg())
			ar := &clustersv1alpha1.AccessRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-without-cluster", "bar"), ar)).To(
				MatchError(apierrors.IsNotFound, "IsNotFound"),
			)
		})

		It("should delete an AccessRequest with finalizers whose referenced Cluster is gone", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-01").Build()
			ar := &clustersv1alpha1.AccessRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-with-finalizer", "bar"), ar)).To(Succeed())
			Expect(ar.Finalizers).ToNot(BeEmpty())
			runGC(env.Client(), enabledCfg())
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-with-finalizer", "bar"), ar)).To(
				MatchError(apierrors.IsNotFound, "IsNotFound"),
			)
		})

	})

	Context("ClusterRequest Reference", func() {

		It("should not delete an AccessRequest whose referenced ClusterRequest still exists", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-02").Build()
			runGC(env.Client(), enabledCfg())
			ar := &clustersv1alpha1.AccessRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-with-clusterrequest", "bar"), ar)).To(Succeed())
		})

		It("should delete an AccessRequest whose referenced ClusterRequest is gone", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-02").Build()
			runGC(env.Client(), enabledCfg())
			ar := &clustersv1alpha1.AccessRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-without-clusterrequest", "bar"), ar)).To(
				MatchError(apierrors.IsNotFound, "IsNotFound"),
			)
		})

	})

	It("should delete an AccessRequest with neither clusterRef nor requestRef", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-03").Build()
		runGC(env.Client(), enabledCfg())
		ar := &clustersv1alpha1.AccessRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-no-ref", "bar"), ar)).To(
			MatchError(apierrors.IsNotFound, "IsNotFound"),
		)
	})

	Context("Garbage Collector Annotation", func() {

		It("should not delete an AccessRequest with the ignore annotation", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-04").Build()
			runGC(env.Client(), enabledCfg())
			ar := &clustersv1alpha1.AccessRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-ignore-annotation", "bar"), ar)).To(Succeed())
		})

		It("should not delete an AccessRequest with a future GC timestamp annotation", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-04").Build()
			runGC(env.Client(), enabledCfg())
			ar := &clustersv1alpha1.AccessRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-future-timestamp", "bar"), ar)).To(Succeed())
		})

		It("should delete an AccessRequest with a past GC timestamp annotation", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-04").Build()
			runGC(env.Client(), enabledCfg())
			ar := &clustersv1alpha1.AccessRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-past-timestamp", "bar"), ar)).To(
				MatchError(apierrors.IsNotFound, "IsNotFound"),
			)
		})

		It("should not delete an AccessRequest with an invalid annotation value", func() {
			env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-04").Build()
			runGC(env.Client(), enabledCfg())
			ar := &clustersv1alpha1.AccessRequest{}
			Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-invalid-annotation", "bar"), ar)).To(Succeed())
		})

	})

	It("should not delete anything when there are no AccessRequests", func() {
		env := testutils.NewEnvironmentBuilder().WithFakeClient(scheme).WithInitObjectPath("testdata", "test-05").Build()
		runGC(env.Client(), enabledCfg())
		// test-05 has a Cluster but no gone ARs; the existing AR should survive
		ar := &clustersv1alpha1.AccessRequest{}
		Expect(env.Client().Get(env.Ctx, ctrlutils.ObjectKey("ar-with-cluster", "bar"), ar)).To(Succeed())
	})

})
