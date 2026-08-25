# CCE Provider 与 CAPA EKS 托管模式部署流程对比

> 对比对象：本项目 CCE 部署流程（`docs/e2e-deployment-guide.md`，四阶段）与 **CAPA（Cluster API Provider AWS）EKS 托管模式**部署流程（EKS 管理集群 A + EC2 跳板机 + EKS 工作负载集群 B）。
>
> - 更新日期：2026-08-25（反映 4 项增强落地：generate/flavor、凭证轮换、多 AZ、跳板机 IAM 委托）
> - 对比范围：**仅关注 EKS/CCE 托管模式（managed control plane + managed node pool）**，不涉及自管（self-managed）节点。
> - 参考：CAPA EKS 托管方案（"控制台创建基础设施 + EC2 内完成所有运维操作"版本）；本项目 `docs/e2e-deployment-guide.md`。

---

## 0. 差异状态总表（先看结论）

| 差异项 | 性质 | 当前状态 |
|---|---|---|
| 集群 A 公网端点 | 配置差异 | ✅ **已抹平**（`deploy-mgmt-cluster` 自动绑 EIP，默认公网+私有，可白名单/可关） |
| 集群 B 生成方式 | 能力差异 | ✅ **已抹平**（`clusterctl generate cluster` + Standard/Turbo flavor） |
| 凭证机制 | 架构差异 | ✅ **基本抹平**（agency/STS 临时凭证 + 跳板机 IAM 委托 + Secret 轮换自动化） |
| 基础设施创建 | 理念差异 | ✅ **CCE 优势**（幂等 CLI 脚本 + 可选控制台人工；CAPA 方案仅人工） |
| 镜像拉取（零公网） | 配置差异 | ✅ **CCE 优势**（SWR/OBS 内网零公网；CAPA 需配 ECR Endpoint 才对等） |
| 集群 B 网络模型 | **平台差异** | ⚠️ **无法抹平**（CCE Standard 需容器 CIDR、Turbo 需 ENI 子网；归因华为云 CCE 平台设计，非 provider 能力） |

---

## 1. 总体流程对比

| 环节 | CCE（本方案） | CAPA EKS 托管 | 差异要点 |
|---|---|---|---|
| 基础设施创建 | **CLI 脚本**（`hack/deploy-*`，幂等、按名复用）**或控制台人工**（文档可选变体） | AWS 控制台（EKS + EC2 + IAM 角色，人工点选） | **CCE 优势**：脚本自动化 + 可选人工，超集 |
| 管理集群 A | CLI 创建 CCE Standard（托管控制面 + 节点池，**自动绑公网 EIP**） | 控制台创建 EKS（托管控制面 + 托管节点组，默认公网+私有） | 公网端点已对齐（`CCE_DEPLOY_PUBLIC`） |
| 跳板机 | ECS `s6.small.1` + SSH 密钥对 + **可选 IAM 委托**（`CCE_DEPLOY_BASTION_AGENCY`） | EC2 `t3.micro` + IAM 角色 | 委托可选，能力对齐（见 §3.1） |
| 工具安装 | kubectl + clusterctl（跳板机） | kubectl + clusterctl + **clusterawsadm** + awscli | CAPA 多 clusterawsadm（IAM 引导） |
| Provider 部署 | `clusterctl init --infrastructure cce`（SWR 内网镜像 + imagePullSecret） | `clusterawsadm bootstrap iam` → `clusterctl init --infrastructure aws` | CAPA 先建 IAM 堆栈 |
| 集群 B 创建 | **`clusterctl generate cluster`**（Standard/Turbo flavor）或手写模板 | `clusterctl generate cluster --flavor eks-managedmachinepool` | **已抹平**（generate/flavor 已补） |
| 集群 B 凭证 | `credentials` Secret（AK/SK）或 agency/STS | `AWS_B64ENCODED_CREDENTIALS`（临时凭证） | 两条路径均支持（见 §3.1） |
| 网络 | VPC + 节点子网 + **容器 CIDR（Standard）/ ENI 子网（Turbo）** | VPC（3 AZ，CNI 直接用 VPC 子网） | **平台差异**（见 §3.4） |
| 镜像拉取 | **零公网**（SWR 内网 + OBS 内网） | EC2 公网拉取（或 ECR/VPC Endpoint） | **CCE 优势**：默认内网 |
| 清理 | 删集群 B → 删管理集群 A（脚本）→ 删 ECS/EIP/VPC | 删集群 B → 删 EKS/EC2（控制台）→ 删 IAM 堆栈 | CAPA 多一步删 IAM 堆栈 |

## 2. 阶段对照

