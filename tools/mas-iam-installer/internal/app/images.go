package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/images"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/installer"
)

type imagesOptions struct {
	root            *RootOptions
	components      string
	mirror          string
	format          string
	local           bool
	includeOptional bool
	installerImage  string
}

const imagesFormats = "list, mapping, mirrorset"

func newImagesCommand(root *RootOptions) *cobra.Command {
	opts := &imagesOptions{root: root}
	command := &cobra.Command{
		Use:   "images",
		Short: "List the images an install pulls, and mirror them to your own registry",
		Long: `List every container image an install of the chosen components pulls on the
cluster. With --mirror, print what you need to serve them from your own
registry instead, for clusters that cannot reach quay.io, docker.io or ghcr.io,
or that hit Docker Hub's rate limit:

  mas-est images --mirror registry.example.com/mas-est --format mapping > mapping.txt
  oc image mirror --keep-manifest-list=true -f mapping.txt
  mas-est images --mirror registry.example.com/mas-est --format mirrorset | oc apply -f -

The mirror set (an ImageTagMirrorSet and an ImageDigestMirrorSet) makes every
node pull these images from the mirror. It needs cluster-admin, and the Machine
Config Operator rolls it out to each node, which can take a while. Then run
mas-est install as usual: image names in the install do not change.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.OutOrStdout())
		},
	}
	flags := command.Flags()
	flags.StringVar(&opts.components, "components", "ldap,keycloak,scim,s3,smtp", "Install components to list images for (same values as install --components)")
	flags.StringVar(&opts.mirror, "mirror", "", "Mirror registry and optional path prefix, e.g. registry.example.com/mas-est")
	flags.StringVar(&opts.format, "format", "list", "Output: list (source refs), mapping (oc image mirror -f input), mirrorset (ImageTagMirrorSet + ImageDigestMirrorSet YAML)")
	flags.BoolVar(&opts.local, "local", false, "The install will run with --local, so the in-cluster installer Job image is not pulled")
	flags.BoolVar(&opts.includeOptional, "include-optional", false, "Include images pulled only by non-default chart settings (PostgreSQL metrics, volume permissions)")
	flags.StringVar(&opts.installerImage, "installer-image", defaultInstallerImage(), "Installer Job image, if install will be given --installer-image")
	return command
}

func (o *imagesOptions) run(out io.Writer) error {
	components, err := config.ParseInstallComponents(o.components)
	if err != nil {
		return err
	}
	if len(components) == 0 {
		return fmt.Errorf("--components is empty")
	}
	format := strings.ToLower(strings.TrimSpace(o.format))
	mirror := strings.TrimRight(strings.TrimSpace(o.mirror), "/")
	switch format {
	case "list":
	case "mapping", "mirrorset":
		if mirror == "" {
			return fmt.Errorf("--format %s needs --mirror <registry>[/<prefix>]", format)
		}
		if strings.Contains(mirror, "://") {
			return fmt.Errorf("--mirror is a registry host and optional path (registry.example.com/mas-est), not a URL")
		}
	default:
		return fmt.Errorf("unknown --format %q; supported values are %s", o.format, imagesFormats)
	}

	paths, err := installer.DiscoverPaths(o.root.RepoRoot)
	if err != nil {
		return err
	}
	all, err := images.Load(paths.RepoRoot)
	if err != nil {
		return err
	}
	selected := images.ForComponents(all, components, !o.local, o.includeOptional)
	// images.yaml pins the CLI's release tag; the Job runs this CLI's version
	// (an rc or dev build has its own tag).
	for i := range selected {
		if hasString(selected[i].Components, images.ComponentInstaller) {
			selected[i].Ref = o.installerImage
		}
	}

	switch format {
	case "list":
		for _, img := range selected {
			fmt.Fprintln(out, img.Ref)
		}
	case "mapping":
		fmt.Fprintf(out, "# oc image mirror --keep-manifest-list=true -f <this file>\n# mas-est %s images for: %s\n",
			strings.TrimPrefix(defaultInstallerImage(), installerImageRepository+":"), strings.Join(components, ","))
		for _, img := range selected {
			fmt.Fprintf(out, "%s=%s\n", img.Ref, images.MirrorRef(img.Ref, mirror))
		}
	case "mirrorset":
		writeMirrorSets(out, selected, mirror)
	}
	return nil
}

func writeMirrorSets(out io.Writer, selected []images.Image, mirror string) {
	repos := images.Repos(selected)
	for i, kind := range []string{"ImageTagMirrorSet", "ImageDigestMirrorSet"} {
		if i > 0 {
			fmt.Fprintln(out, "---")
		}
		listKey := "imageTagMirrors"
		if kind == "ImageDigestMirrorSet" {
			listKey = "imageDigestMirrors"
		}
		fmt.Fprintf(out, "apiVersion: config.openshift.io/v1\nkind: %s\nmetadata:\n  name: mas-est\nspec:\n  %s:\n", kind, listKey)
		for _, repo := range repos {
			fmt.Fprintf(out, "    - source: %s\n      mirrors:\n        - %s\n", repo, images.MirrorRef(repo, mirror))
		}
	}
}

func hasString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
