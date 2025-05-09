package install

import (
	providerv1alpha1 "github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// InstallCRDAPIs installs the CRD APIs in the scheme.
// This is used for the init subcommand.
func InstallCRDAPIs(scheme *runtime.Scheme) *runtime.Scheme {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextv1.AddToScheme(scheme))

	return scheme
}

func InstallOperatorAPIs(scheme *runtime.Scheme) *runtime.Scheme {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(providerv1alpha1.AddToScheme(scheme))

	return scheme
}
