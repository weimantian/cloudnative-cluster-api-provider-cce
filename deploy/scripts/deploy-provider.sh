#!/usr/bin/env bash
# -----------------------------------------------------------------------------
# deploy-provider.sh
#
# Purpose  : Install cloudnative-cluster-api-provider-cce on a management
#            cluster (clusterctl init) and create the per-cluster credentials
#            Secret referenced by workload cluster manifests.
# Usage    : deploy/scripts/deploy-provider.sh
# Depends  : bash, kubectl, clusterctl, CCE_ACCESS_KEY/CCE_SECRET_KEY env vars
#            (see deploy/variables.md)
# Idempotent: yes — clusterctl init and Secret creation are safe to re-run.
# -----------------------------------------------------------------------------
set -euo pipefail

# --- configuration (env only, never hardcode) -------------------------------
: "${CCE_ACCESS_KEY:?CCE_ACCESS_KEY must be exported (see deploy/variables.md)}"
: "${CCE_SECRET_KEY:?CCE_SECRET_KEY must be exported (see deploy/variables.md)}"
: "${CCE_REGION:?CCE_REGION must be exported (e.g. cn-north-4)}"
CLUSTER_NAME="${CLUSTER_NAME:-my-cluster}"
KUBECONFIG_PATH="${MANAGEMENT_CLUSTER_KUBECONFIG:-$HOME/.kube/config}"

export KUBECONFIG="$KUBECONFIG_PATH"

echo "== Installing the CCE infrastructure provider =="
# Requires a published release (metadata.yaml + infrastructure-components.yaml).
clusterctl init --infrastructure cce

echo "== Creating per-cluster credentials Secret =="
# The Secret name matches the workload manifest's credentialsSecretName.
# Use --from-file (process substitution) so the keys never appear in the
# process argv (visible via ps), unlike --from-literal.
kubectl create secret generic "${CLUSTER_NAME}-credentials" \
  --namespace default \
  --from-file=accessKey=<(printf '%s' "$CCE_ACCESS_KEY") \
  --from-file=secretKey=<(printf '%s' "$CCE_SECRET_KEY") \
  --dry-run=client -o yaml | kubectl apply -f -

echo "Provider installed. Next: apply your workload cluster manifest"
echo "  (kubectl apply -f \${WORKLOAD_CLUSTER_MANIFEST:-config/samples/workload-cluster.yaml})"
