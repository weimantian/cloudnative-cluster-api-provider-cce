#!/usr/bin/env bash
# -----------------------------------------------------------------------------
# deploy-kind.sh — one-command local deployment of cloudnative-cluster-api-provider-cce
#                  onto a kind management cluster (development / evaluation).
#
# What it does (all steps verified end-to-end on kind + clusterctl v1.14.0):
#   1. Builds the provider container image (local tag, loaded into kind).
#   2. Creates a kind management cluster (proxy env vars are stripped so the
#      node's containerd can pull images directly — a common pitfall).
#   3. Generates infrastructure-components.yaml from config/default via
#      `kubectl kustomize`, overrides the image to a canonical three-part name
#      (clusterctl requires "registry/org/repo:tag"), and sets
#      imagePullPolicy=Never so the locally-loaded image is used.
#   4. Generates a self-signed CA + webhook server cert and injects the
#      caBundle into the 6 admission webhooks.
#   5. Registers the provider with clusterctl (local file:// source) and runs
#      `clusterctl init --infrastructure cce`.
#   6. Creates the webhook-service-cert Secret and waits for the provider.
#
# Usage:
#   scripts/deploy-kind.sh
#
# Env overrides (all optional):
#   IMG=...            provider image tag (default cce-provider-controller:dev)
#   KIND_CLUSTER=...   kind cluster name (default cce-mgmt)
#   CCE_PROVIDER_VERSION=v0.1.0   provider version string for clusterctl
#
# After this script, create the credentials Secret + workload cluster manifest
# (see README Step-by-Step Deployment steps 5-6, or config/samples/).
# -----------------------------------------------------------------------------
set -euo pipefail

IMG="${IMG:-cce-provider-controller:dev}"
KIND_CLUSTER="${KIND_CLUSTER:-cce-mgmt}"
CCE_PROVIDER_VERSION="${CCE_PROVIDER_VERSION:-v0.1.0}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ARTIFACTS="$REPO_ROOT/_artifacts"
CLUSTERCTL_SRC="/tmp/cce/infrastructure-cce/${CCE_PROVIDER_VERSION}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "ERROR: '$1' not found. Install it first."; exit 1; }; }
need docker; need kind; need kubectl; need clusterctl; need openssl

echo "== [1/6] Building provider image: $IMG =="
(cd "$REPO_ROOT" && docker build -t "$IMG" .)
kind load docker-image "$IMG" --name "$KIND_CLUSTER" 2>/dev/null || {
  echo "kind cluster '$KIND_CLUSTER' not running; creating it..."
  # Strip proxy env vars: kind passes them into the node, and a dead local
  # proxy (127.0.0.1:7890) makes containerd unable to pull images.
  env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u SOCKS_PROXY \
      kind create cluster --name "$KIND_CLUSTER"
  kind load docker-image "$IMG" --name "$KIND_CLUSTER"
}

echo "== [2/6] Generating components with image override =="
mkdir -p "$ARTIFACTS"
cat > "$ARTIFACTS/kustomization.yaml" <<EOF
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../config/default
images:
  - name: swr.cn-north-4.myhuaweicloud.com/cce-provider/controller
    newName: docker.io/library/${IMG%%:*}
    newTag: ${IMG##*:}
patches:
  - target:
      kind: Deployment
      name: cce-provider-controller-manager
    patch: |-
      - op: replace
        path: /spec/template/spec/containers/0/imagePullPolicy
        value: Never
EOF
kubectl kustomize "$ARTIFACTS" > "$ARTIFACTS/infrastructure-components-raw.yaml"

echo "== [3/6] Generating webhook certs and injecting caBundle =="
(
  cd "$ARTIFACTS"
  openssl genrsa -out ca.key 2048 2>/dev/null
  openssl req -x509 -new -nodes -key ca.key -sha256 -days 365 -subj "/CN=cce-provider-ca" -out ca.crt 2>/dev/null
  openssl genrsa -out server.key 2048 2>/dev/null
  cat > server.conf <<'EOF2'
[req]
distinguished_name = dn
req_extensions = ext
prompt = no
[dn]
CN = webhook-service.cce-provider-system.svc
[ext]
subjectAltName = @alt_names
[alt_names]
DNS.1 = webhook-service
DNS.2 = webhook-service.cce-provider-system
DNS.3 = webhook-service.cce-provider-system.svc
DNS.4 = webhook-service.cce-provider-system.svc.cluster.local
EOF2
  openssl req -new -key server.key -out server.csr -config server.conf 2>/dev/null
  cat > server.ext <<'EOF2'
subjectAltName = @alt_names
[alt_names]
DNS.1 = webhook-service
DNS.2 = webhook-service.cce-provider-system
DNS.3 = webhook-service.cce-provider-system.svc
DNS.4 = webhook-service.cce-provider-system.svc.cluster.local
EOF2
  openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
    -out server.crt -days 365 -sha256 -extfile server.ext 2>/dev/null
)

CABUNDLE="$(base64 < "$ARTIFACTS/ca.crt" | tr -d '\n')"
awk -v cab="$CABUNDLE" '
  { print }
  $0 == "  clientConfig:" { print "    caBundle: " cab }
' "$ARTIFACTS/infrastructure-components-raw.yaml" > "$ARTIFACTS/infrastructure-components.yaml"
echo "  injected caBundle into $(grep -c caBundle "$ARTIFACTS/infrastructure-components.yaml") webhooks"

echo "== [4/6] Registering provider with clusterctl =="
mkdir -p "$CLUSTERCTL_SRC" "$HOME/.cluster-api"
cp "$ARTIFACTS/infrastructure-components.yaml" "$REPO_ROOT/metadata.yaml" "$CLUSTERCTL_SRC/"
cat > "$HOME/.cluster-api/clusterctl.yaml" <<EOF
providers:
  - name: "cce"
    url: "file://$CLUSTERCTL_SRC/infrastructure-components.yaml"
    type: "InfrastructureProvider"
EOF

echo "== [5/6] clusterctl init (cert-manager + CAPI core + bootstrap/control-plane + cce) =="
clusterctl init --infrastructure cce --wait-providers

echo "== [6/6] Creating webhook certificate Secret (if missing) =="
kubectl -n cce-provider-system get secret webhook-service-cert >/dev/null 2>&1 || \
  kubectl -n cce-provider-system create secret tls webhook-service-cert \
    --cert="$ARTIFACTS/server.crt" --key="$ARTIFACTS/server.key"

kubectl -n cce-provider-system rollout restart deployment/cce-provider-controller-manager
kubectl -n cce-provider-system wait --for=condition=Available deployment/cce-provider-controller-manager --timeout=120s

echo
echo "Done. The provider is installed on kind cluster '$KIND_CLUSTER'."
echo "Next steps (see README Step-by-Step Deployment):"
echo "  1. kubectl create secret generic my-cluster-credentials --from-literal=accessKey=\$AK --from-literal=secretKey=\$SK"
echo "  2. kubectl create secret generic my-cluster-bootstrap --from-literal=value=\"\""
echo "  3. kubectl apply -f config/samples/cluster-template.yaml  (fill in VERIFY-... placeholders first)"
