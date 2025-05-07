package install

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// InstallCRDAPIs installs the CRD APIs in the scheme.
// This is used for the init subcommand.
func InstallCRDAPIs(scheme *runtime.Scheme) *runtime.Scheme {

	//utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	//utilruntime.Must(apiextv1.AddToScheme(scheme))

	return scheme
}
