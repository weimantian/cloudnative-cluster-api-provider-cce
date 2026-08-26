# 华为云 CCE Provider 部署指导（对标 CAPA/EKS 托管模式）

> 在华为云 CCE 管理集群上安装 Cluster API（CAPI）+ CCE Provider，声明式管理 CCE 工作负载集群。
> 全程通过**华为云控制台**操作（跳板机用控制台 CloudShell 登录，无需本地 SSH 私钥）。

---

## 1. 框架概述

### 1.1 架构

```
┌────────────────────────── 华为云 cn-north-4 ──────────────────────────┐
│                                                                        │
│  跳板机 ECS (capi-bastion)              管理集群 A (CCE)               │
│  · 控制台 CloudShell 登录               · 运行 CAPI core + Provider     │
│  · kubectl + clusterctl                · cert-manager                  │
│         │                                   │                        │
│         └────── kubectl 连集群 A (公网 endpoint) ─┘                   │
│                                                                        │
│  工作负载集群 B (CCE)  ←—— Provider 调 CCE API 创建                    │
│  · 默认 Turbo (eni)，多节点池跨 AZ                                    │
└────────────────────────────────────────────────────────────────────────┘
```

### 1.2 组件一览

| 组件 | 是什么 | 本文档角色 |
|---|---|---|
| CCE | 华为云托管 Kubernetes | 管理集群 A + 工作负载集群 B |
| ECS | 云服务器 | 跳板机（运维入口） |
| SWR | 华为云容器镜像仓库 | 存放所有镜像（public，免认证） |
| CAPI (cluster-api) | K8s 官方集群管理框架 | 声明式管理集群 |
| CCE Provider（本项目） | CAPI 插件，把 CAPI 对象翻译成 CCE API 调用 | 管理集群 A 上运行 |
| cert-manager | 证书管理 | 自动签发 webhook 证书 |

### 1.3 网络形态

| 形态 | 说明 |
|---|---|
| **公网（默认）** | 集群 A 公网+私有 endpoint（`CCE_DEPLOY_PUBLIC=true` 自动绑 EIP），节点公网 IP 出网 |
| **零公网（可选）** | 集群 A 仅内网 endpoint，镜像全走 SWR 内网搬运（详见旧版 `docs/e2e-deployment-guide.md`） |

---

## 2. 前置条件

### 2.1 华为云资源

