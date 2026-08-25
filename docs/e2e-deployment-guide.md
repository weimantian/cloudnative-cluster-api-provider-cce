# 端到端部署文档：ECS 跳板机 + CCE 管理集群 + 工作负载集群

> 本文档记录在华为云上部署 **cloudnative-cluster-api-provider-cce**（本项目）的完整端到端流程。
>
> **架构**：ECS 跳板机（运维入口）→ CCE 管理集群 A（运行 CAPI + Provider）→ CCE 工作负载集群 B（业务集群，私有 API）。
>
> **文档性质**：活文档。后续测试严格按本文档执行，遇到新问题追加到 [踩坑记录](#踩坑记录测试问题--修正) 章节并标记修正。
>
> **选型指引**：若环境能直接访问公网 registry（`quay.io` / `registry.k8s.io`，如海外 region 或允许出网的 VPC），推荐用更简单的 [public-access 方案](public-access-deployment-guide.md)；本方案（零公网）适合私有化 / 内网 / 无法访问公网 registry 的环境。

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

【阶段四：CCE Turbo 变体】（可选，复用阶段一/二）跳板机
  12. 确认 ENI 子网（neutron_subnet_id）
  13. 生成 Turbo 集群 B 配置（mode=eni + ENI 子网 + sub-ENI flavor）
  14. 创建并验证集群 B（Turbo）
  15. 删除集群 B（Turbo）
```

**核心原则**：
1. **集群 A 的创建**（阶段一）在本地执行（调用华为云 CCE API，与执行位置无关）。
2. **集群 B 的所有操作**（阶段二）必须在跳板机上执行——管理集群 A 的 API Server 默认仅内网 endpoint（`https://10.0.x.x:5443`），本地电脑无法直达。
3. **零公网**：节点安装走华为云内网 OBS，CAPI/cert-manager 镜像搬运到 SWR（内网仓库），全程不需要 NAT、不开放公网出网。

---

## 前置条件

### 开始前自检（小白先确认这 5 项，缺一不可）

1. 能登录 [华为云控制台](https://console.huaweicloud.com)，区域切到 `cn-north-4`。
2. 账户**有余额**（CCE 集群 + 节点 + ECS 均按需计费；余额为 0 创建会报 `CCE.01429004`）。
3. 有 **AK/SK**（控制台 → 右上角用户名 → 我的凭证 → 访问密钥）。不确定权限时先用主账号 AK/SK 验证（主账号有全部权限），生产环境再收窄到 CCE/VPC/ECS/EIP/SWR。
4. 本地装好工具：docker / go / kubectl / clusterctl v1.14.0（见下文）。
5. 已 export AK/SK 环境变量（见下文“凭证环境变量”）。

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
export CCE_DEPLOY_REGION='cn-north-4'
export CCE_DEPLOY_AZ='cn-north-4a'
```

### 术语表（小白速查）

| 术语 | 是什么（通俗） | 本文档用途 |
|---|---|---|
| AK / SK | 华为云 API 访问密钥（给程序用的账号密码） | 脚本调华为云 API 的凭证 |
| VPC | 你专属的隔离网络（像一栋楼） | 跳板机 + 集群 A/B 的网络 |
| 子网 | VPC 内的网段（像楼层） | 节点 / 跳板机分配 IP |
| 安全组 | 防火墙规则（放行哪些端口 / IP） | 放行 SSH 22、API 端口 |
| 密钥对 | SSH 公私钥（私钥 = 钥匙，自己保管） | 登录跳板机 / 节点 |
| EIP | 弹性公网 IP（互联网可直接访问） | 跳板机 SSH、集群公网 endpoint |
| kubeconfig | K8s 集群访问配置（地址 + 证书） | kubectl 连集群用 |
| CCE | 华为云托管 Kubernetes 服务 | 集群 A/B 即 CCE 集群 |
| CAPI / clusterctl | K8s 官方集群管理框架 / 其命令行 | 声明式管理集群 |
| Provider | CAPI 插件，把 CAPI 对象翻译成云 API 调用 | 本项目（CCE provider） |

---

## 阶段一：预置基础设施（本地电脑）

### 步骤 1：准备网络与密钥

一键创建 VPC + 节点/ENI 子网 + 密钥对（幂等，按名复用）：

```bash
nocloud go run ./hack/deploy-network
```

| 参数/输出 | 含义 |
|---|---|
| `CLOUD_SDK_AK` / `CLOUD_SDK_SK` | 华为云访问密钥，调用 VPC/ECS API |
| `CCE_DEPLOY_REGION` | 区域，默认 `cn-north-4` |
| 输出 `CCE_DEPLOY_VPC` | VPC ID（`capi-vpc`，CIDR 10.0.0.0/16） |
| 输出 `CCE_DEPLOY_SUBNET` | 节点子网 ID（`capi-subnet-node`，10.0.1.0/24） |
| 输出 `CCE_DEPLOY_KEYPAIR` | 密钥对名（`capi-node-key`，私钥被丢弃，仅节点登录用） |

> ⚠️ **踩坑 #1（DNS）**：脚本建子网时**必须指定 DNS**（`100.125.1.250` + `100.125.129.250`），否则节点拿到错误 DNS、cce-agent 下载失败、永久卡 Installing。已在脚本中修复。

**DNS 说明（在哪设置 / 怎么获取 / 为什么）**：
- **在哪设置**：DNS 是**子网属性**，在创建子网时配置。`deploy-network` 调 VPC API `CreateSubnet` 时显式传 `primaryDns=100.125.1.250`、`secondaryDns=100.125.129.250`（`hack/deploy-network/main.go`）；手动建子网则在 VPC 控制台 → 创建子网 → "DNS 服务器地址"填 `100.125.1.250,100.125.129.250`。
- **怎么获取**：`100.125.1.250` / `100.125.129.250` 是华为云**云内 DNS 服务器地址**（各 region 固定提供，解析 OBS/SWR/IAM 等内网域名）。可查华为云 VPC 官方文档；或从已有子网获取（VPC API `ShowSubnet` / 控制台子网详情 / `hack/survey-hw` 输出）。
- **为什么**：节点用子网 DNS 解析域名，若 DNS 不对（公网 DNS 或 VPC 默认值），解析不到华为云内网 OBS 域名 → cce-agent 下载失败 → 节点永久卡 `Installing`。

**方式二：控制台手动创建网络与密钥**（不跑 `deploy-network` 时，本方式同样满足后续流程）：

1. **创建 VPC**：控制台 → 服务列表 → 网络 → 虚拟私有云 VPC → 创建虚拟私有云。
   - 名称：`capi-vpc`；IPv4 网段：`10.0.0.0/16`；其余默认。记下 **VPC ID**。
2. **创建节点子网**：进入刚创建的 VPC → 子网 → 创建子网。
   - 名称：`capi-subnet-node`；子网网段：`10.0.1.0/24`；可用区：`cn-north-4a`。
   - **DNS 服务器地址**：`100.125.1.250,100.125.129.250`（⚠️ 踩坑 #1：必须显式填，否则节点解析不到内网域名、永久卡 Installing）。记下 **子网 ID**。
3. **（Turbo 才需要）创建 ENI 子网**：同上，名称 `capi-subnet-eni`，网段 `10.0.2.0/24`。
4. **创建密钥对**：控制台 → 计算 → 弹性云服务器 ECS → 密钥对 → 创建密钥对。
   - 名称：`capi-bastion-key`（跳板机/节点 SSH 用）。创建后浏览器自动下载私钥 `capi-bastion-key.pem`，**保存到本地并保留**。

> 完成后把控制台记下的 VPC ID / 子网 ID / 密钥对名，作为后续步骤的 `CCE_DEPLOY_VPC` / `CCE_DEPLOY_SUBNET` / `CCE_DEPLOY_KEYPAIR` 使用。

### 步骤 2：创建跳板机 ECS

```bash
nocloud go run ./hack/deploy-bastion
```

| 参数/输出 | 含义 |
|---|---|
| `CCE_DEPLOY_VPC` / `CCE_DEPLOY_SUBNET` | 跳板机所在 VPC/子网（与集群 A 同 VPC，便于访问内网 API） |
| 默认 flavor | `s6.small.1`（1C2G，最小通用型） |
| 输出 `BASTION_PUBLIC_IP` | 跳板机公网 IP（SSH 登录用） |
| 输出 `capi-bastion-key.pem` | **跳板机私钥（必须保留，SSH 登录用）** |
| `CCE_DEPLOY_BASTION_AGENCY`（可选） | IAM 委托名称：绑定后跳板机可从 ECS metadata 获取临时凭证（对标 CAPA EC2 IAM 角色），适合免静态密钥场景 |

> ⚠️ **踩坑 #2（私钥）**：跳板机密钥对 `capi-bastion-key` 的私钥脚本会保存到本地 `capi-bastion-key.pem`。集群 A 的节点也改用此密钥对（而非 `capi-node-key`），这样节点异常时可 SSH 排查。
>
> ⚠️ **踩坑 #12（Ecs.0314）**：若报 `keypair does not match the user_id`，说明云上存在**其他用户**创建的同名密钥对。删除本地私钥文件（`rm -f capi-bastion-key.pem`）强制脚本新建即可。
**方式二：控制台创建跳板机**（不跑 `deploy-bastion` 时，本方式同样满足后续流程）：

1. 控制台 → 服务列表 → 计算 → 弹性云服务器 ECS → **购买弹性云服务器**。
2. **计费模式**：按需计费（测试用，随时可删）。
3. **基础配置**：
   - 区域 `cn-north-4`；可用区 `cn-north-4a`。
   - 规格 `s6.small.1`（1 vCPU / 2 GiB，最小通用型，够跑 kubectl/clusterctl）。
   - 镜像 `EulerOS 2.0 x86_64`；系统盘默认 40 GiB 即可。
4. **网络配置**：
   - VPC 选 `capi-vpc`、子网选 `capi-subnet-node`（**与集群 A 同 VPC**，便于访问内网 API）。
   - 安全组：新建 `capi-bastion-sg`，入方向放行 **TCP 22**（来源限本机/公司 IP，⚠️ 不要 0.0.0.0/0 全开）。
5. **弹性公网 IP**：分配一个（按带宽计费，5 Mbps 即可），记录公网 IP 作为 `BASTION_PUBLIC_IP`。
6. **登录方式**：密钥对 → 选择 `capi-bastion-key`（⚠️ 踩坑 #2：私钥必须保留；⚠️ 踩坑 #12：若报 Ecs.0314 说明云上已有同名密钥对，换名或先删本地 `capi-bastion-key.pem`）。
7. 点击“立即购买” → 确认 → 等待 ECS 状态变为“运行中”。

> 之后流程不变：`ssh -i capi-bastion-key.pem root@<BASTION_PUBLIC_IP>`。

验证 SSH（首次连接需等 EIP 生效约 30-60 秒）：

```bash
ssh -i capi-bastion-key.pem -o StrictHostKeyChecking=no root@<BASTION_PUBLIC_IP> 'echo SSH_OK; hostname'
```

### 步骤 3：创建 CCE 管理集群 A

> 集群 A 的创建可**直接在本地**执行（调用华为云 CCE API）。

```bash
nocloud CCE_DEPLOY_VPC="$CCE_DEPLOY_VPC" \
  CCE_DEPLOY_SUBNET="$CCE_DEPLOY_SUBNET" \
  CCE_DEPLOY_KEYPAIR='capi-bastion-key' \
  CCE_DEPLOY_K8S_VERSION='v1.35' \
  go run ./hack/deploy-mgmt-cluster
```

| 参数 | 含义 |
|---|---|
| `CCE_DEPLOY_VPC` / `CCE_DEPLOY_SUBNET` | 管理集群所在 VPC/子网 |
| `CCE_DEPLOY_KEYPAIR=capi-bastion-key` | 节点 SSH 密钥对（保留私钥的，可排查） |
| `CCE_DEPLOY_K8S_VERSION=v1.35` | 集群 K8s 版本 |
| 默认 flavor | 集群 `cce.s1.small`，节点 `c6.large.2` ×2 |
| `CCE_DEPLOY_MGMT_AZS`（可选） | 多 AZ 列表（逗号分隔，如 `cn-north-4a,cn-north-4b`）；默认单 AZ（`CCE_DEPLOY_AZ`）。首个 AZ 为主节点池，其余作为扩展组（管理集群高可用） |
| `CCE_DEPLOY_PUBLIC` / `CCE_DEPLOY_PUBLIC_CIDRS`（可选） | 公网访问开关（默认 `true`）+ 来源 IP 白名单（逗号分隔 CIDR，空=全部开放）。对标 CAPA EKS 控制面"公网+私有"端点：默认自动绑定 EIP 开放公网；设 `false` 则仅内网 |
| `CCE_DEPLOY_PUBLIC_NODES`（可选） | 节点出网方式（默认 `true`）：`true` = 每个节点绑公网 EIP（对标 AWS 公有子网，直连出网、无需 NAT）；`false` = 节点私有（需 `hack/nat-egress` NAT 出网）。`CCE_DEPLOY_PUBLIC_NODES_BANDWIDTH` 控制带宽（默认 5 Mbps） |
| 输出 `capi-mgmt.kubeconfig` | **管理集群 kubeconfig（下载到本地）** |

> ⚠️ **踩坑 #3（无公网 endpoint）**：CCE 不自动分配公网 IP——`publicAccess=true` 时仍只有 Internal endpoint。现已由 `deploy-mgmt-cluster` 创建后**自动绑定 EIP**（复用 `hack/bind-eip`）开放公网（对标 CAPA EKS 默认公网+私有端点）；若设 `CCE_DEPLOY_PUBLIC=false` 则保持纯内网，kubeconfig 的 server 是内网 IP，本地无法直达，必须由跳板机访问。
>
> ⚠️ **踩坑 #10（429 限流）**：连续写操作触发 CCE 写限流（10 次/分钟），且每次 429 重试也计入限流计数。`deploy-network`/`deploy-bastion`/`deploy-mgmt-cluster` 已内置 429 退避（自动等窗口清零再重试）；脚本间建议间隔 ≥60s。

**方式二：控制台创建管理集群 A**（不跑 `deploy-mgmt-cluster` 时，本方式同样满足后续流程）：

1. 控制台 → 服务列表 → 计算 → 云容器引擎 CCE → **购买集群**。
2. **集群基础配置**：
   - 集群类型：`CCE Standard`（标准版）。
   - 集群版本：`v1.35`。
   - 集群规模：`cce.s1.small`（50 节点以下，够用）。
   - 计费模式：按需计费（⚠️ 集群 + 节点都计费，测试完记得删）。
   - 集群名称：`capi-mgmt-xxxx`（自定义）。
3. **网络配置**：
   - VPC 选 `capi-vpc`；节点子网选 `capi-subnet-node`。
   - 容器网段 / 服务网段：默认即可（⚠️ 与集群 B 不冲突；同 VPC 下多个集群的容器网段需唯一）。
4. **节点池配置**：
   - 节点规格 `c6.large.2`（2 vCPU / 4 GiB）× 2 个节点。
   - SSH 密钥对 `capi-bastion-key`（保留私钥的，节点异常可 SSH 排查）。
   - 可用区 `cn-north-4a`；其余默认（系统盘 40 GiB）。
5. 确认配置 → 提交，等待集群状态变为“可用”（约 5-10 分钟）。
6. **下载 kubeconfig**：集群详情 → 连接信息 → 下载 kubectl 配置文件，保存到本地，随后上传到跳板机 `/root/capi-mgmt.kubeconfig`。

> ⚠️ 控制台创建的集群 `kubectl` 访问同样走内网 endpoint（踩坑 #3），跳板机同 VPC 可达；下载的 kubeconfig 内网 server 与脚本生成的一致。

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

| 架构 | 构建/搬运必须 amd64（管理集群节点 x86_64）——踩坑 #5 |

> **镜像清单（已备好，public SWR）**：以下 7 个镜像已提前构建/搬运到 **public SWR**（`swr.cn-north-4.myhuaweicloud.com/capi_cce/`），全部 **amd64** 且 **public**（任意节点免认证拉取）。可直接用表内 SWR 地址替换部署清单/组件中的镜像，跳过 4.4 的搬运（或验证已有镜像时直接引用）：

| SWR 仓库（swr.cn-north-4.myhuaweicloud.com/capi_cce/） | 源镜像 | 用途 | 架构 |
|---|---|---|---|
| `cce-provider-controller:latest` | 本地构建 | CCE Provider 控制器 | amd64 |
| `cluster-api-controller:v1.14.0` | registry.k8s.io/cluster-api | CAPI 核心 | amd64 |
| `kubeadm-bootstrap-controller:v1.14.0` | registry.k8s.io/cluster-api | CAPI bootstrap-kubeadm | amd64 |
| `kubeadm-control-plane-controller:v1.14.0` | registry.k8s.io/cluster-api | CAPI control-plane-kubeadm | amd64 |
| `cert-manager-controller:v1.21.1` | quay.io/jetstack | cert-manager 控制器 | amd64 |
| `cert-manager-cainjector:v1.21.1` | quay.io/jetstack | cert-manager CA 注入 | amd64 |
| `cert-manager-webhook:v1.21.1` | quay.io/jetstack | cert-manager webhook | amd64 |
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
> **控制台模式**：步骤 3 用控制台创建集群时，kubeconfig 从控制台[连接信息]下载，同样命名并上传到跳板机 `/root/capi-mgmt.kubeconfig`。

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

### 步骤 8.5：使用 `clusterctl generate cluster` 生成集群 B（推荐，替代手写）

provider 提供 clusterctl 模板（Standard 默认 + Turbo flavor），支持 `clusterctl generate cluster` 自动生成 5 个对象的清单。

```bash
# 跳板机：安装模板到 clusterctl overrides 目录（目录名 = provider 标签/版本）
mkdir -p ~/.cluster-api/overrides/infrastructure-cce/v0.1.0
cp /root/cluster-template-clusterctl.yaml ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template.yaml
cp /root/cluster-template-turbo.yaml    ~/.cluster-api/overrides/infrastructure-cce/v0.1.0/cluster-template-turbo.yaml

# 生成 Standard 集群 B 清单（默认 flavor）
clusterctl generate cluster my-cce-cluster --kubernetes-version v1.35.0 > my-cluster.yaml

# 生成 Turbo 集群 B 清单（eni 网络模式）
clusterctl generate cluster my-cce-cluster --flavor turbo --kubernetes-version v1.35.0 > my-cluster-turbo.yaml
```

| 参数 | 含义 |
|---|---|
| `--flavor turbo` | 选择 Turbo（eni）模板；省略则用默认 Standard（vpc-router）模板 |
| `--kubernetes-version` | K8s 版本（替换 `${KUBERNETES_VERSION}`） |
| `${CLUSTER_NAME}` / `${WORKER_MACHINE_COUNT}` | clusterctl 自动替换；`--worker-machine-count` 控制节点数 |

> ⚠️ 生成后仍需替换 **`VERIFY-*` 占位符**（region/VPC/子网/ENI 子网/密钥对/AZ，clusterctl 不替换这些），再创建 credentials/bootstrap Secret 并 `kubectl apply`（见步骤 9 后半段）。
> 模板源文件：`config/samples/cluster-template-clusterctl.yaml`（Standard）与 `config/samples/cluster-template-turbo.yaml`（Turbo）；纯 kubectl apply 的手写模板仍保留在 `config/samples/cluster-template.yaml`。

### 步骤 9：生成集群 B 配置（托管节点组）+ kubectl apply

> "托管节点组" = CAPI `MachinePool` + 本项目 `CCEManagedMachinePool`（CCE 节点池）。模板在 `config/samples/cluster-template.yaml`。

```bash
# 跳板机：写入标准版（vpc-router）my-cluster.yaml —— 替换 VERIFY-* 占位符
# 模板亦见 config/samples/cluster-template.yaml（本配置为占位符替换后的完整版）
cat > my-cluster.yaml <<'EOF'
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: my-cce-cluster
  namespace: default
spec:
  clusterNetwork:
    pods:
      cidrBlocks: ["10.245.0.0/16"] # 需与同 VPC 其他集群不冲突
    services:
      cidrBlocks: ["10.248.0.0/16"]
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: CCECluster
    name: my-cce-cluster
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1beta2
    kind: CCEManagedControlPlane
    name: my-cce-cluster-control-plane
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: CCECluster
metadata:
  name: my-cce-cluster
  namespace: default
  labels:
    cluster.x-k8s.io/cluster-name: my-cce-cluster
spec:
  region: cn-north-4
  network:
    vpc:
      id: VERIFY-VPC-ID
    subnets:
      - id: VERIFY-SUBNET-ID # 节点子网
        availabilityZone: cn-north-4a
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: CCEManagedControlPlane
metadata:
  name: my-cce-cluster-control-plane
  namespace: default
  labels:
    cluster.x-k8s.io/cluster-name: my-cce-cluster
spec:
  clusterName: my-cce-cluster
  category: CCE # 默认即 CCE，可省略
  version: "v1.35.0" # webhook 要求完整 semver；provider 内部去 patch 调 CCE API
  flavor: cce.s1.small
  containerNetwork:
    mode: vpc-router # Standard（区别于 Turbo 的 eni）
    cidr: 10.245.0.0/16
  serviceNetwork:
    cidr: 10.248.0.0/16
  endpointAccess:
    public: false
  billing:
    mode: 0
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: MachinePool
metadata:
  name: my-cce-cluster-pool-0
  namespace: default
spec:
  clusterName: my-cce-cluster
  replicas: 1
  template:
    spec:
      clusterName: my-cce-cluster
      version: v1.35.0 # CAPI 要求完整 semver
      bootstrap:
        dataSecretName: my-cce-cluster-bootstrap
      infrastructureRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
        kind: CCEManagedMachinePool
        name: my-cce-cluster-pool-0
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: CCEManagedMachinePool
metadata:
  name: my-cce-cluster-pool-0
  namespace: default
  labels:
    cluster.x-k8s.io/cluster-name: my-cce-cluster
spec:
  clusterName: my-cce-cluster
  nodePoolName: pool-0
  flavor: c6.large.2 # Standard 任意通用型即可
  os: Huawei Cloud EulerOS 2.0
  rootVolume:
    size: 40
    type: GPSSD
  dataVolumes:
    - size: 100
      type: GPSSD
  sshKey: VERIFY-KEYPAIR-NAME
  availabilityZone: cn-north-4a
  replicas: 1
  billingMode: 0
EOF

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

## 阶段四：CCE Turbo 变体（eni 网络模式）

> 完整端到端流程（预置基础设施 + 跳板机部署 + provider 安装）与阶段一/二**完全相同**。本阶段只需：
> ① 确认 ENI 子网已建；② 用 Turbo 配置生成集群 B；③ 验证 + 删除。

### 概述：Turbo 与 Standard 的差异

| 维度 | Standard（vpc-router） | Turbo（eni） |
|---|---|---|
| 网络模式 | `containerNetwork.mode: vpc-router`（overlay_l2） | `containerNetwork.mode: eni` |
| 容器网段 | 独立 CIDR（如 10.245.0.0/16） | 走 ENI 子网（无需容器 CIDR） |
| 集群类别 | `category: CCE`（默认） | `category: Turbo`（eni 模式自动推断） |
| 必备网络 | 节点子网 | 节点子网 + **ENI 子网**（`type: eni`） |
| 节点 flavor | 任意通用型（c6.large.2） | **sub-ENI 配额 > 0**（如 c7.large.2；c6sne 已废弃） |
| 必填 spec | — | `containerNetwork.eniSubnets`（webhook 强制） |

### 步骤 12：确认 ENI 子网（阶段一已自动创建）

阶段一 `hack/deploy-network` 会额外创建 ENI 子网 `capi-subnet-eni`（10.0.2.0/24）并输出 `CCE_DEPLOY_ENI_SUBNET`。

> ⚠️ **踩坑 #21（neutron_subnet_id）**：CCE `eniNetwork.subnets[].subnetID` 要求 **neutron_subnet_id**（不是 VPC 网络的 subnet ID）。
> deploy-network 刚建子网时 neutron ID 可能尚未同步（输出为空），稍后用 `hack/survey-hw` 或 VPC 控制台重新查询 `capi-subnet-eni` 的 `neutron_subnet_id` 即可。

### 步骤 13：生成 Turbo 集群 B 配置

Turbo 与 Standard 的差异集中在 `CCEManagedControlPlane`（`mode: eni` + `eniSubnets`）、`CCECluster`（ENI 子网）和节点 flavor。完整模板（替换 VERIFY-* 占位符）：

```yaml
# my-cluster-turbo.yaml —— Turbo 完整配置
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: my-cce-cluster
  namespace: default
spec:
  clusterNetwork:
    pods:
      cidrBlocks: ["10.245.0.0/16"] # 需与同 VPC 其他集群不冲突
    services:
      cidrBlocks: ["10.248.0.0/16"]
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: CCECluster
    name: my-cce-cluster
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1beta2
    kind: CCEManagedControlPlane
    name: my-cce-cluster-control-plane
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: CCECluster
metadata:
  name: my-cce-cluster
  namespace: default
  labels:
    cluster.x-k8s.io/cluster-name: my-cce-cluster
spec:
  region: cn-north-4
  network:
    vpc:
      id: VERIFY-VPC-ID
    subnets:
      - id: VERIFY-SUBNET-ID # 节点子网
        availabilityZone: cn-north-4a
      - id: VERIFY-ENI-SUBNET-ID # Turbo ENI 容器子网
        type: eni
        neutronSubnetId: VERIFY-ENI-NEUTRON-ID
---
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: CCEManagedControlPlane
metadata:
  name: my-cce-cluster-control-plane
  namespace: default
  labels:
    cluster.x-k8s.io/cluster-name: my-cce-cluster
spec:
  clusterName: my-cce-cluster
  category: Turbo # eni 模式 provider 也会自动推断
  version: "v1.35.0" # webhook 要求完整 semver；provider 内部去 patch
  flavor: cce.s1.small
  containerNetwork:
    mode: eni # Turbo 关键
    eniSubnets:
      - VERIFY-ENI-NEUTRON-ID # webhook 强制；eniNetwork API 用 neutron ID
  serviceNetwork:
    cidr: 10.248.0.0/16
  endpointAccess:
    public: false
  billing:
    mode: 0
---
apiVersion: cluster.x-k8s.io/v1beta1
kind: MachinePool
metadata:
  name: my-cce-cluster-pool-0
  namespace: default
spec:
  clusterName: my-cce-cluster
  replicas: 1
  template:
    spec:
      clusterName: my-cce-cluster
      version: v1.35.0
      bootstrap:
        dataSecretName: my-cce-cluster-bootstrap
      infrastructureRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
        kind: CCEManagedMachinePool
        name: my-cce-cluster-pool-0
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: CCEManagedMachinePool
metadata:
  name: my-cce-cluster-pool-0
  namespace: default
  labels:
    cluster.x-k8s.io/cluster-name: my-cce-cluster
spec:
  clusterName: my-cce-cluster
  nodePoolName: pool-0
  flavor: c7.large.2 # Turbo 需 sub-ENI 配额>0（c6sne 系列已废弃）
  os: Huawei Cloud EulerOS 2.0
  rootVolume:
    size: 40
    type: GPSSD
  dataVolumes:
    - size: 100
      type: GPSSD
  sshKey: VERIFY-KEYPAIR-NAME
  availabilityZone: cn-north-4a
  replicas: 1
  billingMode: 0
```

### 步骤 14：创建并验证集群 B（Turbo）

```bash
# 跳板机：创建 credentials + bootstrap Secret（同步骤 9，已存在则跳过）
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default --from-literal=accessKey='<AK>' --from-literal=secretKey='<SK>'
kubectl create secret generic my-cce-cluster-bootstrap \
  --namespace default --from-literal=value=""

# 提交 Turbo 声明
kubectl apply -f my-cluster-turbo.yaml

# 观察（预期：CCECluster NetworkReady=True → 控制面 Ready → 节点池 Ready → Cluster Provisioned）
kubectl get ccecluster -w
kubectl get ccemanagedcontrolplane my-cce-cluster-control-plane -w
kubectl get ccemanagedmachinepool my-cce-cluster-pool-0 -w
kubectl get cluster my-cce-cluster # PHASE=Provisioned

# 验证节点
clusterctl get kubeconfig my-cce-cluster > my-cce-cluster.kubeconfig
kubectl --kubeconfig=my-cce-cluster.kubeconfig get nodes # 1 个 Ready 节点
```

> ⚠️ **踩坑 #22（validator ENI 子网）**：若 CCECluster 报 `NetworkValidationFailed: eni subnet <id> not found in the VPC (CCE.01400002)`，
> 是 validator 未索引 neutron ID 的旧版 bug —— 升级到含 `620fd4e` 修复的 provider 镜像即可。

### 步骤 15：删除集群 B（Turbo）

```bash
# 跳板机
kubectl delete cluster my-cce-cluster
kubectl get cluster,ccecluster,ccemanagedcontrolplane,machinepool,ccemanagedmachinepool -n default # 全部消失
```

> 删除链自动完成（节点池 → CCE 集群 → 控制面 → 集群）。provider 含 `683c99d`（annotation 轮询兜底）时无需任何手动干预；
> 旧版本若停在 "Node pool deletion requested, waiting"，touch 一次 `kubectl annotate ccemanagedmachinepool my-cce-cluster-pool-0 x=1 --overwrite` 即可继续。

### Turbo 特有参数速查

| 参数 | 取值 | 说明 |
|---|---|---|
| `containerNetwork.mode` | `eni` | Turbo 关键（区别于 vpc-router） |
| `containerNetwork.eniSubnets` | neutron_subnet_id 数组 | webhook 强制；eniNetwork API 用 neutron ID |
| `category` | `Turbo`（或留空） | eni 模式自动推断为 Turbo |
| 节点 flavor | sub-ENI 配额 > 0 | 如 c7.large.2；c6sne 系列已废弃 |

## 清理

```bash
# 跳板机：删集群 B
kubectl delete cluster my-cce-cluster

# 本地：删管理集群 A
nocloud go run ./hack/deploy-mgmt-cluster -delete -cluster '<MGMT_CLUSTER_ID>'

# 本地：清理跳板机 ECS + EIP + 密钥对 + 安全组 + 子网 + VPC（见踩坑 #9 #11）
```

---

## 踩坑记录（测试问题 + 修正）

> **活文档核心**：后续测试遇到问题，按序追加记录（现象/根因/修正/状态），并在上方对应步骤同步修正。

| # | 问题现象 | 根因 | 修正 | 状态 |
|---|---|---|---|---|
| 1 | 节点永久卡 `Installing`（kubelet inactive） | 子网未指定 DNS，节点拿到错误 DNS（`10.0.x.254`），无法解析 OBS 域名，cce-agent 下载失败 | 建子网显式指定 `primary_dns=100.125.1.250` + `secondary_dns=100.125.129.250` | ✅ 已修复 |
| 2 | 节点异常无法 SSH 排查 | `deploy-network` 创建密钥对时丢弃私钥 | 节点改用 `capi-bastion-key`（`deploy-bastion` 保留私钥） | ✅ 已修复 |
| 3 | 管理集群 `publicAccess=true` 无公网 endpoint | CCE 不自动分配公网 IP | 跳板机（同 VPC）访问内网 endpoint | ✅ 已记录 |
| 4 | clusterctl 版本偏旧（v1.13.4） | brew 版本落后 | 手动下载 clusterctl v1.14.0 | ✅ 已修复 |
| 5 | provider 镜像在 x86_64 节点无法运行 | Docker Desktop 默认构建 arm64 | `docker build --platform linux/amd64` | ✅ 已修复 |
| 6 | docker push 报 `Invalid image, fail to parse 'manifest.json'` | BuildKit attestation，SWR 不支持 | `--provenance=false --sbom=false` | ✅ 已修复 |
| 7 | `docker pull --platform linux/amd64` 拉到 arm64 | Docker Desktop 对 multi-arch 的 pull --platform 不生效 | `docker buildx imagetools create --platform linux/amd64` | ✅ 已修复 |
| 8 | NAT 网关创建失败 `CBC.30060005` | 余额不足 | 零公网方案不需要 NAT；充值余额 | ✅ 已记录 |
| 9 | Installing 节点无法删除（`CCE_CM.0002`/`CCE.01403006`/`CCE.01400024` 死锁） | CCE 限制 | ECS 层强删节点 → 等 CCE 检测 → 删集群 | ✅ 已记录 |
| 10 | 连续 429 限流（`APIGW.0308`） | CCE 写限流 10 次/分钟，429 重试也计数；部署脚本写操作集中（一次部署 ~10 次写）且无退避 | ① hack 脚本（deploy-network/bastion/mgmt-cluster）已内置 429 退避（等窗口清零再重试）；② 脚本间建议间隔 ≥60s；③ 密集 429 后停止写操作 1-10 分钟 | ✅ 已修复（b3bfb4b） |
| 11 | 删 VPC 报 `vpc contain peering` / `exroutes exists` | 遗留 VPC 对等连接 + 路由 | 先删 peering → 清空路由表 peering 路由 → 删 VPC | ✅ 已记录 |
| 12 | ECS 创建报 `Ecs.0314 keypair does not match user_id` | 云上存在其他用户同名密钥对 | 删本地私钥文件强制新建密钥对 | ✅ 已记录 |
| 13 | CreateCluster 报 `CCE_CM.0402 Version is not support, Version format error` | CCE CreateCluster API 只接受 `v1.35`（major.minor），而 webhook 要求完整 semver `v1.35.0` | cce.go 加 `cceClusterVersion` 去 patch（v1.35.0→v1.35） | ✅ 已修复 |
| 14 | 节点池永久 `WaitingForControlPlane`，控制面 `status.ready` 始终为空 | spec.version（v1.35.0）与实际集群版本（v1.35.5，CCE 自动选最新 patch）精确比较不相等 → 误触发 upgrade → 无 target 后 return → 永远走不到 `Status.Ready=true` | controller 加 `sameMajorMinor` 比较 major.minor（忽略 patch） | ✅ 已修复 |
| 15 | CreateCluster 报 `CCE_CM.0410 Container network CIDR conflict, 10.244.0.0/16 conflict with vpc route dest addr 10.244.1.0/24` | 同 VPC 内各集群容器网段必须唯一，管理集群 A 已占 10.244.0.0/16 | 集群 B 容器/服务网段改为 10.245.0.0/16、10.248.0.0/16 | ✅ 已修复 |
| 16 | 改 `spec.containerNetwork.cidr` 报 `field is immutable after creation` | CCEManagedControlPlane 的 containerNetwork.cidr 创建后不可变 | 删除 Cluster 重建（改网段前先删） | ✅ 已记录 |
| 17 | `rollout restart` 后 pod 仍拉旧镜像 | CCE 节点 containerd 缓存 `latest` tag（imagePullPolicy=IfNotPresent） | deployment 设 `imagePullPolicy: Always` 或推唯一 tag | ✅ 已修复 |
| 18 | 删除失败集群卡 Deleting（finalizer 未移除） | 集群未创建成功（ClusterID 空）时删除，controller 未触发删除 reconcile，finalizer 阻塞 | 手动 `kubectl patch ... --type=json -p '[{"op":"remove","path":"/metadata/finalizers"}]'` | ✅ 已记录 |
| 19 | 删除成功集群时 `MachinePool` 与 `CCEManagedControlPlane` 的 finalizer 卡住，删除链停滞 | ① CAPI MachinePool 控制器删除 CCEManagedMachinePool 后返回空 Result（不 requeue），依赖 watch 事件重触发但未触发；② provider `CCEManagedControlPlane`/`CCEManagedMachinePool` 的 `reconcileDelete` 分支在 scope 构建之前，`RemoveFinalizer` 后无 `Client.Update`/patch 持久化 → finalizer 移除永远写不回 API server | `reconcileDelete` 里 `RemoveFinalizer` 后显式 `Client.Update`；本次 E2E 临时手动移除 finalizer 兜底 | ✅ 已修复（950d550） |
| 20 | Turbo 节点池创建报 `flavor status is abandon`（c6sne.large.2 in cn-north-4a） | 该 flavor 在可用区已废弃，但 sub-ENI 配额 > 0，deploy-network 会误选 | 换活跃的 sub-ENI 配额 > 0 flavor（如 c7.large.2）；deploy-network 已优先活跃 flavor（03c9b9e） | ✅ 已修复 |
| 21 | Turbo 创建报 `NetworkValidationFailed: eni subnet <id> not found in the VPC (CCE.01400002)` | validator `fetchNetwork` 只用普通 subnet ID 索引，而 `eniSubnets` 用 neutron ID 查询 → 误报；另：deploy-network 刚建 ENI 子网时 neutron ID 可能未同步 | validator 同时索引普通 ID 与 neutron ID（620fd4e）；neutron ID 未同步时稍后重查 | ✅ 已修复 |
| 22 | 删除集群时 `CCEManagedMachinePool` 停在 "Node pool deletion requested, waiting"，需 touch 才继续 | reconcileDelete 的 `RequeueAfter` 被 workqueue dedup 吞掉（对象 terminating 时），删节点池轮询停止 | reconcileDelete 删节点池时 bump annotation 触发 watch 兜底（683c99d） | ✅ 已修复 |

---
## 凭证轮换

### AK/SK 轮换（静态凭证）

更新 `<clusterName>-credentials` Secret 的 `accessKey` / `secretKey` 即可，**无需重启 provider**：provider 每次 reconcile 读取最新值，且 controller watch 该 Secret，更新后自动重新 reconcile。

```bash
# 跳板机：原地更新 credentials Secret（新 AK/SK）
kubectl create secret generic my-cce-cluster-credentials \
  --namespace default \
  --from-literal=accessKey='<新AK>' --from-literal=secretKey='<新SK>' \
  --dry-run=client -o yaml | kubectl apply -f -

# 验证：观察 provider 用新凭证继续正常（日志无鉴权错误）
kubectl -n cce-provider-system logs deploy/cce-provider-controller-manager --tail=20
```

### IAM 委托（agency）凭证

当 `CCEClusterIdentity` 引用委托（agency）时，provider 通过 **STS `AssumeAgency`** 获取临时凭证，**每次 reconcile 自动刷新**（对标 CAPA 的 EC2 IAM 角色），无过期运维负担；仅需确保委托的信任策略与权限覆盖 CCE 操作。

> 本项目静态 AK/SK 与 agency 委托两条路径均已支持：默认用 `<clusterName>-credentials` Secret；`spec.identityRef` 指向 `CCEClusterIdentity` 时走委托/STS。

---


## 环境变量速查

| 变量 | 用途 | 示例值 |
|---|---|---|
| `CLOUD_SDK_AK` / `CLOUD_SDK_SK` | 华为云 AK/SK | `<你的AK>` / `<你的SK>` |
| `CCE_DEPLOY_REGION` | 区域 | `cn-north-4` |
| `CCE_DEPLOY_AZ` | 可用区 | `cn-north-4a` |
| `CCE_DEPLOY_VPC` | VPC ID | `62737a53-...` |
| `CCE_DEPLOY_SUBNET` | 节点子网 ID | `c9b7bf51-...` |
| `CCE_DEPLOY_KEYPAIR` | 密钥对名（节点用） | `capi-bastion-key` |
| `CCE_DEPLOY_K8S_VERSION` | 集群 K8s 版本 | `v1.35` |
| `SWR_ORG` | SWR 命名空间 | `capi_cce` |

## 工具速查

| 工具 | 位置 | 用途 |
|---|---|---|
| `hack/deploy-network` | 建 VPC/子网/密钥对（**已含 DNS 修复**） | 阶段一步骤 1 |
| `hack/deploy-bastion` | 建跳板机 ECS（保留私钥） | 阶段一步骤 2 |
| `hack/deploy-mgmt-cluster` | 创建/列出/删除管理集群 | 阶段一步骤 3 |
| `hack/swr-login` | 生成 SWR 临时登录 token | 阶段一步骤 4 |
| `hack/survey-hw` | 盘点所有华为云资源 | 清理后验证 |

**工具分类**：
- **部署工具**（`hack/deploy-*`）：正式 E2E 部署流程（阶段一）使用——`deploy-network`（VPC/子网/密钥对）、`deploy-bastion`（跳板机）、`deploy-mgmt-cluster`（管理集群）；配套中性工具 `hack/swr-login`、`hack/survey-hw`、`hack/cleanup-hw`。
- **冒烟测试工具**：`scripts/smoke-cce.sh` + `hack/cleanup-smoke-clusters` + `hack/check-smoke-env`，项目冒烟测试专用，与正式部署流程相互独立。

### 相关代码位置（部署流程 → 代码文件）

| 部署环节 | 代码/清单 | 说明 |
|---|---|---|
| 阶段一步骤 1 网络/密钥 | `hack/deploy-network/main.go` | VPC/子网/密钥对创建（含 DNS 修复） |
| 阶段一步骤 2 跳板机 | `hack/deploy-bastion/main.go` | 跳板机 ECS + EIP + 安全组 |
| 阶段一步骤 3 管理集群 A | `hack/deploy-mgmt-cluster/main.go` | 管理集群创建/删除（复用 `internal/services/cce`） |
| 阶段一步骤 4 镜像 | `hack/swr-login/main.go` + `Dockerfile` | SWR 登录 token + 镜像构建 |
| Provider 本体 | `cmd/main.go` | manager 启动、feature gates、controller 注册 |
| CRD 类型 | `api/controlplane/v1beta2`、`api/infrastructure/v1beta2` | CCEManagedControlPlane / CCECluster / CCEManagedMachinePool |
| 控制器 | `controllers/ccemanagedcontrolplane_controller.go` 等 | 控制面/集群/节点池 reconcile 循环 |
| 华为云服务层 | `internal/services/cce`、`internal/services/network` | CCE 集群/节点池 API 封装、网络校验 |
| 集群 B 模板 | `config/samples/cluster-template.yaml` | Standard/Turbo 参数注释（本指南步骤 9 / 阶段四为完整版） |
| 部署清单 | `config/default`（kustomize） | 附录 A 生成 infrastructure-components.yaml 的源 |

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

kubectl kustomize "$ARTIFACTS" > "$ARTIFACTS/infrastructure-components.yaml"
```

> **证书说明**：webhook 证书现由 **cert-manager 自动签发**（`config/certmanager` 的 Issuer + Certificate 已随组件部署，webhook 配置带 `cert-manager.io/inject-ca-from` 注解注入 caBundle）。**无需**再手动 `openssl` 生成自签证书 / 注入 caBundle（旧流程已移除）。

## 附录 B：修复 Provider pod（跳板机）

webhook 证书已由 cert-manager 自动签发，**无需手动创建**；仅需处理 Provider 镜像（SWR 私有仓库需 imagePullSecret；public SWR 免认证跳过）：

```bash
  # SWR 私有仓库 imagePullSecret（方式 A；public SWR 方式 B 跳过此步）
  kubectl create secret docker-registry cce-provider-swr-secret \
  --namespace cce-provider-system \
  --docker-server=swr.cn-north-4.myhuaweicloud.com \
  --docker-username='<SWR_USER>' --docker-password='<SWR_PASSWORD>' --docker-email='noreply@huawei.cloud'

  # 给 provider Deployment 加 imagePullSecrets 并重启（方式 B 仅重启）
kubectl -n cce-provider-system patch deployment cce-provider-controller-manager \
  --type=json -p='[{"op":"add","path":"/spec/template/spec/imagePullSecrets","value":[{"name":"cce-provider-swr-secret"}]}]'
kubectl -n cce-provider-system rollout restart deployment/cce-provider-controller-manager
```

> **webhook 证书**：`webhook-service-cert` Secret 由 cert-manager 的 Certificate 自动创建并轮换（webhook 配置 caBundle 经 `inject-ca-from` 注入），不再手动 `openssl`/`create secret tls`。
