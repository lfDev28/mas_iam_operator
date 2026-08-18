package preflight

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
)

func TestRankStorageClassesPrefersBlockOverCephfsDefault(t *testing.T) {
	ranking := RankStorageClasses([]oc.StorageClass{
		{Name: "ocs-storagecluster-cephfs", IsDefault: true},
		{Name: "ocs-external-storagecluster-ceph-rbd", IsDefault: false},
		{Name: "gp3-csi", IsDefault: false},
	})

	if ranking.Recommended != "ocs-external-storagecluster-ceph-rbd" {
		t.Fatalf("expected rbd class to be recommended, got %q", ranking.Recommended)
	}
	if ranking.Warning == "" {
		t.Fatal("expected warning when cephfs is default and rbd exists")
	}
}

func TestParseMASHostRequiresSCIMPath(t *testing.T) {
	_, err := ParseMASHost("https://api.example.com")
	if err == nil {
		t.Fatal("expected missing /scim/v2 path to fail")
	}
}

func TestParseMASHostReturnsHostname(t *testing.T) {
	host, err := ParseMASHost("https://api.example.com/scim/v2")
	if err != nil {
		t.Fatalf("expected valid MAS URL, got error: %v", err)
	}
	if host != "api.example.com" {
		t.Fatalf("expected host api.example.com, got %q", host)
	}
}

func TestCheckOIDCEndpointUnauthorizedPasses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config/oidc/default" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	result := CheckOIDCEndpoint(context.Background(), server.Client(), server.URL+"/scim/v2")
	if result.Status != StatusOK {
		t.Fatalf("expected 401 probe to pass, got %s: %s", result.Status, result.Message)
	}
}

func TestCheckOIDCEndpointNotFoundFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"exception":{"id":"AIUCO1022E","message":"The requested URL could not be found"}}`))
	}))
	defer server.Close()

	result := CheckOIDCEndpoint(context.Background(), server.Client(), server.URL+"/scim/v2")
	if result.Status != StatusError {
		t.Fatalf("expected 404 probe to fail, got %s: %s", result.Status, result.Message)
	}
	for _, want := range []string{"AIUCO1022E", "MAS 9.1+", "--mas-auth-providers ldap,saml"} {
		if !strings.Contains(result.Message, want) {
			t.Fatalf("expected failure message to contain %q, got %q", want, result.Message)
		}
	}
}

func TestCheckOIDCEndpointNetworkErrorWarns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	baseURL := server.URL + "/scim/v2"
	server.Close()

	result := CheckOIDCEndpoint(context.Background(), http.DefaultClient, baseURL)
	if result.Status != StatusWarn {
		t.Fatalf("expected network error to warn, got %s: %s", result.Status, result.Message)
	}
}

// scimServer stubs the two endpoints CheckAPIKey touches: /v1/authenticate at
// the API root and /scim/v2/Profiles under the SCIM base.
func scimServer(t *testing.T, authStatus int, authBody string, profilesStatus int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/authenticate":
			if _, _, ok := r.BasicAuth(); !ok {
				t.Error("expected basic auth on /v1/authenticate")
			}
			w.WriteHeader(authStatus)
			_, _ = w.Write([]byte(authBody))
		case "/scim/v2/Profiles":
			if got := r.Header.Get("Authorization"); got != "Bearer test-jwt" {
				t.Errorf("expected bearer JWT on Profiles call, got %q", got)
			}
			w.WriteHeader(profilesStatus)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
}

func TestCheckAPIKeyForbiddenOnSCIMFails(t *testing.T) {
	server := scimServer(t, http.StatusOK, `{"token":"test-jwt"}`, http.StatusForbidden)
	defer server.Close()

	result := CheckAPIKey(context.Background(), server.Client(), server.URL+"/scim/v2", "masdemo", "secret")
	if result.Status != StatusError {
		t.Fatalf("expected 403 on Profiles to fail, got %s: %s", result.Status, result.Message)
	}
	for _, want := range []string{"masdemo", "AIUCO1003E", "userAdmin and systemAdmin"} {
		if !strings.Contains(result.Message, want) {
			t.Fatalf("expected failure message to contain %q, got %q", want, result.Message)
		}
	}
}

func TestCheckAPIKeyProfilesNotFoundPasses(t *testing.T) {
	server := scimServer(t, http.StatusOK, `{"token":"test-jwt"}`, http.StatusNotFound)
	defer server.Close()

	result := CheckAPIKey(context.Background(), server.Client(), server.URL+"/scim/v2", "masdemo", "secret")
	if result.Status != StatusOK {
		t.Fatalf("expected 404 on Profiles to pass, got %s: %s", result.Status, result.Message)
	}
}

func TestCheckAPIKeyAuthenticateFailureFails(t *testing.T) {
	server := scimServer(t, http.StatusUnauthorized, `{"message":"bad credentials"}`, http.StatusOK)
	defer server.Close()

	result := CheckAPIKey(context.Background(), server.Client(), server.URL+"/scim/v2", "masdemo", "wrong")
	if result.Status != StatusError {
		t.Fatalf("expected authenticate 401 to fail, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "failed to authenticate (HTTP 401)") {
		t.Fatalf("expected authenticate failure message, got %q", result.Message)
	}
}

func TestCheckAPIKeyNetworkErrorWarns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	baseURL := server.URL + "/scim/v2"
	server.Close()

	result := CheckAPIKey(context.Background(), http.DefaultClient, baseURL, "masdemo", "secret")
	if result.Status != StatusWarn {
		t.Fatalf("expected network error to warn, got %s: %s", result.Status, result.Message)
	}
}
