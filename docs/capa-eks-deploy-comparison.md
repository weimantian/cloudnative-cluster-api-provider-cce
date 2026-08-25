# CCE Provider 与 CAPA EKS 托管模式部署流程对比

> 对比对象：本项目 CCE 部署流程（`docs/e2e-deployment-guide.md`，三/四阶段）与 **CAPA（Cluster API Provider AWS）EKS 托管模式**部署流程（EKS 管理集群 A + EC2 跳板机 + EKS 工作负载集群 B）。
>
> - 撰写日期：2026-08-25
> - 对比范围：**仅关注 EKS/CCE 托管模式（managed control plane + managed node pool）**，不涉及自管（self-managed）节点。
> - 参考：CAPA EKS 托管方案（"控制台创建基础设施 + EC2 内完成所有运维操作"版本）；本项目 `docs/e2e-deployment-guide.md`。

---

## 1. 总体流程对比

| 环节 | CCE（本方案） | CAPA EKS 托管 | 差异要点 |
|---|---|---|---|
| 基础设施创建 | **CLI 脚本**（`hack/deploy-*`，幂等、按名复用） | **AWS 控制台**（EKS 集群 A + EC2 + IAM 角色，人工点选） | CCE 可脚本化/CI 化；CAPA 方案控制台人工 |
| 管理集群 A | CLI 创建 CCE Standard（托管控制面 + 节点池） | 控制台创建 EKS（托管控制面 + 托管节点组） | 同为托管 K8s，创建方式不同 |
| 跳板机 | ECS `s6.small.1` + **SSH 密钥对** | EC2 `t3.micro` + **IAM 角色** | 认证方式核心差异（见 §3） |
| 工具安装 | kubectl + clusterctl（跳板机） | kubectl + clusterctl + **clusterawsadm** + awscli | CAPA 多 clusterawsadm（IAM 引导） |
| Provider 部署 | `clusterctl init --infrastructure cce`（SWR 内网镜像 + imagePullSecret） | `clusterawsadm bootstrap iam create-cloudformation-stack` → `clusterctl init --infrastructure aws` | CAPA 先建 IAM 堆栈 |
| 集群 B 创建 | 手写/模板 `my-cluster.yaml`（5 个对象） | `clusterctl generate cluster --flavor eks-managedmachinepool` | CAPA 自动生成 YAML（flavor 机制） |
| 集群 B 凭证 | `credentials` Secret（AK/SK，长期） | `AWS_B64ENCODED_CREDENTIALS`（临时凭证，约 1 小时） | 长期 vs 临时（见 §3） |
| 网络 | VPC + 节点子网 + **ENI 子网**（Turbo） | VPC（3 AZ，CNI 直接用 VPC 子网） | CCE Turbo 需独立 ENI 子网 |
| 镜像拉取 | **零公网**（SWR 内网 + OBS 内网） | EC2 公网拉取（或 ECR/VPC Endpoint） | CCE 适合内网/私有化环境 |
| 清理 | 删集群 B → 删管理集群 A → 删跳板机/VPC（脚本） | 删集群 B → 删 EKS/EC2（控制台）→ 删 IAM CloudFormation 堆栈 | CAPA 多一步删 IAM 堆栈 |

## 2. 阶段对照

| 阶段 | CCE | CAPA EKS |
|---|---|---|
| 一、基础设施 | 本地 CLI：`deploy-network`（VPC/双子网/密钥对）→ `deploy-bastion`（ECS+EIP）→ `deploy-mgmt-cluster`（集群 A）→ 镜像搬运 SWR | 控制台：创建 EKS 集群 A + 托管节点组 → 创建 EC2 跳板机（IAM 角色） |
| 二、跳板机准备 | SSH 登录 → 装 kubectl/clusterctl → `kubectl` 连集群 A | SSH 登录 → 装 kubectl/clusterctl/clusterawsadm → `aws eks update-kubeconfig` |
| 三、权限引导 | （AK/SK 直接可用；可选 IAM agency 委托） | `clusterawsadm bootstrap iam create-cloudformation-stack` + `encode-as-profile` 编码临时凭证 |
| 四、部署 Provider | `clusterctl init --infrastructure cce` + 修复 pod（imagePullSecret/webhook cert） | `clusterctl init --infrastructure aws` |
| 五、集群 B | 写 `my-cluster.yaml` → apply（provider 调 CCE API 建集群） | `clusterctl generate cluster --flavor eks-managedmachinepool` → apply（provider 调 AWS API） |
| 六、验证 | `kubectl get cluster` / `clusterctl get kubeconfig` / `kubectl get nodes` | 相同（`kubectl get cluster -w` / `get kubeconfig` / `get nodes`） |
| 七、清理 | `kubectl delete cluster` → 删集群 A（脚本）→ 删 ECS/EIP/VPC | `kubectl delete cluster` → 删 EKS/EC2（控制台）→ 删 IAM 堆栈 |

## 3. 关键差异分析

### 3.1 凭证机制（最大差异）

| 维度 | CAPA EKS | CCE |
|---|---|---|
| 凭证类型 | **临时凭证**（EC2 IAM 角色 → STS，实例元数据获取） | **静态 AK/SK**（Secret，长期有效） |
| 注入方式 | `AWS_B64ENCODED_CREDENTIALS` 环境变量 → 控制器编码为 Secret | 每集群 `my-cce-cluster-credentials` Secret |
| 时效 | **约 1 小时**过期，需 `clusterawsadm encode-as-profile` 刷新 + **重启 CAPA Pod** | 长期有效，手动轮换 |
| 优点 | 免硬编码密钥；可细粒度 IAM；契合"控制台 + 角色"最佳实践 | 简单直接，无需凭证刷新 |
| 代价 | **凭证过期运维负担**（刷新 + 重启），生产需配套自动刷新 | 静态密钥需安全存储 + 定期轮换 |

