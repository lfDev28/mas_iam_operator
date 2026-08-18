package preflight

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/masadmin"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
)

type Status string

const (
	StatusOK    Status = "ok"
	StatusWarn  Status = "warn"
	StatusError Status = "error"
)

type Input struct {
	Namespace  string
	MASBaseURL string
	// CheckOIDCEndpoint probes whether MAS exposes the OIDC external-IdP
	// Admin API (/config/oidc/default). MAS 9.0 does not ship the endpoint —
	// the unauthenticated probe 404s with exception id AIUCO1022E — while
	// 9.1+ answers 401 (endpoint exists, auth required). Requires MASBaseURL.
	CheckOIDCEndpoint bool
	// APITokenName/APITokenValue, when both set alongside MASBaseURL, verify
	// the MAS API key can authenticate AND is authorized for the SCIM API.
	// Keys minted with all-false permission flags still authenticate but get
	// 403 AIUCO1003E on SCIM endpoints, which otherwise only surfaces as a
	// silent 13-minute timeout in the scim profile-bootstrap Job.
	APITokenName  string
	APITokenValue string
}

type Result struct {
	Name    string
	Status  Status
	Message string
}

type StorageChoice struct {
	Name        string
	Reason      string
	IsDefault   bool
	Recommended bool
}

type StorageRanking struct {
	Default     string
	Recommended string
	Choices     []StorageChoice
	Warning     string
}

type Report struct {
	Namespace     string
	ClusterUser   string
	ClusterServer string
	MASHost       string
	Storage       StorageRanking
	Results       []Result
}

func (c StorageChoice) Display() string {
	parts := []string{c.Name}
	if c.Recommended {
		parts = append(parts, "recommended")
	}
	if c.IsDefault {
		parts = append(parts, "default")
	}
	if c.Reason != "" {
		parts = append(parts, c.Reason)
	}
	return strings.Join(parts, " | ")
}

func (r Report) HasFailures() bool {
	for _, result := range r.Results {
		if result.Status == StatusError {
			return true
		}
	}
	return false
}

