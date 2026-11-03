package masadmin

import (
	"strings"
	"time"
)

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
	// ObservedGeneration is the Kubernetes-convention per-condition
	// generation. The MAS IDPCfg operator has not been seen to set it; it is
	// decoded so freshness can use it whenever it is present.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// IDPStatus is the status block of any IDP config response.
type IDPStatus struct {
	Conditions         []IDPCondition `json:"conditions,omitempty"`
	ObservedGeneration int64          `json:"observedGeneration,omitempty"`
}

// IDPConfigResponse is the common envelope returned by GET/PUT on /config/{ldap,oidc,saml}/{idpId}.
// The spec field varies by provider — keep it raw and let callers inspect status.
type IDPConfigResponse struct {
	Metadata struct {
		CreationTimestamp string `json:"creationTimestamp,omitempty"`
		DeletionTimestamp string `json:"deletionTimestamp,omitempty"`
		Generation        int64  `json:"generation,omitempty"`
	} `json:"metadata"`
	Status IDPStatus `json:"status"`

	// ServerTime is the HTTP Date header of the response that produced this
	// value, i.e. the MAS cluster's clock. Freshness checks compare condition
	// timestamps against it so a skewed laptop clock does not matter.
	ServerTime time.Time `json:"-"`
}

func (r *IDPConfigResponse) setServerTime(t time.Time) { r.ServerTime = t }

// Ready returns true if the response carries a condition of type=Ready,
// status=True. It says nothing about WHEN that condition was set: right after
// a PUT it is usually the condition left over from the previous apply. Use
// ReadyAfter to wait for the reconcile the PUT triggered.
func (r IDPConfigResponse) Ready() bool {
	_, ok := r.readyCondition()
	return ok
}

func (r IDPConfigResponse) readyCondition() (IDPCondition, bool) {
	for _, c := range r.Status.Conditions {
		if c.Type == "Ready" && c.Status == "True" {
			return c, true
		}
	}
	return IDPCondition{}, false
}

// Freshness says whether a Ready=True condition provably reflects a
// reconcile that happened after a given point in time.
type Freshness int

const (
	// NotReady: no Ready=True condition.
	NotReady Freshness = iota
	// ReadyFresh: Ready=True and either observedGeneration has caught up with
	// metadata.generation, or lastTransitionTime is at/after the reference
	// time (minus the allowed clock skew).
	ReadyFresh
	// ReadyStale: Ready=True, but its lastTransitionTime predates the
	// reference time — either the reconcile has not run yet, or it ran and
	// found nothing to change (Ready never flipped).
	ReadyStale
	// ReadyLagging: Ready=True, but observedGeneration is behind
	// metadata.generation — the controller has definitely not reconciled the
	// latest spec yet.
	ReadyLagging
	// ReadyUnknown: Ready=True with neither generation data nor a parseable
	// lastTransitionTime, so freshness cannot be decided.
	ReadyUnknown
)

// ReadyAfter classifies the Ready condition relative to `since` (normally the
// PUT time). observedGeneration wins when MAS exposes it; otherwise
// lastTransitionTime is compared with `since - skew`.
func (r IDPConfigResponse) ReadyAfter(since time.Time, skew time.Duration) Freshness {
	cond, ok := r.readyCondition()
	if !ok {
		return NotReady
	}
	observed := cond.ObservedGeneration
	if observed == 0 {
		observed = r.Status.ObservedGeneration
	}
	return ClassifyReady(r.Metadata.Generation, observed, cond.LastTransitionTime, since, skew)
}

// ClassifyReady is the freshness rule shared by the Admin API and the
// IDPCfg/ObjectStorageCfg CR paths, for a condition already known to be
// Ready=True.
func ClassifyReady(generation, observedGeneration int64, lastTransitionTime string, since time.Time, skew time.Duration) Freshness {
	if generation > 0 && observedGeneration > 0 {
		if observedGeneration >= generation {
			return ReadyFresh
		}
		return ReadyLagging
	}
	if strings.TrimSpace(lastTransitionTime) == "" || since.IsZero() {
		return ReadyUnknown
	}
	transitioned, err := time.Parse(time.RFC3339, strings.TrimSpace(lastTransitionTime))
	if err != nil {
		return ReadyUnknown
	}
	if !transitioned.Before(since.Add(-skew)) {
		return ReadyFresh
	}
	return ReadyStale
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
