# 端到端部署文档：ECS 跳板机 + CCE 管理集群 + 工作负载集群

> 本文档记录在华为云上部署 **cloudnative-cluster-api-provider-cce**（本项目）的完整端到端流程。
>
> **架构**：ECS 跳板机（运维入口）→ CCE 管理集群 A（运行 CAPI + Provider）→ CCE 工作负载集群 B（业务集群，私有 API）。
>
> **文档性质**：活文档。后续测试严格按本文档执行，遇到新问题追加到 [踩坑记录](#踩坑记录测试问题--修正) 章节并标记修正，不要另起新文档。

---

## 架构概述

```
┌──────────────────────── 华为云 cn-north-4 ────────────────────────┐
│                                                                    │
│   ECS 跳板机 (capi-bastion)          CCE 管理集群 A (capi-mgmt-*)  │
│   · 公网 IP + SSH 22                  · CAPI core + cert-manager   │
│   · kubectl / clusterctl              · CCE Provider (本项目)      │
│        │                                    │                     │
│        │ 同 VPC（访问内网 API Server）        │ 华为云 CCE API      │
│        └────────────────────────────────────┤                     │
│                                             ▼                     │
│                                 CCE 工作负载集群 B (私有 API)     │
│                                 · 控制面（华为托管）              │
│                                 · 节点池（CCE 创建）             │
│                                                                    │
│   SWR 镜像仓库 (capi_cce)  ← 所有镜像走内网拉取，零公网出网      │
└────────────────────────────────────────────────────────────────────┘
```

**核心要点**：
1. **管理集群 A 的 API Server 默认仅内网 endpoint**（`https://10.0.x.x:5443`，`publicAccess=true` 也不会自动分配公网 IP，需显式绑定 EIP）。因此 kubectl/clusterctl 操作管理集群必须在**同 VPC 的跳板机**上执行。
2. **零公网方案**：节点安装走华为云内网 OBS（不需要出公网）；CAPI/cert-manager 镜像搬运到 SWR（内网仓库），全程不依赖 NAT 网关、不开放公网出网。

---

## 前置条件

### 华为云资源

| 项目 | 要求 |
|---|---|
| IAM AK/SK | 有 CCE/VPC/EIP/ECS/SWR 操作权限 |
| 账户余额 | 充足（CCE 集群 + 节点 + ECS 按需计费；余额不足会导致 NAT 冻结失败等异常） |
| 区域 | `cn-north-4`（本文档默认） |
| SWR 命名空间 | `capi_cce`（可用 `hack/swr-login` 自动创建） |

### 本地工具（macOS）

```bash
# docker + kubectl + go（go >= 1.26）
brew install docker kubectl go

# clusterctl v1.14.0（必须精确匹配 CAPI 契约）
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/clusterctl-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') -o clusterctl
chmod +x clusterctl && sudo mv clusterctl /usr/local/bin/
clusterctl version   # 必须 v1.14.0（本地 brew 版本可能偏旧，见踩坑 #4）

# docker buildx（Docker Desktop 自带）
docker buildx version
```

### 代理剥离（重要）

所有 `go`/`docker`/`ssh`/`curl` 命令需剥离本地代理环境变量，否则连接华为云/registry 会失败：

```bash
alias nocloud='env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u SOCKS_PROXY -u all_proxy -u ALL_PROXY'
```

### 凭证环境变量

```bash
export CLOUD_SDK_AK='<你的AK>'
export CLOUD_SDK_SK='<你的SK>'
export CCE_SMOKE_REGION='cn-north-4'
export CCE_SMOKE_AZ='cn-north-4a'
```

---

## 步骤总览

| 步骤 | 操作 | 执行位置 | 验证状态 |
|---|---|---|---|
| 1 | 准备网络与密钥（VPC/子网/密钥对） | 本地 | ✅ 已验证 |
| 2 | 创建跳板机 ECS | 本地 | ✅ 已验证 |
| 3 | 创建 CCE 管理集群 A | 本地 | ✅ 已验证 |
| 4 | 构建 provider 镜像并推送 SWR | 本地 | ✅ 已验证 |
| 5 | 搬运 CAPI/cert-manager 镜像到 SWR（零公网） | 本地 | ✅ 已验证 |
| 6 | 生成 manifests + clusterctl init 部署 Provider | 跳板机 | ⏳ 待验证 |
| 7 | 创建 CCE 工作负载集群 B | 跳板机 | ⏳ 待验证 |
| 8 | 验证（跳板机访问集群 B 私有 API） | 跳板机 | ⏳ 待验证 |
| 9 | 清理所有资源 | 本地 | ✅ 已验证 |

> 步骤 6-8 因管理集群节点卡 Installing（已定位为 DNS 问题并修复，见踩坑 #1）未跑通，方案参考 `docs/cloud-to-cloud-deployment-guide.md` 步骤 5-11。

---

## 步骤 1：准备网络与密钥

一键创建 VPC + 节点/ENI 子网 + 密钥对（幂等，按名复用）：

```bash
nocloud go run ./hack/smoke-setup
```

输出示例：

```
VPC: capi-smoke-vpc (352d987e-...)
Node subnet: capi-smoke-subnet-node (id=... neutron=...)
ENI  subnet: capi-smoke-subnet-eni (id=... neutron=...)
Keypair: capi-smoke-key (created)

--- export for scripts ---
export CCE_SMOKE_VPC="352d987e-..."
export CCE_SMOKE_SUBNET="f3ca6c0e-..."
export CCE_SMOKE_KEYPAIR="capi-smoke-key"
```

> ⚠️ **关键约束（踩坑 #1）**：`smoke-setup` 建子网时**必须显式指定 DNS**（`primary_dns=100.125.1.250` + `secondary_dns=100.125.129.250`）。若子网未指定 DNS，节点会拿到错误 DNS（`10.0.x.254`），导致节点安装时无法解析 OBS 域名、cce-agent 下载失败、节点永久卡 Installing。**已在本项目 `hack/smoke-setup` 中修复**。

记录以下值供后续步骤使用：

```bash
export CCE_SMOKE_VPC='<VPC-ID>'
export CCE_SMOKE_SUBNET='<节点子网-ID>'
export CCE_SMOKE_ENI_SUBNET='<ENI子网 neutron-ID>'
export CCE_SMOKE_KEYPAIR='capi-smoke-key'
```

---

## 步骤 2：创建跳板机 ECS

创建跳板机（公网 IP + SSH 22 安全组 + 保留私钥）：

```bash
nocloud go run ./hack/create-bastion
```

输出示例：

```
Keypair capi-bastion-key created, private key saved to capi-bastion-key.pem
Image: 7d940784-...        # Huawei Cloud EulerOS 2.0 x86_64
Security group: c696ea4b-...
ECS created: a431a8be-...
  server status=ACTIVE

BASTION_SERVER_ID=a431a8be-...
BASTION_PUBLIC_IP=116.205.112.173
BASTION_KEY=capi-bastion-key.pem
ssh -i capi-bastion-key.pem root@116.205.112.173
```

> ⚠️ **关键约束（踩坑 #2）**：跳板机密钥对 `capi-bastion-key` 的**私钥必须保留**（脚本已保存到 `capi-bastion-key.pem`）。后续步骤 3 的管理集群节点也改用此密钥对（而非 `capi-smoke-key`，因为 `smoke-setup` 创建 `capi-smoke-key` 时丢弃了私钥，导致节点异常时无法 SSH 排查）。

验证 SSH（首次连接需等 EIP 生效约 30-60 秒）：

```bash
ssh -i capi-bastion-key.pem -o StrictHostKeyChecking=no root@116.205.112.173 'echo SSH_OK; hostname'
```

---

## 步骤 3：创建 CCE 管理集群 A

```bash
nocloud CCE_SMOKE_VPC="$CCE_SMOKE_VPC" \
  CCE_SMOKE_SUBNET="$CCE_SMOKE_SUBNET" \
  CCE_SMOKE_KEYPAIR='capi-bastion-key' \
  CCE_SMOKE_K8S_VERSION='v1.35' \
  go run ./hack/create-mgmt-cluster
```

**做什么**：
- 创建 CCE Standard 集群 `capi-mgmt-<timestamp>`（flavor `cce.s1.small`，vpc-router，公开 API）。
- 创建节点池 `mgmt-pool-0`（默认 2 节点，`c6.large.2`，Huawei Cloud EulerOS 2.0）。
- 等待集群 Available + 节点 Active（超时 30 分钟）。
- 下载 kubeconfig 到 `capi-mgmt.kubeconfig`。

输出示例：

```
creating management cluster "capi-mgmt-90359" ...
cluster created: 3a05bdc4-...
  phase=Available (want Available)
cluster Available: version=v1.35
  endpoint type=Internal url=https://10.0.1.14:5443
node pool created: ... (flavor c6.large.2, nodes 2)
  activeNodes=2 (want 2)
kubeconfig written to capi-mgmt.kubeconfig

MGMT_CLUSTER_ID=...
MGMT_POOL_ID=...
```

> ⚠️ **关键点（踩坑 #3）**：管理集群 `publicAccess=true` 时**只有 Internal endpoint**（`https://10.0.1.x:5443`），无公网 endpoint。kubeconfig 的 server 是内网 IP，只能从**同 VPC 的跳板机**访问。因此后续 kubectl/clusterctl 操作要在跳板机上执行。

**节点 Active 后的验证（可选，本步骤内已含等待）**：

```bash
# 节点状态应为 Active
nocloud CCE_SMOKE_REGION=cn-north-4 go run ./hack/create-mgmt-cluster -list
```

---

## 步骤 4：构建 provider 镜像并推送 SWR

### 4.1 获取 SWR 临时登录 token

```bash
nocloud go run ./hack/swr-login
```

输出（临时 token 约 1 小时有效，过期重跑）：

```
namespace capi_cce ready
SWR_REGISTRY=swr.cn-north-4.myhuaweicloud.com
SWR_USER=cn-north-4@<ID>
SWR_PASSWORD=<临时token>
SWR_IMAGE=swr.cn-north-4.myhuaweicloud.com/capi_cce/cce-provider-controller:latest
```

### 4.2 docker login SWR

```bash
echo '<SWR_PASSWORD>' | docker login swr.cn-north-4.myhuaweicloud.com -u '<SWR_USER>' --password-stdin
```

### 4.3 构建镜像（⚠️ 两个关键参数，见踩坑 #5 #6）

```bash
docker build --platform linux/amd64 --provenance=false --sbom=false \
  -t swr.cn-north-4.myhuaweicloud.com/capi_cce/cce-provider-controller:latest .
```

> **踩坑 #5（架构）**：管理集群节点是 x86_64（`c6.large.2`）。Docker Desktop（Apple Silicon）默认构建 arm64，必须显式 `--platform linux/amd64`。
>
> **踩坑 #6（attestation）**：Docker BuildKit 默认附加 provenance/SBOM attestation，SWR 无法解析（`Invalid image, fail to parse 'manifest.json'`），必须 `--provenance=false --sbom=false`。

### 4.4 推送到 SWR

```bash
docker push swr.cn-north-4.myhuaweicloud.com/capi_cce/cce-provider-controller:latest
```

---

## 步骤 5：搬运 CAPI/cert-manager 镜像到 SWR（零公网）

> 目的：管理集群节点无公网出网（也不需要 NAT）。CAPI core 和 cert-manager 的镜像默认从 `registry.k8s.io`/`quay.io` 拉取，需先搬运到 SWR，让节点走内网拉取。
>
> 版本：CAPI `v1.14.0`，cert-manager `v1.21.1`（clusterctl v1.14.0 的 `CertManagerDefaultVersion`）。

```bash
for img in \
  quay.io/jetstack/cert-manager-controller:v1.21.1 \
  quay.io/jetstack/cert-manager-cainjector:v1.21.1 \
  quay.io/jetstack/cert-manager-webhook:v1.21.1 \
  registry.k8s.io/cluster-api/cluster-api-controller:v1.14.0 \
  registry.k8s.io/cluster-api/kubeadm-bootstrap-controller:v1.14.0 \
  registry.k8s.io/cluster-api/kubeadm-control-plane-controller:v1.14.0; do
  name=$(basename "$img")
  dst="swr.cn-north-4.myhuaweicloud.com/capi_cce/$name"
  echo "--- $name ---"
  docker buildx imagetools create --platform linux/amd64 -t "$dst" "$img"
done
```

> ⚠️ **关键约束（踩坑 #7）**：必须用 `docker buildx imagetools create --platform linux/amd64`（直接从 registry copy 并强制 amd64），**不要**用 `docker pull --platform linux/amd64` + `docker push`——Docker Desktop 的 `pull --platform` 对 multi-arch 镜像不生效（仍拉到 arm64），导致节点无法运行。

---

## 步骤 6：部署 Provider（clusterctl init）

> 参考 `docs/cloud-to-cloud-deployment-guide.md` 步骤 5-10。以下为跳板机上的操作流程。

### 6.1 生成 infrastructure-components.yaml（本地）

```bash
ARTIFACTS=_artifacts/cceswr
mkdir -p "$ARTIFACTS"

# kustomize 覆盖镜像为 SWR 地址
cat > "$ARTIFACTS/kustomization.yaml" <<'EOF'
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../config/default
images:
  - name: swr.cn-north-4.myhuaweicloud.com/cce-provider/controller
    newName: swr.cn-north-4.myhuaweicloud.com/capi_cce/cce-provider-controller
    newTag: latest
EOF

kubectl kustomize "$ARTIFACTS" > "$ARTIFACTS/infrastructure-components-raw.yaml"

# 生成自签 CA + webhook server 证书
cd "$ARTIFACTS"
openssl genrsa -out ca.key 2048 2>/dev/null
openssl req -x509 -new -nodes -key ca.key -sha256 -days 365 -subj "/CN=cce-provider-ca" -out ca.crt 2>/dev/null
openssl genrsa -out server.key 2048 2>/dev/null
cat > server.conf <<'EOF'
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
EOF
openssl req -new -key server.key -out server.csr -config server.conf 2>/dev/null
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 365 -sha256 -extfile server.conf -extensions ext 2>/dev/null
cd ../..

# 注入 caBundle
CABUNDLE="$(base64 < "$ARTIFACTS/ca.crt" | tr -d '\n')"
awk -v cab="$CABUNDLE" '{ print; if ($0 == "  clientConfig:") print "    caBundle: " cab }' \
  "$ARTIFACTS/infrastructure-components-raw.yaml" > "$ARTIFACTS/infrastructure-components.yaml"

echo "注入 caBundle 到 $(grep -c caBundle "$ARTIFACTS/infrastructure-components.yaml") 个 webhook"
```

### 6.2 注册 clusterctl + 上传到跳板机

```bash
CCE_PROVIDER_VERSION=v0.1.0
CLUSTERCTL_SRC="/tmp/cce/infrastructure-cce/${CCE_PROVIDER_VERSION}"
mkdir -p "$CLUSTERCTL_SRC" "$HOME/.cluster-api"
cp "$ARTIFACTS/infrastructure-components.yaml" metadata.yaml "$CLUSTERCTL_SRC/"
cat > "$HOME/.cluster-api/clusterctl.yaml" <<EOF
providers:
  - name: "cce"
    url: "file://$CLUSTERCTL_SRC/infrastructure-components.yaml"
    type: "InfrastructureProvider"
EOF

# 上传到跳板机（跳板机装 kubectl + clusterctl v1.14.0）
scp -i capi-bastion-key.pem capi-mgmt.kubeconfig root@<跳板机IP>:/root/
scp -i capi-bastion-key.pem "$CLUSTERCTL_SRC/infrastructure-components.yaml" metadata.yaml root@<跳板机IP>:/root/
```

### 6.3 跳板机安装工具并部署

```bash
# 跳板机上安装 clusterctl v1.14.0（linux-amd64）+ kubectl
ssh -i capi-bastion-key.pem root@<跳板机IP> 'bash -s' <<'EOF'
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/clusterctl-linux-amd64 -o /usr/local/bin/clusterctl
chmod +x /usr/local/bin/clusterctl
export KUBECONFIG=/root/capi-mgmt.kubeconfig
# 注册 provider（跳板机 ~/.cluster-api/clusterctl.yaml 指向本地 file://）
clusterctl init --infrastructure cce
EOF
```

### 6.4 修复 Provider pod（SWR 私有仓库 + webhook 证书）

```bash
# 跳板机上创建 SWR 拉镜像 Secret + webhook TLS Secret（参考 cloud-to-cloud-deployment-guide 步骤 8）
kubectl create secret docker-registry cce-provider-swr-secret \
  --namespace cce-provider-system \
  --docker-server=swr.cn-north-4.myhuaweicloud.com \
  --docker-username='<SWR_USER>' --docker-password='<SWR_PASSWORD>' --docker-email='noreply@huawei.cloud'

kubectl -n cce-provider-system create secret tls webhook-service-cert \
  --cert=server.crt --key=server.key

kubectl -n cce-provider-system patch deployment cce-provider-controller-manager \
  --type=json -p='[{"op":"add","path":"/spec/template/spec/imagePullSecrets","value":[{"name":"cce-provider-swr-secret"}]}]'
```

---

## 步骤 7：创建 CCE 工作负载集群 B

> 参考 `docs/cloud-to-cloud-deployment-guide.md` 步骤 11。

```bash
# 跳板机上
cp config/samples/cluster-template.yaml /tmp/my-cluster.yaml
# 替换占位符：VERIFY-VPC-ID / VERIFY-SUBNET-ID / VERIFY-KEYPAIR-NAME

kubectl create secret generic my-cce-cluster-credentials \
  --namespace default --from-literal=accessKey='<AK>' --from-literal=secretKey='<SK>'
kubectl create secret generic my-cce-cluster-bootstrap \
  --namespace default --from-literal=value=""

kubectl apply -f /tmp/my-cluster.yaml
kubectl get ccemanagedcontrolplane my-cce-cluster-control-plane -w   # 等 Ready=True
kubectl get ccemanagedmachinepool my-cce-cluster-pool-0 -w            # 等 Ready=True, Replicas=1
```

---

## 步骤 8：验证（跳板机访问集群 B 私有 API）

```bash
# 跳板机上（与集群 B 同 VPC，可访问内网 API Server）
clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig
kubectl --kubeconfig=my-cce-cluster.kubeconfig get nodes
```

> 集群 B 默认 `endpointAccess.public: false`，kubeconfig server 是内网 IP，只能从同 VPC 的跳板机访问——这正是跳板机存在的意义。

---

## 步骤 9：清理所有资源

> ⚠️ 清理顺序严格按依赖倒序，否则会因资源占用/死锁失败（见踩坑 #8 #9）。

```bash
# 9.1 删除工作负载集群 B（跳板机）
kubectl delete cluster my-cce-cluster

# 9.2 删除管理集群 A（节点卡 Installing 时的特殊处理见踩坑 #9）
nocloud go run ./hack/create-mgmt-cluster -delete -cluster '<MGMT_CLUSTER_ID>'

# 9.3 清理跳板机 ECS + EIP + 密钥对 + 安全组 + 子网 + VPC
# （见踩坑 #9：若 VPC 有 peering 对等连接，需先删 peering 再删 VPC）
```

> 完整清理脚本可参考本次测试使用的临时脚本逻辑（删 ECS → 删密钥对 → 删 EIP → 删安全组 → 删子网 → 删 VPC → 删 peering）。

---

## 踩坑记录（测试问题 + 修正）

> **本表为活文档核心**：后续测试遇到问题，按序追加记录（现象 / 根因 / 修正），并在上方对应步骤同步修正。

| # | 问题现象 | 根因 | 修正 | 状态 |
|---|---|---|---|---|
| 1 | 管理集群节点永久卡 `Installing`（8+ 小时），`kubelet inactive` | 子网未指定 DNS，节点拿到错误 DNS（`10.0.x.254` 不响应），无法解析 OBS 域名，cce-agent 下载失败 | `smoke-setup` 建子网显式指定 `primary_dns=100.125.1.250` + `secondary_dns=100.125.129.250` | ✅ 已修复 |
| 2 | 节点异常时无法 SSH 排查 | `smoke-setup` 创建密钥对时丢弃了私钥（`NovaCreateKeypair` 响应用 `_` 忽略） | 跳板机/节点改用 `capi-bastion-key`（`create-bastion` 脚本保留私钥到 `capi-bastion-key.pem`） | ✅ 已修复 |
| 3 | 管理集群 `publicAccess=true` 仍无公网 endpoint | CCE 创建集群不自动分配公网 IP，需显式绑定 EIP | 用同 VPC 的跳板机访问内网 endpoint（或显式 `hack/bind-eip` 绑定） | ✅ 已记录 |
| 4 | `clusterctl version` 是 v1.13.4，与 CAPI v1.14 契约不匹配 | brew 安装的 clusterctl 版本偏旧 | 手动下载 clusterctl v1.14.0 | ✅ 已修复 |
| 5 | 构建的 provider 镜像在 x86_64 节点无法运行 | Docker Desktop（Apple Silicon）默认构建 arm64 | `docker build --platform linux/amd64` | ✅ 已修复 |
| 6 | `docker push` 到 SWR 报 `Invalid image, fail to parse 'manifest.json'` | BuildKit 默认附加 attestation（provenance/SBOM），SWR 不支持 | `docker build --provenance=false --sbom=false` | ✅ 已修复 |
| 7 | `docker pull --platform linux/amd64` 拉到 arm64 镜像 | Docker Desktop 对 multi-arch 镜像的 `pull --platform` 不生效 | 用 `docker buildx imagetools create --platform linux/amd64` 搬运镜像 | ✅ 已修复 |
| 8 | NAT 网关创建失败 `CBC.30060005 Frozen CbcDeposit Failed!` | 账户余额不足，NAT 冻结失败 | 零公网方案（搬运镜像到 SWR）**不需要 NAT**；充值余额 | ✅ 已记录 |
| 9 | 节点卡 Installing 时无法删节点/节点池/集群（`CCE_CM.0002`/`CCE.01403006`/`CCE.01400024` 死锁） | CCE 限制：Installing 节点禁止删除，且阻止删节点池/集群 | 从 ECS 层强删节点（`DeleteServers`）→ 等 CCE 检测节点消失（约 10 分钟）→ 删集群 | ✅ 已记录 |
| 10 | 连续写操作触发 429 限流（`APIGW.0308`，10 次/分钟） | 华为云写 API 限流 | 控制写操作频率，触发后等待限流窗口（1-8 分钟）再重试 | ✅ 已记录 |
| 11 | 删 VPC 报 `vpc contain peering` / `exroutes exists` | 遗留 VPC 对等连接（peering）+ 路由表 peering 路由 | 先删 peering 对等连接 → 清空路由表 peering 路由 → 删 VPC | ✅ 已记录 |

---

## 环境变量速查

| 变量 | 用途 | 示例值 |
|---|---|---|
| `CLOUD_SDK_AK` / `CLOUD_SDK_SK` | 华为云 AK/SK | `<你的AK>` / `<你的SK>` |
| `CCE_SMOKE_REGION` | 区域 | `cn-north-4` |
| `CCE_SMOKE_AZ` | 可用区 | `cn-north-4a` |
| `CCE_SMOKE_VPC` | VPC ID | `352d987e-...` |
| `CCE_SMOKE_SUBNET` | 节点子网 ID | `f3ca6c0e-...` |
| `CCE_SMOKE_ENI_SUBNET` | ENI 子网 neutron ID | `26b466cb-...` |
| `CCE_SMOKE_KEYPAIR` | 密钥对名称（节点用） | `capi-bastion-key` |
| `CCE_SMOKE_K8S_VERSION` | 集群 K8s 版本 | `v1.35` |
| `SWR_ORG` | SWR 命名空间 | `capi_cce` |

## 工具速查

| 工具 | 位置 | 用途 |
|---|---|---|
| `hack/smoke-setup` | `hack/smoke-setup/main.go` | 一键创建 VPC/子网/密钥对（**已含 DNS 修复**） |
| `hack/create-bastion` | `hack/create-bastion/main.go` | 创建跳板机 ECS（保留私钥，SSH 可达） |
| `hack/create-mgmt-cluster` | `hack/create-mgmt-cluster/main.go` | 创建/列出/删除管理集群 |
| `hack/swr-login` | `hack/swr-login/main.go` | 生成 SWR 临时登录 token + 确保命名空间 |
| `hack/survey-hw` | `hack/survey-hw/main.go` | 盘点所有华为云资源（清理后验证） |
| `hack/nat-egress` | `hack/nat-egress/main.go` | NAT 出网（**零公网方案不需要**） |
