# 华为云 CCE Provider 部署指导 · 无跳板机版（本地直连）

> 在华为云 CCE 管理集群上安装 Cluster API（CAPI）+ CCE Provider，声明式管理 CCE 工作负载集群。
> **对标 EKS 公网端点的标准用法**：集群 A 开公网 endpoint，**本地电脑直接 `kubectl` 连接**，无需跳板机 ECS。

> 需要跳板机（私有端点/生产隔离）？见 [docs/deployment-guide.md](docs/deployment-guide.md)（跳板机版）。

---

## 1. 框架概述

### 1.1 架构

```
┌──────── 本地电脑（本指南所有命令在此执行）────────┐
│  kubectl + clusterctl                             │
│         │  直连公网 endpoint                      │
└─────────┼─────────────────────────────────────────┘
          ▼
┌────────────────────────── 华为云 cn-north-4 ──────────────────────────┐
│  管理集群 A (CCE)  ←—— 运行 CAPI core + Provider + cert-manager        │
│  · 公网 endpoint（https://<公网IP>:5443）                              │
│         │                                                             │
│  工作负载集群 B (CCE)  ←—— Provider 调 CCE API 创建                   │
│  · 默认 Turbo (eni)，多节点池跨 AZ                                    │
└──────────────────────────────────────────────────────────────────────┘
```

### 1.2 组件一览

| 组件 | 是什么 | 本文档角色 |
|---|---|---|
| CCE | 华为云托管 Kubernetes | 管理集群 A + 工作负载集群 B |
| CAPI (cluster-api) | K8s 官方集群管理框架 | 声明式管理集群 |
| CCE Provider（本项目） | CAPI 插件，把 CAPI 对象翻译成 CCE API 调用 | 管理集群 A 上运行 |
| cert-manager | 证书管理 | 自动签发 webhook 证书 |
| SWR | 华为云容器镜像仓库 | 存放所有镜像（public，免认证） |

### 1.3 对标 EKS

| EKS/CAPA | 本项目 |
|---|---|
| AWS 控制台创建 EKS（公网端点） | 华为云控制台创建 CCE 集群 A（公网 endpoint） |
| 本地 `aws eks update-kubeconfig` + kubectl | 本地 kubeconfig（server 改公网 endpoint）+ kubectl |
| `clusterctl init --infrastructure aws` | `clusterctl init --infrastructure cce`（本地源） |
| `--flavor eks-managedmachinepool` | 默认 Turbo 多 pool |

---

## 2. 前置条件

### 2.1 华为云资源

