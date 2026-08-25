# 公网访问部署指导（对标 CAPA EKS 托管模式）

> 在华为云 CCE 管理集群上安装 CCE Provider，通过 Cluster API 声明式管理 CCE 工作负载集群。本方案采用 **CAPA EKS 托管模式的公网部署形态**：跳板机公网 SSH + 管理集群公网 endpoint + 节点公网拉取镜像（不经 SWR 搬运、不经零公网优化）。

> 对比：`docs/e2e-deployment-guide.md`（零公网，镜像全走 SWR/OBS 内网）适合私有化/内网环境；**本方案适合能访问公网 registry 的环境**（如海外 region 或允许出网的 VPC），体验更接近 CAPA。

---

## 架构概述

```
┌──────────────────────────────────────────────────────────────────┐
│  华为云 cn-north-4                                                 │
│                                                                    │
│  ┌────────────────────────────┐    ┌───────────────────────────┐  │
│  │  跳板机 ECS (公网)          │    │  管理集群 A (CCE Standard) │  │
│  │  capi-bastion              │    │  capi-mgmt-<timestamp>    │  │
│  │  · 公网 SSH (22)           │    │  · 公网 endpoint(EIP 自动绑)│  │
│  │  · 公网出站(装工具/拉镜像)   │    │  · CAPI core + Provider    │  │
│  └─────────────┬──────────────┘    │  · cert-manager            │  │
│                │ SSH                └──────────┬────────────────┘  │
│                ▼                               │ NAT 出网          │
│           ┌─────────┐                          ▼                    │
│           │ 运维人员 │                   quay.io / registry.k8s.io │
│           └─────────┘                   (公网直拉, 不搬运 SWR)      │
│                                                                    │
│  ┌────────────────────────────┐                                    │
│  │  工作负载集群 B (CCE)        │<── Provider 调用 CCE API 创建      │
│  │  · 公网或私有 endpoint      │                                    │
│  │  · 节点池(公网出站拉镜像)     │                                    │
│  └────────────────────────────┘                                    │
└──────────────────────────────────────────────────────────────────┘
```

**核心要点**（对标 CAPA）：
- **跳板机公网**：SSH 公网入站 + 出站 curl 下载工具 / 拉镜像（对标 CAPA EC2）。
- **管理集群 A 公网**：`deploy-mgmt-cluster` 自动绑定 EIP，公网+私有 endpoint（对标 CAPA EKS 公网+私有端点）。
- **公网拉镜像**：集群 A 节点经 NAT 网关出网，直接从 `quay.io`/`registry.k8s.io` 拉取 cert-manager/CAPI 镜像，**不搬运 SWR**（对标 CAPA 公网 registry）。
- Provider 镜像交付两种方式：**私有 SWR**（客户自推镜像 + imagePullSecret）或 **public SWR**（已发布公开镜像，免认证，对标 CAPA 官方镜像）。

---

## 前置条件

### 本地工具

```bash
# macOS
brew install docker kubectl go

# clusterctl v1.14.0（必须精确匹配 CAPI v1.14 契约）
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/clusterctl-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') -o clusterctl
chmod +x clusterctl && sudo mv clusterctl /usr/local/bin/
clusterctl version  # 应输出 v1.14.0
```

### 华为云资源

| 项目 | 要求 |
|---|---|
| IAM AK/SK | 有 CCE/VPC/EIP/NAT/ECS 操作权限 |
| 账户余额 | 充足（CCE 集群 + 节点 + NAT + EIP 按需计费） |
| VPC + 子网 | 已存在，或用 `hack/deploy-network` 一键创建 |
| SSH 密钥对 | 已存在，或用 `hack/deploy-bastion` 创建 |
| SWR 仓库 | 一个组织/命名空间（如 `capi_cce`）：方式 A 放客户自推的私有 Provider 镜像；方式 B 放已发布的 public Provider 镜像 |

### 代理剥离（重要）

所有 `go`/`kubectl`/`clusterctl`/`docker` 命令都需剥离本地代理：

```bash
alias nocloud='env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u SOCKS_PROXY -u all_proxy -u ALL_PROXY'
```

---

## 步骤总览

