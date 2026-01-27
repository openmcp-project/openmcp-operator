package utils_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openmcp-project/openmcp-operator/lib/utils"
)

const (
	controllerName      = "test-controller"
	onboardingName      = "foo"
	onboardingNamespace = "foo"
)

var _ = Describe("Utils", func() {
	Context("StableRequestNamespace", func() {
		It("should return a stable namespace for requests", func() {
			expectedNamespace := "ob-foo"

			Expect(utils.StableRequestNamespace(onboardingNamespace)).To(Equal(expectedNamespace))
		})
	})

	Context("StableRequestNameMCP", func() {
		It("should return a stable name for MCP requests", func() {
			expectedName := fmt.Sprint("mcp-", "s3bjttoxfpm4nwah2zjulethzznvp2yl2vwsdf5rnkvz4ocvmjqq")

			Expect(utils.StableRequestNameMCP(onboardingName, controllerName)).To(Equal(expectedName))
		})
	})

	Context("StableRequestNameOnboarding", func() {
		It("should return a stable name for Onboarding requests", func() {
			expectedName := fmt.Sprint("ob-", "s3bjttoxfpm4nwah2zjulethzznvp2yl2vwsdf5rnkvz4ocvmjqq")

			Expect(utils.StableRequestNameOnboarding(onboardingName, controllerName)).To(Equal(expectedName))
		})
	})

	Context("StableRequestNameWorkload", func() {
		It("should return a stable name for Workload requests", func() {
			expectedName := fmt.Sprint("wl-", "s3bjttoxfpm4nwah2zjulethzznvp2yl2vwsdf5rnkvz4ocvmjqq")

			Expect(utils.StableRequestNameWorkload(onboardingName, controllerName)).To(Equal(expectedName))
		})
	})

	Context("StableMCPNamespace", func() {
		It("should compute the MCP namespace on the platform cluster", func() {
			expectedNamespace, err := utils.StableMCPNamespace(onboardingName, onboardingNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(expectedNamespace).To(Equal("mcp--ed00e8ed-8a09-8c62-a9b8-e6fc20255174"))
		})
	})

	Context("StableMCPIdentifier", func() {
		It("should compute a stable MCP identifier", func() {
			expectedIdentifier, err := utils.StableMCPIdentifier(onboardingName, onboardingNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(expectedIdentifier).To(Equal("ed00e8ed-8a09-8c62-a9b8-e6fc20255174"))
		})
	})
})
