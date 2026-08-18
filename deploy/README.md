# deploy — Deployment Scripts

This directory contains the deployment tooling for `cloudnative-cluster-api-provider-cce`.

> **Status note**: the provider is under development (incubating). The scripts below
> describe the target deployment flow and become fully executable once a release
> (with `metadata.yaml` + `infrastructure-components.yaml`) is published.
>
> 中文版 / Chinese: [README.zh-CN.md](README.zh-CN.md)

## Layout

| Path | Purpose |
|---|---|
| `scripts/deploy-provider.sh` | Install the provider on a management cluster via `clusterctl init --infrastructure cce`, and create the per-cluster credentials Secret. |
| `scripts/destroy.sh` | **Destructive**: delete workload cluster(s) and remove the provider. Requires interactive confirmation. |
| `variables.md` | Full list of environment variables consumed by the scripts (no hardcoded secrets). |
| `scripts/` (parent, i.e. `scripts/`) | Auxiliary helpers, e.g. `scripts/check-prerequisites.sh`. |

## Security Rules

- All sensitive values (`CCE_ACCESS_KEY`, `CCE_SECRET_KEY`, ...) are read **only** from
  environment variables or interactive prompts. **Never hardcode defaults.**
- The `secret-scan` CI workflow (gitleaks) scans this directory on every push.
- Scripts are validated in CI (`iac-validate` workflow: `bash -n` + `shellcheck`).

## Cleanup

Always run `scripts/destroy.sh` after evaluation to avoid continued billing
(CCE cluster + nodes are billed on demand).
