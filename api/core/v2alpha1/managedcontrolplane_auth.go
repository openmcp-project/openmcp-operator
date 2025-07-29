package v2alpha1

import (
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

// AuthenticationConfiguration contains the configuration for the enabled OpenID Connect identity providers
type AuthenticationConfiguration struct {
	// +kubebuilder:validation:Optional
	EnableSystemIdentityProvider *bool `json:"enableSystemIdentityProvider"`
	// +kubebuilder:validation:Optional
	IdentityProviders []IdentityProvider `json:"identityProviders,omitempty"`
}

// IdentityProvider contains the configuration for an OpenID Connect identity provider
type IdentityProvider struct {
	// Name is the name of the identity provider.
	// The name must be unique among all identity providers.
	// The name must only contain lowercase letters.
	// The length must not exceed 63 characters.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z]+$`
	Name string `json:"name"`
	// IssuerURL is the issuer URL of the identity provider.
	// +kubebuilder:validation:Required
	IssuerURL string `json:"issuerURL"`
	// ClientID is the client ID of the identity provider.
	// +kubebuilder:validation:Required
	ClientID string `json:"clientID"`
	// UsernameClaim is the claim that contains the username.
	// +kubebuilder:validation:Required
	UsernameClaim string `json:"usernameClaim"`
	// GroupsClaim is the claim that contains the groups.
	// +kubebuilder:validation:Optional
	GroupsClaim string `json:"groupsClaim"`
	// CABundle: When set, the OpenID server's certificate will be verified by one of the authorities in the bundle.
	// Otherwise, the host's root CA set will be used.
	// +kubebuilder:validation:Optional
	CABundle string `json:"caBundle,omitempty"`
	// SigningAlgs is the list of allowed JOSE asymmetric signing algorithms.
	// +kubebuilder:validation:Optional
	SigningAlgs []string `json:"signingAlgs,omitempty"`
	// RequiredClaims is a map of required claims. If set, the identity provider must provide these claims in the ID token.
	// +kubebuilder:validation:Optional
	RequiredClaims map[string]string `json:"requiredClaims,omitempty"`

	// ClientAuthentication contains configuration for OIDC clients
	// +kubebuilder:validation:Optional
	ClientConfig ClientAuthenticationConfig `json:"clientConfig,omitempty"`
}

// ClientAuthenticationConfig contains configuration for OIDC clients
type ClientAuthenticationConfig struct {
	// ClientSecret is a references to a secret containing the client secret.
	// The client secret will be added to the generated kubeconfig with the "--oidc-client-secret" flag.
	// +kubebuilder:validation:Optional
	ClientSecret *commonapi.LocalSecretReference `json:"clientSecret,omitempty"`
	// ExtraConfig is added to the client configuration in the kubeconfig.
	// Can either be a single string value, a list of string values or no value.
	// Must not contain any of the following keys:
	// - "client-id"
	// - "client-secret"
	// - "issuer-url"
	//
	// +kubebuilder:validation:Optional
	ExtraConfig map[string]SingleOrMultiStringValue `json:"extraConfig,omitempty"`
}

// SingleOrMultiStringValue is a type that can hold either a single string value or a list of string values.
type SingleOrMultiStringValue struct {
	// Value is a single string value.
	Value string `json:"value,omitempty"`
	// Values is a list of string values.
	Values []string `json:"values,omitempty"`
}
