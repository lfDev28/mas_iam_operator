# Screenshots to capture

Temporary checklist. Save each image as `docs/images/<file>` at about 1400 px wide, then delete this file.

| File | Screen | Must be visible | Used in |
|---|---|---|---|
| `mas-login-providers.png` | MAS login page | The three sign-in options: LDAP, OIDC and SAML | GUIDE, Log in with the demo users |
| `mas-admin-providers-configured.png` | MAS administration, Configurations, the SSO or identity provider panel | LDAP, OIDC and SAML each shown as configured | GUIDE, What gets installed |
| `keycloak-scim-users-group.png` | Keycloak admin console, realm `maximo`, Groups, `mas-scim-users`, Members tab | `scim.user1` and `scim.user2` as members; the realm name in the header | GUIDE, Add a SCIM user |
| `scim-user-in-mas-and-manage.png` | Two panes: MAS Users showing `scim.user1`, and Manage Users showing the same user | The username in both, and the Manage entitlement in MAS | GUIDE, Add a SCIM user |
| `minio-buckets.png` | MinIO console at the `mas-minio-console` route, Buckets page | `mas-s3-demo`, `mas-s3-demobackup`, `mas-s3-demorecovery` | GUIDE, Connect S3 to Manage |
| `mailpit-inbox.png` | Mailpit web UI at the `mas-mailpit` route | At least one captured message from MAS in the list | GUIDE, Connect SMTP |

Redact the cluster hostname in the browser address bar before saving.