| 项目 | 要求 |
|---|---|
| 账户 | 能登录 [华为云控制台](https://console.huaweicloud.com)，区域 `cn-north-4` |
| 余额 | 充足（CCE 集群 + 节点按需计费；余额 0 报 `CCE.01429004`） |
| AK/SK | 控制台 → 右上角用户名 → 我的凭证 → 访问密钥（需 CCE/VPC/ECS/EIP/SWR 权限） |
| 配额 | CCE 集群配额（默认 50） |
| 本地网络 | **能访问集群 A 的公网 endpoint**（`https://<公网IP>:5443`） |

### 2.2 本地工具（macOS/Linux）

```bash
# kubectl
brew install kubectl          # macOS；或 curl 下载对应平台二进制
# clusterctl v1.14.0（必须精确匹配 CAPI 契约）
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/clusterctl-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') -o clusterctl
chmod +x clusterctl && sudo mv clusterctl /usr/local/bin/
kubectl version --client && clusterctl version
```

### 2.3 术语表

| 术语 | 是什么（通俗） | 本文档用途 |
|---|---|---|
| AK / SK | 华为云 API 访问密钥（给程序用的账号密码） | Provider 调华为云 API 的凭证 |
| VPC / 子网 | 隔离网络 / 网段 | 集群 A/B 的网络 |
| 密钥对 | SSH 公私钥 | 节点 SSH 排查 |
| EIP | 弹性公网 IP | 集群 A 公网 endpoint |
| CCE | 华为云托管 Kubernetes | 集群 A/B 即 CCE 集群 |
| SWR | 华为云容器镜像仓库 | 存所有镜像（public） |
| CAPI / clusterctl | K8s 集群管理框架 / 其命令行 | 声明式管理集群 |
| Provider | CAPI 插件，翻译成云 API | 本项目（CCE provider） |
| MachinePool | CAPI 的节点池对象 | 对应 CCE 节点池；多 AZ = 多 MachinePool |

### 2.4 开始前自检

1. 能登录华为云控制台，区域 `cn-north-4`。
2. 账户有余额。
3. 有 AK/SK。
4. 本地装好 kubectl + clusterctl v1.14.0。
5. 本地网络能到集群 A 公网 endpoint（阶段一创建后验证）。

---

## 3. SWR 公共镜像清单（已备好，可直接用）

以下 **8 个镜像**已提前构建/搬运到 **public SWR**（`swr.cn-north-4.myhuaweicloud.com/capi_cce/`），全部 **amd64**（X86）且 **public**（匿名免认证拉取，已验证）。部署时直接用，无需 imagePullSecret、无需本地构建镜像。

| SWR 仓库（`swr.cn-north-4.myhuaweicloud.com/capi_cce/`） | 来源 | 用途 |
|---|---|---|
| `cluster-api-cce-controller:latest` | 本地构建 | **CCE Provider 控制器**（本项目） |
| `cluster-api-controller:v1.14.0` | registry.k8s.io/cluster-api | CAPI 核心 |
| `kubeadm-bootstrap-controller:v1.14.0` | registry.k8s.io/cluster-api | CAPI bootstrap-kubeadm |
| `kubeadm-control-plane-controller:v1.14.0` | registry.k8s.io/cluster-api | CAPI control-plane-kubeadm |
| `cert-manager-controller:v1.21.1` | quay.io/jetstack | cert-manager 控制器 |
| `cert-manager-cainjector:v1.21.1` | quay.io/jetstack | cert-manager CA 注入 |
| `cert-manager-webhook:v1.21.1` | quay.io/jetstack | cert-manager webhook |
| `capi-cce-tools:latest` | 本地打包 | 工具：kubectl v1.30 + clusterctl v1.14（备选） |

> 完整路径示例：`swr.cn-north-4.myhuaweicloud.com/capi_cce/cluster-api-cce-controller:latest`。

---

## 4. 总体流程

```
【阶段一：控制台创建基础设施】纯点击，无 CLI
  1. VPC + 子网 + 密钥对（控制台）
  2. CCE 管理集群 A（控制台：集群 + 节点池 + 公网 endpoint + 下载 kubeconfig）

【阶段二：本地直连集群 A】
  3. 配置 kubeconfig（server 改公网 endpoint）
  4. 下载 provider 组件（curl GitHub）
  5. clusterctl init（本地源 + 镜像走 SWR）
  6. Provider 镜像（方式 B：public SWR 免认证）

【阶段三：工作负载集群 B + 验证】
  7. 创建集群 B（默认 Turbo 多 pool，3 节点 3 AZ）
  8. 验证 + 扩缩容
```

| 步骤 | 操作 | 耗时 | 产物 |
|---|---|---|---|
| 1 | VPC/子网/密钥对 | 3 min | 网络 + 密钥对 |
| 2 | CCE 管理集群 A | 10-20 min | 集群 A + kubeconfig |
| 3 | 配置 kubeconfig | 1 min | 本地连集群 A |
| 4 | 下载组件 | 2 min | components/metadata/模板 |
| 5 | clusterctl init | 5 min | CAPI + Provider Running |
| 6 | Provider 镜像 | 1 min | 方式 B 免认证 |
| 7 | 集群 B | 10-20 min | 集群 B Provisioned |
| 8 | 验证 + 扩缩容 | 5 min | 节点 Ready + scale |

---

## 5. 部署步骤

### 阶段一：控制台创建基础设施（纯点击）

**步骤 1：VPC + 子网 + 密钥对（控制台）**

1. 控制台 → 网络 → 虚拟私有云 VPC → 创建虚拟私有云：名称 `capi-vpc`，网段 `10.0.0.0/16`。
2. 进入 VPC → 子网 → 创建子网：
   - 节点子网 `capi-subnet-node`：网段 `10.0.1.0/24`；**DNS 服务器地址保持默认**（华为云自动填云内 DNS `100.125.1.250,100.125.129.250`，可解析 OBS/SWR 内网域名——勿改成公网 DNS）。
   - ENI 子网 `capi-subnet-eni`：网段 `10.0.2.0/24`（**Turbo 容器网络用**；与节点子网同一入口：VPC → 子网 → 创建子网，DNS 保持默认）。创建后记下子网 ID 和 **neutron_subnet_id**（子网详情 → 网络 ID），集群 A 网络配置要用。
3. **创建密钥对**：控制台 → 计算 → 弹性云服务器 ECS → 密钥对 → 创建密钥对：
   - 名称：`capi-bastion-key`；点击「确定」后浏览器**自动下载私钥 `capi-bastion-key.pem`**，**保存并保留**（节点 SSH 排查用）。
   - **记录密钥对名称**：后续节点池（集群 A/B）都要选它（集群 B 模板的 `VERIFY-KEYPAIR-NAME`）。

**步骤 2：CCE 管理集群 A（控制台，公网 endpoint）**

1. 控制台 → 计算 → 云容器引擎 CCE → 购买集群：
   - **集群名称：`capi-mgmt`**（固定，后续 kubeconfig/引用都以它为准）；集群类型：**CCE Turbo**（默认，eni 网络）；版本 `v1.35`；规模 `cce.s1.small`；按需计费。
   - 网络：VPC `capi-vpc`、节点子网 `capi-subnet-node`、**ENI 子网 `capi-subnet-eni`**（Turbo 必填，控制台对应「**容器子网**」字段，下拉选 `capi-subnet-eni`）。
   - 节点池：规格 `c7.large.2`（sub-ENI 配额）×2 节点，密钥对 `capi-bastion-key`，可用区 `cn-north-4a`。
2. 提交，等待集群「可用」（约 5-10 分钟）。
3. **公网 endpoint**：集群详情 → 连接信息 → 绑定公网 IP，记录 `https://<公网IP>:5443`（对标 EKS 公网端点）。
4. **下载 kubeconfig**：连接信息 → 下载 kubectl 配置文件 → 保存到本地 `~/.kube/capi-mgmt.kubeconfig`（步骤 3 用 `export KUBECONFIG` 指向）。

> 若用 Standard（vpc-router）集群：集群类型选 CCE Standard，不填 ENI 子网，节点规格任意通用型（`c6.large.2`）。

### 阶段二：本地直连集群 A

**步骤 3：配置 kubeconfig（server 改公网 endpoint）**

控制台下载的 kubeconfig server 是内网地址，本地直连需改为公网 endpoint：

```bash
export KUBECONFIG=~/.kube/capi-mgmt.kubeconfig
kubectl config set-cluster capi-mgmt --server=https://<集群A公网IP>:5443
kubectl get nodes    # 应看到 2 个 Ready 节点（本地直连公网 endpoint）
```

> 若集群 A 公网来源 IP 有限制，需把本地出口 IP 加入白名单。

**步骤 4：下载 provider 组件（curl GitHub）**

```bash
# 本项目组件（components + metadata + 模板，已发布到 GitHub）
BASE=https://raw.githubusercontent.com/weimantian/cloudnative-cluster-api-provider-cce/main
curl -L -o /tmp/infrastructure-components.yaml $BASE/release/infrastructure-components.yaml
curl -L -o /tmp/metadata.yaml $BASE/metadata.yaml
mkdir -p /tmp/templates && cd /tmp/templates
curl -L -O $BASE/config/samples/cluster-template.yaml
curl -L -O $BASE/config/samples/cluster-template-standard.yaml
curl -L -O $BASE/config/samples/cluster-template-turbo.yaml

# CAPI 官方组件（core/bootstrap/control-plane），镜像改 SWR
cd /tmp
for c in core bootstrap control-plane; do
  curl -L -o $c-components.yaml https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/$c-components.yaml
done
curl -L -o capi-metadata.yaml https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/metadata.yaml

sed -i '' 's|registry.k8s.io/cluster-api/cluster-api-controller:v1.14.0|swr.cn-north-4.myhuaweicloud.com/capi_cce/cluster-api-controller:v1.14.0|g' /tmp/core-components.yaml
sed -i '' 's|registry.k8s.io/cluster-api/kubeadm-bootstrap-controller:v1.14.0|swr.cn-north-4.myhuaweicloud.com/capi_cce/kubeadm-bootstrap-controller:v1.14.0|g' /tmp/bootstrap-components.yaml
sed -i '' 's|registry.k8s.io/cluster-api/kubeadm-control-plane-controller:v1.14.0|swr.cn-north-4.myhuaweicloud.com/capi_cce/kubeadm-control-plane-controller:v1.14.0|g' /tmp/control-plane-components.yaml
```

> GitHub 慢时，在 URL 前加加速前缀（如 `https://ghfast.top/`）。macOS 的 `sed` 用 `-i ''`。

**步骤 5：clusterctl init（方式 A / 方式 B 二选一，各自完整可复制）**

**方式 A：clusterctl 默认（cert-manager 由 clusterctl 自动装，quay.io 拉取）**

```bash
# ===== 方式 A（完整版，一次复制执行）=====

# 1. 组织 repository 目录 + 配置 clusterctl
mkdir -p /tmp/repository/{cluster-api,bootstrap-kubeadm,control-plane-kubeadm}/v1.14.0
mkdir -p /tmp/repository/infrastructure-cce/v0.1.0
cp /tmp/core-components.yaml          /tmp/repository/cluster-api/v1.14.0/
cp /tmp/capi-metadata.yaml            /tmp/repository/cluster-api/v1.14.0/metadata.yaml
cp /tmp/bootstrap-components.yaml     /tmp/repository/bootstrap-kubeadm/v1.14.0/
cp /tmp/capi-metadata.yaml            /tmp/repository/bootstrap-kubeadm/v1.14.0/metadata.yaml
cp /tmp/control-plane-components.yaml /tmp/repository/control-plane-kubeadm/v1.14.0/
cp /tmp/capi-metadata.yaml            /tmp/repository/control-plane-kubeadm/v1.14.0/metadata.yaml
cp /tmp/infrastructure-components.yaml /tmp/repository/infrastructure-cce/v0.1.0/
cp /tmp/metadata.yaml                  /tmp/repository/infrastructure-cce/v0.1.0/metadata.yaml

mkdir -p ~/.cluster-api
cat > ~/.cluster-api/clusterctl.yaml <<'EOF'
providers:
  - name: "cluster-api"
    url: "file:///tmp/repository/cluster-api/v1.14.0/core-components.yaml"
    type: "CoreProvider"
  - name: "kubeadm"
    url: "file:///tmp/repository/bootstrap-kubeadm/v1.14.0/bootstrap-components.yaml"
    type: "BootstrapProvider"
  - name: "kubeadm"
    url: "file:///tmp/repository/control-plane-kubeadm/v1.14.0/control-plane-components.yaml"
    type: "ControlPlaneProvider"
  - name: "cce"
    url: "file:///tmp/repository/infrastructure-cce/v0.1.0/infrastructure-components.yaml"
    type: "InfrastructureProvider"
EOF

# 2. clusterctl init（自动装 cert-manager + CAPI + bootstrap + control-plane + cce）
export KUBECONFIG=~/.kube/capi-mgmt.kubeconfig
clusterctl init --core cluster-api --bootstrap kubeadm --control-plane kubeadm --infrastructure cce
```

> ⚠️ `~/.cluster-api/overrides/` 目录会干扰 init，先删除。若卡 cert-manager（quay.io 慢），改用方式 B。

**方式 B：public SWR 免认证（全部镜像走 SWR，完整安装）**

> 所有镜像（CAPI core/bootstrap/control-plane + cert-manager + provider）均从 public SWR 拉取，不依赖 quay.io / registry.k8s.io 公网连通性。

```bash
# ===== 方式 B：全部镜像走 public SWR（完整安装，一次复制执行）=====

# 1. 组织 repository 目录 + 配置 clusterctl
mkdir -p /tmp/repository/{cluster-api,bootstrap-kubeadm,control-plane-kubeadm}/v1.14.0
mkdir -p /tmp/repository/infrastructure-cce/v0.1.0
cp /tmp/core-components.yaml          /tmp/repository/cluster-api/v1.14.0/
cp /tmp/capi-metadata.yaml            /tmp/repository/cluster-api/v1.14.0/metadata.yaml
cp /tmp/bootstrap-components.yaml     /tmp/repository/bootstrap-kubeadm/v1.14.0/
cp /tmp/capi-metadata.yaml            /tmp/repository/bootstrap-kubeadm/v1.14.0/metadata.yaml
cp /tmp/control-plane-components.yaml /tmp/repository/control-plane-kubeadm/v1.14.0/
cp /tmp/capi-metadata.yaml            /tmp/repository/control-plane-kubeadm/v1.14.0/metadata.yaml
cp /tmp/infrastructure-components.yaml /tmp/repository/infrastructure-cce/v0.1.0/
cp /tmp/metadata.yaml                  /tmp/repository/infrastructure-cce/v0.1.0/metadata.yaml

mkdir -p ~/.cluster-api
cat > ~/.cluster-api/clusterctl.yaml <<'EOF'
providers:
  - name: "cluster-api"
    url: "file:///tmp/repository/cluster-api/v1.14.0/core-components.yaml"
    type: "CoreProvider"
  - name: "kubeadm"
    url: "file:///tmp/repository/bootstrap-kubeadm/v1.14.0/bootstrap-components.yaml"
    type: "BootstrapProvider"
  - name: "kubeadm"
    url: "file:///tmp/repository/control-plane-kubeadm/v1.14.0/control-plane-components.yaml"
    type: "ControlPlaneProvider"
  - name: "cce"
    url: "file:///tmp/repository/infrastructure-cce/v0.1.0/infrastructure-components.yaml"
    type: "InfrastructureProvider"
EOF

# 2. 安装 cert-manager（SWR 镜像）
curl -L -o /tmp/cert-manager.yaml https://github.com/cert-manager/cert-manager/releases/download/v1.21.1/cert-manager.yaml
sed -i '' 's|quay.io/jetstack/cert-manager-controller:v1.21.1|swr.cn-north-4.myhuaweicloud.com/capi_cce/cert-manager-controller:v1.21.1|g' /tmp/cert-manager.yaml
sed -i '' 's|quay.io/jetstack/cert-manager-cainjector:v1.21.1|swr.cn-north-4.myhuaweicloud.com/capi_cce/cert-manager-cainjector:v1.21.1|g' /tmp/cert-manager.yaml
sed -i '' 's|quay.io/jetstack/cert-manager-webhook:v1.21.1|swr.cn-north-4.myhuaweicloud.com/capi_cce/cert-manager-webhook:v1.21.1|g' /tmp/cert-manager.yaml
kubectl apply -f /tmp/cert-manager.yaml
kubectl -n cert-manager rollout status deploy/cert-manager deploy/cert-manager-cainjector deploy/cert-manager-webhook --timeout=180s

# 3. clusterctl init（安装 CAPI + bootstrap + control-plane + cce，全部走 SWR）
export KUBECONFIG=~/.kube/capi-mgmt.kubeconfig
clusterctl init --core cluster-api --bootstrap kubeadm --control-plane kubeadm --infrastructure cce
```

> ⚠️ `~/.cluster-api/overrides/` 目录会干扰 init，先删除。

> ⚠️ `~/.cluster-api/overrides/` 目录会干扰 init，先删除。



**步骤 6：Provider 镜像（方式 B：public SWR 免认证）**

clusterctl init 装完，provider 镜像从 public SWR 免认证直拉，webhook 证书由 cert-manager 自动签发——**无任何手动步骤**：

```bash
kubectl get pods -n capi-cce-system    # capi-cce-controller-manager 1/1 Running
kubectl get certificate -n capi-cce-system serving-cert   # Ready=True
```

### 阶段三：工作负载集群 B + 验证

**步骤 7：创建集群 B（默认 Turbo 多 pool，3 节点 3 AZ）**

```bash
mkdir -p ~/.cluster-api/overrides/infrastructure-cce/v0.1.0
cp /tmp/templates/cluster-template.yaml          ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template.yaml
cp /tmp/templates/cluster-template-standard.yaml ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template-standard.yaml
cp /tmp/templates/cluster-template-turbo.yaml    ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template-turbo.yaml

# 生成（默认 Turbo 多 pool；--worker-machine-count=1 → 3 个 pool 各 1 节点 = 3 节点 3 AZ）
clusterctl generate cluster my-cce-cluster --kubernetes-version v1.35.0 --worker-machine-count=1 > my-cluster.yaml

# 替换 VERIFY-*（region/VPC/子网/ENI 子网/密钥对/AZ/AZ2/AZ3/flavor）
sed -i '' \
  -e 's|VERIFY-REGION|cn-north-4|g' -e 's|VERIFY-VPC-ID|<VPC-ID>|g' \
  -e 's|VERIFY-SUBNET-ID|<节点子网-ID>|g' -e 's|VERIFY-ENI-SUBNET-ID|<ENI子网-ID>|g' \
  -e 's|VERIFY-ENI-NEUTRON-ID|<ENI子网-neutron-ID>|g' \
  -e 's|VERIFY-AZ2|cn-north-4b|g' -e 's|VERIFY-AZ3|cn-north-4c|g' \
  -e 's|VERIFY-AZ|cn-north-4a|g' -e 's|VERIFY-KEYPAIR-NAME|capi-bastion-key|g' \
  -e 's|VERIFY-FLAVOR|c7.large.2|g' \
  my-cluster.yaml

# 创建凭据 Secret + bootstrap Secret
export CLOUD_SDK_AK='<你的AK>' CLOUD_SDK_SK='<你的SK>'
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default --from-literal=accessKey="$CLOUD_SDK_AK" --from-literal=secretKey="$CLOUD_SDK_SK"
kubectl create secret generic my-cce-cluster-bootstrap \
  --namespace default --from-literal=value=""

# （可选）集群 B 公网 endpoint：本地直连验证节点时需要（默认私网，本地无法直达）
# 需要公网则取消注释执行；保持私网则跳过
sed -i '' 's|    public: false|    public: true|g' my-cluster.yaml

kubectl apply -f my-cluster.yaml
```

> 多 AZ = 多 MachinePool（对标 EKS 多节点组）：`pool-0/1/2` 各在一个 AZ。⚠️ 各 AZ 需有可用 sub-ENI flavor（如 `cn-north-4c` 常缺 `c7.large.2`，换 `at7.large.1` 或换 AZ）。

**步骤 8：验证 + 扩缩容**

```bash
kubectl get cluster my-cce-cluster -w        # PHASE=Provisioned
kubectl get ccemanagedcontrolplane my-cce-cluster-control-plane -w   # Ready=True

clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig

# 验证节点——按集群 B endpoint 方式二选一：
# 公网（步骤 7 可选已开启）：本地直连
kubectl --kubeconfig=my-cce-cluster.kubeconfig get nodes -o wide   # 3 个 Ready 节点
# 私网（默认，本地无法直达）：在同 VPC 的跳板机内执行上述命令

# 扩缩容 = 调整 replicas（期望值，对标 EKS desiredSize；同一个 scale 命令，replicas 调大=扩容、调小=减容）
# 扩容：pool-0 replicas 1→3（集群 B 节点 3→5）
kubectl scale machinepool my-cce-cluster-pool-0 --replicas=3
kubectl get machinepool my-cce-cluster-pool-0 -w      # 等 CURRENT/AVAILABLE=3（约 2-3 分钟）

# 减容：pool-0 replicas 3→1（集群 B 节点 5→3）——CCE 缩容需节点排水，稍慢
kubectl scale machinepool my-cce-cluster-pool-0 --replicas=1
kubectl get machinepool my-cce-cluster-pool-0 -w      # 等 CURRENT/AVAILABLE=1（约 3-5 分钟）
```

---

## 6. 踩坑问题记录

| # | 问题现象 | 根因 | 修正 | 状态 |
|---|---|---|---|---|
| 1 | 节点永久卡 `Installing` | 子网 DNS 被改成非云内 DNS（API 创建子网不填 `primary_dns` 默认为空） | 控制台创建保持默认；API/脚本（`deploy-network`）显式填云内 DNS `100.125.1.250,100.125.129.250`（cn-north-4） | ✅ |
| 2 | 集群无公网 endpoint | CCE 不自动分配公网 IP | 集群详情绑定 EIP | ✅ |
| 3 | 本地连集群 A 失败 | kubeconfig server 是内网地址 | `kubectl config set-cluster --server=<公网IP>:5443` | ✅ |
| 4 | 连续 429 限流（`APIGW.0308`） | CCE 写限流 10 次/分钟，429 重试也计数 | provider 内置 3min 退避；操作间隔 ≥60s | ✅ |
| 5 | `clusterctl init` 卡 `Fetching providers` | 从 GitHub 拉 CAPI 组件 | 本地下载组件 + 镜像改 SWR + 本地 repository | ✅ |
| 6 | `CCE_CM.0004 type and network mode not match` | webhook 默认 category=Turbo 而 mode=vpc-router | category 跟随网络模式 + 校验 | ✅ |
| 7 | 节点全在 default 组 | CCE 扩展组创建时不分节点 | 多 MachinePool（每 AZ 一个） | ✅ |
| 8 | 某 AZ flavor 售罄/无 sub-ENI | 资源紧张（如 4c 无 2C4G） | 换 flavor（`at7.large.1`）或换 AZ | ✅ |
| 9 | 集群 B kubeconfig 本地连不上 | 集群 B 默认私有 endpoint | 开集群 B 公网 endpoint（`spec.endpointAccess.public=true`） | ✅ |
| 10 | 删除集群卡 finalizer | 未成功创建的集群删除路径 | 手动移除 finalizer | ✅ |

---

## 7. 清理资源

```bash
# 1. 删工作负载集群 B（本地，CAPI 删除链）
kubectl delete cluster my-cce-cluster

# 2. 删管理集群 A + VPC（控制台删除）
#    CCE 控制台删集群 A → VPC 控制台删子网 + VPC（EIP 随集群删除释放）
```

---

## 8. 其他

### 8.1 凭证轮换

```bash
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default --from-literal=accessKey='<新AK>' --from-literal=secretKey='<新SK>' \
  --dry-run=client -o yaml | kubectl apply -f -
```

### 8.2 命令速查表（全部在本地执行）

| 操作 | 命令 |
|---|---|
| 连集群 A | `export KUBECONFIG=~/.kube/capi-mgmt.kubeconfig && kubectl get nodes` |
| 初始化 Provider | `clusterctl init --core cluster-api --bootstrap kubeadm --control-plane kubeadm --infrastructure cce` |
| 生成集群 B | `clusterctl generate cluster my-cce-cluster --kubernetes-version v1.35.0 --worker-machine-count=1 > my-cluster.yaml` |
| 提交集群 B | `kubectl apply -f my-cluster.yaml` |
| 监控状态 | `kubectl get cluster my-cce-cluster -w` |
| 获取集群 B kubeconfig | `clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig` |
| 验证节点 | `kubectl --kubeconfig=my-cce-cluster.kubeconfig get nodes` |
| 扩缩容 | `kubectl scale machinepool my-cce-cluster-pool-0 --replicas=N` |
| 删集群 B | `kubectl delete cluster my-cce-cluster` |

### 8.3 EKS 能力对照

| 能力 | EKS / CAPA | 本项目 |
|---|---|---|
| 集群类型 | EKS 托管控制面 | CCE 托管（Turbo 默认 / Standard 可选） |
| 节点数 | `MachinePool.spec.replicas` | 同（`kubectl scale`） |
| 节点规格 | `instanceType` | `CCEManagedMachinePool.spec.flavor` |
| 多 AZ | 节点组跨 AZ / 多节点组 | 多 MachinePool（每 AZ 一个） |
| 本地访问 | `aws eks update-kubeconfig`（公网端点） | kubeconfig server 改公网 endpoint |

---

## 附录 A：跳板机模式（生产安全，可选）

需要**私有端点 / 安全隔离**（集群 A 不暴露公网）时，用跳板机 ECS 模式：

- 创建 ECS 跳板机（与集群 A 同 VPC）。
- 控制台 CloudShell 登录跳板机，在跳板机内执行阶段二/三命令（kubeconfig 用内网 server，无需改公网）。
- 完整步骤见 [docs/deployment-guide.md](docs/deployment-guide.md)。

## 附录 B：hack 脚本（可选自动化）

主路径用控制台 + 本地 CLI；以下 hack 脚本可在任意有 go 的环境运行，一键创建/清理基础设施（调华为云 API，替代控制台手动步骤）：

| 脚本 | 用途 | 关键环境变量 |
|---|---|---|
| `hack/deploy-network` | 一键 VPC/子网/密钥对 | `CLOUD_SDK_AK/SK`、`CCE_DEPLOY_REGION` |
| `hack/deploy-mgmt-cluster` | 一键管理集群 A（默认 Turbo + 公网 endpoint） | `CCE_DEPLOY_CATEGORY`、`CCE_DEPLOY_ENI_SUBNET`、`CCE_DEPLOY_PUBLIC` 等 |
| `hack/cleanup-hw` | 删 CCE 集群/节点池/EIP | `-cluster <id>` |
| `hack/survey-hw` | 盘点所有资源 | `CLOUD_SDK_AK/SK` |
