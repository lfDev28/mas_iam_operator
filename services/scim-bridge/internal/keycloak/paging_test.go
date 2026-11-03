package keycloak

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// newPagingKeycloak serves `total` users with real first/max paging. Any
// request whose window covers the user at position `bad` fails with the
// 400 "Cannot parse the JSON" Keycloak returns when one user cannot be
// serialised.
func newPagingKeycloak(t *testing.T, total, bad int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/realms/test/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "stub", "expires_in": 60, "token_type": "Bearer"})
	})
	mux.HandleFunc("/admin/realms/test/users", func(w http.ResponseWriter, r *http.Request) {
		first, _ := strconv.Atoi(r.URL.Query().Get("first"))
		max, _ := strconv.Atoi(r.URL.Query().Get("max"))
		if max == 0 {
			max = 100
		}
		end := first + max
		if end > total {
			end = total
		}
		if bad >= first && bad < end {
			http.Error(w, `{"error":"Cannot parse the JSON"}`, http.StatusBadRequest)
			return
		}
		out := []map[string]any{}
		for i := first; i < end; i++ {
			out = append(out, map[string]any{"id": fmt.Sprintf("id-%d", i), "username": fmt.Sprintf("user%d", i), "enabled": true})
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	return httptest.NewServer(mux)
}

func TestListUsersPageFallbackKeepsPagingAtSizeOne(t *testing.T) {
	server := newPagingKeycloak(t, 30, 7)
	defer server.Close()
	client := newGroupTestClient(t, server.URL)

	users, raw, err := client.ListUsersPage(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("ListUsersPage: %v", err)
	}
	if raw != 10 {
		t.Fatalf("expected rawCount to cover the whole window so paging continues, got %d", raw)
	}
	if len(users) != 9 {
		t.Fatalf("expected 9 readable users (only the unreadable one skipped), got %d", len(users))
	}
	for _, u := range users {
		if u.ID == "id-7" {
			t.Fatalf("unreadable user should be skipped")
		}
	}

	// A clean window still uses one request and returns everything.
	users, raw, err = client.ListUsersPage(context.Background(), 10, 10)
	if err != nil || raw != 10 || len(users) != 10 {
		t.Fatalf("clean page: users=%d raw=%d err=%v", len(users), raw, err)
	}
}

func TestListUsersPageFallbackStopsAtEndOfRealm(t *testing.T) {
	server := newPagingKeycloak(t, 5, 2)
	defer server.Close()
	client := newGroupTestClient(t, server.URL)

	users, raw, err := client.ListUsersPage(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("ListUsersPage: %v", err)
	}
	if len(users) != 4 || raw != 5 {
		t.Fatalf("expected 4 users and rawCount 5 (short page ends paging), got users=%d raw=%d", len(users), raw)
	}
}
