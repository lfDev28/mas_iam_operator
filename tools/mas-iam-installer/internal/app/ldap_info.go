package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/config"
	executil "github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/exec"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/oc"
	"github.com/lfDev28/mas_iam_operator/tools/mas-iam-installer/internal/ui"
)

const defaultLDAPBaseDN = "dc=demo,dc=local"

type ldapInfoOptions struct {
	namespace         string
	release           string
	baseDN            string
	showPassword      bool
	showUserPasswords bool
}

func newLDAPInfoCommand() *cobra.Command {
	defaults := config.LoadInstallConfigFromEnv()
	opts := &ldapInfoOptions{
		namespace: defaults.Namespace,
		release:   config.DefaultIAMRelease,
		baseDN:    defaultLDAPBaseDN,
	}

	command := &cobra.Command{
		Use:   "ldap-info",
		Short: "Show connection details for the bundled OpenLDAP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd.Context())
		},
	}

	flags := command.Flags()
	flags.StringVar(&opts.namespace, "namespace", opts.namespace, "Target namespace")
	flags.StringVar(&opts.release, "release", opts.release, "MAS EST IAM stack release name")
	flags.StringVar(&opts.baseDN, "base-dn", opts.baseDN, "LDAP base DN")
	flags.BoolVar(&opts.showPassword, "show-password", false, "Print the LDAP admin bind password")
	flags.BoolVar(&opts.showUserPasswords, "show-user-passwords", false, "Print seeded demo user passwords")

	return command
}

func (o *ldapInfoOptions) run(ctx context.Context) error {
	runner := executil.NewRunner()
	client := oc.NewClient(runner)
	if _, err := ensureClusterLogin(ctx, runner, client, ui.IsInteractive()); err != nil {
		return err
	}

	release := strings.TrimSpace(o.release)
	if release == "" {
		release = config.DefaultIAMRelease
	}
	namespace := strings.TrimSpace(o.namespace)
	if namespace == "" {
		namespace = config.DefaultNamespace
	}
	baseDN := strings.TrimSpace(o.baseDN)
	if baseDN == "" {
		baseDN = defaultLDAPBaseDN
	}

	adminSecretName := release + "-openldap-admin"
	userPasswordSecretName := release + "-openldap-user-passwords"
	tlsSecretName := release + "-keycloak-openldap-tls"
	serviceHost := fmt.Sprintf("%s-openldap.%s.svc.cluster.local", release, namespace)
	serviceURL := fmt.Sprintf("ldaps://%s:636", serviceHost)
	bindDN := fmt.Sprintf("cn=admin,%s", baseDN)

	adminSecret, err := client.SecretData(ctx, namespace, adminSecretName)
	if err != nil {
		return fmt.Errorf("read LDAP admin secret %s/%s: %w", namespace, adminSecretName, err)
	}

	adminPassword := adminSecret["password"]
	if adminPassword == "" {
		return fmt.Errorf("LDAP admin secret %s/%s does not contain key password", namespace, adminSecretName)
	}

	fmt.Fprintln(os.Stdout, "OpenLDAP connection details")
	fmt.Fprintf(os.Stdout, "Namespace: %s\n", namespace)
	fmt.Fprintf(os.Stdout, "Release: %s\n", release)
	fmt.Fprintf(os.Stdout, "Internal URL: %s\n", serviceURL)
	fmt.Fprintf(os.Stdout, "Host: %s\n", serviceHost)
	fmt.Fprintln(os.Stdout, "Port: 636")
	fmt.Fprintln(os.Stdout, "TLS: enabled")
	fmt.Fprintf(os.Stdout, "TLS secret: %s\n", tlsSecretName)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "Base DN: %s\n", baseDN)
	fmt.Fprintf(os.Stdout, "Users DN: ou=users,%s\n", baseDN)
	fmt.Fprintf(os.Stdout, "Groups DN: ou=groups,%s\n", baseDN)
	fmt.Fprintln(os.Stdout, "User attribute: uid")
	fmt.Fprintln(os.Stdout, "Group object class: groupOfUniqueNames")
	fmt.Fprintln(os.Stdout, "Group member attribute: uniqueMember")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "Bind DN: %s\n", bindDN)
	fmt.Fprintf(os.Stdout, "Bind secret: %s key password\n", adminSecretName)
	if o.showPassword {
		fmt.Fprintf(os.Stdout, "Bind password: %s\n", adminPassword)
	} else {
		fmt.Fprintln(os.Stdout, "Bind password: hidden (rerun with --show-password)")
	}
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Seeded demo users:")
	printDemoUserPasswords(ctx, client, namespace, userPasswordSecretName, o.showUserPasswords)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Cluster-internal MAS LDAP settings:")
	fmt.Fprintf(os.Stdout, "  URL: %s\n", serviceURL)
	fmt.Fprintf(os.Stdout, "  Bind DN: %s\n", bindDN)
	fmt.Fprintf(os.Stdout, "  Base DN: %s\n", baseDN)
	fmt.Fprintf(os.Stdout, "  Users DN: ou=users,%s\n", baseDN)
	fmt.Fprintf(os.Stdout, "  Groups DN: ou=groups,%s\n", baseDN)
	fmt.Fprintln(os.Stdout, "  SSL/TLS: enabled")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "Local test: oc -n %s port-forward svc/%s-openldap 1636:636\n", namespace, release)

	return nil
}

func printDemoUserPasswords(ctx context.Context, client *oc.Client, namespace, secretName string, showPasswords bool) {
	userPasswords, err := client.SecretData(ctx, namespace, secretName)
	if err != nil {
		fmt.Fprintf(os.Stdout, "  unable to read %s: %v\n", secretName, err)
		return
	}

	usernames := make([]string, 0, len(userPasswords))
	for username := range userPasswords {
		usernames = append(usernames, username)
	}
	sort.Strings(usernames)
	for _, username := range usernames {
		if showPasswords {
			fmt.Fprintf(os.Stdout, "  %s: %s\n", username, userPasswords[username])
		} else {
			fmt.Fprintf(os.Stdout, "  %s\n", username)
		}
	}
	if !showPasswords {
		fmt.Fprintf(os.Stdout, "  passwords hidden (rerun with --show-user-passwords; secret %s)\n", secretName)
	}
}
