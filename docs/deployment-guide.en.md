# Huawei Cloud CCE Provider Deployment Guide

> Install Cluster API (CAPI) + the CCE Provider on a Huawei Cloud CCE management cluster and declaratively manage CCE workload clusters.
> **Production practice**: all infrastructure is created in the Huawei Cloud console; every CLI command runs inside an ECS bastion terminal; your local computer is not involved.

---

## 1. Framework Overview

### 1.1 Architecture

```
┌────────────────────────── Huawei Cloud cn-north-4 ──────────────────────────┐
│                                                                             │
│  Bastion ECS (capi-bastion)                Management cluster A (CCE)       │
│  · Console CloudShell login                · Runs CAPI core + Provider      │
│  · kubectl + clusterctl                    · cert-manager                   │
│         │                                      │                           │
│         └────── kubectl to cluster A (public endpoint) ─┘                  │
│                                                                             │
│  Workload cluster B (CCE)  ←── Provider calls CCE API to create            │
│  · Default Turbo (eni), multiple node pools across AZs                     │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1.2 Components at a Glance

| Component | What it is | Role in this guide |
|---|---|---|
| CCE | Huawei Cloud managed Kubernetes | Management cluster A + workload cluster B |
| ECS | Cloud server | Bastion (ops entry, logged in via console CloudShell) |
| SWR | Huawei Cloud container image registry | Hosts all images (public, no auth) |
| CAPI (cluster-api) | Kubernetes official cluster management framework | Declaratively manages clusters |
| CCE Provider (this project) | CAPI plugin translating CAPI objects into CCE API calls | Runs on management cluster A |
| cert-manager | Certificate management | Auto-issues webhook certificates |

---

## 2. Prerequisites

### 2.1 Huawei Cloud Resources

| Item | Requirement |
|---|---|
| Account | Can log in to the [Huawei Cloud console](https://console.huaweicloud.com), region `cn-north-4` |
| Balance | Sufficient (CCE clusters + nodes + ECS are billed on demand; zero balance fails with `CCE.01429004`) |
| AK/SK | Console → top-right username → My Credentials → Access Keys (needs CCE/VPC/ECS/EIP/SWR permissions) |
| Quota | CCE cluster quota (default 50) |

> ⚠️ Huawei Cloud has no EC2 "IAM instance role" equivalent; credentials are provided via **AK/SK environment variables** (an IAM agency is optional, see Appendix A).

### 2.2 Glossary

| Term | What it is (plain words) | Used for |
|---|---|---|
| AK / SK | Huawei Cloud API access keys (programmatic username/password) | Provider credentials for Huawei Cloud APIs |
| VPC | Your private isolated network (like a building) | Network for bastion + clusters A/B |
| Subnet | A CIDR range inside the VPC (like a floor) | IPs for nodes / bastion |
| Security group | Firewall rules (which ports / IPs are allowed) | Open SSH 22, API ports |
| Key pair | SSH public/private key | Node SSH troubleshooting (bastion login uses CloudShell, no private key needed) |
| EIP | Elastic public IP | Bastion / cluster A public endpoint |
| CCE | Huawei Cloud managed Kubernetes | Clusters A/B are CCE clusters |
| SWR | Huawei Cloud container image registry | Hosts all images (public) |
| CAPI / clusterctl | K8s cluster management framework / its CLI | Declarative cluster management |
| Provider | CAPI plugin translating into cloud APIs | This project (CCE provider) |
| CloudShell | Huawei Cloud console's in-browser cloud shell | Log in to the bastion (no local SSH) |
| MachinePool | CAPI's node pool object | Maps to a CCE node pool; multi-AZ = multiple MachinePools |

### 2.3 Pre-flight Checks

1. You can log in to the Huawei Cloud console and the region is set to `cn-north-4`.
2. The account has balance.
3. You have AK/SK (verify with the main account first if permissions are uncertain).
4. Note the AK/SK down for later (`export` inside the bastion).

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
| `capi-cce-tools:latest` | Packaged locally | Bastion tools: kubectl v1.30 + clusterctl v1.14 |

> The current `cluster-api-cce-controller:latest` digest is `e4d624fc` (includes kubeconfig endpoint override + 429 back-off fix).
> Full path example: `swr.cn-north-4.myhuaweicloud.com/capi_cce/cluster-api-cce-controller:latest`.

---

## 4. Overall Flow

```
[Stage 1: Create infrastructure in the console] click only, no CLI
  1. VPC + subnet + key pair (console)
  2. ECS bastion (console, CloudShell login)
  3. CCE management cluster A (console: cluster + node pool + public endpoint + download kubeconfig)

