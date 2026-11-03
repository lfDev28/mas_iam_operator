package preflight

import (
	"context"
	"net/http"
	"testing"
)

// --configure-mas-auth without scim only needs the key to authenticate: a key
// without SCIM permissions must not fail preflight on that path.
func TestCheckAPIKeyAuthenticatesSkipsSCIMProbe(t *testing.T) {
	server := scimServer(t, http.StatusOK, `{"token":"test-jwt"}`, http.StatusForbidden)
	defer server.Close()

	result := CheckAPIKeyAuthenticates(context.Background(), server.Client(), server.URL+"/scim/v2", "masdemo", "secret")
	if result.Status != StatusOK {
		t.Fatalf("auth-only key check = %s: %s, want ok without touching /Profiles", result.Status, result.Message)
	}

	bad := scimServer(t, http.StatusUnauthorized, `{}`, http.StatusOK)
	defer bad.Close()
	if result := CheckAPIKeyAuthenticates(context.Background(), bad.Client(), bad.URL+"/scim/v2", "masdemo", "wrong"); result.Status != StatusError {
		t.Fatalf("bad key = %s, want error", result.Status)
	}
}