func Run(ctx context.Context, client *oc.Client, input Input) Report {
	report := Report{Namespace: input.Namespace}

	if err := client.CheckAvailable(); err != nil {
		report.Results = append(report.Results, Result{
			Name:    "oc",
			Status:  StatusError,
			Message: err.Error(),
		})
		return report
	}
	report.Results = append(report.Results, Result{
		Name:    "oc",
		Status:  StatusOK,
		Message: "oc CLI found on PATH",
	})

	user, server, err := client.WhoAmI(ctx)
	if err != nil {
		report.Results = append(report.Results, Result{
			Name:    "cluster-login",
			Status:  StatusError,
			Message: err.Error(),
		})
		return report
	}
	report.ClusterUser = user
	report.ClusterServer = server
	report.Results = append(report.Results, Result{
		Name:    "cluster-login",
		Status:  StatusOK,
		Message: fmt.Sprintf("logged in as %s against %s", user, server),
	})

	classes, err := client.StorageClasses(ctx)
	if err != nil {
		report.Results = append(report.Results, Result{
			Name:    "storage-classes",
			Status:  StatusError,
			Message: err.Error(),
		})
	} else if len(classes) == 0 {
		report.Results = append(report.Results, Result{
			Name:    "storage-classes",
			Status:  StatusError,
			Message: "no storage classes were returned by oc get sc",
		})
	} else {
		report.Storage = RankStorageClasses(classes)
		report.Results = append(report.Results, Result{
			Name:    "storage-classes",
			Status:  StatusOK,
			Message: fmt.Sprintf("discovered %d storage classes; recommended=%s", len(classes), report.Storage.Recommended),
		})
		if report.Storage.Warning != "" {
			report.Results = append(report.Results, Result{
				Name:    "storage-selection",
				Status:  StatusWarn,
				Message: report.Storage.Warning,
			})
		}
	}

	if strings.TrimSpace(input.MASBaseURL) == "" {
		report.Results = append(report.Results, Result{
			Name:    "mas-base-url",
			Status:  StatusWarn,
			Message: "skipped because no SCIM-backed component was selected",
		})
		return report
	}

	masHost, err := ParseMASHost(input.MASBaseURL)
	if err != nil {
		report.Results = append(report.Results, Result{
			Name:    "mas-base-url",
			Status:  StatusError,
			Message: err.Error(),
		})
		return report
	}
	report.MASHost = masHost
	report.Results = append(report.Results, Result{
		Name:    "mas-base-url",
		Status:  StatusOK,
		Message: fmt.Sprintf("parsed MAS host %s", masHost),
	})

	// Direct MAS probes run before the route lookup: the lookup warns and
	// returns early on oc errors, and a transient oc blip must not skip these.
	if input.CheckOIDCEndpoint {
		report.Results = append(report.Results, CheckOIDCEndpoint(ctx, masHTTPClient(), input.MASBaseURL))
	}
	if input.APITokenName != "" && input.APITokenValue != "" {
		report.Results = append(report.Results, CheckAPIKey(ctx, masHTTPClient(), input.MASBaseURL, input.APITokenName, input.APITokenValue))
	}

	routes, err := client.RoutesByHost(ctx, masHost)
	if err != nil {
		report.Results = append(report.Results, Result{
			Name:    "mas-route-lookup",
			Status:  StatusWarn,
			Message: fmt.Sprintf("route lookup could not be completed for host %s: %v", masHost, err),
		})
		return report
	}

	switch len(routes) {
	case 0:
		report.Results = append(report.Results, Result{
			Name:    "mas-route-lookup",
			Status:  StatusWarn,
			Message: fmt.Sprintf("no OpenShift route matched host %s; MAS CA auto-detect may need manual route details", masHost),
		})
	case 1:
		report.Results = append(report.Results, Result{
			Name:    "mas-route-lookup",
			Status:  StatusOK,
			Message: fmt.Sprintf("matched route %s/%s", routes[0].Namespace, routes[0].Name),
		})
	default:
		refs := make([]string, 0, len(routes))
		for _, route := range routes {
			refs = append(refs, fmt.Sprintf("%s/%s", route.Namespace, route.Name))
		}
		report.Results = append(report.Results, Result{
			Name:    "mas-route-lookup",
			Status:  StatusWarn,
			Message: fmt.Sprintf("multiple routes matched host %s: %s", masHost, strings.Join(refs, ", ")),
		})
	}

	return report
}

// masHTTPClient returns the client used for direct MAS probes. TLS
// verification is skipped because MAS routes are commonly signed by a
// cluster-local CA that is not in the system trust store — same policy as
// internal/masadmin's WithInsecureTLS, which the installer always enables.
func masHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// CheckOIDCEndpoint probes GET {root}/config/oidc/default without auth, where
// {root} is MASBaseURL minus the /scim/v2 suffix. MAS 9.1+ answers 401
// (endpoint exists, auth required); MAS 9.0 answers 404 with exception id
// AIUCO1022E because the OIDC external-IdP Admin API does not exist there.
// Network errors only warn — preflight must not hard-fail on a transient
// cluster blip.
func CheckOIDCEndpoint(ctx context.Context, httpClient *http.Client, masBaseURL string) Result {
	const name = "mas-oidc-endpoint"
	probeURL := masadmin.StripSCIMSuffix(masBaseURL) + "/config/oidc/default"
	host := probeURL
	if parsed, err := url.Parse(probeURL); err == nil && parsed.Hostname() != "" {
		host = parsed.Hostname()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return Result{Name: name, Status: StatusWarn, Message: fmt.Sprintf("could not build OIDC endpoint probe for %s: %v", probeURL, err)}
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return Result{Name: name, Status: StatusWarn, Message: fmt.Sprintf("could not reach %s: %v; skipping OIDC endpoint check", probeURL, err)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))

	switch {
	case resp.StatusCode == http.StatusNotFound:
		code := ""
		if strings.Contains(string(body), "AIUCO1022E") {
			code = " AIUCO1022E"
		}
		return Result{
			Name:    name,
			Status:  StatusError,
			Message: fmt.Sprintf("MAS at %s does not expose the OIDC external-IdP API (HTTP 404%s; requires MAS 9.1+). Re-run with --mas-auth-providers ldap,saml or upgrade MAS.", host, code),
		}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden ||
		(resp.StatusCode >= 200 && resp.StatusCode < 300):
		return Result{Name: name, Status: StatusOK, Message: fmt.Sprintf("MAS at %s exposes the OIDC external-IdP API", host)}
	default:
		return Result{Name: name, Status: StatusWarn, Message: fmt.Sprintf("unexpected HTTP %d from %s; cannot confirm the OIDC external-IdP API is available", resp.StatusCode, probeURL)}
	}
}

