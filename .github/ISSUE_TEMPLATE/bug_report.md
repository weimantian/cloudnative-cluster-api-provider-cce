---
name: Bug Report
about: Report a defect in cloudnative-cluster-api-provider-cce
title: "[Bug] "
labels: bug
assignees: ''
---

## Description

A clear and concise description of the problem.

## Expected Behavior

What should happen.

## Actual Behavior

What actually happens (include error messages, controller logs, conditions).

## Steps to Reproduce

1. ... (e.g. `kubectl apply -f workload-cluster.yaml`)
2. ...
3. ...

## Environment

- Provider version / commit:
- Cluster API version:
- Kubernetes version (management cluster):
- Huawei Cloud CCE type: `CCE` / `Turbo`
- Region:
- Management cluster type (`kind` / cloud):

## Logs / Artifacts

Relevant controller logs (redact any credentials), `clusterctl describe cluster`, CR `status.conditions`.

## Additional Context

Anything else that might help, e.g. links to the verification checklist
([docs/research-sources.md](https://github.com/ORG/cloudnative-cluster-api-provider-cce/blob/main/docs/research-sources.md)).