| 项目 | 要求 |
|---|---|
| 账户 | 能登录 [华为云控制台](https://console.huaweicloud.com)，区域 `cn-north-4` |
| 余额 | 充足（CCE 集群 + 节点 + ECS 按需计费；余额 0 报 `CCE.01429004`） |
| AK/SK | 控制台 → 右上角用户名 → 我的凭证 → 访问密钥（需 CCE/VPC/ECS/EIP/SWR 权限） |
| 配额 | CCE 集群配额（默认 50） |
| 本地工具（仅阶段一） | docker / go / kubectl（构建镜像 + 生成 manifests + 跑 hack 脚本） |

### 2.2 术语表

| 术语 | 是什么（通俗） | 本文档用途 |
|---|---|---|
| AK / SK | 华为云 API 访问密钥（给程序用的账号密码） | hack 脚本调华为云 API 的凭证 |
| VPC | 你专属的隔离网络（像一栋楼） | 跳板机 + 集群 A/B 的网络 |
| 子网 | VPC 内的网段（像楼层） | 节点 / 跳板机分配 IP |
| 安全组 | 防火墙规则（放行哪些端口 / IP） | 放行 SSH 22、API 端口 |
| 密钥对 | SSH 公私钥（私钥 = 钥匙） | 节点 SSH 排查（跳板机登录走 CloudShell，无需私钥） |
| EIP | 弹性公网 IP | 跳板机 / 集群 A 公网 endpoint |
| CCE | 华为云托管 Kubernetes | 集群 A/B 即 CCE 集群 |
| SWR | 华为云容器镜像仓库 | 存所有镜像（public） |
| CAPI / clusterctl | K8s 集群管理框架 / 其命令行 | 声明式管理集群 |
| Provider | CAPI 插件，翻译成云 API | 本项目（CCE provider） |
| CloudShell | 华为云控制台的云命令行（浏览器内） | 登录跳板机（免本地 SSH） |
| MachinePool | CAPI 的节点池对象 | 对应 CCE 节点池；多 AZ = 多 MachinePool |

### 2.3 开始前自检（确认这 5 项）

1. 能登录华为云控制台，区域切到 `cn-north-4`。
2. 账户有余额。
3. 有 AK/SK（不确定权限先用主账号验证）。
4. 本地装好工具：docker / go / kubectl（仅阶段一用）。
5. 已 export AK/SK 环境变量（见阶段一）。

---

## 3. SWR 公共镜像清单（已备好，可直接用）

以下 **8 个镜像**已提前构建/搬运到 **public SWR**（`swr.cn-north-4.myhuaweicloud.com/capi_cce/`），全部 **amd64**（X86）且 **public**（匿名免认证拉取，已验证）。部署时直接用，无需 imagePullSecret。

| SWR 仓库（`swr.cn-north-4.myhuaweicloud.com/capi_cce/`） | 来源 | 用途 |
|---|---|---|
| `cluster-api-cce-controller:latest` | 本地构建 | **CCE Provider 控制器**（本项目） |
| `cluster-api-controller:v1.14.0` | registry.k8s.io/cluster-api | CAPI 核心 |
| `kubeadm-bootstrap-controller:v1.14.0` | registry.k8s.io/cluster-api | CAPI bootstrap-kubeadm |
| `kubeadm-control-plane-controller:v1.14.0` | registry.k8s.io/cluster-api | CAPI control-plane-kubeadm |
| `cert-manager-controller:v1.21.1` | quay.io/jetstack | cert-manager 控制器 |
| `cert-manager-cainjector:v1.21.1` | quay.io/jetstack | cert-manager CA 注入 |
| `cert-manager-webhook:v1.21.1` | quay.io/jetstack | cert-manager webhook |
| `capi-cce-tools:latest` | 本地打包 | 跳板机工具：kubectl v1.30 + clusterctl v1.14 |

> 当前 `cluster-api-cce-controller:latest` digest `e4d624fc`（含 kubeconfig 端点覆盖 + 429 退避修复）。
> 完整路径示例：`swr.cn-north-4.myhuaweicloud.com/capi_cce/cluster-api-cce-controller:latest`。

---

## 4. 总体流程（三阶段）

```
【阶段一：预置基础设施】本地电脑（hack 脚本调华为云 API）
  1. 准备网络（VPC/子网/密钥对）
  2. 创建跳板机 ECS（公网）
  3. 创建 CCE 管理集群 A（公网 endpoint）

【阶段二：跳板机部署】控制台 CloudShell（浏览器内操作，免本地 SSH）
  4. CloudShell 登录跳板机 + 装工具
  5. 连集群 A（公网 endpoint）
  6. clusterctl init（离线本地源 + 镜像走 SWR）
  7. Provider 镜像（方式 B：public SWR 免认证）

【阶段三：工作负载集群 B + 验证】CloudShell
  8. 创建集群 B（默认 Turbo 多 pool，3 节点 3 AZ）
  9. 验证 + 扩缩容
```

| 步骤 | 操作 | 耗时 | 产物 |
|---|---|---|---|
| 1 | 网络与密钥 | 2 min | VPC/子网/密钥对 |
| 2 | 跳板机 ECS | 3 min | 跳板机（公网 IP） |
| 3 | 管理集群 A | 10-20 min | 集群 A + kubeconfig |
| 4 | CloudShell + 工具 | 5 min | kubectl + clusterctl |
| 5 | 连集群 A | 1 min | 2 节点 Ready |
| 6 | clusterctl init | 5 min | CAPI + Provider Running |
| 7 | Provider 镜像 | 1 min | 方式 B 免认证 |
| 8 | 集群 B | 10-20 min | 集群 B Provisioned |
| 9 | 验证 + 扩缩容 | 5 min | 节点 Ready + scale |

---

## 5. 部署步骤

### 阶段一：预置基础设施（本地电脑）

> 以下 hack 脚本在本地电脑运行（调华为云 API），**不涉及登录跳板机**。

**步骤 1：网络与密钥**

```bash
# 本地：一键创建 VPC + 节点/ENI 子网 + 密钥对（幂等）
nocloud CLOUD_SDK_AK=<AK> CLOUD_SDK_SK=<SK> CCE_DEPLOY_REGION=cn-north-4 \
  go run ./hack/deploy-network
```

输出记录：`CCE_DEPLOY_VPC`、`CCE_DEPLOY_SUBNET`、`CCE_DEPLOY_ENI_SUBNET`（neutron ID）、密钥对。

> 控制台方式：VPC → 创建虚拟私有云（`capi-vpc` 10.0.0.0/16）+ 子网（`capi-subnet-node` 10.0.1.0/24，**DNS 必须填 `100.125.1.250,100.125.129.250`**）+ ENI 子网（`capi-subnet-eni` 10.0.2.0/24，Turbo 用）。

**步骤 2：跳板机 ECS（控制台创建）**

```bash
# 本地脚本（可选，或直接控制台创建）
nocloud CLOUD_SDK_AK=<AK> CLOUD_SDK_SK=<SK> \
  CCE_DEPLOY_VPC=<VPC-ID> CCE_DEPLOY_SUBNET=<节点子网-ID> \
  go run ./hack/deploy-bastion
```

控制台创建：ECS → 购买弹性云服务器 → 按需计费 → 规格 `s6.small.1`（1C2G）→ 镜像 EulerOS 2.0 → VPC/子网选 `capi-vpc`/`capi-subnet-node` → 安全组放行 **TCP 22** → 绑定 EIP → 密钥对 `capi-bastion-key`。

> ⚠️ 跳板机登录用控制台 **CloudShell**（下一步），**无需下载私钥到本地**。

**步骤 3：管理集群 A（公网 endpoint）**

```bash
# 本地：创建管理集群 A（默认 Turbo；standard 设 CCE_DEPLOY_CATEGORY=standard）
nocloud CLOUD_SDK_AK=<AK> CLOUD_SDK_SK=<SK> \
  CCE_DEPLOY_VPC=<VPC-ID> CCE_DEPLOY_SUBNET=<节点子网-ID> \
  CCE_DEPLOY_ENI_SUBNET=<ENI-neutron-ID> \
  CCE_DEPLOY_KEYPAIR=capi-bastion-key CCE_DEPLOY_K8S_VERSION=v1.35 \
  go run ./hack/deploy-mgmt-cluster
```

关键环境变量（均可省略）：

| 变量 | 说明 |
|---|---|
| `CCE_DEPLOY_CATEGORY` | `turbo`（默认，eni，需 `CCE_DEPLOY_ENI_SUBNET`）/ `standard`（vpc-router） |
| `CCE_DEPLOY_ENI_SUBNET` | Turbo 必填：ENI 子网 neutron ID |
| `CCE_DEPLOY_SERVICE_CIDR` | 服务网段（默认 `10.247.0.0/16`，创建后不可变） |
| `CCE_DEPLOY_CONTAINER_CIDR` | 容器网段（默认 `10.244.0.0/16`；Turbo 忽略） |
| `CCE_DEPLOY_MGMT_FLAVOR` | 节点规格（默认 Turbo `c7.large.2` / Standard `c6.large.2`） |
| `CCE_DEPLOY_PUBLIC` | 公网 endpoint 开关（默认 `true` 自动绑 EIP） |

产物：`capi-mgmt.kubeconfig`（管理集群 kubeconfig）+ 集群 A 公网 endpoint（如 `https://<EIP>:5443`）。

> ⏱️ 总耗时 10-20 分钟（集群 5-10 分钟 + 节点池 5-10 分钟），输出间隔长属正常。中断后可用 `-list` 查 ID + `-pool -cluster <ID>` 补建节点池。

### 阶段二：跳板机部署（控制台 CloudShell）

> 以下全部在**华为云控制台 CloudShell** 内操作（浏览器），**无需本地 SSH**。

**步骤 4：CloudShell 登录跳板机 + 装工具**

1. 控制台 → 弹性云服务器 ECS → 跳板机实例 → **远程登录 → CloudShell**（浏览器内打开跳板机 shell）。
2. 上传文件（见步骤 5 的 components/metadata/模板）——CloudShell 工具栏「上传文件」。
3. 装工具：

```bash
# CloudShell（跳板机）内：下载 kubectl + clusterctl（linux/amd64）
curl -LO "https://dl.k8s.io/release/v1.30.0/bin/linux/amd64/kubectl"
install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/clusterctl-linux-amd64 -o clusterctl
chmod +x clusterctl && mv clusterctl /usr/local/bin/clusterctl

kubectl version --client && clusterctl version
```

> ⚠️ 慢网备选：用 SWR 工具镜像 `capi-cce-tools`（见第 3 章）——`docker pull` + `docker cp` 提取（需先装 docker）。⚠️ 勿用 `yum install kubectl`（华为云源只有 v1.23，连不了 v1.35）。

**步骤 5：连集群 A（公网 endpoint）**

1. 本地生成 manifests（`kubectl kustomize config/default`，见附录 A），连同 `metadata.yaml` 上传到跳板机（CloudShell 上传，或放 `/root/`）。
2. CloudShell 内：

```bash
export KUBECONFIG=/root/capi-mgmt.kubeconfig
kubectl get nodes    # 应看到 2 个 Ready 节点
```

**步骤 6：clusterctl init（离线本地源 + 镜像走 SWR）**

本地下载 CAPI 官方组件（v1.14.0）并把镜像改为 SWR：

```bash
# 本地
curl -L -o /tmp/core-components.yaml https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/core-components.yaml
curl -L -o /tmp/bootstrap-components.yaml https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/bootstrap-components.yaml
curl -L -o /tmp/control-plane-components.yaml https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/control-plane-components.yaml
curl -L -o /tmp/capi-metadata.yaml https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/metadata.yaml
# 镜像改 SWR（public）：
sed -i '' 's|registry.k8s.io/cluster-api/cluster-api-controller:v1.14.0|swr.cn-north-4.myhuaweicloud.com/capi_cce/cluster-api-controller:v1.14.0|g' /tmp/core-components.yaml
sed -i '' 's|registry.k8s.io/cluster-api/kubeadm-bootstrap-controller:v1.14.0|swr.cn-north-4.myhuaweicloud.com/capi_cce/kubeadm-bootstrap-controller:v1.14.0|g' /tmp/bootstrap-components.yaml
sed -i '' 's|registry.k8s.io/cluster-api/kubeadm-control-plane-controller:v1.14.0|swr.cn-north-4.myhuaweicloud.com/capi_cce/kubeadm-control-plane-controller:v1.14.0|g' /tmp/control-plane-components.yaml
```

上传到跳板机 `/root/`，CloudShell 内组织 repository 目录 + 配置 clusterctl：

```bash
mkdir -p /root/repository/{cluster-api,bootstrap-kubeadm,control-plane-kubeadm}/v1.14.0
mkdir -p /root/repository/infrastructure-cce/v0.1.0
cp /root/core-components.yaml          /root/repository/cluster-api/v1.14.0/
cp /root/capi-metadata.yaml            /root/repository/cluster-api/v1.14.0/metadata.yaml
cp /root/bootstrap-components.yaml     /root/repository/bootstrap-kubeadm/v1.14.0/
cp /root/capi-metadata.yaml            /root/repository/bootstrap-kubeadm/v1.14.0/metadata.yaml
cp /root/control-plane-components.yaml /root/repository/control-plane-kubeadm/v1.14.0/
cp /root/capi-metadata.yaml            /root/repository/control-plane-kubeadm/v1.14.0/metadata.yaml
cp /root/infrastructure-components.yaml /root/repository/infrastructure-cce/v0.1.0/
cp /root/metadata.yaml                  /root/repository/infrastructure-cce/v0.1.0/metadata.yaml

cat > /root/.cluster-api/clusterctl.yaml <<'EOF'
providers:
  - name: "cluster-api"
    url: "file:///root/repository/cluster-api/v1.14.0/core-components.yaml"
    type: "CoreProvider"
  - name: "kubeadm"
    url: "file:///root/repository/bootstrap-kubeadm/v1.14.0/bootstrap-components.yaml"
    type: "BootstrapProvider"
  - name: "kubeadm"
    url: "file:///root/repository/control-plane-kubeadm/v1.14.0/control-plane-components.yaml"
    type: "ControlPlaneProvider"
  - name: "cce"
    url: "file:///root/repository/infrastructure-cce/v0.1.0/infrastructure-components.yaml"
    type: "InfrastructureProvider"
EOF

export KUBECONFIG=/root/capi-mgmt.kubeconfig
clusterctl init --core cluster-api --bootstrap kubeadm --control-plane kubeadm --infrastructure cce
```

> ⚠️ `~/.cluster-api/overrides/` 目录会干扰 init（优先用 override 而非配置 url），init 前先删除。

**步骤 7：Provider 镜像（方式 B：public SWR 免认证）**

clusterctl init 装完，provider 镜像从 public SWR（`cluster-api-cce-controller:latest`）免认证直拉，webhook 证书由 cert-manager 自动签发——**无任何手动步骤**：

```bash
kubectl -n capi-cce-system get pods    # capi-cce-controller-manager 1/1 Running
kubectl get certificate -n capi-cce-system serving-cert   # Ready=True（自动签发）
```

### 阶段三：工作负载集群 B + 验证（CloudShell）

**步骤 8：创建集群 B（默认 Turbo 多 pool，3 节点 3 AZ）**

上传模板（`cluster-template.yaml` 默认 Turbo / `cluster-template-standard.yaml` / `cluster-template-turbo.yaml`）到跳板机 `/root/`，CloudShell 内：

```bash
mkdir -p ~/.cluster-api/overrides/infrastructure-cce/v0.1.0
cp /root/cluster-template.yaml          ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template.yaml
cp /root/cluster-template-standard.yaml ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template-standard.yaml
cp /root/cluster-template-turbo.yaml    ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template-turbo.yaml

# 生成（默认 Turbo 多 pool；--worker-machine-count=1 → 3 个 pool 各 1 节点 = 3 节点 3 AZ）
clusterctl generate cluster my-cce-cluster --kubernetes-version v1.35.0 --worker-machine-count=1 > my-cluster.yaml

# 替换 VERIFY-*（region/VPC/子网/ENI 子网/密钥对/AZ/AZ2/AZ3/flavor）
#   VERIFY-AZ/AZ2/AZ3 = cn-north-4a/4b/4c；VERIFY-FLAVOR = c7.large.2（Turbo）

# 创建凭据 Secret + bootstrap Secret
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default --from-literal=accessKey='<AK>' --from-literal=secretKey='<SK>'
kubectl create secret generic my-cce-cluster-bootstrap \
  --namespace default --from-literal=value=""

kubectl apply -f my-cluster.yaml
```

> 多 AZ = 多 MachinePool（对标 EKS 多节点组）：`pool-0/1/2` 各在一个 AZ。⚠️ 各 AZ 需有可用 sub-ENI flavor（如 `cn-north-4c` 常缺 `c7.large.2`，需换 `at7.large.1` 或换 AZ）。

**步骤 9：验证 + 扩缩容**

```bash
# 等 Provisioned
kubectl get cluster my-cce-cluster -w        # PHASE=Provisioned
kubectl get ccemanagedcontrolplane my-cce-cluster-control-plane -w   # Ready=True

# 获取 kubeconfig（provider 用当前 Internal endpoint 生成）并验证节点
clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig
kubectl --kubeconfig=my-cce-cluster.kubeconfig get nodes -o wide   # 3 个 Ready 节点，3 个 AZ

# 扩缩容（对标 EKS 节点组 desiredSize，按 pool 操作）
kubectl scale machinepool my-cce-cluster-pool-0 --replicas=3   # 扩容
kubectl scale machinepool my-cce-cluster-pool-0 --replicas=1   # 减容
```

---

## 6. 踩坑问题记录

| # | 问题现象 | 根因 | 修正 | 状态 |
|---|---|---|---|---|
| 1 | 节点永久卡 `Installing` | 子网未指定 DNS | 子网 DNS 填 `100.125.1.250,100.125.129.250` | ✅ |
| 2 | `publicAccess=true` 无公网 endpoint | CCE 不自动分配公网 IP | `deploy-mgmt-cluster` 创建后自动绑 EIP | ✅ |
| 3 | 连续 429 限流（`APIGW.0308`） | CCE 写限流 10 次/分钟，429 重试也计数 | hack 脚本 + provider 均内置 3min 退避；脚本间隔 ≥60s | ✅ |
| 4 | provider 镜像在 x86_64 无法运行 | Docker Desktop 默认 arm64 | `docker build --platform linux/amd64` | ✅ |
| 5 | 镜像推 SWR 报 `Invalid image, fail to parse 'manifest.json'` | OCI manifest，SWR 不支持 | `docker save` + `crane push` | ✅ |
| 6 | 跳板机下载工具极慢 | 华为云国际出口慢 | SWR 工具镜像 `capi-cce-tools` 提取 / 本地下载上传 | ✅ |
| 7 | `clusterctl init` 卡 `Fetching providers` | 从 GitHub 拉 CAPI 组件 | 本地下载组件 + 镜像改 SWR + 本地 repository | ✅ |
| 8 | `CCE_CM.0004 Cluster type and network mode is not match` | webhook 默认 category=Turbo 而 mode=vpc-router | category 跟随网络模式 + 校验 | ✅ |
| 9 | 节点全在 default 组（扩展组无节点） | CCE 扩展组创建时不分节点 | 改用**多 MachinePool**（每 AZ 一个） | ✅ |
| 10 | 某 AZ flavor 售罄/无 sub-ENI | 资源紧张（如 4c 无 2C4G） | 换 flavor（`at7.large.1`）或换 AZ | ✅ |
| 11 | kubeconfig server 地址过期（连不上） | CCE cert API 返回创建时旧地址 | provider 用当前 Internal endpoint 覆盖（`59f43ca`） | ✅ |
| 12 | 删除集群卡 finalizer | 未成功创建的集群删除路径 | 手动移除 finalizer + `cleanup-hw` 删 CCE 资源 | ✅ |

---

## 7. 其他

### 7.1 清理资源（按顺序）

```bash
# 1. 删集群 B（CAPI 删除链）
kubectl delete cluster my-cce-cluster
# 2. 删管理集群 A（本地）
nocloud CLOUD_SDK_AK=<AK> CLOUD_SDK_SK=<SK> go run ./hack/cleanup-hw -cluster <MGMT_CLUSTER_ID>
# 3. 删 EIP / ECS / 密钥对 / 安全组 / 子网 / VPC（控制台或 cleanup-hw）
```

### 7.2 凭证轮换

```bash
# 更新 credentials Secret（无需重启 provider，watch 自动生效）
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default --from-literal=accessKey='<新AK>' --from-literal=secretKey='<新SK>' \
  --dry-run=client -o yaml | kubectl apply -f -
```

### 7.3 环境变量速查

| 变量 | 用途 | 示例 |
|---|---|---|
| `CLOUD_SDK_AK` / `CLOUD_SDK_SK` | 华为云 AK/SK | `<AK>` / `<SK>` |
| `CCE_DEPLOY_REGION` | 区域 | `cn-north-4` |
| `CCE_DEPLOY_VPC` / `CCE_DEPLOY_SUBNET` | VPC / 节点子网 ID | `82fe9d4d-...` |
| `CCE_DEPLOY_ENI_SUBNET` | ENI 子网 neutron ID（Turbo） | `e0cdcc6b-...` |
| `CCE_DEPLOY_KEYPAIR` | 节点 SSH 密钥对 | `capi-bastion-key` |
| `CCE_DEPLOY_CATEGORY` | 集群类别（默认 turbo） | `turbo` / `standard` |
| `CCE_DEPLOY_K8S_VERSION` | K8s 版本 | `v1.35` |

### 7.4 工具速查

| 工具 | 用途 |
|---|---|
| `hack/deploy-network` | 创建 VPC/子网/密钥对 |
| `hack/deploy-bastion` | 创建跳板机 ECS |
| `hack/deploy-mgmt-cluster` | 创建/列出/删除管理集群 |
| `hack/cleanup-hw` | 删除 CCE 集群/节点池/EIP |
| `hack/survey-hw` | 盘点所有华为云资源 |
| `hack/swr-login` | 获取 SWR 临时登录凭据 |

### 7.5 EKS 能力对照

| 能力 | EKS / CAPA | 本项目 |
|---|---|---|
| 集群类型 | EKS 托管控制面 | CCE 托管（Turbo 默认 / Standard 可选） |
| 节点数 | `MachinePool.spec.replicas` | 同（`kubectl scale`） |
| 节点规格 | `instanceType` | `CCEManagedMachinePool.spec.flavor` |
| 多 AZ | 节点组跨 AZ / 多节点组 | 多 MachinePool（每 AZ 一个） |
| ServiceCIDR | 创建时指定不可变 | `CCE_DEPLOY_SERVICE_CIDR` |

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
  - name: swr.cn-north-4.myhuaweicloud.com/capi_cce/cluster-api-cce-controller
    newName: swr.cn-north-4.myhuaweicloud.com/capi_cce/cluster-api-cce-controller
    newTag: latest
EOF
kubectl kustomize "$ARTIFACTS" > "$ARTIFACTS/infrastructure-components.yaml"
```

> webhook 证书由 cert-manager 自动签发（`config/certmanager` Issuer/Certificate + `inject-ca-from` 注解），无需手动 openssl。
