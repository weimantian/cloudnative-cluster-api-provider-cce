# 端到端部署文档：ECS 跳板机 + CCE 管理集群 + 工作负载集群

> 本文档记录在华为云上部署 **cloudnative-cluster-api-provider-cce**（本项目）的完整端到端流程。
>
> **架构**：ECS 跳板机（运维入口）→ CCE 管理集群 A（运行 CAPI + Provider）→ CCE 工作负载集群 B（业务集群，私有 API）。
>
> **文档性质**：活文档。后续测试严格按本文档执行，遇到新问题追加到 [踩坑记录](#踩坑记录测试问题--修正) 章节并标记修正。

---

## 总体流程（三阶段）

```
【阶段一：预置基础设施】本地电脑（通过华为云 API）
  1. 准备网络（VPC/子网/密钥对）
  2. 创建跳板机 ECS
  3. 创建 CCE 管理集群 A
  4. 构建 provider 镜像 + 搬运 CAPI 镜像到 SWR

【阶段二：跳板机部署】本地 SSH 登录跳板机（所有集群 B 操作在此完成）
  5. SSH 登录跳板机
  6. 安装 kubectl + clusterctl
  7. 配置 kubectl 连接集群 A
  8. clusterctl init 部署 Provider 到集群 A
  9. 生成集群 B 配置（托管节点组）+ kubectl apply 提交

【阶段三：验证】本地电脑
  10. kubectl get cluster 查看集群 A 管理的所有集群
  11. clusterctl get kubeconfig 获取集群 B 访问凭据
```

**核心原则**：
1. **集群 A 的创建**（阶段一）在本地执行（调用华为云 CCE API，与执行位置无关）。
2. **集群 B 的所有操作**（阶段二）必须在跳板机上执行——管理集群 A 的 API Server 默认仅内网 endpoint（`https://10.0.x.x:5443`），本地电脑无法直达。
3. **零公网**：节点安装走华为云内网 OBS，CAPI/cert-manager 镜像搬运到 SWR（内网仓库），全程不需要 NAT、不开放公网出网。

---

## 前置条件

### 华为云资源

| 项目 | 要求 |
|---|---|
| IAM AK/SK | 有 CCE/VPC/EIP/ECS/SWR 操作权限 |
| 账户余额 | 充足（CCE 集群 + 节点 + ECS 按需计费） |
| 区域 | `cn-north-4`（本文档默认） |
| 配额 | CCE 集群配额（默认 50，`hack/diag-quota` 可查） |

### 本地电脑工具（macOS，阶段一 + 构建镜像用）

```bash
brew install docker kubectl go    # go >= 1.26

# clusterctl v1.14.0（必须精确匹配 CAPI v1.14 契约，brew 版本偏旧需手动下载）
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/clusterctl-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') -o clusterctl
chmod +x clusterctl && sudo mv clusterctl /usr/local/bin/clusterctl
clusterctl version   # 必须 v1.14.0
```

### 代理剥离（重要）

本地所有 `go`/`docker`/`ssh` 命令需剥离代理，否则连接华为云/registry 失败：

```bash
alias nocloud='env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u SOCKS_PROXY -u all_proxy -u ALL_PROXY'
```

### 凭证环境变量（本地）

```bash
export CLOUD_SDK_AK='<你的AK>'
export CLOUD_SDK_SK='<你的SK>'
export CCE_SMOKE_REGION='cn-north-4'
export CCE_SMOKE_AZ='cn-north-4a'
```

---

## 阶段一：预置基础设施（本地电脑）

### 步骤 1：准备网络与密钥

一键创建 VPC + 节点/ENI 子网 + 密钥对（幂等，按名复用）：

```bash
nocloud go run ./hack/smoke-setup
```

| 参数/输出 | 含义 |
|---|---|
| `CLOUD_SDK_AK` / `CLOUD_SDK_SK` | 华为云访问密钥，调用 VPC/ECS API |
| `CCE_SMOKE_REGION` | 区域，默认 `cn-north-4` |
| 输出 `CCE_SMOKE_VPC` | VPC ID（`capi-smoke-vpc`，CIDR 10.0.0.0/16） |
| 输出 `CCE_SMOKE_SUBNET` | 节点子网 ID（`capi-smoke-subnet-node`，10.0.1.0/24） |
| 输出 `CCE_SMOKE_KEYPAIR` | 密钥对名（`capi-smoke-key`，私钥被丢弃，仅节点登录用） |

> ⚠️ **踩坑 #1（DNS）**：脚本建子网时**必须指定 DNS**（`100.125.1.250` + `100.125.129.250`），否则节点拿到错误 DNS、cce-agent 下载失败、永久卡 Installing。已在脚本中修复。

### 步骤 2：创建跳板机 ECS

```bash
nocloud go run ./hack/create-bastion
```

| 参数/输出 | 含义 |
|---|---|
| `CCE_SMOKE_VPC` / `CCE_SMOKE_SUBNET` | 跳板机所在 VPC/子网（与集群 A 同 VPC，便于访问内网 API） |
| 默认 flavor | `s6.small.1`（1C2G，最小通用型） |
| 输出 `BASTION_PUBLIC_IP` | 跳板机公网 IP（SSH 登录用） |
| 输出 `capi-bastion-key.pem` | **跳板机私钥（必须保留，SSH 登录用）** |

> ⚠️ **踩坑 #2（私钥）**：跳板机密钥对 `capi-bastion-key` 的私钥脚本会保存到本地 `capi-bastion-key.pem`。集群 A 的节点也改用此密钥对（而非 `capi-smoke-key`），这样节点异常时可 SSH 排查。
>
> ⚠️ **踩坑 #12（Ecs.0314）**：若报 `keypair does not match the user_id`，说明云上存在**其他用户**创建的同名密钥对。删除本地私钥文件（`rm -f capi-bastion-key.pem`）强制脚本新建即可。

验证 SSH（首次连接需等 EIP 生效约 30-60 秒）：

```bash
ssh -i capi-bastion-key.pem -o StrictHostKeyChecking=no root@<BASTION_PUBLIC_IP> 'echo SSH_OK; hostname'
```

### 步骤 3：创建 CCE 管理集群 A

> 集群 A 的创建可**直接在本地**执行（调用华为云 CCE API）。

```bash
nocloud CCE_SMOKE_VPC="$CCE_SMOKE_VPC" \
  CCE_SMOKE_SUBNET="$CCE_SMOKE_SUBNET" \
  CCE_SMOKE_KEYPAIR='capi-bastion-key' \
  CCE_SMOKE_K8S_VERSION='v1.35' \
  go run ./hack/create-mgmt-cluster
```

| 参数 | 含义 |
|---|---|
| `CCE_SMOKE_VPC` / `CCE_SMOKE_SUBNET` | 管理集群所在 VPC/子网 |
| `CCE_SMOKE_KEYPAIR=capi-bastion-key` | 节点 SSH 密钥对（保留私钥的，可排查） |
| `CCE_SMOKE_K8S_VERSION=v1.35` | 集群 K8s 版本 |
| 默认 flavor | 集群 `cce.s1.small`，节点 `c6.large.2` ×2 |
| 输出 `capi-mgmt.kubeconfig` | **管理集群 kubeconfig（下载到本地）** |

> ⚠️ **踩坑 #3（无公网 endpoint）**：管理集群 `publicAccess=true` 仍**只有 Internal endpoint**（`https://10.0.x.x:5443`）。kubeconfig 的 server 是内网 IP，本地无法直达，必须由跳板机访问。
>
> ⚠️ **踩坑 #10（429 限流）**：连续写操作触发 CCE 写限流（10 次/分钟），且每次 429 重试也计入限流计数。需停止写操作等窗口清零（1-10 分钟），或后台自动重试。

### 步骤 4：构建 provider 镜像 + 搬运 CAPI 镜像到 SWR

```bash
# 4.1 获取 SWR 临时登录 token（约 1 小时有效）
nocloud go run ./hack/swr-login
# 输出 SWR_USER / SWR_PASSWORD / SWR_IMAGE

# 4.2 构建 provider 镜像（amd64，禁用 attestation）
docker build --platform linux/amd64 --provenance=false --sbom=false \
  -t swr.cn-north-4.myhuaweicloud.com/capi_cce/cce-provider-controller:latest .

# 4.3 登录 + 推送
echo '<SWR_PASSWORD>' | docker login swr.cn-north-4.myhuaweicloud.com -u '<SWR_USER>' --password-stdin
docker push swr.cn-north-4.myhuaweicloud.com/capi_cce/cce-provider-controller:latest

# 4.4 搬运 CAPI + cert-manager 镜像到 SWR（零公网）
for img in \
  quay.io/jetstack/cert-manager-controller:v1.21.1 \
  quay.io/jetstack/cert-manager-cainjector:v1.21.1 \
  quay.io/jetstack/cert-manager-webhook:v1.21.1 \
  registry.k8s.io/cluster-api/cluster-api-controller:v1.14.0 \
  registry.k8s.io/cluster-api/kubeadm-bootstrap-controller:v1.14.0 \
  registry.k8s.io/cluster-api/kubeadm-control-plane-controller:v1.14.0; do
  name=$(basename "$img")
  dst="swr.cn-north-4.myhuaweicloud.com/capi_cce/$name"
  docker buildx imagetools create --platform linux/amd64 -t "$dst" "$img"
done
```

| 参数 | 含义 |
|---|---|
| `--platform linux/amd64` | 强制 amd64（管理集群节点是 x86_64）——踩坑 #5 |
| `--provenance=false --sbom=false` | 禁用 BuildKit attestation，SWR 不支持——踩坑 #6 |
| `buildx imagetools create` | 从 registry 直拷并强制 amd64（`docker pull --platform` 不生效）——踩坑 #7 |

---

## 阶段二：跳板机部署（SSH 登录跳板机）

> 以下所有命令在**跳板机**上执行（本地 SSH 登录）。集群 B 的所有操作必须在此完成。

### 步骤 5：SSH 登录跳板机

```bash
ssh -i capi-bastion-key.pem root@<BASTION_PUBLIC_IP>
```

| 参数 | 含义 |
|---|---|
| `-i capi-bastion-key.pem` | 指定跳板机私钥文件 |
| `root@<IP>` | 跳板机公网 IP + 默认用户 root（EulerOS 镜像） |

### 步骤 6：安装 kubectl + clusterctl

```bash
# kubectl v1.30（linux-amd64）
curl -LO "https://dl.k8s.io/release/v1.30.0/bin/linux/amd64/kubectl"
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl

# clusterctl v1.14.0（必须匹配 CAPI 契约）
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/clusterctl-linux-amd64 -o clusterctl
chmod +x clusterctl && sudo mv clusterctl /usr/local/bin/clusterctl

# 验证
kubectl version --client
clusterctl version
```

| 参数 | 含义 |
|---|---|
| `-LO` | curl 跟随重定向（-L）+ 按远程文件名保存（-O） |
| `install -o root -g root -m 0755` | 安装为 root 所有、0755 权限 |
| clusterctl 版本 | **必须 v1.14.0**（踩坑 #4：brew 版本偏旧） |

### 步骤 7：配置 kubectl 连接集群 A

> 管理集群 kubeconfig 在步骤 3 已下载（本地 `capi-mgmt.kubeconfig`），需上传到跳板机。

```bash
# 本地：上传 kubeconfig 到跳板机
scp -i capi-bastion-key.pem capi-mgmt.kubeconfig root@<BASTION_PUBLIC_IP>:/root/

# 跳板机：配置 kubeconfig 并验证连接
export KUBECONFIG=/root/capi-mgmt.kubeconfig
kubectl get nodes      # 应看到 2 个 Ready 节点
```

| 参数 | 含义 |
|---|---|
| `KUBECONFIG=/root/capi-mgmt.kubeconfig` | 指定管理集群 kubeconfig 路径 |
| `kubectl get nodes` | 验证跳板机能连接集群 A（内网 endpoint，同 VPC 可达） |

### 步骤 8：clusterctl init 部署 Provider

> 需先把步骤 4 生成的 `infrastructure-components.yaml` + `metadata.yaml` 上传到跳板机，并注册 clusterctl。

```bash
# 本地：生成 manifests（见"附录 A"）

# 本地：上传 manifests + metadata 到跳板机
scp -i capi-bastion-key.pem _artifacts/cceswr/infrastructure-components.yaml root@<BASTION_PUBLIC_IP>:/root/
scp -i capi-bastion-key.pem metadata.yaml root@<BASTION_PUBLIC_IP>:/root/

# 跳板机：注册 clusterctl provider
mkdir -p ~/.cluster-api
cat > ~/.cluster-api/clusterctl.yaml <<'EOF'
providers:
  - name: "cce"
    url: "file:///root/infrastructure-components.yaml"
    type: "InfrastructureProvider"
EOF

# 跳板机：部署
export KUBECONFIG=/root/capi-mgmt.kubeconfig
clusterctl init --infrastructure cce
```

| 参数 | 含义 |
|---|---|
| `--infrastructure cce` | 指定安装的 infrastructure provider 为 cce |
| `file:///root/infrastructure-components.yaml` | clusterctl 本地 provider 源 |

> 部署后需修复 provider pod（SWR 私有仓库 imagePullSecret + webhook TLS Secret），详见"附录 B"。

### 步骤 9：生成集群 B 配置（托管节点组）+ kubectl apply

> "托管节点组" = CAPI `MachinePool` + 本项目 `CCEManagedMachinePool`（CCE 节点池）。模板在 `config/samples/cluster-template.yaml`。

```bash
# 跳板机：复制模板并替换占位符（模板已上传到跳板机）
cp cluster-template.yaml my-cluster.yaml

# 替换三个占位符
sed -i \
  -e 's/VERIFY-VPC-ID/<VPC-ID>/' \
  -e 's/VERIFY-SUBNET-ID/<节点子网-ID>/' \
  -e 's/VERIFY-KEYPAIR-NAME/capi-bastion-key/' \
  my-cluster.yaml

# 创建凭据 Secret + bootstrap Secret（CAPI v1.14 MachinePool 契约要求）
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default --from-literal=accessKey='<AK>' --from-literal=secretKey='<SK>'
kubectl create secret generic my-cce-cluster-bootstrap \
  --namespace default --from-literal=value=""

# 提交集群 B 声明给管理集群
kubectl apply -f my-cluster.yaml
```

| 参数 | 含义 |
|---|---|
| `VERIFY-VPC-ID` / `VERIFY-SUBNET-ID` | 集群 B 的 VPC/子网（与集群 A 同 VPC 或独立） |
| `VERIFY-KEYPAIR-NAME` | 节点密钥对（`capi-bastion-key`） |
| `<cluster>-credentials` | 集群 B 的凭据 Secret（Provider 用它调 CCE API 创建集群 B） |
| `<cluster>-bootstrap` | 空 bootstrap Secret（CAPI v1.14 MachinePool 契约要求存在） |
| `kubectl apply` | 提交声明，集群 A 的控制器开始创建集群 B |

---

## 阶段三：验证（本地电脑）

### 步骤 10：kubectl get cluster 查看集群 A 管理的所有集群

```bash
# 本地（或跳板机，KUBECONFIG 指向集群 A）
kubectl get cluster
kubectl get ccemanagedcontrolplane my-cce-cluster-control-plane -w   # 等 Ready=True
kubectl get ccemanagedmachinepool my-cce-cluster-pool-0 -w            # 等 Ready=True, Replicas=1
```

### 步骤 11：clusterctl get kubeconfig 访问集群 B

```bash
# 获取集群 B 的 kubeconfig
clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig

# 访问集群 B（私有 API，需同 VPC 的跳板机执行）
kubectl --kubeconfig=my-cce-cluster.kubeconfig get nodes
```

> ⚠️ 集群 B 默认 `endpointAccess.public: false`，kubeconfig server 是内网 IP，**只能从同 VPC 的跳板机访问**。

---

## 清理

```bash
# 跳板机：删集群 B
kubectl delete cluster my-cce-cluster

# 本地：删管理集群 A
nocloud go run ./hack/create-mgmt-cluster -delete -cluster '<MGMT_CLUSTER_ID>'

# 本地：清理跳板机 ECS + EIP + 密钥对 + 安全组 + 子网 + VPC（见踩坑 #9 #11）
```

---

## 踩坑记录（测试问题 + 修正）

> **活文档核心**：后续测试遇到问题，按序追加记录（现象/根因/修正/状态），并在上方对应步骤同步修正。

| # | 问题现象 | 根因 | 修正 | 状态 |
|---|---|---|---|---|
| 1 | 节点永久卡 `Installing`（kubelet inactive） | 子网未指定 DNS，节点拿到错误 DNS（`10.0.x.254`），无法解析 OBS 域名，cce-agent 下载失败 | 建子网显式指定 `primary_dns=100.125.1.250` + `secondary_dns=100.125.129.250` | ✅ 已修复 |
| 2 | 节点异常无法 SSH 排查 | `smoke-setup` 创建密钥对时丢弃私钥 | 节点改用 `capi-bastion-key`（`create-bastion` 保留私钥） | ✅ 已修复 |
| 3 | 管理集群 `publicAccess=true` 无公网 endpoint | CCE 不自动分配公网 IP | 跳板机（同 VPC）访问内网 endpoint | ✅ 已记录 |
| 4 | clusterctl 版本偏旧（v1.13.4） | brew 版本落后 | 手动下载 clusterctl v1.14.0 | ✅ 已修复 |
| 5 | provider 镜像在 x86_64 节点无法运行 | Docker Desktop 默认构建 arm64 | `docker build --platform linux/amd64` | ✅ 已修复 |
| 6 | docker push 报 `Invalid image, fail to parse 'manifest.json'` | BuildKit attestation，SWR 不支持 | `--provenance=false --sbom=false` | ✅ 已修复 |
| 7 | `docker pull --platform linux/amd64` 拉到 arm64 | Docker Desktop 对 multi-arch 的 pull --platform 不生效 | `docker buildx imagetools create --platform linux/amd64` | ✅ 已修复 |
| 8 | NAT 网关创建失败 `CBC.30060005` | 余额不足 | 零公网方案不需要 NAT；充值余额 | ✅ 已记录 |
| 9 | Installing 节点无法删除（`CCE_CM.0002`/`CCE.01403006`/`CCE.01400024` 死锁） | CCE 限制 | ECS 层强删节点 → 等 CCE 检测 → 删集群 | ✅ 已记录 |
| 10 | 连续 429 限流（`APIGW.0308`） | CCE 写限流 10 次/分钟，429 重试也计数 | 停止写操作等窗口清零，或后台自动重试 | ✅ 已记录 |
| 11 | 删 VPC 报 `vpc contain peering` / `exroutes exists` | 遗留 VPC 对等连接 + 路由 | 先删 peering → 清空路由表 peering 路由 → 删 VPC | ✅ 已记录 |
| 12 | ECS 创建报 `Ecs.0314 keypair does not match user_id` | 云上存在其他用户同名密钥对 | 删本地私钥文件强制新建密钥对 | ✅ 已记录 |
| 13 | CreateCluster 报 `CCE_CM.0402 Version is not support, Version format error` | CCE CreateCluster API 只接受 `v1.35`（major.minor），而 webhook 要求完整 semver `v1.35.0` | cce.go 加 `cceClusterVersion` 去 patch（v1.35.0→v1.35） | ✅ 已修复 |
| 14 | 节点池永久 `WaitingForControlPlane`，控制面 `status.ready` 始终为空 | spec.version（v1.35.0）与实际集群版本（v1.35.5，CCE 自动选最新 patch）精确比较不相等 → 误触发 upgrade → 无 target 后 return → 永远走不到 `Status.Ready=true` | controller 加 `sameMajorMinor` 比较 major.minor（忽略 patch） | ✅ 已修复 |
| 15 | CreateCluster 报 `CCE_CM.0410 Container network CIDR conflict, 10.244.0.0/16 conflict with vpc route dest addr 10.244.1.0/24` | 同 VPC 内各集群容器网段必须唯一，管理集群 A 已占 10.244.0.0/16 | 集群 B 容器/服务网段改为 10.245.0.0/16、10.248.0.0/16 | ✅ 已修复 |
| 16 | 改 `spec.containerNetwork.cidr` 报 `field is immutable after creation` | CCEManagedControlPlane 的 containerNetwork.cidr 创建后不可变 | 删除 Cluster 重建（改网段前先删） | ✅ 已记录 |
| 17 | `rollout restart` 后 pod 仍拉旧镜像 | CCE 节点 containerd 缓存 `latest` tag（imagePullPolicy=IfNotPresent） | deployment 设 `imagePullPolicy: Always` 或推唯一 tag | ✅ 已修复 |
| 18 | 删除失败集群卡 Deleting（finalizer 未移除） | 集群未创建成功（ClusterID 空）时删除，controller 未触发删除 reconcile，finalizer 阻塞 | 手动 `kubectl patch ... --type=json -p '[{"op":"remove","path":"/metadata/finalizers"}]'` | ✅ 已记录 |
| 19 | 删除成功集群时 `MachinePool` 与 `CCEManagedControlPlane` 的 finalizer 卡住，删除链停滞 | ① CAPI MachinePool 控制器删除 CCEManagedMachinePool 后返回空 Result（不 requeue），依赖 watch 事件重触发但未触发；② provider `CCEManagedControlPlane`/`CCEManagedMachinePool` 的 `reconcileDelete` 分支在 scope 构建之前，`RemoveFinalizer` 后无 `Client.Update`/patch 持久化 → finalizer 移除永远写不回 API server | `reconcileDelete` 里 `RemoveFinalizer` 后显式 `Client.Update`；本次 E2E 临时手动移除 finalizer 兜底 | ✅ 已修复（950d550） |

---

## 环境变量速查

| 变量 | 用途 | 示例值 |
|---|---|---|
| `CLOUD_SDK_AK` / `CLOUD_SDK_SK` | 华为云 AK/SK | `<你的AK>` / `<你的SK>` |
| `CCE_SMOKE_REGION` | 区域 | `cn-north-4` |
| `CCE_SMOKE_AZ` | 可用区 | `cn-north-4a` |
| `CCE_SMOKE_VPC` | VPC ID | `62737a53-...` |
| `CCE_SMOKE_SUBNET` | 节点子网 ID | `c9b7bf51-...` |
| `CCE_SMOKE_KEYPAIR` | 密钥对名（节点用） | `capi-bastion-key` |
| `CCE_SMOKE_K8S_VERSION` | 集群 K8s 版本 | `v1.35` |
| `SWR_ORG` | SWR 命名空间 | `capi_cce` |

## 工具速查

| 工具 | 位置 | 用途 |
|---|---|---|
| `hack/smoke-setup` | 建 VPC/子网/密钥对（**已含 DNS 修复**） | 阶段一步骤 1 |
| `hack/create-bastion` | 建跳板机 ECS（保留私钥） | 阶段一步骤 2 |
| `hack/create-mgmt-cluster` | 创建/列出/删除管理集群 | 阶段一步骤 3 |
| `hack/swr-login` | 生成 SWR 临时登录 token | 阶段一步骤 4 |
| `hack/survey-hw` | 盘点所有华为云资源 | 清理后验证 |

---

## 附录 A：生成 infrastructure-components.yaml（本地）

```bash
ARTIFACTS=_artifacts/cceswr
mkdir -p "$ARTIFACTS"

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

# 生成自签 CA + webhook server 证书（CN/SAN 见 server.conf）
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

CABUNDLE="$(base64 < "$ARTIFACTS/ca.crt" | tr -d '\n')"
awk -v cab="$CABUNDLE" '{ print; if ($0 == "  clientConfig:") print "    caBundle: " cab }' \
  "$ARTIFACTS/infrastructure-components-raw.yaml" > "$ARTIFACTS/infrastructure-components.yaml"
```

## 附录 B：修复 Provider pod（跳板机）

```bash
# SWR 私有仓库 imagePullSecret
kubectl create secret docker-registry cce-provider-swr-secret \
  --namespace cce-provider-system \
  --docker-server=swr.cn-north-4.myhuaweicloud.com \
  --docker-username='<SWR_USER>' --docker-password='<SWR_PASSWORD>' --docker-email='noreply@huawei.cloud'

# webhook TLS Secret
kubectl -n cce-provider-system create secret tls webhook-service-cert \
  --cert=server.crt --key=server.key

# 给 provider Deployment 加 imagePullSecrets 并重启
kubectl -n cce-provider-system patch deployment cce-provider-controller-manager \
  --type=json -p='[{"op":"add","path":"/spec/template/spec/imagePullSecrets","value":[{"name":"cce-provider-swr-secret"}]}]'
kubectl -n cce-provider-system rollout restart deployment/cce-provider-controller-manager
```