[Stage 2: Bastion deployment] console CloudShell (in-browser; every command runs inside the bastion)
  4. Install tools (kubectl + clusterctl)
  5. Connect to cluster A (kubeconfig)
  6. clusterctl init (GitHub Release components + images from SWR)
  7. Provider images (Method B: public SWR, no auth)

[Stage 3: Workload cluster B + verification] CloudShell
  8. Create cluster B (default Turbo multi-pool, 3 nodes in 3 AZs)
  9. Verify + scale
```

| Step | Action | Time | Deliverable |
|---|---|---|---|
| 1 | VPC/subnet/key pair | 3 min | Network + key pair |
| 2 | ECS bastion | 3 min | Bastion (public IP) |
| 3 | CCE management cluster A | 10-20 min | Cluster A + kubeconfig |
| 4 | Install tools | 5 min | kubectl + clusterctl |
| 5 | Connect to cluster A | 1 min | 2 nodes Ready |
| 6 | clusterctl init | 5 min | CAPI + Provider Running |
| 7 | Provider images | 1 min | Method B no-auth |
| 8 | Cluster B | 10-20 min | Cluster B Provisioned |
| 9 | Verify + scale | 5 min | Nodes Ready + scale |

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

**Step 2: ECS bastion (console)**

1. ECS → Buy Elastic Cloud Server:
   - Billing mode: Pay-per-use; region `cn-north-4`; flavor `s6.small.1` (1C2G); image EulerOS 2.0.
   - Network: VPC `capi-vpc`, subnet `capi-subnet-node`.
   - Security group: create new, allow inbound **TCP 22** (source restricted to your company IP).
   - EIP: bind one (note the public IP).
   - Key pair: `capi-bastion-key`.
2. Wait for the ECS to be "Running".

> From here on, log in to the bastion with the console **CloudShell** (step 4) — **no need to download the private key locally**.

**Step 3: CCE management cluster A (console)**

1. Console → Compute → Cloud Container Engine CCE → Buy Cluster:
   - **Cluster name: `capi-mgmt`** (fixed; the kubeconfig/references all use it); cluster type: **CCE Turbo** (default, eni network); version `v1.35`; scale `cce.s1.small`; pay-per-use.
   - Network: VPC `capi-vpc`, node subnet `capi-subnet-node`, **ENI subnet `capi-subnet-eni`** (mandatory for Turbo; in the console this is the "**container subnet**" field — pick `capi-subnet-eni` from the dropdown).
   - Node pool: **node subnet must be `capi-subnet-node`** (⚠️ not the ENI subnet — the ENI subnet is for containers only); flavor `c7.xlarge.2` (4U8G, sub-ENI quota) × **3** nodes (⚠️ the management cluster runs CAPI + cert-manager + CCE monitoring; 2C4G ×2 saturates the pod count and leaves the provider Pending; 3 × 4U8G is the recommended config), key pair `capi-bastion-key`, availability zone `cn-north-4a`.
2. Submit and wait for the cluster to become "Available" (about 5-10 minutes).
3. **Public endpoint**: cluster details → Connection Information → bind a public IP.
4. **Download kubeconfig**: Connection Information → Download kubectl config (the downloaded file is named `capi-mgmt-kubeconfig.yaml`) → save locally, **upload to the bastion as `/root/capi-mgmt-kubeconfig.yaml`** (CloudShell file upload, used in step 5):

```bash
# Confirm after the CloudShell upload
ls -l /root/capi-mgmt-kubeconfig.yaml
```

> For a Standard (vpc-router) cluster: pick CCE Standard as the cluster type, skip the ENI subnet, and use any general-purpose node flavor (`c6.large.2`).

### Stage 2: Bastion Deployment (console CloudShell)

> Every command below **runs inside the bastion** (Console → ECS → bastion → Remote Login → CloudShell).

**Step 4: Install tools (kubectl + clusterctl)**

```bash
# Run inside CloudShell (bastion)
export CLOUD_SDK_AK='<your-AK>' CLOUD_SDK_SK='<your-SK>'   # Provider credentials (needed later)

