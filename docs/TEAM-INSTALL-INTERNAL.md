# MAS External Services Toolkit Internal Team Note

This is the short internal note I want the team to use for the first rollout of `mas-est`.

## Why I Built It

The problem I was trying to solve was straightforward: getting a usable IAM test environment together for MAS cases was taking too long and too many manual steps.

For support work, that usually means we need some mix of the following in place before we can even start testing the real issue:

- an IdP
- LDAP-backed users and groups
- SCIM provisioning flow
- certificates and trust wiring between the components
- demo users and other seed data

We were effectively rebuilding that setup by hand too often. `mas-est` exists to turn that into one repeatable path.

## What This Tool Is

`mas-est` is an all-in-one helper for standing up a working MAS External Services Toolkit plus SCIM bridge setup on OpenShift.

In practical terms, it gives us a quick way to stand up:

- Keycloak as the test IdP
- OpenLDAP with seeded users and groups
- PostgreSQL for the IAM stack
- SCIM bridge wiring into MAS
- certificate and CA bundle setup between the components
- bootstrap jobs and demo data needed to get moving faster

That gives us a fast base for IAM cases across LDAP, SCIM, and SAML-oriented testing, provided the scenario is not dependent on a vendor-specific IdP feature.

The aim is not to perfectly mirror every customer identity stack. The aim is to give us a fast, repeatable IAM lab that is close enough to reproduce common user flows and isolate where a problem actually lives.

It is not meant to be a perfect or final product yet. I am releasing it to the team because it is already useful, I am using it myself, and I want us all using the same path instead of everyone carrying slightly different install notes.

## Current Position

This is an internal-first release.

It is still a work in progress, but it has now been exercised through real cluster runs rather than just local development.

What I have tested so far:

- fresh-cluster install validation end to end
- CLI bootstrap from the published container image
- uninstall and reinstall on an existing cluster
- operator install reaching `Succeeded`
- Keycloak, OpenLDAP, PostgreSQL, and SCIM bridge becoming healthy
- MAS profile bootstrap completing successfully
- SCIM bridge syncing against MAS and handling existing users cleanly
- storage selection on different block/RBD storage-class families

That is enough for an internal team rollout. I would still treat it as something we improve as we use it.

## What I Want It Used For

Use `mas-est` when you need to get up and running quickly for IAM cases, especially:

- reproducing SCIM issues
- reproducing generic user lifecycle issues
- testing LDAP and IdP-backed login flows
- testing SAML-oriented IAM flows where the issue is not tied to a specific enterprise IdP product
- creating sample SCIM users and resources
- validating bridge behavior
- resetting and rerunning an IAM test environment
- shortening the setup time for support and troubleshooting work

The main value here is speed and consistency. Instead of piecing the install together manually, the tool gives us one repeatable path.

## Why This Helps With Customer Cases

For a lot of IAM cases, the first question is whether the issue is really specific to the customer's enterprise IdP, or whether it is a more general lifecycle or provisioning problem.

That is where this tool helps.

Keycloak is not Entra, but it is a good open-source stand-in for the kinds of flows we care about most often:

- user creation and updates
- enable/disable flows
- LDAP-backed identity data
- SAML-adjacent trust and login behaviour
- SCIM provisioning into MAS
- certificate and trust chain behavior

That means we can use `mas-est` to answer questions like:

- can MAS consume the user correctly at all?
- does the SCIM update behave as expected?
- is the issue in the identity flow, or in the customer's IdP-specific mapping/config?
- can we reproduce the behaviour outside the customer's exact Entra tenant?

It gives us a controlled test setup so we can separate generic IAM behaviour from customer-environment specifics.

## What It Is Not

At this stage, I would not position it as a polished general-purpose product installer.

It is an internal support accelerator. It is meant to help us get to a working IAM and SCIM test environment faster, with fewer manual steps and less guesswork.

It also has clear limits:

- it does not reproduce every enterprise IdP feature
- it does not model every Entra-specific attribute mapping or provisioning expression
- it does not prove behaviour that is unique to a customer's exact IdP product or tenant configuration

So if the problem is tied to an IdP-specific feature, we still need to validate that in the customer flow itself. `mas-est` helps us narrow the problem space, not replace customer-specific testing.

## How The Team Should Use It

The supported flow is:

1. bootstrap the local `mas-est` command from the container image
2. run `mas-est preflight`
3. run `mas-est install`
4. use `mas-est status` and `mas-est logs` for checks
5. use `mas-est uninstall` when you want to reset and rerun

The full install steps are in:

- `README.md`
- `docs/BETA-QUICKSTART.md`
- `docs/BETA-KNOWN-LIMITATIONS.md`
- `docs/INSTALL-ALL-IN-ONE.md`

Use `docs/BETA-INSTALL-TUTORIAL.md` only as a validation capture checklist when preparing screenshots or release notes.

## What Has Been Proven

These are the things I am comfortable claiming because they have been tested:

- the CLI bootstrap works
- the all-in-one install flow works
- the uninstall and reinstall flow works
- the operator path works from the published catalog
- PostgreSQL and the SCIM bridge can be pinned to explicit storage classes
- the SCIM bridge deployment no longer carries the broken helper sidecar that was blocking readiness
- the install now does proper preflight, status, and logs handling

## Current Support Boundary

The way I would frame this internally is:

- supported for building a fast IAM test rig for support and troubleshooting
- useful for SCIM, LDAP, IdP, certificate, and general user-flow validation
- not a claim that we emulate Entra feature-for-feature
- not a claim that every SAML or SCIM issue can be reproduced without the customer's own IdP

If a case depends on Entra-only behavior, proprietary provisioning expressions, or tenant-specific policy, we should expect to use `mas-est` for partial reproduction and isolation, not as the final proof by itself.

## Known Reality

There are still rough edges, and I would rather state them plainly:

- this tool still depends on the published operator and bridge artifacts being current
- cluster prerequisites still matter, especially storage classes and image registry health
- some environments will still need explicit storage choices instead of relying on the default
- it is good enough for team use now, but I still expect to tighten it up as people use it

## What I Need Back From The Team

If something fails, I want the failure reported with evidence, not just "install failed".

Please capture:

```bash
oc whoami
oc whoami --show-server
mas-est preflight
mas-est status --namespace mas-est
mas-est logs --namespace mas-est --component operator
mas-est logs --namespace mas-est --component keycloak
mas-est logs --namespace mas-est --component bridge
```

That is usually enough for me to tell whether the issue is:

- cluster prerequisite
- published artifact
- repo logic
- bad input

## Why I Am Releasing It Now

The main reason is practical: it is already saving time.

I would rather have the team using one tested, supported internal tool now than keep everyone on a mix of manual steps, stale notes, and one-off commands. This release is the point where it becomes useful as shared tooling, even though I still consider it a work in progress.
