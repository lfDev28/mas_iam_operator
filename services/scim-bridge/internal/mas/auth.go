package mas

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/config"
)

// FetchToken obtains a MAS token via basic auth against /v1/authenticate.
// cfg.BaseURL may include /scim/v2; the suffix is stripped. The call goes
// through the same HTTP client as SCIM calls (MAS_CA_FILE on top of the
// system roots, skip-verify, RequestTimeout) and is bounded by ctx.
func FetchToken(ctx context.Context, cfg config.MASConfig, username, password string) (string, error) {
	httpClient, err := NewHTTPClient(cfg)
	if err != nil {
		return "", err
	}
	root := strings.TrimSuffix(strings.TrimRight(cfg.BaseURL, "/"), "/scim/v2")
	authURL := root + "/v1/authenticate"

	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(username, password)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return "", fmt.Errorf("auth request failed: %s", resp.Status)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode auth response: %w", err)
	}
	if body.Token == "" {
		return "", fmt.Errorf("auth response missing token")
	}
	return body.Token, nil
}
