package masadmin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

// samlMetadataFieldName is the multipart form-data field name used for the
// IdP metadata XML upload. The OpenAPI schema only specifies the binary body,
// not the field name; "file" matches the IBM Developer "Try it" panel default.
// If MAS rejects the upload, tweak this constant.
const samlMetadataFieldName = "file"

// SetLDAP creates or updates the LDAP configuration with the given idpId.
// idpId must be 3-24 characters; the API rejects anything outside that range.
func (c *Client) SetLDAP(ctx context.Context, idpId string, req LDAPConfigRequest) (*IDPConfigResponse, error) {
	if err := validateIDPID(idpId); err != nil {
		return nil, err
	}
	out := &IDPConfigResponse{}
	if err := c.doJSON(ctx, http.MethodPut, "/config/ldap/"+url.PathEscape(idpId), req, out); err != nil {
		return nil, err
	}
	return out, nil
}

// TestLDAP runs a server-side LDAP bind test without persisting the config.
func (c *Client) TestLDAP(ctx context.Context, req LDAPConfigRequest) (*LDAPTestResult, error) {
	out := &LDAPTestResult{}
	if err := c.doJSON(ctx, http.MethodPost, "/config/ldap-test", req, out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteLDAP removes the LDAP configuration with the given idpId.
func (c *Client) DeleteLDAP(ctx context.Context, idpId string) error {
	if err := validateIDPID(idpId); err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodDelete, "/config/ldap/"+url.PathEscape(idpId), nil, nil)
}

// GetLDAP fetches the current LDAP configuration with the given idpId.
// Returns nil and a not-found-style error if the configuration does not exist.
func (c *Client) GetLDAP(ctx context.Context, idpId string) (*IDPConfigResponse, error) {
	if err := validateIDPID(idpId); err != nil {
		return nil, err
	}
	out := &IDPConfigResponse{}
	if err := c.doJSON(ctx, http.MethodGet, "/config/ldap/"+url.PathEscape(idpId), nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetOIDC creates or updates the OIDC configuration with the given idpId.
func (c *Client) SetOIDC(ctx context.Context, idpId string, req OIDCConfigRequest) (*IDPConfigResponse, error) {
	if err := validateIDPID(idpId); err != nil {
		return nil, err
	}
	out := &IDPConfigResponse{}
	if err := c.doJSON(ctx, http.MethodPut, "/config/oidc/"+url.PathEscape(idpId), req, out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteOIDC removes the OIDC configuration with the given idpId.
func (c *Client) DeleteOIDC(ctx context.Context, idpId string) error {
	if err := validateIDPID(idpId); err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodDelete, "/config/oidc/"+url.PathEscape(idpId), nil, nil)
}

// GetOIDC fetches the current OIDC configuration with the given idpId.
func (c *Client) GetOIDC(ctx context.Context, idpId string) (*IDPConfigResponse, error) {
	if err := validateIDPID(idpId); err != nil {
		return nil, err
	}
	out := &IDPConfigResponse{}
	if err := c.doJSON(ctx, http.MethodGet, "/config/oidc/"+url.PathEscape(idpId), nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetSAMLSP creates or updates the SAML service-provider configuration.
// The IdP metadata XML must be uploaded separately via SetSAMLIDPMetadata.
func (c *Client) SetSAMLSP(ctx context.Context, idpId string, req SAMLSPConfigRequest) (*IDPConfigResponse, error) {
	if err := validateIDPID(idpId); err != nil {
		return nil, err
	}
	out := &IDPConfigResponse{}
	if err := c.doJSON(ctx, http.MethodPut, "/config/saml/"+url.PathEscape(idpId), req, out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetSAMLIDPMetadata uploads the IdP metadata XML for the given SAML idpId.
// metadataXML is the raw XML descriptor bytes; this method wraps it in the
// multipart/form-data envelope the API expects.
func (c *Client) SetSAMLIDPMetadata(ctx context.Context, idpId string, metadataXML []byte) (*SAMLIDPMetadataResponse, error) {
	if err := validateIDPID(idpId); err != nil {
		return nil, err
	}
	if len(metadataXML) == 0 {
		return nil, errors.New("masadmin: SAML IdP metadata XML is empty")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(samlMetadataFieldName, fmt.Sprintf("%s-idp-metadata.xml", idpId))
	if err != nil {
		return nil, fmt.Errorf("masadmin: create multipart part: %w", err)
	}
	if _, err := part.Write(metadataXML); err != nil {
		return nil, fmt.Errorf("masadmin: write multipart part: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("masadmin: close multipart writer: %w", err)
	}

	out := &SAMLIDPMetadataResponse{}
	if err := c.doMultipart(ctx, http.MethodPut, "/config/saml/"+url.PathEscape(idpId)+"/metadata", body.Bytes(), writer.FormDataContentType(), out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteSAML removes the SAML configuration with the given idpId, including
// any uploaded IdP metadata.
func (c *Client) DeleteSAML(ctx context.Context, idpId string) error {
	if err := validateIDPID(idpId); err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodDelete, "/config/saml/"+url.PathEscape(idpId), nil, nil)
}

// GetSAML fetches the current SAML configuration with the given idpId.
func (c *Client) GetSAML(ctx context.Context, idpId string) (*IDPConfigResponse, error) {
	if err := validateIDPID(idpId); err != nil {
		return nil, err
	}
	out := &IDPConfigResponse{}
	if err := c.doJSON(ctx, http.MethodGet, "/config/saml/"+url.PathEscape(idpId), nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

func validateIDPID(id string) error {
	id = strings.TrimSpace(id)
	if l := len(id); l < 3 || l > 24 {
		return fmt.Errorf("masadmin: idpId %q must be 3-24 characters (got %d)", id, l)
	}
	return nil
}
