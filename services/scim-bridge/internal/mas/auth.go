package mas

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// FetchToken obtains a MAS token via basic auth against the /v1/authenticate endpoint.
// masBase may include /scim/v2; the suffix will be stripped automatically.
func FetchToken(masBase, username, password string, insecureSkipVerify bool) (string, error) {
	root := strings.TrimRight(masBase, "/")
	if strings.HasSuffix(root, "/scim/v2") {
		root = strings.TrimSuffix(root, "/scim/v2")
	}
	authURL := root + "/v1/authenticate"
	req, err := http.NewRequest(http.MethodGet, authURL, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(username, password)
	client := http.DefaultClient
	if insecureSkipVerify {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		client = &http.Client{Transport: tr}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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