// CheckAPIKey verifies the MAS API key in two steps: mint a JWT via GET
// {root}/v1/authenticate with basic auth, then GET {base}/Profiles with that
// JWT as a Bearer token ({base} keeps the /scim/v2 suffix). Authentication
// succeeding proves nothing about SCIM access — a key minted with all-false
// permission flags authenticates but gets 403 AIUCO1003E on SCIM endpoints;
// the SCIM API requires userAdmin + systemAdmin. Network errors only warn.
func CheckAPIKey(ctx context.Context, httpClient *http.Client, masBaseURL, tokenName, tokenValue string) Result {
	const name = "mas-api-key"
	authURL := masadmin.StripSCIMSuffix(masBaseURL) + "/v1/authenticate"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return Result{Name: name, Status: StatusWarn, Message: fmt.Sprintf("could not build authenticate request for %s: %v", authURL, err)}
	}
	req.SetBasicAuth(tokenName, tokenValue)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return Result{Name: name, Status: StatusWarn, Message: fmt.Sprintf("could not reach %s: %v; skipping API key check", authURL, err)}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{
			Name:    name,
			Status:  StatusError,
			Message: fmt.Sprintf("MAS API key %s failed to authenticate (HTTP %d). Check %s / %s.", tokenName, resp.StatusCode, config.EnvMASAPITokenName, config.EnvMASAPITokenValue),
		}
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Token == "" {
		return Result{Name: name, Status: StatusWarn, Message: fmt.Sprintf("authenticate response from %s contained no token; cannot verify SCIM authorization", authURL)}
	}

	profilesURL := strings.TrimRight(strings.TrimSpace(masBaseURL), "/") + "/Profiles"
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, profilesURL, nil)
	if err != nil {
		return Result{Name: name, Status: StatusWarn, Message: fmt.Sprintf("could not build SCIM probe for %s: %v", profilesURL, err)}
	}
	req.Header.Set("Authorization", "Bearer "+payload.Token)
	req.Header.Set("Accept", "application/json")
	resp, err = httpClient.Do(req)
	if err != nil {
		return Result{Name: name, Status: StatusWarn, Message: fmt.Sprintf("could not reach %s: %v; skipping SCIM authorization check", profilesURL, err)}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
	resp.Body.Close()

	switch {
	// 404 still proves authorization: an all-false-permissions key gets 403
	// AIUCO1003E before any routing/resource lookup happens.
	case (resp.StatusCode >= 200 && resp.StatusCode < 300) || resp.StatusCode == http.StatusNotFound:
		return Result{Name: name, Status: StatusOK, Message: fmt.Sprintf("MAS API key %s authenticated and is authorized for the SCIM API", tokenName)}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return Result{
			Name:    name,
			Status:  StatusError,
			Message: fmt.Sprintf("MAS API key %s authenticated but is not authorized for the SCIM API (HTTP %d AIUCO1003E). Recreate the key with userAdmin and systemAdmin permissions.", tokenName, resp.StatusCode),
		}
	default:
		return Result{Name: name, Status: StatusWarn, Message: fmt.Sprintf("unexpected HTTP %d from %s; cannot confirm SCIM authorization", resp.StatusCode, profilesURL)}
	}
}

