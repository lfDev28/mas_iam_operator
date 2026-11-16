// Package images reads images.yaml, the repo's one list of container images,
// so `mas-est images` can say which images an install pulls and how to mirror
// them.
package images

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileName is images.yaml's name at the repo root (and at the root of the
// runtime tree bootstrap extracts from the CLI image).
const FileName = "images.yaml"

// Component values that are not install components.
const (
	// ComponentInstaller is the in-cluster install Job (the CLI image).
	ComponentInstaller = "installer"
	// ComponentOperator is the OLM operator, bundle and catalog, pulled
	// whenever ldap or keycloak is installed.
	ComponentOperator = "operator"
)

// Image is one `images:` entry.
type Image struct {
	Ref        string
	Kind       string
	Archs      []string
	Components []string
	Optional   bool
	Note       string
}

// Load reads <root>/images.yaml.
func Load(root string) ([]Image, error) {
	path := filepath.Join(root, FileName)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w (runtime extracted by an older mas-est? run `mas-est update`)", path, err)
	}
	defer f.Close()
	list, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return list, nil
}

// Parse reads the flat images.yaml schema (see the file's header): entries
// under `images:` start with `- ref:` and carry scalar or [a, b] values. The
// `allowlist:` section is skipped: those refs are never pulled.
func Parse(r io.Reader) ([]Image, error) {
	var (
		out     []Image
		cur     *Image
		section string
		lineNo  int
	)
	flush := func() {
		if cur != nil && section == "images" {
			out = append(out, *cur)
		}
		cur = nil
	}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		lineNo++
		line := stripComment(sc.Text())
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && strings.HasSuffix(line, ":") {
			flush()
			section = strings.TrimSuffix(line, ":")
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ref:") {
			flush()
			cur = &Image{Ref: unquote(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ref:")))}
			continue
		}
		if cur == nil {
			continue
		}
		key, val, ok := strings.Cut(trimmed, ":")
		if !ok {
			return nil, fmt.Errorf("line %d: expected key: value", lineNo)
		}
		val = strings.TrimSpace(val)
		switch key {
		case "kind":
			cur.Kind = unquote(val)
		case "archs":
			cur.Archs = list(val)
		case "components":
			cur.Components = list(val)
		case "optional":
			cur.Optional = unquote(val) == "true"
		case "note":
			cur.Note = unquote(val)
		}
	}
	flush()
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func stripComment(line string) string {
	if i := strings.Index(line, " #"); i >= 0 {
		return line[:i]
	}
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return ""
	}
	return line
}

func unquote(s string) string {
	return strings.Trim(strings.TrimSpace(s), `"'`)
}

func list(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") {
		if s == "" {
			return nil
		}
		return []string{unquote(s)}
	}
	var out []string
	for _, item := range strings.Split(strings.Trim(s, "[]"), ",") {
		if item = unquote(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// ForComponents returns the images an install of the given (normalized)
// install components pulls on the cluster, in file order. inCluster adds the
// installer Job image; optional entries are left out unless includeOptional.
func ForComponents(all []Image, components []string, inCluster, includeOptional bool) []Image {
	want := map[string]bool{}
	for _, c := range components {
		want[c] = true
	}
	if want["ldap"] || want["keycloak"] {
		want[ComponentOperator] = true
	}
	if inCluster {
		want[ComponentInstaller] = true
	}
	var out []Image
	for _, img := range all {
		if img.Optional && !includeOptional {
			continue
		}
		for _, c := range img.Components {
			if want[c] {
				out = append(out, img)
				break
			}
		}
	}
	return out
}

// SplitRef splits registry/path[:tag|@digest] into registry, path (no
// registry) and the suffix including its ':' or '@'.
func SplitRef(ref string) (registry, path, suffix string) {
	name := ref
	if i := strings.Index(name, "@"); i >= 0 {
		name, suffix = name[:i], name[i:]
	} else if i := strings.LastIndex(name, ":"); i > strings.LastIndex(name, "/") {
		name, suffix = name[:i], name[i:]
	}
	registry, path, ok := strings.Cut(name, "/")
	if !ok || !(strings.ContainsAny(registry, ".:") || registry == "localhost") {
		// Docker Hub shorthand (library/x, user/x, x).
		registry, path = "docker.io", name
		if !strings.Contains(path, "/") {
			path = "library/" + path
		}
	}
	return registry, path, suffix
}

// Repo returns ref without its tag or digest.
func Repo(ref string) string {
	registry, path, _ := SplitRef(ref)
	return registry + "/" + path
}

// MirrorRef maps ref into mirror (registry[/prefix]), keeping the path, so
// quay.io/a/b:t under registry.example.com/mas-est becomes
// registry.example.com/mas-est/a/b:t.
func MirrorRef(ref, mirror string) string {
	_, path, suffix := SplitRef(ref)
	return strings.TrimRight(mirror, "/") + "/" + path + suffix
}

// Repos returns the distinct source repositories of list, sorted.
func Repos(list []Image) []string {
	seen := map[string]bool{}
	var out []string
	for _, img := range list {
		if r := Repo(img.Ref); !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}
