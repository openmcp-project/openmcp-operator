package provider

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	testutils "github.com/openmcp-project/controller-utils/pkg/testing"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	apiinstall "github.com/openmcp-project/openmcp-operator/api/install"
	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/provider/install"
)

var _ = Describe("Deployment Controller", func() {

	Context("Reconcile", func() {

		var (
			scheme          = apiinstall.InstallOperatorAPIsPlatform(runtime.NewScheme())
			environment     = "test-environment"
			systemNamespace = "openmcp-system"
		)

		buildTestEnvironment := func(testdataDir string, gvk schema.GroupVersionKind) *testutils.Environment {
			return testutils.NewEnvironmentBuilder().
				WithFakeClient(scheme).
				WithInitObjectPath("testdata", testdataDir).
				WithReconcilerConstructor(func(c client.Client) reconcile.Reconciler {
					return NewProviderReconciler(gvk, c, environment, systemNamespace)
				}).
				Build()
		}

		getProvider := func(env *testutils.Environment, provider *unstructured.Unstructured) (*v1alpha1.DeploymentSpec, *v1alpha1.DeploymentStatus) {
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(provider), provider)).To(Succeed())
			deploymentSpec, err := deploymentSpecFromUnstructured(provider)
			Expect(err).NotTo(HaveOccurred(), "DeploymentSpec should be retrievable from provider")
			deploymentStatus, err := deploymentStatusFromUnstructured(provider)
			Expect(err).NotTo(HaveOccurred(), "DeploymentStatus should be retrievable from provider")
			return deploymentSpec, deploymentStatus
		}

		reconcileProvider := func(env *testutils.Environment, req reconcile.Request, gvk schema.GroupVersionKind) *v1alpha1.DeploymentSpec {
			provider := &unstructured.Unstructured{}
			provider.SetGroupVersionKind(gvk)
			provider.SetName(req.Name)

			env.ShouldReconcile(req, "Reconcile should not return an error")
			deploymentSpec, deploymentStatus := getProvider(env, provider)
			Expect(deploymentStatus).NotTo(BeNil(), "Status should not be nil")
			Expect(controllerutil.ContainsFinalizer(provider, openmcpFinalizer)).To(BeTrue(), "Finalizer should be present")
			Expect(deploymentStatus.ObservedGeneration).To(Equal(provider.GetGeneration()), "ObservedGeneration should be set")
			Expect(deploymentStatus.Phase).To(Equal(phaseProgressing), "Phase should be progressing")
			Expect(isInitialized(deploymentStatus)).To(BeFalse(), "Provider should not yet be initialized")
			Expect(isProviderInstalledAndReady(deploymentStatus)).To(BeFalse(), "Provider should not yet be ready")
			values := install.NewValues(provider, deploymentSpec, environment, systemNamespace)
			job := install.NewJobMutator(values, deploymentSpec, nil).Empty()
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(job), job)).To(Succeed())
			Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("test-image:v0.1.0"), "Job container image should match the provider spec")
			if len(deploymentSpec.InitCommand) > 0 {
				Expect(job.Spec.Template.Spec.Containers[0].Args).To(ContainElements(deploymentSpec.InitCommand), "Job container args should contain the overwritten init command")
			} else {
				Expect(job.Spec.Template.Spec.Containers[0].Args).To(ContainElement("init"), "Job container args should contain the init command")
			}
			Expect(job.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--environment=test-environment"), "Job container args should contain the environment")
			Expect(job.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--verbosity=DEBUG"), "Job container args should contain the verbosity")
			Expect(job.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--provider-name="+req.Name), "Job container args should contain the provider name")
			Expect(job.Spec.Template.Spec.Containers[0].Env).To(HaveLen(5), "Job container should have an environment variables")
			Expect(job.Spec.Template.Spec.Containers[0].Env[0].Name).To(Equal("NAME"), "Job container environment variable name should match the provider spec")
			Expect(job.Spec.Template.Spec.Containers[0].Env[0].Value).To(Equal("test-name"), "Job container environment variable value should match the provider spec")

			// 2nd reconcile (after init job is completed)
			job.Status.Succeeded = 1
			Expect(env.Client().Status().Update(env.Ctx, job)).To(Succeed(), "Status update of init job should succeed")
			env.ShouldReconcile(req, "2nd reconcile should not return an error")
			_, deploymentStatus = getProvider(env, provider)
			Expect(deploymentStatus.Phase).To(Equal(phaseProgressing), "Phase should be progressing")
			Expect(isInitialized(deploymentStatus)).To(BeTrue(), "Provider should be initialized")
			Expect(isProviderInstalledAndReady(deploymentStatus)).To(BeFalse(), "Provider should not yet be ready")
			deploy := install.NewDeploymentMutator(values).Empty()
			Expect(env.Client().Get(env.Ctx, client.ObjectKeyFromObject(deploy), deploy)).To(Succeed())
			Expect(deploy.Spec.Template.Spec.Containers[0].Image).To(Equal("test-image:v0.1.0"), "Deployment container image should match the provider spec")
			if len(deploymentSpec.RunCommand) > 0 {
				Expect(deploy.Spec.Template.Spec.Containers[0].Args).To(ContainElements(deploymentSpec.RunCommand), "Deployment container args should contain the overwritten run command")
			} else {
				Expect(deploy.Spec.Template.Spec.Containers[0].Args).To(ContainElement("run"), "Deployment container args should contain the run command")
			}
			Expect(deploy.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--environment=test-environment"), "Deployment container args should contain the environment")
			Expect(deploy.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--verbosity=DEBUG"), "Deployment container args should contain the verbosity")
			Expect(deploy.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--provider-name="+req.Name), "Deployment container args should contain the provider name")
			Expect(deploy.Spec.Template.Spec.Containers[0].Env).To(HaveLen(5), "Deployment container should have an environment variables")
			Expect(deploy.Spec.Template.Spec.Containers[0].Env[0].Name).To(Equal("NAME"), "Deployment container environment variable name should match the provider spec")
			Expect(deploy.Spec.Template.Spec.Containers[0].Env[0].Value).To(Equal("test-name"), "Deployment container environment variable value should match the provider spec")
			Expect(deploy.Spec.Replicas).To(Equal(&deploymentSpec.RunReplicas), "Deployment replicas should match the provider spec")

			if deploymentSpec.RunReplicas > 1 {
				Expect(deploy.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--leader-elect=true"), "Deployment container args should contain the leader-elect flag")
			}

			if len(deploymentSpec.TopologySpreadConstraints) > 0 {
				Expect(deploy.Spec.Template.Spec.TopologySpreadConstraints).To(Equal(deploymentSpec.TopologySpreadConstraints), "Deployment topology spread constraints should match the provider spec")

				for _, tsc := range deploymentSpec.TopologySpreadConstraints {
					for labelKey, labelValue := range tsc.LabelSelector.MatchLabels {
						Expect(deploy.Spec.Template.Labels).To(HaveKeyWithValue(labelKey, labelValue), "Deployment pod template labels should contain the topology spread constraint labels")
					}
				}
			}

			// 3rd reconcile (after deployment is ready)
			deploy.Status.Replicas = *deploy.Spec.Replicas
			deploy.Status.UpdatedReplicas = *deploy.Spec.Replicas
			deploy.Status.AvailableReplicas = *deploy.Spec.Replicas
			Expect(env.Client().Status().Update(env.Ctx, deploy)).To(Succeed(), "Status update of deployment should succeed")
			env.ShouldReconcile(req, "3rd reconcile should not return an error")
			_, deploymentStatus = getProvider(env, provider)
			Expect(deploymentStatus.Phase).To(Equal(phaseReady), "Phase should be ready")
			Expect(isInitialized(deploymentStatus)).To(BeTrue(), "Provider should be initialized")
			Expect(isProviderInstalledAndReady(deploymentStatus)).To(BeTrue(), "Provider should be ready")

			// delete the provider
			Expect(env.Client().Delete(env.Ctx, provider)).To(Succeed(), "Provider deletion should succeed")
			env.ShouldReconcile(req, "Reconcile after provider deletion should not return an error")
			return deploymentSpec
		}

		It("should reconcile a cluster provider", func() {
			env := buildTestEnvironment("test-01", v1alpha1.ClusterProviderGKV())
			req := testutils.RequestFromStrings("cluster-provider-test-01")
			reconcileProvider(env, req, v1alpha1.ClusterProviderGKV())
		})

		It("should reconcile a service provider", func() {
			env := buildTestEnvironment("test-02", v1alpha1.ServiceProviderGKV())
			req := testutils.RequestFromStrings("service-provider-test-02")
			reconcileProvider(env, req, v1alpha1.ServiceProviderGKV())
		})

		It("should reconcile a platform service", func() {
			env := buildTestEnvironment("test-03", v1alpha1.PlatformServiceGKV())
			req := testutils.RequestFromStrings("platform-service-test-03")
			reconcileProvider(env, req, v1alpha1.PlatformServiceGKV())
		})

		It("should reconcile a platform service with overwritten init and run commands", func() {
			env := buildTestEnvironment("test-04", v1alpha1.PlatformServiceGKV())
			req := testutils.RequestFromStrings("platform-service-test-04")
			deploymentSpec := reconcileProvider(env, req, v1alpha1.PlatformServiceGKV())
			Expect(deploymentSpec.InitCommand).To(HaveLen(2))
			Expect(deploymentSpec.RunCommand).To(HaveLen(2))
		})

		It("should reconcile a service providers with multiple run replicas and topology spread constraints", func() {
			env := buildTestEnvironment("test-05", v1alpha1.ServiceProviderGKV())
			req := testutils.RequestFromStrings("service-provider-test-05")
			deploymentSpec := reconcileProvider(env, req, v1alpha1.ServiceProviderGKV())
			Expect(deploymentSpec.RunReplicas).To(Equal(int32(3)))
			Expect(deploymentSpec.TopologySpreadConstraints).To(HaveLen(2))
		})

	})

	Context("Converter", func() {

		It("should convert a deploymentSpec into an unstructured and back", func() {
			spec := &v1alpha1.DeploymentSpec{
				Image: "test-image:v0.1.0",
				ImagePullSecrets: []commonapi.LocalObjectReference{
					{Name: "test-secret-1"},
					{Name: "test-secret-2"},
				},
			}

			provider := &unstructured.Unstructured{}
			provider.Object = map[string]interface{}{}
			Expect(deploymentSpecIntoUnstructured(spec, provider)).To(Succeed())

			spec2, err := deploymentSpecFromUnstructured(provider)
			Expect(err).NotTo(HaveOccurred())
			Expect(spec2).To(Equal(spec2))

			provider2 := &unstructured.Unstructured{}
			provider2.Object = map[string]interface{}{}
			Expect(deploymentSpecIntoUnstructured(spec2, provider2)).To(Succeed())
			Expect(provider2.Object).To(Equal(provider.Object))
		})

		It("should convert a deploymentStatus into an unstructured and back", func() {
			status := &v1alpha1.DeploymentStatus{
				Conditions:         nil,
				ObservedGeneration: 6,
				Phase:              phaseProgressing,
			}

			provider := &unstructured.Unstructured{}
			provider.Object = map[string]interface{}{}
			Expect(deploymentStatusIntoUnstructured(status, provider)).To(Succeed())

			status2, err := deploymentStatusFromUnstructured(provider)
			Expect(err).NotTo(HaveOccurred())
			Expect(status2).To(Equal(status))

			provider2 := &unstructured.Unstructured{}
			provider2.Object = map[string]interface{}{}
			Expect(deploymentStatusIntoUnstructured(status2, provider2)).To(Succeed())
			Expect(provider2.Object).To(Equal(provider.Object))
		})
	})
})