> CCE provider 实际也支持**身份委托**（`CCEClusterIdentity` / IAM agency），对标 CAPA 的 IAM 角色模式——但 `e2e-deployment-guide.md` 演示的是 AK/SK 路径。若需对齐 CAPA 的"免静态密钥"体验，可走 agency 路径（项目 `internal/services/iam` 已有委托支持，参考 `docs/handoff-p1-3-iam-agency.md`）。

### 3.2 基础设施创建：自动化 vs 人工

- **CAPA 方案**：管理集群 A、EC2、IAM 角色全部**控制台点选**——符合"控制台创建 + EC2 运维"的最佳实践，但**不可脚本化、不可复现**（每次人工）。
- **CCE 方案**：`hack/deploy-network` / `deploy-bastion` / `deploy-mgmt-cluster` **幂等脚本**（按名查找、存在即复用）——**可重复执行、可进 CI/GitOps**，但对使用者有 CLI 要求。

> 理念差异：CAPA 把"管理面"留在控制台（人工、可见），CCE 把"管理面"做成脚本（自动化、可复现）。二者各有适用场景：控制台更适合一次性/演示/人工审计；脚本更适合反复部署/CI/自动化交付。

### 3.3 集群 B 生成方式

- **CAPA**：`clusterctl generate cluster <name> --flavor eks-managedmachinepool`——官方**模板 + flavor 机制**自动生成 YAML（支持托管节点组 / 自管节点组 / EKS 托管等 flavor）。
- **CCE**：手写 5 个对象（`Cluster` / `CCECluster` / `CCEManagedControlPlane` / `MachinePool` / `CCEManagedMachinePool`），或复制 `config/samples/cluster-template.yaml` 替换占位符。

> **CCE 缺 `clusterctl generate cluster` + flavor 能力**（当前只有静态模板），是相对 CAPA 的一个明确可改进点（项目 `config/samples/clusterclass-template.yaml` 已有 ClusterClass 雏形，可演进为 generate/flavor 支持）。

### 3.4 网络模型

- **CAPA EKS**：VPC CNI 直接使用 VPC 子网（无需独立容器子网），3 AZ 高可用，私有/公有子网混布。
- **CCE Turbo**：需独立 **ENI 子网**（`type: eni` + neutron_subnet_id），容器网络走 ENI；**CCE Standard** 则与 EKS 相当（节点子网 + 独立容器 CIDR）。

> CCE 多一个 ENI 子网概念（Turbo 特有）。Standard 模式网络模型与 EKS 相当。

### 3.5 零公网（CCE 特色）

- **CCE**：镜像走 **SWR 内网**、节点组件走 **OBS 内网**，全程不开放公网出网——适合私有化/内网/等保环境。
- **CAPA**：EC2 需公网（SSH 登录 + 拉取镜像/registry），或额外配置 VPC Endpoint / ECR。

> "零公网"是 CCE 方案在内网部署场景的明确优势（管理集群 A 无公网 endpoint，跳板机同 VPC 访问内网 API）。

## 4. 命令速查对照（跳板机）

| 操作 | CAPA EKS | CCE |
|---|---|---|
| 连管理集群 A | `aws eks update-kubeconfig --region <r> --name <a>` | `export KUBECONFIG=/root/capi-mgmt.kubeconfig` |
| IAM/权限引导 | `clusterawsadm bootstrap iam create-cloudformation-stack` | （AK/SK 直接可用；可选 agency） |
| 编码凭证 | `export AWS_B64ENCODED_CREDENTIALS=$(clusterawsadm ... encode-as-profile)` | `kubectl create secret generic my-cce-cluster-credentials ...` |
| 初始化 | `clusterctl init --infrastructure aws` | `clusterctl init --infrastructure cce` |
| 生成集群 B | `clusterctl generate cluster my-c --flavor eks-managedmachinepool ...` | `kubectl apply -f my-cluster.yaml`（手写） |
| 监控 | `kubectl get cluster -w` | 相同 |
| kubeconfig | `clusterctl get kubeconfig my-c` | 相同 |
| 验证节点 | `kubectl --kubeconfig=... get nodes` | 相同 |
| 删集群 B | `kubectl delete cluster my-c` | 相同 |

## 5. 对 CCE 方案的可改进点（对齐 CAPA）

1. **`clusterctl generate cluster` + flavor 支持**（最高价值）——当前手写 5 对象 YAML 对用户不友好；可基于 `clusterclass-template.yaml` 演进为 generate/flavor。
2. **凭证刷新自动化**——若推广 agency（IAM 委托）路径，需配套凭证轮换机制（对标 CAPA 的 `controller update-credentials`）。
3. **管理集群 A 高可用**——CAPA 强调 3 AZ；CCE 管理集群 A 默认 `cce.s1.small`（1 控制节点）+ 2 节点单 AZ，生产建议多 AZ / 更高规格。
4. **跳板机认证对齐**——CAPA 用 IAM 角色（免静态密钥）；CCE 用 SSH 密钥对（简单），若需"免密钥"可演进为华为云 IAM 委托 + 云主机信任。

---

## 附录：本文档依据

- 本项目部署流程：`docs/e2e-deployment-guide.md`（阶段一~四，含 Standard/Turbo）
- CAPA EKS 托管方案：用户提供的"控制台创建基础设施 + EC2 内完成所有运维操作"版本
- 本项目 IAM 委托支持：`docs/handoff-p1-3-iam-agency.md`、`internal/services/iam`
