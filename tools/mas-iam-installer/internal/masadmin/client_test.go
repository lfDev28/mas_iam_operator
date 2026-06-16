package masadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStripSCIMSuffix(t *testing.T) {
	cases := map[string]string{
		"https://api.mas91.example.com/scim/v2":  "https://api.mas91.example.com",
		"https://api.mas91.example.com/scim/v2/": "https://api.mas91.example.com",
		"https://api.mas91.example.com/":         "https://api.mas91.example.com",
		"https://api.mas91.example.com":          "https://api.mas91.example.com",
		"  https://api.mas91.example.com/scim/v2  ": "https://api.mas91.example.com",
	}
	for in, want := range cases {
		if got := StripSCIMSuffix(in); got != want {
			t.Errorf("StripSCIMSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewClientValidatesInputs(t *testing.T) {
	if _, err := NewClient("", "name", "value"); err == nil {
		t.Fatal("expected error for empty baseURL")
	}
	if _, err := NewClient("https://api.example.com", "", "value"); err == nil {
		t.Fatal("expected error for empty token name")
	}
	if _, err := NewClient("https://api.example.com", "name", ""); err == nil {
		t.Fatal("expected error for empty token value")
	}
	if _, err := NewClient("api.example.com", "name", "value"); err == nil {
		t.Fatal("expected error for URL missing scheme")
	}
	c, err := NewClient("https://api.example.com/scim/v2", "name", "value")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got, want := c.BaseURL(), "https://api.example.com"; got != want {
		t.Errorf("BaseURL = %q, want %q", got, want)
	}
}

func TestValidateIDPID(t *testing.T) {
	if err := validateIDPID("ab"); err == nil {
		t.Fatal("expected error for 2-char id")
	}
	if err := validateIDPID(strings.Repeat("a", 25)); err == nil {
		t.Fatal("expected error for 25-char id")
	}
	if err := validateIDPID("mas-est-ldap"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// newTestServer builds an httptest.Server that mints a JWT on
// /v1/authenticate and answers all other requests via handler.
func newTestServer(t *testing.T, jwt string, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/authenticate", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "tester" || pass != "secret" {
			http.Error(w, "bad basic auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": jwt})
	})
	mux.HandleFunc("/", handler)
	return httptest.NewServer(mux)
}

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := NewClient(srv.URL, "tester", "secret", WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestAuthenticateCachesToken(t *testing.T) {
	var auths int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/authenticate", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&auths, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "jwt-1"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(t, srv)

	for i := 0; i < 3; i++ {
		tok, err := c.Authenticate(context.Background())
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
		if tok != "jwt-1" {
			t.Fatalf("got token %q, want jwt-1", tok)
		}
	}
	if atomic.LoadInt32(&auths) != 1 {
		t.Errorf("auth endpoint hit %d times, want 1", auths)
	}
}

func TestAuthenticateFailsOnBadCreds(t *testing.T) {
	srv := newTestServer(t, "jwt-1", func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()
	c, err := NewClient(srv.URL, "wrong", "wrong", WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.Authenticate(context.Background()); err == nil {
		t.Fatal("expected error from bad credentials")
	}
}

func TestSetLDAPSendsExpectedRequest(t *testing.T) {
	want := LDAPConfigRequest{
		DisplayName:  "MAS EST LDAP",
		URL:          "ldaps://openldap.mas-est.svc:636",
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "p@ssw0rd",
		UserIDMap:    "*:uid",
		Certificates: []Certificate{{Alias: "openldap-1", CRT: "-----BEGIN CERTIFICATE-----\nMIIE\n-----END CERTIFICATE-----"}},
	}
	srv := newTestServer(t, "jwt-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if got, wantPath := r.URL.Path, "/config/ldap/mas-est-ldap"; got != wantPath {
			t.Errorf("path = %s, want %s", got, wantPath)
		}
		if got := r.Header.Get("x-access-token"); got != "jwt-1" {
			t.Errorf("x-access-token = %q, want jwt-1", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		var got LDAPConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if got.URL != want.URL || got.BindDN != want.BindDN || got.UserIDMap != want.UserIDMap {
			t.Errorf("decoded body mismatch: %+v", got)
		}
		if len(got.Certificates) != 1 || got.Certificates[0].Alias != "openldap-1" {
			t.Errorf("certificates not propagated: %+v", got.Certificates)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{"creationTimestamp":"2026-06-15T00:00:00Z"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}`))
	})
	defer srv.Close()
	c := newTestClient(t, srv)

	resp, err := c.SetLDAP(context.Background(), "mas-est-ldap", want)
	if err != nil {
		t.Fatalf("SetLDAP: %v", err)
	}
	if !resp.Ready() {
		t.Errorf("expected Ready, got conditions %+v", resp.Status.Conditions)
	}
}

func TestSetOIDCSendsExpectedRequest(t *testing.T) {
	want := OIDCConfigRequest{
		DisplayName:          "MAS EST OIDC",
		ClientID:             "mas-est-oidc",
		ClientSecret:         "shhh",
		DiscoveryEndpointURL: "https://keycloak.example.com/realms/maximo/.well-known/openid-configuration",
		Certificates:         []Certificate{{Alias: "keycloak-1", CRT: "-----BEGIN CERTIFICATE-----\nAAA\n-----END CERTIFICATE-----"}},
	}
	srv := newTestServer(t, "jwt-1", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config/oidc/mas-est-oidc" || r.Method != http.MethodPut {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var got OIDCConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if got.ClientID != want.ClientID || got.DiscoveryEndpointURL != want.DiscoveryEndpointURL {
			t.Errorf("body mismatch: %+v", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{},"status":{}}`))
	})
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := c.SetOIDC(context.Background(), "mas-est-oidc", want); err != nil {
		t.Fatalf("SetOIDC: %v", err)
	}
}

func TestSetSAMLSPAndMetadata(t *testing.T) {
	var sawSPCall, sawMetadataCall bool
	srv := newTestServer(t, "jwt-1", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/config/saml/mas-est-saml" && r.Method == http.MethodPut:
			sawSPCall = true
			var got SAMLSPConfigRequest
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode SP body: %v", err)
			}
			if got.Issuer != "mas-est-saml" || got.NameIDFormat != "email" {
				t.Errorf("SP body mismatch: %+v", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"metadata":{},"status":{}}`))
		case r.URL.Path == "/config/saml/mas-est-saml/metadata" && r.Method == http.MethodPut:
			sawMetadataCall = true
			ctype, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || ctype != "multipart/form-data" {
				t.Fatalf("Content-Type = %q (err=%v), want multipart/form-data", r.Header.Get("Content-Type"), err)
			}
			mr := multipart.NewReader(r.Body, params["boundary"])
			part, err := mr.NextPart()
			if err != nil {
				t.Fatalf("read part: %v", err)
			}
			if part.FormName() != samlMetadataFieldName {
				t.Errorf("form field = %q, want %q", part.FormName(), samlMetadataFieldName)
			}
			body, _ := io.ReadAll(part)
			if !strings.Contains(string(body), "<EntityDescriptor") {
				t.Errorf("uploaded body missing XML: %s", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"creationTimestamp":"2026-06-15T00:00:00Z","idpMetadata.xml":"<EntityDescriptor/>"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	c := newTestClient(t, srv)

	spInit := true
	if _, err := c.SetSAMLSP(context.Background(), "mas-est-saml", SAMLSPConfigRequest{
		Issuer:              "mas-est-saml",
		ServiceProviderName: "mas-est-saml-sp",
		NameIDFormat:        "email",
		SPInitiatedLogout:   &spInit,
	}); err != nil {
		t.Fatalf("SetSAMLSP: %v", err)
	}
	if _, err := c.SetSAMLIDPMetadata(context.Background(), "mas-est-saml", []byte("<EntityDescriptor></EntityDescriptor>")); err != nil {
		t.Fatalf("SetSAMLIDPMetadata: %v", err)
	}
	if !sawSPCall || !sawMetadataCall {
		t.Errorf("missed call(s): sp=%v metadata=%v", sawSPCall, sawMetadataCall)
	}
}

func TestRetriesOnUnauthorized(t *testing.T) {
	var authHits, configHits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/authenticate", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&authHits, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": fmt.Sprintf("jwt-%d", authHits)})
	})
	mux.HandleFunc("/config/ldap/", func(w http.ResponseWriter, r *http.Request) {
		hits := atomic.AddInt32(&configHits, 1)
		if hits == 1 {
			http.Error(w, "expired", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{},"status":{}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := c.SetLDAP(context.Background(), "mas-est-ldap", LDAPConfigRequest{URL: "ldaps://x", UserIDMap: "*:uid"}); err != nil {
		t.Fatalf("SetLDAP: %v", err)
	}
	if got := atomic.LoadInt32(&authHits); got != 2 {
		t.Errorf("auth hits = %d, want 2 (initial + retry)", got)
	}
	if got := atomic.LoadInt32(&configHits); got != 2 {
		t.Errorf("config hits = %d, want 2", got)
	}
}

func TestAPIErrorSurfacesMessage(t *testing.T) {
	srv := newTestServer(t, "jwt-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":400,"message":"CUDRS0001E: bindDN required"}`))
	})
	defer srv.Close()
	c := newTestClient(t, srv)
	_, err := c.SetLDAP(context.Background(), "mas-est-ldap", LDAPConfigRequest{URL: "ldaps://x", UserIDMap: "*:uid"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "CUDRS0001E") {
		t.Errorf("error does not surface message: %v", err)
	}
}
