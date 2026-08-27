# Huawei Cloud CCE Provider Deployment Guide · No-bastion Edition (local direct)

> Install Cluster API (CAPI) + the CCE Provider on a Huawei Cloud CCE management cluster and declaratively manage CCE workload clusters.
> **The standard way with a public endpoint**: cluster A enables a public endpoint and your **local computer connects directly with `kubectl`** — no bastion ECS needed.

> Need a bastion (private endpoint / production isolation)? See [docs/deployment-guide.md](docs/deployment-guide.md) (bastion edition).

---

## 1. Framework Overview

### 1.1 Architecture

```
┌──────── Local computer (every command in this guide runs here) ────────┐
│  kubectl + clusterctl                                                   │
│         │  direct to the public endpoint                                │
└─────────┼───────────────────────────────────────────────────────────────┘
          ▼
┌────────────────────────── Huawei Cloud cn-north-4 ──────────────────────────┐
│  Management cluster A (CCE)  ←── runs CAPI core + Provider + cert-manager   │
│  · public endpoint (https://<public-IP>:5443)                               │
│         │                                                                   │
│  Workload cluster B (CCE)  ←── Provider calls CCE API to create            │
│  · Default Turbo (eni), multiple node pools across AZs                      │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1.2 Components at a Glance

| Component | What it is | Role in this guide |
|---|---|---|
| CCE | Huawei Cloud managed Kubernetes | Management cluster A + workload cluster B |
| CAPI (cluster-api) | Kubernetes official cluster management framework | Declaratively manages clusters |
| CCE Provider (this project) | CAPI plugin translating CAPI objects into CCE API calls | Runs on management cluster A |
| cert-manager | Certificate management | Auto-issues webhook certificates |
| SWR | Huawei Cloud container image registry | Hosts all images (public, no auth) |

---

## 2. Prerequisites

### 2.1 Huawei Cloud Resources

| Item | Requirement |
|---|---|
| Account | Can log in to the [Huawei Cloud console](https://console.huaweicloud.com), region `cn-north-4` |
| Balance | Sufficient (CCE clusters + nodes are billed on demand; zero balance fails with `CCE.01429004`) |
| AK/SK | Console → top-right username → My Credentials → Access Keys (needs CCE/VPC/ECS/EIP/SWR permissions) |
| Quota | CCE cluster quota (default 50) |
| Local network | **Can reach cluster A's public endpoint** (`https://<public-IP>:5443`) |

### 2.2 Local Tools (macOS/Linux)

```bash
# kubectl
brew install kubectl          # macOS; or download the binary for your platform with curl
# clusterctl v1.14.0 (must match the CAPI contract exactly)
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/clusterctl-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') -o clusterctl
chmod +x clusterctl && sudo mv clusterctl /usr/local/bin/
kubectl version --client && clusterctl version
```

### 2.3 Glossary

| Term | What it is (plain words) | Used for |
|---|---|---|
| AK / SK | Huawei Cloud API access keys (programmatic username/password) | Provider credentials for Huawei Cloud APIs |
| VPC / subnet | Isolated network / CIDR range | Network for clusters A/B |
| Key pair | SSH public/private key | Node SSH troubleshooting |
| EIP | Elastic public IP | Cluster A public endpoint |
| CCE | Huawei Cloud managed Kubernetes | Clusters A/B are CCE clusters |
| SWR | Huawei Cloud container image registry | Hosts all images (public) |
| CAPI / clusterctl | K8s cluster management framework / its CLI | Declarative cluster management |
| Provider | CAPI plugin translating into cloud APIs | This project (CCE provider) |
| MachinePool | CAPI's node pool object | Maps to a CCE node pool; multi-AZ = multiple MachinePools |

### 2.4 Pre-flight Checks

1. You can log in to the Huawei Cloud console and the region is `cn-north-4`.
2. The account has balance.
3. You have AK/SK.
4. kubectl + clusterctl v1.14.0 are installed locally.
5. Your local network can reach cluster A's public endpoint (verify after Stage 1).

---

## 3. Public SWR Image List (pre-built, ready to use)

The following **8 images** are pre-built / mirrored to **public SWR** (`swr.cn-north-4.myhuaweicloud.com/capi_cce/`). All are **amd64** (x86) and **public** (anonymous pull without auth, verified). Use them directly — no `imagePullSecret`, no local image builds.

