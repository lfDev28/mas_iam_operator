# Beta Release Plan

## Objective

The next release should be an internal beta of `mas-iam` that is good at one thing: getting a working MAS IAM plus SCIM bridge test environment up quickly and repeatably for support work.

The immediate goal is not Entra parity.
The immediate goal is a stable install path and a reliable default demo flow that coworkers can use without hand-assembling the stack.

## Release Positioning

This should be positioned as:

- internal beta
- support and troubleshooting accelerator
- one supported default demo flow
- still under active development

This should not be positioned as:

- a final polished product
- a feature-for-feature replacement for Entra provisioning
- a complete identity simulation platform

## Beta Support Boundary

The beta should explicitly support:

- bootstrapping the local `mas-iam` command from the published container image
- `mas-iam preflight`
- `mas-iam install`
- `mas-iam status`
- `mas-iam logs`
- `mas-iam wipe`
- one default MAS SCIM profile flow
- demo users being created and synced into one MAS workspace/profile
- rapid recreation of a basic IAM lab for support cases

The beta should not promise:

- group-based SCIM profile assignment
- Entra-style provisioning scope by group
- expression-based attribute mapping like Entra `Switch(...)`
- full identity-provider parity across customer environments
- multi-profile isolation as the primary supported flow

## Why This Scope Is Correct

The install and bootstrap path is now close to being usable for coworkers.
The remaining product value is already high even without advanced routing features:

- stand up Keycloak, LDAP, PostgreSQL, and SCIM bridge quickly
- wire the certificates and trust correctly
- create demo profile data
- reproduce common IAM and SCIM flows
- reset and rerun environments quickly

That is enough for an internal beta.

Holding the beta until the bridge reaches Entra-level routing flexibility would slow down release without improving the main support workflow enough to justify the delay.

## Current Known Gaps

These are real gaps, but they should be treated as beta limitations rather than blockers unless they break the default install path.

### Bridge behavior

- SCIM profile routing is currently based on the Keycloak user attribute `masProfile`
- group membership is not currently used to choose the MAS SCIM profile
- the bridge does not evaluate Entra-style attribute mapping expressions
- adopted pre-existing MAS users may not receive fresh workspace assignment unless they are recreated cleanly

### Productization

- the published image and manifests still need steady validation across coworker environments
- docs still need to be tightened around the supported beta path
- tutorial-style documentation with screenshots still needs to be captured from a clean install run

## Must-Fix Before Beta

These are the items that should be treated as release blockers.

- the published `mas-iam-tool` image must bootstrap correctly on both `linux/amd64` and `linux/arm64`
- the published install path must avoid Docker Hub rate-limit dependencies for core bootstrap/install jobs
- a coworker should be able to complete bootstrap, preflight, install, and status checks without manual cluster patching
- the default demo user flow must create usable users in MAS with the expected workspace assignment
- user-facing docs must match the actual beta flow

## Beta Limitations To Document Clearly

These should be documented explicitly in team-facing material.

- the primary supported beta path is one default demo profile/workspace flow
- advanced profile routing is not the main supported beta scenario
- group-based routing is not implemented yet
- customer-specific IdP behavior may still need customer-side validation
- stale MAS-side user state may require cleanup and recreation in some cases

## Recommended Beta Messaging

The release should say, in substance:

- this is an internal beta
- it is meant to help us get a working IAM environment up quickly
- it is especially useful for SCIM, LDAP, certificate, and general IAM user-flow testing
- it does not claim Entra-equivalent feature coverage
- feedback should come back with evidence using `preflight`, `status`, and `logs`

## Post-Beta Roadmap

These are good next-phase enhancements, but they should not block the beta release.

### Bridge enhancements

- group-based profile routing
- precedence rules between `masProfile` and group-derived routing
- optional provisioning scoping by group
- better remediation for adopted users missing workspace assignment
- richer diagnostics when users exist in MAS but are not usable in the expected workspace

### CLI and UX

- more polished prompt flow
- cleaner summaries at the end of install/wipe runs
- improved validation and friendlier recovery guidance
- stronger install tutorial material with screenshots

### Documentation

- final internal beta quickstart
- screenshot-based install guide
- support playbook for common failures
- clearer explanation of where `mas-iam` helps and where customer-specific IdP validation is still required

## Practical Next Steps

1. Finish validating the published beta install path on coworker environments.
2. Tighten the team-facing docs around the one supported default flow.
3. Release `mas-iam` as an internal beta with explicit limitations.
4. Capture feedback from real support use.
5. Use that feedback to prioritize post-beta bridge enhancements.


## Related Planning Docs

- `specs/post-beta-roadmap.md`