| 阶段 | CCE | CAPA EKS |
|---|---|---|
| 一、基础设施 | 本地 CLI：`deploy-network`（VPC/双子网/密钥对）→ `deploy-bastion`（ECS+EIP+可选委托）→ `deploy-mgmt-cluster`（集群 A + 自动绑公网 EIP + 多 AZ 可选）→ 镜像搬运 SWR | 控制台：创建 EKS 集群 A + 托管节点组 → 创建 EC2 跳板机（IAM 角色） |
| 二、跳板机准备 | SSH 登录 → 装 kubectl/clusterctl → `kubectl` 连集群 A | SSH 登录 → 装 kubectl/clusterctl/clusterawsadm → `aws eks update-kubeconfig` |
| 三、权限引导 | （AK/SK 直接可用；可选 IAM agency 委托 / STS 临时凭证） | `clusterawsadm bootstrap iam` + `encode-as-profile` 编码临时凭证 |
| 四、部署 Provider | `clusterctl init --infrastructure cce` + 修复 pod（imagePullSecret/webhook cert） | `clusterctl init --infrastructure aws` |
| 五、集群 B | `clusterctl generate cluster`（或手写）→ apply | `clusterctl generate cluster --flavor eks-managedmachinepool` → apply |
| 六、验证 | `kubectl get cluster` / `clusterctl get kubeconfig` / `kubectl get nodes` | 相同 |
| 七、清理 | `kubectl delete cluster` → 删集群 A（脚本）→ 删 ECS/EIP/VPC | `kubectl delete cluster` → 删 EKS/EC2（控制台）→ 删 IAM 堆栈 |

## 3. 关键差异分析

### 3.1 凭证机制（架构差异，已基本抹平）

| 维度 | CAPA EKS | CCE | 状态 |
|---|---|---|---|
| 凭证类型 | 临时凭证（EC2 IAM 角色 → STS） | **静态 AK/SK（默认）** 或 **agency 委托 → STS 临时凭证** | ✅ 两条路径 |
| 跳板机认证 | IAM 角色（免静态密钥） | SSH 密钥对（默认）或 **IAM 委托**（`CCE_DEPLOY_BASTION_AGENCY`） | ✅ 已补委托 |
| 轮换 | `encode-as-profile` 刷新 + 重启 Pod | **更新 Secret 自动生效**（controller watch credentials Secret）+ agency/STS 每次 reconcile 自动刷新 | ✅ 已实现 |
| 优/代价 | 免硬编码密钥，但有过期运维负担 | AK/SK 简单需管理；agency 路径对标 CAPA 体验 | 能力对齐 |

> CCE 已实现 agency/STS（`CCEClusterIdentity` + `AssumeAgency`）、跳板机 IAM 委托、凭证轮换自动化（`433ba95`），**能力上与 CAPA 的 IAM 角色模式对齐**；E2E 演示仍以 AK/SK 为主。

### 3.2 基础设施创建（CCE 优势）

- **CCE 方案**：`hack/deploy-*` **幂等脚本**（按名查找、存在即复用）——可重复执行、可进 CI/GitOps；文档亦提供**控制台人工变体**（步骤 2/3 控制台操作说明），两种方式均可。
- **CAPA 方案（该版本）**：仅控制台人工点选（不可脚本化、不可复现）。

> **结论：CCE 为超集（自动化 + 可选人工），此项为 CCE 优势**，非差距。（CAPA 生态本身亦有 Terraform/eksctl 等自动化工具，仅该方案选择控制台。）

### 3.3 集群 B 生成方式（已抹平）

- **CAPA**：`clusterctl generate cluster --flavor eks-managedmachinepool`（模板 + flavor）。
- **CCE**：**已补 `clusterctl generate cluster`**（`9abcc86`）——`config/samples/cluster-template-clusterctl.yaml`（Standard 默认）+ `cluster-template-turbo.yaml`（Turbo flavor），装到 overrides 目录即可；手写模板（`cluster-template.yaml`）保留。

> **结论：已抹平。** 用法与 CAPA 一致：`clusterctl generate cluster <name> [--flavor turbo] --kubernetes-version v1.35.0`。

### 3.4 网络模型（平台差异，无法抹平）

| | CAPA EKS | CCE Standard (vpc-router) | CCE Turbo (eni) |
|---|---|---|---|
| 节点网络 | VPC 子网 | VPC 子网（hostNetwork） | VPC 子网 |
| Pod 网络 | **直接用 VPC 子网 IP** | **独立容器 CIDR**（10.244/10.245 等） | **ENI 子网 IP** |
| 用户额外规划 | 无（只需 VPC/子网） | 容器网段 + 服务网段 | ENI 子网 |
| 与 VPC CNI 语义接近度 | 基准 | 低（多容器网段 + VPC 路由） | 高（Pod 直接用 IP，但仍需 ENI 子网） |