| SWR repo (`swr.cn-north-4.myhuaweicloud.com/capi_cce/`) | Source | Purpose |
|---|---|---|
| `cluster-api-cce-controller:latest` | Built locally | **CCE Provider controller** (this project) |
| `cluster-api-controller:v1.14.0` | registry.k8s.io/cluster-api | CAPI core |
| `kubeadm-bootstrap-controller:v1.14.0` | registry.k8s.io/cluster-api | CAPI bootstrap-kubeadm |
| `kubeadm-control-plane-controller:v1.14.0` | registry.k8s.io/cluster-api | CAPI control-plane-kubeadm |
| `cert-manager-controller:v1.21.1` | quay.io/jetstack | cert-manager controller |
| `cert-manager-cainjector:v1.21.1` | quay.io/jetstack | cert-manager CA injection |
| `cert-manager-webhook:v1.21.1` | quay.io/jetstack | cert-manager webhook |
| `capi-cce-tools:latest` | Packaged locally | Tools: kubectl v1.30 + clusterctl v1.14 (fallback) |

> Full path example: `swr.cn-north-4.myhuaweicloud.com/capi_cce/cluster-api-cce-controller:latest`.

---

## 4. Overall Flow

```
[Stage 1: Create infrastructure in the console] click only, no CLI
  1. VPC + subnet + key pair (console)
  2. CCE management cluster A (console: cluster + node pool + public endpoint + download kubeconfig)

[Stage 2: Connect to cluster A from your local machine]
  3. Configure kubeconfig (point the server at the public endpoint)
  4. clusterctl init (GitHub Release components + images from SWR)
  5. Provider images (Method B: public SWR, no auth)

[Stage 3: Workload cluster B + verification]
  6. Create cluster B (default Turbo multi-pool, 3 nodes in 3 AZs)
  7. Verify + scale
```

| Step | Action | Time | Deliverable |
|---|---|---|---|
| 1 | VPC/subnet/key pair | 3 min | Network + key pair |
| 2 | CCE management cluster A | 10-20 min | Cluster A + kubeconfig |
| 3 | Configure kubeconfig | 1 min | Local connection to cluster A |
| 4 | clusterctl init | 5 min | CAPI + Provider Running |
| 5 | Provider images | 1 min | Method B no-auth |
| 6 | Cluster B | 10-20 min | Cluster B Provisioned |
| 7 | Verify + scale | 5 min | Nodes Ready + scale |

---

## 5. Deployment Steps

### Stage 1: Create Infrastructure in the Console (click only)

**Step 1: VPC + subnet + key pair (console)**

1. Console → Network → Virtual Private Cloud VPC → Create VPC: name `capi-vpc`, CIDR `10.0.0.0/16`.
2. Open the VPC → Subnets → Create subnet:
   - Node subnet `capi-subnet-node`: CIDR `10.0.1.0/24`; **keep the DNS server addresses at the default** (Huawei Cloud auto-fills the in-cloud DNS `100.125.1.250,100.125.129.250`, which resolves OBS/SWR intranet domain names — do not change to public DNS).
   - ENI subnet `capi-subnet-eni`: CIDR `10.0.2.0/24` (**used for the Turbo container network**; same entry as the node subnet: VPC → Subnets → Create subnet, DNS stays default). After creation, note the subnet ID and **neutron_subnet_id** (subnet details → Network ID) — the cluster A network configuration needs them.