# kubectl + clusterctl (linux/amd64)
curl -LO "https://dl.k8s.io/release/v1.30.0/bin/linux/amd64/kubectl"
install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/clusterctl-linux-amd64 -o clusterctl
chmod +x clusterctl && mv clusterctl /usr/local/bin/clusterctl

kubectl version --client && clusterctl version
```

> ⚠️ Slow-network fallback: use the SWR tools image `capi-cce-tools` (see chapter 3) with `docker pull` + `docker cp` to extract the binaries. ⚠️ Do not use `yum install kubectl` (the Huawei Cloud repo only has v1.23).

**Step 5: Connect to cluster A (kubeconfig)**

Upload the `capi-mgmt-kubeconfig.yaml` downloaded in step 3 to the bastion `/root/` (CloudShell file upload), then verify:

```bash
export KUBECONFIG=/root/capi-mgmt-kubeconfig.yaml
kubectl get nodes    # expect 2 Ready nodes
```

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
> Run the Method A / Method B commands below after stripping the proxy (or setting NO_PROXY); the same applies to kubectl connecting to cluster A.

**Method A: clusterctl default (cert-manager installed automatically by clusterctl, pulled from quay.io)**

```bash
# ===== Method A (complete, copy-paste in one go) =====

# 1. Download the ready-made clusterctl.yaml (all 4 providers configured, pointing at the GitHub release)
mkdir -p /root/.cluster-api
curl -L -o /root/.cluster-api/clusterctl.yaml \
  https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/clusterctl.yaml

# 2. clusterctl init (installs cert-manager + CAPI + bootstrap + control-plane + cce automatically)
export KUBECONFIG=/root/capi-mgmt-kubeconfig.yaml
clusterctl init --core cluster-api --bootstrap kubeadm --control-plane kubeadm --infrastructure cce
```

> ⚠️ A `~/.cluster-api/overrides/` directory interferes with init — delete it first. If it stalls on cert-manager (slow quay.io), switch to Method B.

**Method B: public SWR without auth (all images from SWR, full install)**

> All images (CAPI core/bootstrap/control-plane + cert-manager + provider) are pulled from public SWR; no dependency on public quay.io / registry.k8s.io connectivity.

```bash
# ===== Method B: all images from public SWR (full install, copy-paste in one go) =====

# 1. Download the ready-made clusterctl.yaml (all 4 providers configured, pointing at the GitHub release)
mkdir -p /root/.cluster-api
curl -L -o /root/.cluster-api/clusterctl.yaml \
  https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/clusterctl.yaml

# 2. Install cert-manager (SWR images, ready-made yaml, no sed needed)
curl -L -o /root/cert-manager.yaml \
  https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/cert-manager.yaml
kubectl apply -f /root/cert-manager.yaml
kubectl -n cert-manager rollout status deploy/cert-manager deploy/cert-manager-cainjector deploy/cert-manager-webhook --timeout=180s

# 3. clusterctl init (installs CAPI + bootstrap + control-plane + cce, all from SWR)
export KUBECONFIG=/root/capi-mgmt-kubeconfig.yaml
clusterctl init --core cluster-api --bootstrap kubeadm --control-plane kubeadm --infrastructure cce
```

> ⚠️ A `~/.cluster-api/overrides/` directory interferes with init — delete it first.

**Step 7: Provider images (Method B: public SWR without auth)**

Once clusterctl init finishes, the provider image is pulled directly from public SWR without auth and the webhook certificate is auto-issued by cert-manager — **no manual steps**:

```bash
kubectl -n capi-cce-system get pods    # capi-cce-controller-manager 1/1 Running
kubectl get certificate -n capi-cce-system serving-cert   # Ready=True
```

### Stage 3: Workload Cluster B + Verification (CloudShell)

**Step 8: Create cluster B (default Turbo multi-pool, 3 nodes in 3 AZs)**

```bash
# 1. Download the cluster B templates (published to GitHub)
curl -L -o /root/cluster-template.yaml          https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/cluster-template.yaml
curl -L -o /root/cluster-template-standard.yaml https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/cluster-template-standard.yaml
curl -L -o /root/cluster-template-turbo.yaml    https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/cluster-template-turbo.yaml

# 2. Install the templates into the clusterctl overrides
mkdir -p ~/.cluster-api/overrides/infrastructure-cce/v0.1.0
cp /root/cluster-template.yaml          ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template.yaml
cp /root/cluster-template-standard.yaml ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template-standard.yaml
cp /root/cluster-template-turbo.yaml    ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template-turbo.yaml