| 步骤 | 操作 | 耗时 |
|---|---|---|
| 1 | 准备网络与密钥 | 2 min |
| 2 | 创建跳板机（公网） | 2 min |
| 3 | 创建管理集群 A（公网 endpoint） | 10-15 min |
| 4 | 配置 NAT 出网（公网拉镜像） | 2 min |
| 5 | SSH 登录跳板机 + 装工具 | 5 min |
| 6 | 连集群 A（公网 endpoint） | 1 min |
| 7 | 安装 Provider（clusterctl init，公网直拉） | 5 min |
| 8 | Provider 镜像交付（私有 SWR / public SWR 二选一）+ webhook cert | 2 min |
| 9 | 创建工作负载集群 B | 10-20 min |
| 10 | 验证 | 2 min |
| 清理 | 删除所有资源 | 10 min |

---

## 步骤 1：准备网络与密钥

```bash
nocloud CLOUD_SDK_AK=<AK> CLOUD_SDK_SK=<SK> CCE_DEPLOY_REGION=cn-north-4 \
  go run ./hack/deploy-network
```

输出：

```
VPC: capi-vpc (...)
Node subnet: capi-subnet-node (id=..., neutron=...)
ENI  subnet: capi-subnet-eni (id=..., neutron=...)
Keypair: capi-node-key (created)
...
export CCE_DEPLOY_REGION="cn-north-4"
export CCE_DEPLOY_VPC="..."
export CCE_DEPLOY_SUBNET="..."
export CCE_DEPLOY_ENI_SUBNET="..."  # neutron_subnet_id
export CCE_DEPLOY_KEYPAIR="capi-node-key"
```

记下 `CCE_DEPLOY_VPC` / `CCE_DEPLOY_SUBNET` / `CCE_DEPLOY_ENI_SUBNET`，后续步骤用。

## 步骤 2：创建跳板机（公网）

```bash
nocloud CLOUD_SDK_AK=<AK> CLOUD_SDK_SK=<SK> \
  CCE_DEPLOY_REGION=cn-north-4 \
  CCE_DEPLOY_VPC=<VPC-ID> \
  CCE_DEPLOY_SUBNET=<节点子网-ID> \
  go run ./hack/deploy-bastion
```

输出：

```
BASTION_SERVER_ID=...
BASTION_PUBLIC_IP=<公网IP>
BASTION_KEY=capi-bastion-key.pem
ssh -i capi-bastion-key.pem root@<公网IP>
```

> 跳板机用 `capi-bastion-key` 密钥对（私钥保存在本地 `capi-bastion-key.pem`），对标 CAPA EC2 密钥对登录。

## 步骤 3：创建管理集群 A（公网 endpoint）

```bash
nocloud CLOUD_SDK_AK=<AK> CLOUD_SDK_SK=<SK> \
  CCE_DEPLOY_REGION=cn-north-4 \
  CCE_DEPLOY_VPC=<VPC-ID> \
  CCE_DEPLOY_SUBNET=<节点子网-ID> \
  CCE_DEPLOY_KEYPAIR=capi-bastion-key \
  CCE_DEPLOY_K8S_VERSION=v1.35 \
  go run ./hack/deploy-mgmt-cluster
```

输出（含公网绑定）：

```
creating management cluster "capi-mgmt-xxxxx" ...
cluster Available: version=v1.35
  endpoint type=Internal url=https://10.0.x.x:5443
  endpoint type=External url=https://<公网IP>:5443   ← 自动绑 EIP（对标 CAPA 公网+私有）
binding public EIP (hack/bind-eip)…
node pool has 2 active node(s)
kubeconfig written to capi-mgmt.kubeconfig
MGMT_CLUSTER_ID=...
```

> **对标 CAPA**：管理集群 A 默认公网+私有 endpoint（`CCE_DEPLOY_PUBLIC=true` 自动绑 EIP）。设 `CCE_DEPLOY_PUBLIC_CIDRS` 可限制公网来源 IP；设 `CCE_DEPLOY_PUBLIC=false` 则仅私有。

## 步骤 4：节点出网（公网 IP 默认 / NAT 可选）

节点出网有两种方式，**默认走节点公网 IP**（对标 AWS 公有子网）：

**方式 A（默认，推荐）**：节点公网 IP

  `deploy-mgmt-cluster` 默认 `CCE_DEPLOY_PUBLIC_NODES=true`，创建节点池时给每个节点绑公网 EIP，节点直连出网（对标 AWS 公有子网 + IGW），**无需 NAT 网关**。步骤 3 已自动完成，无需额外操作。

  **方式 B（可选）**：NAT 网关

若设 `CCE_DEPLOY_PUBLIC_NODES=false`（节点私有），则需 NAT 网关出网：

