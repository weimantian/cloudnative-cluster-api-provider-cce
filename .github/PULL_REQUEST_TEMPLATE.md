---
name: Pull Request
about: Submit changes to cloudnative-cluster-api-provider-cce
title: ""
labels: []
assignees: []
---

## Related Issue

Closes #<!-- issue number -->

## Change Description

What this PR does and why.

## DCO Confirmation

- [ ] I agree to the [Developer Certificate of Origin](https://developercertificate.org/) and confirm that all my commits carry a `Signed-off-by:` trailer (`git commit -s`).

## Checklist

- [ ] Code compiles and `make lint` passes
- [ ] Unit / envtest tests added or updated and passing (`make test`)
- [ ] Webhook / API changes documented
- [ ] README / docs updated (if user-facing)
- [ ] No secrets or credentials committed (secret-scan workflow)
- [ ] Deploy scripts validated if changed (`deploy/scripts/*`, iac-validate workflow)
- [ ] Markdown lint passes

## Test Evidence

Describe how the change was tested (unit, envtest, manual, e2e), including any
`clusterctl describe cluster` output.