> **结论：两种模式都比 EKS 多一个"容器网络规划"维度（Standard=容器 CIDR，Turbo=ENI 子网），差异源于华为云 CCE 平台产品设计**，provider 已完整适配（`vpc-router`/`eni` 映射、ENI 子网 validator、Turbo flavor），但规划维度无法通过 provider 消除。Turbo（ENI）在"Pod 直接用 IP"语义上最接近 EKS VPC CNI。

### 3.5 零公网（CCE 优势）

- **CCE**：镜像走 **SWR 内网**、节点组件走 **OBS 内网**，全程不开放公网出站——适合私有化/内网/等保环境。
- **CAPA**：EC2 需公网（SSH + 拉镜像），或额外配置 VPC Endpoint / ECR。

> "零公网"是 CCE 在内网部署场景的明确优势；CAPA 需配 ECR/S3 VPC Endpoint 才对等。

## 4. 命令速查对照（跳板机）

| 操作 | CAPA EKS | CCE |
|---|---|---|
| 连管理集群 A | `aws eks update-kubeconfig --region <r> --name <a>` | `export KUBECONFIG=/root/capi-mgmt.kubeconfig`（公网 EIP 已自动绑定） |
| IAM/权限引导 | `clusterawsadm bootstrap iam create-cloudformation-stack` | （AK/SK 直接可用；可选 agency/委托） |
| 编码凭证 | `export AWS_B64ENCODED_CREDENTIALS=$(clusterawsadm ... encode-as-profile)` | `kubectl create secret generic my-cce-cluster-credentials ...`（轮换：更新 Secret 自动生效） |
| 初始化 | `clusterctl init --infrastructure aws` | `clusterctl init --infrastructure cce` |
| 生成集群 B | `clusterctl generate cluster my-c --flavor eks-managedmachinepool ...` | `clusterctl generate cluster my-c [--flavor turbo] --kubernetes-version v1.35.0` |
| 监控 | `kubectl get cluster -w` | 相同 |
| kubeconfig | `clusterctl get kubeconfig my-c` | 相同 |
| 验证节点 | `kubectl --kubeconfig=... get nodes` | 相同 |
| 删集群 B | `kubectl delete cluster my-c` | 相同 |

## 5. 对 CCE 方案的可改进点（已实施 4/4）

| 改进点（对标 CAPA） | 状态 | Commit |
|---|---|---|
| `clusterctl generate cluster` + flavor | ✅ 已实施 | `9abcc86` |
| 凭证轮换自动化 | ✅ 已实施（watch Secret 自动 reconcile + agency/STS 自动刷新） | `433ba95` |
| 管理集群 A 多 AZ | ✅ 已实施（`CCE_DEPLOY_MGMT_AZS` 扩展组） | `5795b0d` |
| 跳板机 IAM 委托对齐 | ✅ 已实施（`CCE_DEPLOY_BASTION_AGENCY`） | `b80cf98` |

> 另有：集群 A 公网端点对齐（自动绑 EIP，`4a7c80f`）；控制台创建变体文档（`1b969ac`）。

### 待规划：Provider 正式发布态改造（对标 CAPA clusterctl init 一步完成）

**现状**：当前为开发态部署——组件走本地 file://、Provider 镜像私有 SWR、webhook 静态证书（预创建 Secret + 注入 caBundle），需额外步骤（SWR Secret + webhook cert）。

**目标**：clusterctl init --infrastructure cce 对标 CAPA 一步完成（公网组件 + 公网镜像 + webhook 自动），需 3 项：

1. **组件发布公网**：infrastructure-components.yaml + metadata.yaml 发布到 GitHub Release（clusterctl 从 release 拉取，取代本地 file://）。
2. **镜像发布公网**：Provider 镜像推 GHCR（public），infrastructure-components.yaml 镜像地址从 SWR 改为 GHCR，节点公网直拉，省 SWR imagePullSecret。
3. **webhook 改 cert-manager 自动签发**：config/ 加 cert-manager 配置（config/certmanager + deployment 注入注解 cert-manager.io/inject-ca-from），去掉手动 webhook-service-cert + caBundle。

**说明**：无需改 Go 业务代码（控制器/服务逻辑与镜像/证书部署形态无关），仅改发布配置（config/ 清单 + 镜像地址）+ 发布动作（GitHub Release + GHCR）。

**验收标准**：全新环境 clusterctl init --infrastructure cce 一步完成，无需 SWR Secret / 手动 webhook cert。

---

## 附录：本文档依据

- 本项目部署流程：`docs/e2e-deployment-guide.md`（阶段一~四，含 Standard/Turbo、控制台变体）
- CAPA EKS 托管方案：用户提供的"控制台创建基础设施 + EC2 内完成所有运维操作"版本
- 本项目 IAM 委托支持：`docs/handoff-p1-3-iam-agency.md`、`internal/services/iam`
