package utils_test

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openmcp-project/openmcp-operator/lib/utils"
)

const (
	controllerName      = "test-controller"
	onboardingName      = "foo"
	onboardingNamespace = "foo"
)

func TestUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Utils Test Suite")
}

var _ = Describe("Utils", func() {
	Context("StableControllerNamespace", func() {
		It("should return a stable namespace for controllers", func() {
			expectedNamespace := fmt.Sprint("x33lfeffopjdj5decdwbrcje75inbmpthligzyqlvvooqay7tpoq", "-system")

			Expect(utils.StableControllerNamespace(controllerName)).To(Equal(expectedNamespace))
		})
	})

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
})
