# 华为云 CCE Provider 部署指导

> 在华为云 CCE 管理集群上安装 Cluster API（CAPI）+ CCE Provider，声明式管理 CCE 工作负载集群。
> **生产实践**：基础设施全部在华为云控制台创建，所有 CLI 命令在 ECS 跳板机终端内执行，本地电脑不参与。

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
| ECS | 云服务器 | 跳板机（运维入口，控制台 CloudShell 登录） |
| SWR | 华为云容器镜像仓库 | 存放所有镜像（public，免认证） |
| CAPI (cluster-api) | K8s 官方集群管理框架 | 声明式管理集群 |
| CCE Provider（本项目） | CAPI 插件，把 CAPI 对象翻译成 CCE API 调用 | 管理集群 A 上运行 |
| cert-manager | 证书管理 | 自动签发 webhook 证书 |



---

## 2. 前置条件

### 2.1 华为云资源

| 项目 | 要求 |
|---|---|
| 账户 | 能登录 [华为云控制台](https://console.huaweicloud.com)，区域 `cn-north-4` |
| 余额 | 充足（CCE 集群 + 节点 + ECS 按需计费；余额 0 报 `CCE.01429004`） |
| AK/SK | 控制台 → 右上角用户名 → 我的凭证 → 访问密钥（需 CCE/VPC/ECS/EIP/SWR 权限） |
| 配额 | CCE 集群配额（默认 50） |

> ⚠️ 华为云无 EC2「IAM 实例角色」等价物，凭证用 **AK/SK 环境变量**（也可选 IAM 委托，见附录 A）。

### 2.2 术语表

| 术语 | 是什么（通俗） | 本文档用途 |
|---|---|---|
| AK / SK | 华为云 API 访问密钥（给程序用的账号密码） | Provider 调华为云 API 的凭证 |
| VPC | 你专属的隔离网络（像一栋楼） | 跳板机 + 集群 A/B 的网络 |
| 子网 | VPC 内的网段（像楼层） | 节点 / 跳板机分配 IP |
| 安全组 | 防火墙规则（放行哪些端口 / IP） | 放行 SSH 22、API 端口 |
| 密钥对 | SSH 公私钥 | 节点 SSH 排查（跳板机登录走 CloudShell，无需私钥） |
| EIP | 弹性公网 IP | 跳板机 / 集群 A 公网 endpoint |
| CCE | 华为云托管 Kubernetes | 集群 A/B 即 CCE 集群 |
| SWR | 华为云容器镜像仓库 | 存所有镜像（public） |
| CAPI / clusterctl | K8s 集群管理框架 / 其命令行 | 声明式管理集群 |
| Provider | CAPI 插件，翻译成云 API | 本项目（CCE provider） |
| CloudShell | 华为云控制台的云命令行（浏览器内） | 登录跳板机（免本地 SSH） |
| MachinePool | CAPI 的节点池对象 | 对应 CCE 节点池；多 AZ = 多 MachinePool |

### 2.3 开始前自检

1. 能登录华为云控制台，区域切到 `cn-north-4`。
2. 账户有余额。
3. 有 AK/SK（不确定权限先用主账号验证）。
4. 记录 AK/SK 备用（跳板机内 `export`）。

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
| `capi-cce-tools:latest` | 本地打包 | 跳板机工具：kubectl v1.30 + clusterctl v1.14 |

> 当前 `cluster-api-cce-controller:latest` digest `e4d624fc`（含 kubeconfig 端点覆盖 + 429 退避修复）。
> 完整路径示例：`swr.cn-north-4.myhuaweicloud.com/capi_cce/cluster-api-cce-controller:latest`。

---

## 4. 总体流程

```
【阶段一：控制台创建基础设施】纯点击，无 CLI
  1. VPC + 子网 + 密钥对（控制台）
  2. ECS 跳板机（控制台，CloudShell 登录）
  3. CCE 管理集群 A（控制台：集群 + 节点池 + 公网 endpoint + 下载 kubeconfig）

【阶段二：跳板机部署】控制台 CloudShell（浏览器内，所有命令在跳板机内）
  4. 装工具（kubectl + clusterctl）
  5. 连集群 A（kubeconfig）
  6. clusterctl init（GitHub Release 组件 + 镜像走 SWR）
  7. Provider 镜像（方式 B：public SWR 免认证）

【阶段三：工作负载集群 B + 验证】CloudShell
  8. 创建集群 B（默认 Turbo 多 pool，3 节点 3 AZ）
  9. 验证 + 扩缩容
```

| 步骤 | 操作 | 耗时 | 产物 |
|---|---|---|---|
| 1 | VPC/子网/密钥对 | 3 min | 网络 + 密钥对 |
| 2 | ECS 跳板机 | 3 min | 跳板机（公网 IP） |
| 3 | CCE 管理集群 A | 10-20 min | 集群 A + kubeconfig |
| 4 | 装工具 | 5 min | kubectl + clusterctl |
| 5 | 连集群 A | 1 min | 2 节点 Ready |
| 6 | clusterctl init | 5 min | CAPI + Provider Running |
| 7 | Provider 镜像 | 1 min | 方式 B 免认证 |
| 8 | 集群 B | 10-20 min | 集群 B Provisioned |
| 9 | 验证 + 扩缩容 | 5 min | 节点 Ready + scale |

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

**步骤 2：ECS 跳板机（控制台）**

1. ECS → 购买弹性云服务器：
   - 计费模式：按需；区域 `cn-north-4`；规格 `s6.small.1`（1C2G）；镜像 EulerOS 2.0。
   - 网络：VPC `capi-vpc`、子网 `capi-subnet-node`。
   - 安全组：新建，入方向放行 **TCP 22**（来源限公司 IP）。
   - 弹性公网 IP：绑定一个（记录公网 IP）。
   - 密钥对：`capi-bastion-key`。
2. 等待 ECS「运行中」。

> 后续登录跳板机用控制台 **CloudShell**（步骤 4），**无需下载私钥到本地**。

**步骤 3：CCE 管理集群 A（控制台）**

1. 控制台 → 计算 → 云容器引擎 CCE → 购买集群：
   - **集群名称：`capi-mgmt`**（固定，后续 kubeconfig/引用都以它为准）；集群类型：**CCE Turbo**（默认，eni 网络）；版本 `v1.35`；规模 `cce.s1.small`；按需计费。
   - 网络：VPC `capi-vpc`、节点子网 `capi-subnet-node`、**ENI 子网 `capi-subnet-eni`**（Turbo 必填，控制台对应「**容器子网**」字段，下拉选 `capi-subnet-eni`）。
   - 节点池：**节点子网选 `capi-subnet-node`**（⚠️ 不是 ENI 子网——ENI 子网只给容器用）；规格 `c7.xlarge.2`（4U8G，sub-ENI 配额）× **3** 节点（⚠️ 管理集群要跑 CAPI + cert-manager + CCE 监控，2C4G×2 会 pod 数满导致 provider Pending；3×4U8G 是推荐配置），密钥对 `capi-bastion-key`，可用区 `cn-north-4a`。
2. 提交，等待集群「可用」（约 5-10 分钟）。
3. **公网 endpoint**：集群详情 → 连接信息 → 绑定公网 IP。
4. **下载 kubeconfig**：连接信息 → 下载 kubectl 配置文件（下载的文件名为 `capi-mgmt-kubeconfig.yaml`）→ 保存到本地，**上传到跳板机 `/root/capi-mgmt-kubeconfig.yaml`**（CloudShell 文件上传，步骤 5 用）：

```bash
# CloudShell 上传后确认
ls -l /root/capi-mgmt-kubeconfig.yaml
```

> 若用 Standard（vpc-router）集群：集群类型选 CCE Standard，不填 ENI 子网，节点规格任意通用型（`c6.large.2`）。

### 阶段二：跳板机部署（控制台 CloudShell）

> 以下命令**全部在跳板机内执行**（控制台 → ECS → 跳板机 → 远程登录 → CloudShell）。

**步骤 4：装工具（kubectl + clusterctl）**

```bash
# CloudShell（跳板机）内执行
export CLOUD_SDK_AK='<你的AK>' CLOUD_SDK_SK='<你的SK>'   # Provider 凭证（后续用到）

# kubectl + clusterctl（linux/amd64）
curl -LO "https://dl.k8s.io/release/v1.30.0/bin/linux/amd64/kubectl"
install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
curl -L https://github.com/kubernetes-sigs/cluster-api/releases/download/v1.14.0/clusterctl-linux-amd64 -o clusterctl
chmod +x clusterctl && mv clusterctl /usr/local/bin/clusterctl

kubectl version --client && clusterctl version
```

> ⚠️ 慢网备选：用 SWR 工具镜像 `capi-cce-tools`（见第 3 章）`docker pull` + `docker cp` 提取。⚠️ 勿用 `yum install kubectl`（华为云源只有 v1.23）。



**步骤 5：连集群 A（kubeconfig）**

上传步骤 3 下载的 `capi-mgmt-kubeconfig.yaml` 到跳板机 `/root/`（CloudShell 文件上传），验证：

```bash
export KUBECONFIG=/root/capi-mgmt-kubeconfig.yaml
kubectl get nodes    # 应看到 2 个 Ready 节点
```

> ⚠️ **终端代理会干扰 GitHub 拉取**：`clusterctl`（Go 程序）和 `curl` 默认走 `http_proxy`/`https_proxy` 环境变量。若终端配了不可达的代理（如 `127.0.0.1:7890`），GitHub API/资产拉取会静默失败，表现为 `failed to find releases tagged with a valid semantic version number`、`SSL_ERROR_SYSCALL` 等。执行前先检查并处理：
>
> ```bash
> env | grep -i proxy   # 看是否有代理变量
> # 方案一（彻底）：剥离代理，下面所有命令都不走代理
> unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY SOCKS_PROXY
> # 方案二（保留代理）：仅让 GitHub 域名绕过代理
> export NO_PROXY=github.com,api.github.com,raw.githubusercontent.com,*.githubusercontent.com
> ```
>
> 方式 A / 方式 B 的命令请在剥离代理（或设置 NO_PROXY）后执行；kubectl 连集群 A 同理。

**方式 A：clusterctl 默认（cert-manager 由 clusterctl 自动装，quay.io 拉取）**

```bash
# ===== 方式 A（完整版，一次复制执行）=====

# 1. 下载现成的 clusterctl.yaml（4 个 provider 已配好，指向 GitHub release）
mkdir -p /root/.cluster-api
curl -L -o /root/.cluster-api/clusterctl.yaml \
  https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/clusterctl.yaml

# 2. clusterctl init（自动装 cert-manager + CAPI + bootstrap + control-plane + cce）
export KUBECONFIG=/root/capi-mgmt-kubeconfig.yaml
clusterctl init --core cluster-api --bootstrap kubeadm --control-plane kubeadm --infrastructure cce
```

> ⚠️ `~/.cluster-api/overrides/` 目录会干扰 init，先删除。若卡 cert-manager（quay.io 慢），改用方式 B。

**方式 B：public SWR 免认证（全部镜像走 SWR，完整安装）**

> 所有镜像（CAPI core/bootstrap/control-plane + cert-manager + provider）均从 public SWR 拉取，不依赖 quay.io / registry.k8s.io 公网连通性。

```bash
# ===== 方式 B：全部镜像走 public SWR（完整安装，一次复制执行）=====

# 1. 下载现成的 clusterctl.yaml（4 个 provider 已配好，指向 GitHub release）
mkdir -p /root/.cluster-api
curl -L -o /root/.cluster-api/clusterctl.yaml \
  https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/clusterctl.yaml

# 2. 安装 cert-manager（SWR 镜像，现成 yaml，无需 sed）
curl -L -o /root/cert-manager.yaml \
  https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/cert-manager.yaml
kubectl apply -f /root/cert-manager.yaml
kubectl -n cert-manager rollout status deploy/cert-manager deploy/cert-manager-cainjector deploy/cert-manager-webhook --timeout=180s

# 3. clusterctl init（安装 CAPI + bootstrap + control-plane + cce，全部走 SWR）
export KUBECONFIG=/root/capi-mgmt-kubeconfig.yaml
clusterctl init --core cluster-api --bootstrap kubeadm --control-plane kubeadm --infrastructure cce
```

> ⚠️ `~/.cluster-api/overrides/` 目录会干扰 init，先删除。

> ⚠️ `~/.cluster-api/overrides/` 目录会干扰 init，先删除。



**步骤 7：Provider 镜像（方式 B：public SWR 免认证）**

clusterctl init 装完，provider 镜像从 public SWR 免认证直拉，webhook 证书由 cert-manager 自动签发——**无任何手动步骤**：

```bash
kubectl -n capi-cce-system get pods    # capi-cce-controller-manager 1/1 Running
kubectl get certificate -n capi-cce-system serving-cert   # Ready=True
```

### 阶段三：工作负载集群 B + 验证（CloudShell）

**步骤 8：创建集群 B（默认 Turbo 多 pool，3 节点 3 AZ）**

```bash
# 1. 下载集群 B 模板（已发布到 GitHub）
curl -L -o /root/cluster-template.yaml          https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/cluster-template.yaml
curl -L -o /root/cluster-template-standard.yaml https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/cluster-template-standard.yaml
curl -L -o /root/cluster-template-turbo.yaml    https://github.com/weimantian/cloudnative-cluster-api-provider-cce/releases/download/v0.1.0/cluster-template-turbo.yaml

# 2. 安装模板到 clusterctl overrides
mkdir -p ~/.cluster-api/overrides/infrastructure-cce/v0.1.0
cp /root/cluster-template.yaml          ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template.yaml
cp /root/cluster-template-standard.yaml ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template-standard.yaml
cp /root/cluster-template-turbo.yaml    ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template-turbo.yaml

# 生成（默认 Turbo 多 pool；--worker-machine-count=1 → 3 个 pool 各 1 节点 = 3 节点 3 AZ）
clusterctl generate cluster my-cce-cluster --kubernetes-version v1.35.0 --worker-machine-count=1 > my-cluster.yaml

# 可选：Standard（vpc-router）版（无需 ENI 子网；节点通用型 flavor，如 c6.large.2）
# clusterctl generate cluster my-cce-cluster --flavor standard --kubernetes-version v1.35.0 --worker-machine-count=1 > my-cluster.yaml

# 替换 VERIFY-*（region/VPC/子网/ENI 子网/密钥对/AZ/AZ2/AZ3/flavor）
sed -i \
  -e 's|VERIFY-REGION|cn-north-4|g' -e 's|VERIFY-VPC-ID|<VPC-ID>|g' \
  -e 's|VERIFY-SUBNET-ID|<节点子网-ID>|g' -e 's|VERIFY-ENI-SUBNET-ID|<ENI子网-ID>|g' \
  -e 's|VERIFY-ENI-NEUTRON-ID|<ENI子网-neutron-ID>|g' \
  -e 's|VERIFY-AZ2|cn-north-4b|g' -e 's|VERIFY-AZ3|cn-north-4g|g' \   # AZ3 -> AZ7 (cn-north-4g)
  -e 's|VERIFY-AZ|cn-north-4a|g' -e 's|VERIFY-KEYPAIR-NAME|capi-bastion-key|g' \
  -e 's|VERIFY-FLAVOR|c7.large.2|g' \
  my-cluster.yaml

# 创建凭据 Secret + bootstrap Secret
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default --from-literal=accessKey="$CLOUD_SDK_AK" --from-literal=secretKey="$CLOUD_SDK_SK"
kubectl create secret generic my-cce-cluster-bootstrap \
  --namespace default --from-literal=value=""

kubectl apply -f my-cluster.yaml
```

> 多 AZ = 多 MachinePool：`pool-0/1/2` 各在一个 AZ。
> ⚠️ ① 各 AZ 需有可用 sub-ENI flavor：pool-2 用 AZ7（`cn-north-4g`，有 `c7.large.2`）；若某 AZ 缺货，可换 `at7.large.1` 或换其他 AZ。
> ⚠️ ② **节点池 AZ 创建后不可变**：AZ/flavor 填错必须删池重建（delete machinepool + ccemanagedmachinepool → 改 yaml → apply），不能只 patch。
> ⚠️ ③ 所有 `VERIFY-*` 都要替换（10 个），替换后 `grep VERIFY my-cluster.yaml` 应无输出；`VERIFY-AZ` 最后替换（先 AZ2/AZ3）。

**步骤 9：验证 + 扩缩容**

```bash
kubectl get cluster my-cce-cluster -w        # PHASE=Provisioned
kubectl get ccemanagedcontrolplane my-cce-cluster-control-plane -w   # Ready=True

clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig
ls -l my-cce-cluster.kubeconfig   # 应有几 KB 内容；无输出则 `kubectl get secret my-cce-cluster-kubeconfig -n default` 检查（不存在则等 1-2 分钟）
kubectl --kubeconfig=my-cce-cluster.kubeconfig get nodes -o wide   # 3 个 Ready 节点（跳板机同 VPC，私网 endpoint 可直达）

# 扩缩容 = 调整 replicas（期望值；同一个 scale 命令，replicas 调大=扩容、调小=减容）
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
| 3 | 连续 429 限流（`APIGW.0308`） | CCE 写限流 10 次/分钟，429 重试也计数 | provider 内置 3min 退避；操作间隔 ≥60s | ✅ |
| 4 | 跳板机下载工具极慢 | 华为云国际出口慢 | SWR 工具镜像 `capi-cce-tools` 提取 / ghfast 加速 | ✅ |
| 5 | `clusterctl init` 卡 `Fetching providers` | 从 GitHub 拉 CAPI 组件 | 本地下载组件 + 镜像改 SWR + 本地 repository | ✅ |
| 6 | `CCE_CM.0004 type and network mode not match` | webhook 默认 category=Turbo 而 mode=vpc-router | category 跟随网络模式 + 校验 | ✅ |
| 7 | 节点全在 default 组 | CCE 扩展组创建时不分节点 | 多 MachinePool（每 AZ 一个） | ✅ |
| 8 | 某 AZ flavor 售罄/无 sub-ENI | 资源紧张（如 4c 无 2C4G） | 换 flavor（`at7.large.1`）或换 AZ | ✅ |
| 9 | kubeconfig server 地址过期 | CCE cert API 返回旧地址 | provider 用当前 Internal endpoint 覆盖 | ✅ |
| 10 | 删除集群卡 finalizer | 未成功创建的集群删除路径 | 手动移除 finalizer | ✅ |
| 11 | provider 等 pod 卡 Pending（`Too many pods`） | 管理集群节点规格小（2C4G×2，pod 上限 16/节点，被 CCE 自带监控占满） | 集群 A 用 4U8G（c7.xlarge.2）×3；Pending pod 自动调度 | ✅ |
| 12 | 节点池 AZ 填错（创建后不可变） | CCE 节点池 AZ 创建后不可改，patch 不重建 | 删池重建（delete machinepool → 改 yaml → apply） | ✅ |
| 13 | 集群 B 创建失败 `Az [VERIFY-AZ] is not in available az list` | `VERIFY-AZ\b` 在 macOS sed 不生效（BSD 不支持 \b），主 AZ 漏替换 | sed 先替换 AZ2/AZ3 再 AZ（不用 \b）；替换后 `grep VERIFY` 确认 | ✅ |
| 14 | `clusterctl get kubeconfig` 无输出 | kubeconfig Secret 未生成（provider 等待中） | `kubectl get secret my-cce-cluster-kubeconfig -n default`；等 1-2 分钟或查 provider 日志 | ✅ |

---

## 7. 清理资源

```bash
# 1. 删工作负载集群 B（CloudShell，CAPI 删除链）
kubectl delete cluster my-cce-cluster

# 2. 删管理集群 A + 跳板机 + VPC（控制台删除）
#    CCE 控制台删集群 A → ECS 控制台删跳板机 → VPC 控制台删子网 + VPC
#    （EIP 在集群/ECS 删除时自动释放）
```

---

## 8. 其他

### 8.1 凭证轮换

```bash
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default --from-literal=accessKey='<新AK>' --from-literal=secretKey='<新SK>' \
  --dry-run=client -o yaml | kubectl apply -f -
```

### 8.2 命令速查表（全部在跳板机执行）

| 操作 | 命令 |
|---|---|
| 连集群 A | `export KUBECONFIG=/root/capi-mgmt-kubeconfig.yaml && kubectl get nodes` |
| 初始化 Provider | `clusterctl init --core cluster-api --bootstrap kubeadm --control-plane kubeadm --infrastructure cce` |
| 生成集群 B | `clusterctl generate cluster my-cce-cluster --kubernetes-version v1.35.0 --worker-machine-count=1 > my-cluster.yaml` |
| 提交集群 B | `kubectl apply -f my-cluster.yaml` |
| 监控状态 | `kubectl get cluster my-cce-cluster -w` |
| 获取集群 B kubeconfig | `clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig` |
| 验证节点 | `kubectl --kubeconfig=my-cce-cluster.kubeconfig get nodes` |
| 扩缩容 | `kubectl scale machinepool my-cce-cluster-pool-0 --replicas=N` |
| 删集群 B | `kubectl delete cluster my-cce-cluster` |



---

## 附录 A：hack 脚本（可选自动化）

> 主路径用控制台 + CloudShell；以下 hack 脚本可在任意有 go 的环境运行，一键创建/清理基础设施（调华为云 API，替代控制台手动步骤）。

| 脚本 | 用途 | 关键环境变量 |
|---|---|---|
| `hack/deploy-network` | 一键 VPC/子网/密钥对 | `CLOUD_SDK_AK/SK`、`CCE_DEPLOY_REGION` |
| `hack/deploy-bastion` | 一键跳板机 ECS | `CCE_DEPLOY_VPC/SUBNET` |
| `hack/deploy-mgmt-cluster` | 一键管理集群 A（默认 Turbo） | `CCE_DEPLOY_CATEGORY`、`CCE_DEPLOY_ENI_SUBNET`、`CCE_DEPLOY_SERVICE_CIDR` 等 |
| `hack/cleanup-hw` | 删 CCE 集群/节点池/EIP | `-cluster <id>` |
| `hack/survey-hw` | 盘点所有资源 | `CLOUD_SDK_AK/SK` |
| `hack/swr-login` | 获取 SWR 临时登录凭据 | `CLOUD_SDK_AK/SK` |

> **IAM 委托（可选）**：跳板机可绑定 IAM 委托（`CCE_DEPLOY_BASTION_AGENCY`），从 ECS metadata 获取临时凭证（免静态 AK/SK），详见旧版 `docs/e2e-deployment-guide.md` 凭证轮换章节。