func RankStorageClasses(classes []oc.StorageClass) StorageRanking {
	sorted := append([]oc.StorageClass(nil), classes...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	ranking := StorageRanking{}
	remaining := map[string]oc.StorageClass{}
	for _, class := range sorted {
		remaining[class.Name] = class
		if class.IsDefault {
			ranking.Default = class.Name
		}
	}

	addChoice := func(name, reason string) {
		class, ok := remaining[name]
		if !ok {
			return
		}
		delete(remaining, name)
		choice := StorageChoice{
			Name:      class.Name,
			Reason:    reason,
			IsDefault: class.IsDefault,
		}
		ranking.Choices = append(ranking.Choices, choice)
	}

	for _, name := range []string{"ocs-external-storagecluster-ceph-rbd", "rook-ceph-block"} {
		addChoice(name, "preferred block/RBD class")
	}

	for _, class := range sorted {
		if _, ok := remaining[class.Name]; !ok {
			continue
		}
		lower := strings.ToLower(class.Name)
		if strings.Contains(lower, "rbd") || strings.Contains(lower, "block") {
			addChoice(class.Name, "matches block/RBD preference")
		}
	}

	if ranking.Default != "" {
		addChoice(ranking.Default, "cluster default")
	}

	for _, class := range sorted {
		if _, ok := remaining[class.Name]; ok {
			addChoice(class.Name, "available storage class")
		}
	}

	if len(ranking.Choices) > 0 {
		ranking.Recommended = ranking.Choices[0].Name
		for idx := range ranking.Choices {
			if ranking.Choices[idx].Name == ranking.Recommended {
				ranking.Choices[idx].Recommended = true
			}
		}
	}

	if ranking.Default != "" && ranking.Recommended != "" {
		defaultLower := strings.ToLower(ranking.Default)
		if strings.Contains(defaultLower, "cephfs") && ranking.Recommended != ranking.Default &&
			(strings.Contains(strings.ToLower(ranking.Recommended), "rbd") || strings.Contains(strings.ToLower(ranking.Recommended), "block")) {
			ranking.Warning = fmt.Sprintf("default storage class %s looks filesystem-backed; preferring %s for PostgreSQL", ranking.Default, ranking.Recommended)
		}
	}

	return ranking
}

func ParseMASHost(rawURL string) (string, error) {
	if strings.TrimSpace(rawURL) == "" {
		return "", fmt.Errorf("MAS base URL is required (%s)", config.EnvMASBaseURL)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse MAS base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("MAS base URL must include scheme and host, for example https://api.<mas-instance>.<domain>/scim/v2")
	}
	if !strings.Contains(parsed.Path, "/scim/v2") {
		return "", fmt.Errorf("MAS base URL must include /scim/v2")
	}
	return parsed.Hostname(), nil
}

func Print(w io.Writer, report Report) {
	if report.ClusterServer != "" || report.ClusterUser != "" || report.Namespace != "" {
		if report.ClusterServer != "" {
			fmt.Fprintf(w, "[context] cluster=%s\n", report.ClusterServer)
		}
		if report.ClusterUser != "" {
			fmt.Fprintf(w, "[context] user=%s\n", report.ClusterUser)
		}
		if report.Namespace != "" {
			fmt.Fprintf(w, "[context] namespace=%s\n", report.Namespace)
		}
		if report.Storage.Default != "" {
			fmt.Fprintf(w, "[context] default_storage_class=%s\n", report.Storage.Default)
		}
		if report.Storage.Recommended != "" {
			fmt.Fprintf(w, "[context] recommended_storage_class=%s\n", report.Storage.Recommended)
		}
		if report.MASHost != "" {
			fmt.Fprintf(w, "[context] mas_host=%s\n", report.MASHost)
		}
	}
	for _, result := range report.Results {
		fmt.Fprintf(w, "[%s] %s: %s\n", result.Status, result.Name, result.Message)
	}
}