```bash
nocloud CLOUD_SDK_AK=<AK> CLOUD_SDK_SK=<SK> \
  CCE_DEPLOY_REGION=cn-north-4 \
  CCE_DEPLOY_VPC=<VPC-ID> \
  CCE_DEPLOY_SUBNET=<节点子网-ID> \
  go run ./hack/nat-egress -mode create
```

> 有了节点公网 IP（方式 A）或 NAT（方式 B），集群 A 节点才能从 `quay.io`/`registry.k8s.io` 公网拉取 cert-manager/CAPI 镜像（本方案不搬运 SWR，对标 CAPA 公网 registry）。

## 步骤 5：SSH 登录跳板机 + 安装工具

```bash
ssh -i capi-bastion-key.pem -o StrictHostKeyChecking=no root@<BASTION_PUBLIC_IP>
```

在跳板机上（公网出站，直接 curl 下载，无需本地中转）：

```bash
# kubectl v1.30（linux-amd64）
curl -LO "https://dl.k8s.io/release/v1.30.0/bin/linux/amd64/kubectl"
install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl

# clusterctl v1.14.0
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/clusterctl-linux-amd64 -o clusterctl
chmod +x clusterctl && mv clusterctl /usr/local/bin/clusterctl

kubectl version --client
clusterctl version
```

## 步骤 6：连集群 A（公网 endpoint）

```bash
# 上传 kubeconfig（本地 scp）——或直接从公网 endpoint 连接
scp -i capi-bastion-key.pem capi-mgmt.kubeconfig root@<BASTION_PUBLIC_IP>:/root/
ssh -i capi-bastion-key.pem root@<BASTION_PUBLIC_IP> \
  'export KUBECONFIG=/root/capi-mgmt.kubeconfig; kubectl get nodes'
# 应看到 2 个 Ready 节点
```

> 管理集群 A 有公网 endpoint，kubeconfig 也可用公网 server 从跳板机直接访问（对标 CAPA EKS 公网端点）。

## 步骤 7：安装 Provider（clusterctl init，公网直拉）

```bash
# 本地：生成 manifests（见 e2e 指南附录 A），上传 metadata + components 到跳板机
scp -i capi-bastion-key.pem _artifacts/cceswr/infrastructure-components.yaml metadata.yaml \
  root@<BASTION_PUBLIC_IP>:/root/

# 跳板机：注册 clusterctl（本地 provider 源）
mkdir -p ~/.cluster-api
cat > ~/.cluster-api/clusterctl.yaml <<'EOF'
providers:
  - name: "cce"
    url: "file:///root/cce/infrastructure-cce/v0.1.0/infrastructure-components.yaml"
    type: "InfrastructureProvider"
EOF

# 跳板机：clusterctl init（cert-manager/CAPI 从公网 registry 直拉，不搬运 SWR）
export KUBECONFIG=/root/capi-mgmt.kubeconfig
clusterctl init --infrastructure cce
```

> **对标 CAPA**：本步骤 cert-manager（quay.io）、CAPI（registry.k8s.io）**公网直拉**（节点有 NAT 出网），无需搬运到 SWR + imagePullSecret（省去 e2e 指南的"搬运 CAPI 镜像"步骤）。

## 步骤 8：Provider 镜像交付（私有 SWR / public SWR 二选一）+ webhook cert

Provider 镜像交付有两种方式：**方式 A 私有 SWR**（客户自推镜像 + imagePullSecret）或**方式 B public SWR**（已发布公开镜像，免认证直拉，对标 CAPA 官方镜像）。webhook TLS cert 两种方式都需要。

### 方式 A：私有 SWR（客户自建镜像）

镜像推到**私有** SWR 仓库，节点拉取需 imagePullSecret：

```bash
# 跳板机
export KUBECONFIG=/root/capi-mgmt.kubeconfig

# A.1 SWR imagePullSecret（Provider 镜像）
nocloud CLOUD_SDK_AK=<AK> CLOUD_SDK_SK=<SK> go run ./hack/swr-login   # 输出 SWR_USER/PASSWORD
kubectl -n cce-provider-system create secret docker-registry cce-provider-swr-secret \
  --docker-server=swr.cn-north-4.myhuaweicloud.com \
  --docker-username='<SWR_USER>' --docker-password='<SWR_PASSWORD>' --docker-email='noreply@huawei.cloud'

# A.2 加 imagePullSecrets + 重启（方式 B 跳过此步）
kubectl -n cce-provider-system patch deployment cce-provider-controller-manager \
  --type=json -p='[{"op":"add","path":"/spec/template/spec/imagePullSecrets","value":[{"name":"cce-provider-swr-secret"}]}]'
kubectl -n cce-provider-system rollout restart deployment/cce-provider-controller-manager
```

