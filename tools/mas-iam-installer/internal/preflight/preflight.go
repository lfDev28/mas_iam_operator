package preflight

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
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