3. **Create a key pair**: Console → Compute → Elastic Cloud Server ECS → Key Pairs → Create key pair:
   - Name: `capi-bastion-key`; after clicking "OK" the browser **automatically downloads the private key `capi-bastion-key.pem`** — **save and keep it** (for node SSH troubleshooting).
   - **Note the key pair name**: every node pool (clusters A/B) selects it (the cluster B template's `VERIFY-KEYPAIR-NAME`).

**Step 2: CCE management cluster A (console, public endpoint)**

1. Console → Compute → Cloud Container Engine CCE → Buy Cluster:
   - **Cluster name: `capi-mgmt`** (fixed; the kubeconfig/references all use it); cluster type: **CCE Turbo** (default, eni network); version `v1.35`; scale `cce.s1.small`; pay-per-use.
   - Network: VPC `capi-vpc`, node subnet `capi-subnet-node`, **ENI subnet `capi-subnet-eni`** (mandatory for Turbo; in the console this is the "**container subnet**" field — pick `capi-subnet-eni` from the dropdown).
   - Node pool: **node subnet must be `capi-subnet-node`** (⚠️ not the ENI subnet — the ENI subnet is for containers only); flavor `c7.xlarge.2` (4U8G, sub-ENI quota) × **3** nodes (⚠️ the management cluster runs CAPI + cert-manager + CCE monitoring; 2C4G ×2 saturates the pod count and leaves the provider Pending; 3 × 4U8G is the recommended config), key pair `capi-bastion-key`, availability zone `cn-north-4a`.
2. Submit and wait for the cluster to become "Available" (about 5-10 minutes).
3. **Public endpoint**: cluster details → Connection Information → bind a public IP, note `https://<public-IP>:5443`.
4. **Download kubeconfig**: Connection Information → Download kubectl config (the file is named `capi-mgmt-kubeconfig.yaml`, browsers save it to `~/Downloads` by default) → **move it to `~/.kube/`** (keep the filename so `export KUBECONFIG` can use it directly):

```bash
mkdir -p ~/.kube
mv ~/Downloads/capi-mgmt-kubeconfig.yaml ~/.kube/
```

> For a Standard (vpc-router) cluster: pick CCE Standard as the cluster type, skip the ENI subnet, and use any general-purpose node flavor (`c6.large.2`).

### Stage 2: Connect to Cluster A from Your Local Machine

**Step 3: Configure kubeconfig (point the server at the public endpoint)**

The console-downloaded kubeconfig server is an intranet address; for direct local access, change it to the public endpoint:

```bash
export KUBECONFIG=~/.kube/capi-mgmt-kubeconfig.yaml
kubectl config set-cluster capi-mgmt --server=https://<cluster-A-public-IP>:5443
kubectl get nodes    # expect 2 Ready nodes (direct over the public endpoint)
```

> If cluster A restricts the public source IPs, add your local egress IP to the allowlist.

> ⚠️ **A terminal proxy breaks GitHub fetches**: `clusterctl` (a Go program) and `curl` honor the `http_proxy`/`https_proxy` environment variables by default. If your terminal has an unreachable proxy configured (e.g. `127.0.0.1:7890`), GitHub API/asset fetches fail silently, shown as `failed to find releases tagged with a valid semantic version number`, `SSL_ERROR_SYSCALL`, etc. Check and handle it before running anything:
>
> ```bash
> env | grep -i proxy   # see if proxy variables exist
> # Option 1 (thorough): strip the proxy, none of the commands below use it
> unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY SOCKS_PROXY
> # Option 2 (keep the proxy): bypass it for GitHub domains only
> export NO_PROXY=github.com,api.github.com,raw.githubusercontent.com,*.githubusercontent.com
> ```
>
> Run the Method A / Method B commands below after stripping the proxy (or setting NO_PROXY); the same applies to kubectl connecting to cluster A's public endpoint.

**Method A: clusterctl default (cert-manager installed automatically by clusterctl, pulled from quay.io)**

```bash
# ===== Method A (complete, copy-paste in one go) =====

# 1. Download the ready-made clusterctl.yaml (all 4 providers configured, pointing at the GitHub release)
mkdir -p ~/.cluster-api
curl -L -o ~/.cluster-api/clusterctl.yaml \
  https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/clusterctl.yaml

# 2. clusterctl init (installs cert-manager + CAPI + bootstrap + control-plane + cce automatically)
export KUBECONFIG=~/.kube/capi-mgmt-kubeconfig.yaml
clusterctl init --core cluster-api --bootstrap kubeadm --control-plane kubeadm --infrastructure cce
```

> ⚠️ A `~/.cluster-api/overrides/` directory interferes with init — delete it first. If it stalls on cert-manager (slow quay.io), switch to Method B.

**Method B: public SWR without auth (all images from SWR, full install)**

> All images (CAPI core/bootstrap/control-plane + cert-manager + provider) are pulled from public SWR; no dependency on public quay.io / registry.k8s.io connectivity.

```bash
# ===== Method B: all images from public SWR (full install, copy-paste in one go) =====

# 1. Download the ready-made clusterctl.yaml (all 4 providers configured, pointing at the GitHub release)
mkdir -p ~/.cluster-api
curl -L -o ~/.cluster-api/clusterctl.yaml \
  https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/clusterctl.yaml

# 2. Install cert-manager (SWR images, ready-made yaml, no sed needed)
curl -L -o /tmp/cert-manager.yaml \
  https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/cert-manager.yaml
kubectl apply -f /tmp/cert-manager.yaml
kubectl -n cert-manager rollout status deploy/cert-manager deploy/cert-manager-cainjector deploy/cert-manager-webhook --timeout=180s

# 3. clusterctl init (installs CAPI + bootstrap + control-plane + cce, all from SWR)
export KUBECONFIG=~/.kube/capi-mgmt-kubeconfig.yaml
clusterctl init --core cluster-api --bootstrap kubeadm --control-plane kubeadm --infrastructure cce
```

> ⚠️ A `~/.cluster-api/overrides/` directory interferes with init — delete it first.

**Step 5: Provider images (Method B: public SWR without auth)**

Once clusterctl init finishes, the provider image is pulled directly from public SWR without auth and the webhook certificate is auto-issued by cert-manager — **no manual steps**:

```bash
kubectl get pods -n capi-cce-system    # capi-cce-controller-manager 1/1 Running
kubectl get certificate -n capi-cce-system serving-cert   # Ready=True
```

### Stage 3: Workload Cluster B + Verification

**Step 6: Create cluster B (default Turbo multi-pool, 3 nodes in 3 AZs)**

```bash
# 1. Download the cluster B templates (published to GitHub)
mkdir -p /tmp/templates
curl -L -O https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/cluster-template.yaml
curl -L -O https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/cluster-template-standard.yaml
curl -L -O https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/cluster-template-turbo.yaml

# 2. Install the templates into the clusterctl overrides
mkdir -p ~/.cluster-api/overrides/infrastructure-cce/v0.1.0
cp /tmp/templates/cluster-template.yaml          ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template.yaml
cp /tmp/templates/cluster-template-standard.yaml ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template-standard.yaml
cp /tmp/templates/cluster-template-turbo.yaml    ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template-turbo.yaml

# Generate (default Turbo multi-pool; --worker-machine-count=1 → 1 node per pool × 3 pools = 3 nodes in 3 AZs)
clusterctl generate cluster my-cce-cluster --kubernetes-version v1.35.0 --worker-machine-count=1 > my-cluster.yaml

# Optional: Standard (vpc-router) flavor (no ENI subnet needed; general-purpose node flavor, e.g. c6.large.2)
# clusterctl generate cluster my-cce-cluster --flavor standard --kubernetes-version v1.35.0 --worker-machine-count=1 > my-cluster.yaml

# Replace the VERIFY-* placeholders (region/VPC/subnet/ENI subnet/key pair/AZ/AZ2/AZ3/flavor)
sed -i '' \
  -e 's|VERIFY-REGION|cn-north-4|g' -e 's|VERIFY-VPC-ID|<VPC-ID>|g' \
  -e 's|VERIFY-SUBNET-ID|<node-subnet-ID>|g' -e 's|VERIFY-ENI-SUBNET-ID|<ENI-subnet-ID>|g' \
  -e 's|VERIFY-ENI-NEUTRON-ID|<ENI-subnet-neutron-ID>|g' \
  -e 's|VERIFY-AZ2|cn-north-4b|g' -e 's|VERIFY-AZ7|cn-north-4g|g' \   # AZ7 = cn-north-4g
  -e 's|VERIFY-AZ|cn-north-4a|g' -e 's|VERIFY-KEYPAIR-NAME|capi-bastion-key|g' \
  -e 's|VERIFY-FLAVOR|c7.large.2|g' \
  my-cluster.yaml

# Create the credentials Secret + bootstrap Secret
export CLOUD_SDK_AK='<your-AK>' CLOUD_SDK_SK='<your-SK>'
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default --from-literal=accessKey="$CLOUD_SDK_AK" --from-literal=secretKey="$CLOUD_SDK_SK"
kubectl create secret generic my-cce-cluster-bootstrap \
  --namespace default --from-literal=value=""

# (Optional) cluster B public endpoint: needed to verify nodes locally (default is private, not directly reachable from your machine)
# Uncomment to enable the public endpoint, or skip to keep it private
sed -i '' 's|    public: false|    public: true|g' my-cluster.yaml

kubectl apply -f my-cluster.yaml
```

> Multi-AZ = multiple MachinePools: `pool-0/1/2` each lives in one AZ.
> ⚠️ ① Every AZ must have an available sub-ENI flavor: pool-2 uses AZ7 (`cn-north-4g`, has `c7.large.2`); if an AZ runs out, switch to `at7.large.1` or another AZ.
> ⚠️ ② **A node pool's AZ is immutable after creation**: if the AZ/flavor is wrong you must delete the pool and recreate it (`kubectl delete machinepool <name>` + `ccemanagedmachinepool <name>` → edit the yaml → `kubectl apply`); patching alone does not work (CCE does not rebuild the pool).
> ⚠️ ③ Replace every `VERIFY-*` placeholder (10 of them): REGION / VPC-ID / SUBNET-ID / ENI-SUBNET-ID / ENI-NEUTRON-ID / AZ / AZ2 / AZ3 / KEYPAIR-NAME / FLAVOR — missing any one of them fails the creation. After replacing, `grep VERIFY my-cluster.yaml` should output nothing.

**Step 7: Verify + scale**

```bash
kubectl get cluster my-cce-cluster -w        # PHASE=Provisioned
kubectl get ccemanagedcontrolplane my-cce-cluster-control-plane -w   # Ready=True

clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig
ls -l my-cce-cluster.kubeconfig   # should have a few KB of content
> If `clusterctl get kubeconfig` outputs nothing: check whether the kubeconfig Secret was created
> `kubectl get secret my-cce-cluster-kubeconfig -n default` — if missing, wait 1-2 min (the provider is generating it) or check the provider logs

# Verify nodes — pick one based on cluster B's endpoint mode:
# Public (optional step 6 enabled): connect directly from your machine
kubectl --kubeconfig=my-cce-cluster.kubeconfig get nodes -o wide   # 3 Ready nodes
# Private (default, not directly reachable locally): run the same command on a bastion in the same VPC
# (node verification does not depend on the kubeconfig: MachinePools at 1/1 already mean the nodes are Ready)

# Scaling = adjusting replicas (the desired count; the same scale command — larger replicas scales up, smaller scales down)
# Scale up: pool-0 replicas 1→3 (cluster B nodes 3→5)
kubectl scale machinepool my-cce-cluster-pool-0 --replicas=3
kubectl get machinepool my-cce-cluster-pool-0 -w      # wait for CURRENT/AVAILABLE=3 (about 2-3 min)

# Scale down: pool-0 replicas 3→1 (cluster B nodes 5→3) — CCE drains nodes on scale-down, a bit slower
kubectl scale machinepool my-cce-cluster-pool-0 --replicas=1
kubectl get machinepool my-cce-cluster-pool-0 -w      # wait for CURRENT/AVAILABLE=1 (about 3-5 min)
```

---

## 6. Troubleshooting / Pitfall Log

| # | Symptom | Root cause | Fix | Status |
|---|---|---|---|---|
| 1 | Nodes stuck at `Installing` forever | Subnet DNS changed to non-in-cloud DNS (creating a subnet via API without `primary_dns` leaves it empty) | Keep the default when creating in the console; API/scripts (`deploy-network`) explicitly set the in-cloud DNS `100.125.1.250,100.125.129.250` (cn-north-4) | ✅ |
| 2 | Cluster has no public endpoint | CCE does not auto-assign a public IP | Bind an EIP in the cluster details | ✅ |
| 3 | Local connection to cluster A fails | The kubeconfig server is an intranet address | `kubectl config set-cluster --server=<public-IP>:5443` | ✅ |
| 4 | Repeated 429 throttling (`APIGW.0308`) | CCE write throttling is 10 req/min and 429 retries also count | Provider has a built-in 3-min back-off; keep operations ≥60s apart | ✅ |
| 5 | `clusterctl init` stuck at `Fetching providers` | Pulling CAPI components from GitHub | Download components locally + point images at SWR + local repository | ✅ |
| 6 | `CCE_CM.0004 type and network mode not match` | Webhook defaulted category=Turbo while mode=vpc-router | category follows the network mode + validation | ✅ |
| 7 | All nodes land in the `default` group | CCE extended groups are not AZ-aware at creation | Multiple MachinePools (one per AZ) | ✅ |
| 8 | A flavor is sold out / no sub-ENI in some AZ | Tight resources (e.g. no 2C4G in 4c) | Switch flavor (`at7.large.1`) or AZ | ✅ |
| 9 | Cluster B kubeconfig unreachable locally | Cluster B defaults to a private endpoint | Enable cluster B's public endpoint (`spec.endpointAccess.public=true`) | ✅ |
| 10 | Cluster deletion stuck on a finalizer | Deletion path for clusters that never became Available | Remove the finalizer manually | ✅ |
| 11 | Provider pods stuck Pending (`Too many pods`) | Management cluster nodes too small (2C4G×2, 16 pods/node cap filled by CCE's own monitoring) | Use 4U8G (c7.xlarge.2) ×3 for cluster A; Pending pods schedule automatically | ✅ |
| 12 | Node pool AZ wrong (immutable after creation) | CCE node pool AZ cannot be changed after creation; patch does not rebuild | Delete the pool and recreate (delete machinepool → edit yaml → apply) | ✅ |
| 13 | Cluster B creation fails `Az [VERIFY-AZ] is not in available az list` | `VERIFY-AZ\b` does not work on macOS sed (BSD lacks `\b`), the primary AZ was left unreplaced | Replace AZ2/AZ7 first, then AZ (no `\b`); confirm with `grep VERIFY` afterwards | ✅ |
| 14 | `clusterctl get kubeconfig` outputs nothing | The kubeconfig Secret has not been created yet (provider still working) | `kubectl get secret my-cce-cluster-kubeconfig -n default`; wait 1-2 min or check provider logs | ✅ |

---

## 7. Clean Up Resources

```bash
# 1. Delete workload cluster B (local, CAPI deletion chain)
kubectl delete cluster my-cce-cluster

# 2. Delete management cluster A + VPC (console deletion)
#    CCE console: delete cluster A → VPC console: delete subnets + VPC (EIPs are released when the cluster is deleted)
```

---

## 8. Misc

### 8.1 Credential Rotation

```bash
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default --from-literal=accessKey='<new-AK>' --from-literal=secretKey='<new-SK>' \
  --dry-run=client -o yaml | kubectl apply -f -
```

### 8.2 Command Cheat Sheet (all run locally)

| Action | Command |
|---|---|
| Connect to cluster A | `export KUBECONFIG=~/.kube/capi-mgmt-kubeconfig.yaml && kubectl get nodes` |
| Initialize the provider | `clusterctl init --core cluster-api --bootstrap kubeadm --control-plane kubeadm --infrastructure cce` |
| Generate cluster B | `clusterctl generate cluster my-cce-cluster --kubernetes-version v1.35.0 --worker-machine-count=1 > my-cluster.yaml` |
| Submit cluster B | `kubectl apply -f my-cluster.yaml` |
| Watch status | `kubectl get cluster my-cce-cluster -w` |
| Get cluster B kubeconfig | `clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig` |
| Verify nodes | `kubectl --kubeconfig=my-cce-cluster.kubeconfig get nodes` |
| Scale | `kubectl scale machinepool my-cce-cluster-pool-0 --replicas=N` |
| Delete cluster B | `kubectl delete cluster my-cce-cluster` |

---

## Appendix A: Bastion Mode (production-safe, optional)

When you need **private endpoints / security isolation** (cluster A not exposed publicly), use the bastion ECS mode:

- Create an ECS bastion (in the same VPC as cluster A).
- Log in to the bastion via the console CloudShell and run the Stage 2/3 commands inside it (the kubeconfig uses the intranet server; no public-endpoint rewrite needed).
- Full steps: see [docs/deployment-guide.md](docs/deployment-guide.md).

## Appendix B: hack Scripts (optional automation)

The main path uses the console + local CLI; the hack scripts below run on any host with Go and create/clean up infrastructure in one command (they call the Huawei Cloud APIs and replace the manual console steps):

| Script | Purpose | Key environment variables |
|---|---|---|
| `hack/deploy-network` | One-shot VPC/subnet/key pair | `CLOUD_SDK_AK/SK`, `CCE_DEPLOY_REGION` |
| `hack/deploy-mgmt-cluster` | One-shot management cluster A (default Turbo + public endpoint) | `CCE_DEPLOY_CATEGORY`, `CCE_DEPLOY_ENI_SUBNET`, `CCE_DEPLOY_PUBLIC`, etc. |
| `hack/cleanup-hw` | Delete CCE clusters/node pools/EIPs | `-cluster <id>` |
| `hack/survey-hw` | Inventory all resources | `CLOUD_SDK_AK/SK` |
