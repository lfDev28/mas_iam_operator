package masadmin

// Certificate is a single PEM-formatted certificate with a unique alias.
type Certificate struct {
	Alias string `json:"alias"`
	CRT   string `json:"crt"`
}

// LDAPConfigRequest is the body of PUT /config/ldap/{idpId}.
type LDAPConfigRequest struct {
	DisplayName  string        `json:"displayName,omitempty"`
	URL          string        `json:"url"`
	BaseDN       string        `json:"baseDN"`
	BindDN       string        `json:"bindDN"`
	BindPassword string        `json:"bindPassword"`
	UserIDMap    string        `json:"userIdMap"`
	Certificates []Certificate `json:"certificates"`
}

// OIDCConfigRequest is the body of PUT /config/oidc/{idpId}.
type OIDCConfigRequest struct {
	DisplayName                       string        `json:"displayName,omitempty"`
	ClientID                          string        `json:"clientId"`
	ClientSecret                      string        `json:"clientSecret"`
	DiscoveryEndpointURL              string        `json:"discoveryEndpointUrl,omitempty"`
	IssuerIdentifier                  string        `json:"issuerIdentifier,omitempty"`
	AuthorizationEndpointURL          string        `json:"authorizationEndpointUrl,omitempty"`
	TokenEndpointURL                  string        `json:"tokenEndpointUrl,omitempty"`
	JWKEndpointURL                    string        `json:"jwkEndpointUrl,omitempty"`
	TokenEndpointAuthMethod           string        `json:"tokenEndpointAuthMethod,omitempty"`
	TokenEndpointAuthSigningAlgorithm string        `json:"tokenEndpointAuthSigningAlgorithm,omitempty"`
	UserIdentifier                    string        `json:"userIdentifier,omitempty"`
	SignatureAlgorithm                string        `json:"signatureAlgorithm,omitempty"`
	Certificates                      []Certificate `json:"certificates"`
}

// SAMLSPConfigRequest is the body of PUT /config/saml/{idpId} — service-provider side only.
// The IdP metadata XML is uploaded separately via PUT /config/saml/{idpId}/metadata.
type SAMLSPConfigRequest struct {
	DisplayName         string `json:"displayName,omitempty"`
	Issuer              string `json:"issuer"`
	ServiceProviderName string `json:"serviceProviderName"`
	NameIDFormat        string `json:"nameIDFormat"`
	SPInitiatedLogout   *bool  `json:"spInitiatedLogout,omitempty"`
}

// LDAPTestResult is the response from POST /config/ldap-test.
type LDAPTestResult struct {
	Success bool `json:"success"`
}

// IDPCondition is one entry in the response status.conditions array.
type IDPCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

// IDPStatus is the status block of any IDP config response.
type IDPStatus struct {
	Conditions []IDPCondition `json:"conditions,omitempty"`
}

// IDPConfigResponse is the common envelope returned by GET/PUT on /config/{ldap,oidc,saml}/{idpId}.
// The spec field varies by provider — keep it raw and let callers inspect status.
type IDPConfigResponse struct {
	Metadata struct {
		CreationTimestamp string `json:"creationTimestamp,omitempty"`
		DeletionTimestamp string `json:"deletionTimestamp,omitempty"`
	} `json:"metadata"`
	Status IDPStatus `json:"status"`
}

// Ready returns true if the response carries a condition of type=Ready, status=True.
func (r IDPConfigResponse) Ready() bool {
	for _, c := range r.Status.Conditions {
		if c.Type == "Ready" && c.Status == "True" {
			return true
		}
	}
	return false
}

// SAMLIDPMetadataResponse is the response from GET /config/saml/{idpId}/metadata.
type SAMLIDPMetadataResponse struct {
	CreationTimestamp string `json:"creationTimestamp,omitempty"`
	DeletionTimestamp string `json:"deletionTimestamp,omitempty"`
	IDPMetadataXML    string `json:"idpMetadata.xml,omitempty"`
}

// APIError is the JSON error envelope returned for 4xx/5xx responses.
type APIError struct {
	Status    int    `json:"-"`
	ErrorCode int    `json:"error,omitempty"`
	Message   string `json:"message,omitempty"`
	Exception struct {
		ID         string `json:"id,omitempty"`
		Properties []any  `json:"properties,omitempty"`
	} `json:"exception,omitempty"`
}
