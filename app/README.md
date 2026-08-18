# app — Example Applications

This directory is reserved for **example applications** that demonstrate how to use
`cloudnative-cluster-api-provider-cce` (e.g., a sample workload to verify a
provisioned CCE cluster).

> 中文版 / Chinese: [README.zh-CN.md](README.zh-CN.md)

## Rules (per Huawei Cloud solution developer kit governance §4.6)

- **Dependency manifest**: any example code must include a locked dependency
  manifest (e.g., `requirements.txt`, `pom.xml`, `go.mod`) so it runs on standard
  Huawei Cloud runtimes.
- **Configuration via environment**: all connection info and secrets must be read
  from environment variables — consistent with `deploy/variables.md`; never
  hardcode credentials.
- **Comments**: key logic must carry Chinese or English comments; functions and
  classes must have docstrings.

## Status

Empty — example applications will be added in a later milestone (see
[docs/requirements-design.md](../docs/requirements-design.md) for the roadmap).