### 方式 B：public SWR（已发布公开镜像）

Provider 镜像已提前推到**公开** SWR 仓库（如 `swr.cn-north-4.myhuaweicloud.com/capi_cce/cce-provider-controller:latest` 设为 **public**），节点**免认证直拉**（对标 CAPA 官方镜像），**无需 imagePullSecret**：

```bash
# 跳板机
export KUBECONFIG=/root/capi-mgmt.kubeconfig
kubectl -n cce-provider-system rollout restart deployment/cce-provider-controller-manager
```

> 将 SWR 仓库设为 public：控制台 → 容器镜像服务 SWR → 仓库 → 管理 → 设置为"公开"（公开后任意账号/匿名均可 `docker pull`，适合 Provider 开源发布的场景）。

### webhook TLS cert（cert-manager 自动签发）

webhook 证书已由 **cert-manager 自动签发**（`config/certmanager` 的 Issuer + Certificate 随组件部署，`webhook-service-cert` Secret 自动创建，webhook 配置 caBundle 经 `cert-manager.io/inject-ca-from` 注入）——**无需手动创建**。

> **方式 B 一次性完成**：`clusterctl init` 装完（cert-manager → provider），webhook 证书自动签发 + public 镜像免认证拉取，provider pod 直接 Running，**无任何手动步骤**（对标 CAPA）。
> 注意：cert-manager/CAPI 的 pod 本方案用公网镜像（不换 SWR）；Provider 镜像按方式 A（私有 + imagePullSecret）或方式 B（public 免认证）交付。

## 步骤 9：创建工作负载集群 B

```bash
# 跳板机：用 clusterctl generate cluster（Standard 默认 / Turbo flavor）
mkdir -p ~/.cluster-api/overrides/infrastructure-cce/v0.1.0
# 上传 cluster-template-clusterctl.yaml / cluster-template-turbo.yaml 后：
cp /root/cluster-template-clusterctl.yaml ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template.yaml
cp /root/cluster-template-turbo.yaml    ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template-turbo.yaml

# 生成（Standard）
clusterctl generate cluster my-cce-cluster --kubernetes-version v1.35.0 > my-cluster.yaml
# 或 Turbo
clusterctl generate cluster my-cce-cluster --flavor turbo --kubernetes-version v1.35.0 > my-cluster.yaml

# 替换 VERIFY-* 占位符（region/VPC/子网/ENI 子网/密钥对/AZ）
# 创建凭据 Secret + bootstrap Secret
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default --from-literal=accessKey='<AK>' --from-literal=secretKey='<SK>'
kubectl create secret generic my-cce-cluster-bootstrap \
  --namespace default --from-literal=value=""

kubectl apply -f my-cluster.yaml
```

## 步骤 10：验证

```bash
# 跳板机
kubectl get cluster my-cce-cluster -w        # 等 PHASE=Provisioned
kubectl get ccemanagedcontrolplane my-cce-cluster-control-plane -w   # Ready=True
kubectl get ccemanagedmachinepool my-cce-cluster-pool-0 -w          # Ready=True

clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig
kubectl --kubeconfig=my-cce-cluster.kubeconfig get nodes            # 1 个 Ready 节点
```

> 集群 B 可设 `endpointAccess.public: true` 走公网（对标 CAPA EKS 公网端点），否则默认私有（同 VPC 跳板机访问）。

---

## 清理

```bash
# 跳板机：删集群 B
kubectl delete cluster my-cce-cluster

# 本地：删管理集群 A
nocloud CLOUD_SDK_AK=<AK> CLOUD_SDK_SK=<SK> CCE_DEPLOY_REGION=cn-north-4 \
  go run ./hack/deploy-mgmt-cluster -delete -cluster <MGMT_CLUSTER_ID>

# 本地：删 NAT 出网资源
nocloud CLOUD_SDK_AK=<AK> CLOUD_SDK_SK=<SK> CCE_DEPLOY_REGION=cn-north-4 \
  go run ./hack/nat-egress -mode delete-all

# 本地：删跳板机 ECS + EIP + 密钥对 + 安全组 + 子网 + VPC（见 e2e 指南清理章节）
```

---

## 故障排查

