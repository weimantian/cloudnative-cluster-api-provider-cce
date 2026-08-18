#!/usr/bin/env bash
# -----------------------------------------------------------------------------
# destroy.sh
#
# Purpose  : DESTRUCTIVE cleanup — delete workload cluster(s) (CCE cluster,
#            node pools, kubeconfig Secret) and remove the provider from the
#            management cluster.
# Usage    : deploy/scripts/destroy.sh [cluster-name]  (default: my-cluster)
# Depends  : bash, kubectl, clusterctl
# Safety   : Requires interactive confirmation before any destructive step.
#            Never deletes anything without an explicit 'yes' answer.
# -----------------------------------------------------------------------------
set -euo pipefail

CLUSTER_NAME="${1:-my-cluster}"
KUBECONFIG_PATH="${MANAGEMENT_CLUSTER_KUBECONFIG:-$HOME/.kube/config}"
export KUBECONFIG="$KUBECONFIG_PATH"

confirm() {
  local prompt="$1"
  local answer
  printf '%s [yes/NO]: ' "$prompt"
  read -r answer
  [[ "$answer" == "yes" ]]
}

echo "This script will delete the workload cluster '${CLUSTER_NAME}' (including"
echo "the CCE cluster, its node pools, and the kubeconfig Secret) and remove"
echo "the CCE infrastructure provider from the management cluster."
echo

confirm "Delete workload cluster '${CLUSTER_NAME}'?" || { echo "Aborted."; exit 1; }
kubectl delete cluster "${CLUSTER_NAME}" --namespace default || true

confirm "Remove the CCE infrastructure provider from the management cluster?" || { echo "Kept provider. Done."; exit 0; }
clusterctl delete --infrastructure cce || true

echo "Cleanup finished. Verify in the CCE console that no cluster/node pools"
echo "and no stray EIP/EVS/ELB resources remain."