# Generate (default Turbo multi-pool; --worker-machine-count=1 → 1 node per pool × 3 pools = 3 nodes in 3 AZs)
clusterctl generate cluster my-cce-cluster --kubernetes-version v1.35.0 --worker-machine-count=1 > my-cluster.yaml

# Optional: Standard (vpc-router) flavor (no ENI subnet needed; general-purpose node flavor, e.g. c6.large.2)
# clusterctl generate cluster my-cce-cluster --flavor standard --kubernetes-version v1.35.0 --worker-machine-count=1 > my-cluster.yaml

# Replace the VERIFY-* placeholders (region/VPC/subnet/ENI subnet/key pair/AZ/AZ2/AZ3/flavor)
sed -i \
  -e 's|VERIFY-REGION|cn-north-4|g' -e 's|VERIFY-VPC-ID|<VPC-ID>|g' \
  -e 's|VERIFY-SUBNET-ID|<node-subnet-ID>|g' -e 's|VERIFY-ENI-SUBNET-ID|<ENI-subnet-ID>|g' \
  -e 's|VERIFY-ENI-NEUTRON-ID|<ENI-subnet-neutron-ID>|g' \
  -e 's|VERIFY-AZ2|cn-north-4b|g' -e 's|VERIFY-AZ3|cn-north-4c|g' \
  -e 's|VERIFY-AZ|cn-north-4a|g' -e 's|VERIFY-KEYPAIR-NAME|capi-bastion-key|g' \
  -e 's|VERIFY-FLAVOR|c7.large.2|g' \
  my-cluster.yaml

# Create the credentials Secret + bootstrap Secret
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default --from-literal=accessKey="$CLOUD_SDK_AK" --from-literal=secretKey="$CLOUD_SDK_SK"
kubectl create secret generic my-cce-cluster-bootstrap \
  --namespace default --from-literal=value=""

kubectl apply -f my-cluster.yaml
```

> Multi-AZ = multiple MachinePools: `pool-0/1/2` each lives in one AZ.
> ⚠️ ① Every AZ must have an available sub-ENI flavor: e.g. `cn-north-4c` often lacks `c7.large.2` (sold out / insufficient resources) — switch to `at7.large.1` or use `cn-north-4a/4b`.
> ⚠️ ② **A node pool's AZ is immutable after creation**: if the AZ/flavor is wrong you must delete the pool and recreate it (delete machinepool + ccemanagedmachinepool → edit the yaml → apply); patching alone does not work.
> ⚠️ ③ Replace every `VERIFY-*` placeholder (10 of them); after replacing, `grep VERIFY my-cluster.yaml` should output nothing; replace `VERIFY-AZ` last (do AZ2/AZ3 first).

**Step 9: Verify + scale**

```bash
kubectl get cluster my-cce-cluster -w        # PHASE=Provisioned
kubectl get ccemanagedcontrolplane my-cce-cluster-control-plane -w   # Ready=True

clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig
ls -l my-cce-cluster.kubeconfig   # should have a few KB of content; if empty, check `kubectl get secret my-cce-cluster-kubeconfig -n default` (wait 1-2 min if missing)
kubectl --kubeconfig=my-cce-cluster.kubeconfig get nodes -o wide   # 3 Ready nodes (bastion is in the same VPC, the private endpoint is directly reachable)

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
| 3 | Repeated 429 throttling (`APIGW.0308`) | CCE write throttling is 10 req/min and 429 retries also count | Provider has a built-in 3-min back-off; keep operations ≥60s apart | ✅ |
| 4 | Downloading tools on the bastion is extremely slow | Huawei Cloud international egress is slow | SWR tools image `capi-cce-tools` / ghfast accelerator | ✅ |
| 5 | `clusterctl init` stuck at `Fetching providers` | Pulling CAPI components from GitHub | Download components locally + point images at SWR + local repository | ✅ |
| 6 | `CCE_CM.0004 type and network mode not match` | Webhook defaulted category=Turbo while mode=vpc-router | category follows the network mode + validation | ✅ |
| 7 | All nodes land in the `default` group | CCE extended groups are not AZ-aware at creation | Multiple MachinePools (one per AZ) | ✅ |
| 8 | A flavor is sold out / no sub-ENI in some AZ | Tight resources (e.g. no 2C4G in 4c) | Switch flavor (`at7.large.1`) or AZ | ✅ |
| 9 | kubeconfig server address is stale | The CCE cert API returns an old address | Provider overrides with the current Internal endpoint | ✅ |
| 10 | Cluster deletion stuck on a finalizer | Deletion path for clusters that never became Available | Remove the finalizer manually | ✅ |
| 11 | Provider pods stuck Pending (`Too many pods`) | Management cluster nodes too small (2C4G×2, 16 pods/node cap filled by CCE's own monitoring) | Use 4U8G (c7.xlarge.2) ×3 for cluster A; Pending pods schedule automatically | ✅ |
| 12 | Node pool AZ wrong (immutable after creation) | CCE node pool AZ cannot be changed after creation; patch does not rebuild | Delete the pool and recreate (delete machinepool → edit yaml → apply) | ✅ |
| 13 | Cluster B creation fails `Az [VERIFY-AZ] is not in available az list` | `VERIFY-AZ\b` does not work on macOS sed (BSD lacks `\b`), the primary AZ was left unreplaced | Replace AZ2/AZ3 first, then AZ (no `\b`); confirm with `grep VERIFY` afterwards | ✅ |
| 14 | `clusterctl get kubeconfig` outputs nothing | The kubeconfig Secret has not been created yet (provider still working) | `kubectl get secret my-cce-cluster-kubeconfig -n default`; wait 1-2 min or check provider logs | ✅ |

---

## 7. Clean Up Resources

```bash
# 1. Delete workload cluster B (CloudShell, CAPI deletion chain)
kubectl delete cluster my-cce-cluster

# 2. Delete management cluster A + bastion + VPC (console deletion)
#    CCE console: delete cluster A → ECS console: delete bastion → VPC console: delete subnets + VPC
#    (EIPs are released automatically when the cluster/ECS is deleted)
```

---

## 8. Misc

### 8.1 Credential Rotation

```bash
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default --from-literal=accessKey='<new-AK>' --from-literal=secretKey='<new-SK>' \
  --dry-run=client -o yaml | kubectl apply -f -
```

### 8.2 Command Cheat Sheet (all run on the bastion)

| Action | Command |
|---|---|
| Connect to cluster A | `export KUBECONFIG=/root/capi-mgmt-kubeconfig.yaml && kubectl get nodes` |
| Initialize the provider | `clusterctl init --core cluster-api --bootstrap kubeadm --control-plane kubeadm --infrastructure cce` |
| Generate cluster B | `clusterctl generate cluster my-cce-cluster --kubernetes-version v1.35.0 --worker-machine-count=1 > my-cluster.yaml` |
| Submit cluster B | `kubectl apply -f my-cluster.yaml` |
| Watch status | `kubectl get cluster my-cce-cluster -w` |
| Get cluster B kubeconfig | `clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig` |
| Verify nodes | `kubectl --kubeconfig=my-cce-cluster.kubeconfig get nodes` |
| Scale | `kubectl scale machinepool my-cce-cluster-pool-0 --replicas=N` |
| Delete cluster B | `kubectl delete cluster my-cce-cluster` |

---

## Appendix A: hack Scripts (optional automation)

> The main path uses the console + CloudShell; the hack scripts below run on any host with Go and create/clean up infrastructure in one command (they call the Huawei Cloud APIs and replace the manual console steps).

| Script | Purpose | Key environment variables |
|---|---|---|
| `hack/deploy-network` | One-shot VPC/subnet/key pair | `CLOUD_SDK_AK/SK`, `CCE_DEPLOY_REGION` |
| `hack/deploy-bastion` | One-shot bastion ECS | `CCE_DEPLOY_VPC/SUBNET` |
| `hack/deploy-mgmt-cluster` | One-shot management cluster A (default Turbo) | `CCE_DEPLOY_CATEGORY`, `CCE_DEPLOY_ENI_SUBNET`, `CCE_DEPLOY_SERVICE_CIDR`, etc. |
| `hack/cleanup-hw` | Delete CCE clusters/node pools/EIPs | `-cluster <id>` |
| `hack/survey-hw` | Inventory all resources | `CLOUD_SDK_AK/SK` |
| `hack/swr-login` | Get a temporary SWR login credential | `CLOUD_SDK_AK/SK` |

> **IAM agency (optional)**: the bastion can bind an IAM agency (`CCE_DEPLOY_BASTION_AGENCY`) and fetch temporary credentials from the ECS metadata (no static AK/SK), see the credential-rotation chapter of the legacy `docs/e2e-deployment-guide.md`.