### Provider pod 卡在 ContainerCreating / ImagePullBackOff
- **方式 A（私有 SWR）**：未建 `cce-provider-swr-secret` 或 SWR 凭据错误 → 执行步骤 8 方式 A（imagePullSecret + imagePullSecrets patch）。
- **方式 B（public SWR）**：确认仓库已设为 public（免认证），否则拉取会 401/ImagePullBackOff。
- **webhook cert 未建**：创建 `webhook-service-cert`（步骤 8 webhook 段）后重启 provider。

### cert-manager / CAPI pod ImagePullBackOff（公网拉镜像失败）
- 确认 NAT 网关 + SNAT 规则 ACTIVE（`hack/nat-egress -mode list`）。
- 确认节点所在子网被 SNAT 规则覆盖（`hack/nat-egress -mode create` 已默认覆盖 `CCE_DEPLOY_SUBNET`）。
- 若 `registry.k8s.io`/`quay.io` 在所在 region 不可达（国内网络），改用 e2e 指南的 **SWR 搬运方案**（零公网）。

### 集群 A 无公网 endpoint
- 确认 `CCE_DEPLOY_PUBLIC` 未设 `false`（默认 `true` 自动绑 EIP）。
- 若绑 EIP 失败，手动 `nocloud go run ./hack/bind-eip -cluster <MGMT_CLUSTER_ID>`。

### 集群创建失败 `APIGW.0308`（429 限流）
CCE 写限流 10 次/分钟，停止写操作等窗口清零（1-10 分钟）。

### 代理导致连接失败
所有命令前加 `nocloud`（剥离本地代理）。

---

## 环境变量速查

| 变量 | 用途 | 示例值 |
|---|---|---|
| `CLOUD_SDK_AK` / `CLOUD_SDK_SK` | 华为云 AK/SK | `<AK>` / `<SK>` |
| `CCE_DEPLOY_REGION` | 区域 | `cn-north-4` |
| `CCE_DEPLOY_VPC` | VPC ID | `a375f6cf-...` |
| `CCE_DEPLOY_SUBNET` | 节点子网 ID | `f462cf46-...` |
| `CCE_DEPLOY_ENI_SUBNET` | ENI 子网 neutron ID（Turbo） | `62fcda00-...` |
| `CCE_DEPLOY_KEYPAIR` | 节点 SSH 密钥对 | `capi-bastion-key` |
| `CCE_DEPLOY_K8S_VERSION` | K8s 版本 | `v1.35` |
| `CCE_DEPLOY_PUBLIC` | 集群 A 公网开关（默认 true） | `true` / `false` |
| `CCE_DEPLOY_PUBLIC_CIDRS` | 公网来源白名单（逗号分隔） | `1.2.3.4/32` |
| `CCE_DEPLOY_BASTION_AGENCY` | 跳板机 IAM 委托（可选） | `capi-agency` |
| `CCE_DEPLOY_MGMT_AZS` | 管理集群多 AZ（可选） | `cn-north-4a,cn-north-4b` |

## 工具速查

| 工具 | 用途 |
|---|---|
| `hack/deploy-network` | 建 VPC/双子网/密钥对 |
| `hack/deploy-bastion` | 建跳板机（公网 SSH，可选 IAM 委托） |
| `hack/deploy-mgmt-cluster` | 创建/删除管理集群（公网 endpoint 自动绑 EIP） |
| `hack/nat-egress` | 创建/删除 NAT 出网（公网拉镜像） |
| `hack/bind-eip` | 给既有集群绑定公网 EIP |
| `hack/swr-login` | 生成 SWR 临时登录 token |
| `hack/survey-hw` | 盘点所有华为云资源 |

---

## 与零公网方案（e2e-deployment-guide.md）的差异

| 维度 | 本方案（公网，对标 CAPA） | e2e 指南（零公网） |
|---|---|---|
| 集群 A 端点 | 公网+私有（自动绑 EIP） | 仅私有（内网 endpoint） |
| 镜像拉取 | 公网直拉 quay.io/registry.k8s.io（NAT 出网） | 搬运 SWR + OBS 内网 |
| CAPI 镜像 | 公网直拉，不搬运 | 搬运到 SWR |
| imagePullSecret | 仅 Provider pod | Provider + cert-manager + CAPI 全部 |
| NAT 网关 | 需要（节点出网） | 不需要 |
| 适用场景 | 可访问公网 registry（海外/允许出网） | 私有化/内网/等保 |
