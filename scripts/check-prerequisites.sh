#!/usr/bin/env bash
# -----------------------------------------------------------------------------
# check-prerequisites.sh
#
# Purpose  : Verify the local environment prerequisites for deploying
#            cloudnative-cluster-api-provider-cce.
# Usage    : scripts/check-prerequisites.sh [--clusterctl-version v1.x]
# Depends  : bash, kubectl, clusterctl (optional: kind for local management cluster)
# Idempotent: yes — read-only checks, no state changes.
# -----------------------------------------------------------------------------
set -euo pipefail

CLUSTERCTL_VERSION="${CLUSTERCTL_VERSION:-v1.9.0}"
fail=0

say_ok()   { printf '[ OK ] %s\n' "$1"; }
say_miss() { printf '[MISS] %s\n' "$1"; fail=1; }

echo "== Checking prerequisites =="

# kubectl
if command -v kubectl >/dev/null 2>&1; then
  say_ok "kubectl found: $(kubectl version --client -o yaml 2>/dev/null | grep -m1 gitVersion || true)"
else
  say_miss "kubectl not found (install: https://kubernetes.io/docs/tasks/tools/)"
fi

# clusterctl
if command -v clusterctl >/dev/null 2>&1; then
  say_ok "clusterctl found: $(clusterctl version -o short 2>/dev/null || true)"
else
  say_miss "clusterctl not found (install: https://cluster-api.sigs.k8s.io/user/quick-start.html#install-clusterctl)"
fi

# management cluster reachability
if kubectl cluster-info >/dev/null 2>&1; then
  say_ok "management cluster reachable"
else
  say_miss "management cluster NOT reachable (is kubeconfig configured?)"
fi

# credentials (only presence, not values)
if [[ -n "${CCE_ACCESS_KEY:-}" && -n "${CCE_SECRET_KEY:-}" ]]; then
  say_ok "CCE_ACCESS_KEY / CCE_SECRET_KEY are set"
else
  say_miss "CCE_ACCESS_KEY and CCE_SECRET_KEY must be exported (see deploy/variables.md)"
fi

echo
if [[ "$fail" -eq 0 ]]; then
  echo "All prerequisites satisfied."
else
  echo "Some prerequisites are missing — fix the [MISS] items above."
  exit 1
fi
