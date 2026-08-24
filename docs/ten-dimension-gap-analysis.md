# 华为云 CCE 插件开发规划（十维度）差距分析

> 基线：用户《华为云 CCE 插件开发规划文档（十个维度）》
> 对照对象：本仓库 `cloudnative-cluster-api-provider-cce`（CAPI + CCE managed 模式）
> 审计基线：`docs/capa-alignment-final-summary.md`（CAPA v2.13.0 / CAPI v1.14.0）
> 命名映射：文档 `HuaweiCloudManagedControlPlane`/`HuaweiCloudManagedMachinePool`/`HuaweiCloudCluster` ↔ 仓库 `CCEManagedControlPlane`/`CCEManagedMachinePool`/`CCECluster`

每个维度标注：✅ 已实现 / 🟡 部分实现 / ❌ 未实现，并附代码证据与 P0/P1/P2 缺口。

---

## 一、理解 CCE 与 EKS 的核心差异

**结论：✅ 已实现（设计层）**

- CCE 控制面全托管、节点池 = ECS 节点组，对齐 EKS managed 模式定位（Turbo 默认，README「CCE Standard + CCE Turbo」）。
- 差异点已固化在 `docs/architecture-design.md` 与 `docs/research-sources.md`（每条设计决策附验证事实）。
- 关键差异映射已落地代码：CCE 需要先有 VPC/子网才能建集群（`spec.network` 校验）、控制面公网访问需 EIP/ELB、容器网段按 VPC 唯一、Turbo 需 ENI 子网（`eniSubnets`）等。

**缺口**：无（该维度为认知/设计前提，已覆盖）。

---

## 二、插件整体架构与 CRD 设计

**结论：🟡 部分实现（架构与 CRD 齐全，字段命名/位置有差异）**

### 2.1 CRD

| 文档字段/CRD | 仓库对应 | 状态 |
|---|---|---|
| `HuaweiCloudManagedControlPlane` | `CCEManagedControlPlane`（`api/controlplane/v1beta2/ccemanagedcontrolplane_types.go`） | ✅ 存在 |
| `HuaweiCloudManagedMachinePool` | `CCEManagedMachinePool`（`api/infrastructure/v1beta2/ccemanagedmachinepool_types.go`） | ✅ 存在 |
| `HuaweiCloudCluster`（可选） | `CCECluster`（`api/infrastructure/v1beta2/ccecluster_types.go`） | ✅ 存在（非可选，承担 VPC/网络归属） |
| 身份 CRD | `CCEClusterIdentity`（static/role/controller 三种，`identity_types.go`） | ✅ 存在（超出文档，对齐 CAPA） |

字段命名差异（文档 → 仓库）：
- `spec.instanceType` → `spec.flavor`
- `spec.scaling` → `spec.autoscaling`
- `spec.region`（文档挂在 ControlPlane）→ 实际在 **`CCECluster.spec.region`**（`ccecluster_controller.go` 读取 `cceCluster.Spec.Region` 做网络校验；ControlPlane 经 `clusterNetwork()` 间接读取）。**这是与文档的结构性差异**，需在用户文档说明。
- 其余 `os`/`rootVolume`/`dataVolumes`/`labels`/`taints`/`nodePoolName` 均对齐。

### 2.2 控制器逻辑（Reconcile / 幂等性 / 最终一致性）

- Reconcile：`controllers/ccemanagedcontrolplane_controller.go`、`ccemanagedmachinepool_controller.go`、`ccecluster_controller.go` 三者协作。✅
- 幂等性：创建走「先查后建」+ 冲突即采纳（adopt，同 CIDR 触发冲突即复用已有集群，README「Adopting an existing cluster」）。✅
- 最终一致性：`status.conditions` + `observedGeneration` 驱动，异步轮询 CCE 任务（`upgradeTaskID`、`nodePoolID` 落地）。✅

**缺口（P2）**：文档 `spec.region` 位置差异需在 README/示例 YAML 中显式标注，避免用户按文档误填。

---

## 三、凭证管理与 IAM 集成

**结论：✅ 已实现（AK/SK、Agency、STS 临时凭证刷新、Workload Identity 齐全）**

### 3.1 凭证获取

| 文档要求 | 实现 | 状态 |
|---|---|---|
| AK/SK | `CCEClusterStaticIdentity`（`SecretRef` → Secret `accessKey`/`secretKey`）+ 控制器默认 `CLOUD_SDK_AK/SK` 环境（`identity_types.go`、`scope.go`） | ✅ |
| IAM 委托 Agency | `CCEClusterRoleIdentity`（`spec.agencyName`，对应 CAPA AssumeRole 身份）；`spec.agencyTrustPolicy` 声明信任策略后由 provider 自动创建（`internal/services/iam` `EnsureAgency`） | ✅ |
| **STS 临时凭证刷新** | ✅ `internal/credentials` `Provider.AssumeAgency`（STS v1）→ `credentials.Resolve` 产出临时 AK/SK/SecurityToken；静态 AK/SK 缓存、SecurityToken 不缓存 | ✅ |

### 3.2 IAM 角色与策略

| 文档要求 | 实现 | 状态 |
|---|---|---|
| 控制器权限 | RBAC manifest + IAM 权限清单（`docs/smoke-test-checklist.md` §2） | ✅ |
| 每集群独立 IAM 角色 | `identityRef` 每集群引用（对齐 CAPA 三身份），权限策略经 `accessPolicies` 映射 CCE 角色 | ✅ |
| Workload Identity | `spec.podIdentityAssociations`（CCE pod 级 IAM，EKS Pod Identity/IRSA 等价物） | ✅ |

---

## 四、网络与基础设施准备

**结论：🟡 部分实现（VPC 双模式、API Server 公网、NAT 齐全、安全组自动创建；VPC Endpoint 私网管理缺失）**

### 4.1 VPC 与子网

- 用户提供 vs 自动创建：✅ **双模式**——`api/common/types.go` 的 `NetworkSpec` 三态（managed 创建 / adopt 采纳 / BYO 引用，owned tag 标记 adopt），`internal/services/network/manager.go` 负责 VPC/子网/NAT 创建与删除。
- 三种网络模型：`containerNetwork.mode`（`overlay_l2` / `vpc-router` / `eni`）✅。

### 4.2 安全组

| 文档要求 | 实现 | 状态 |
|---|---|---|
| 控制面 SG 自动创建 | CCE 平台自动创建（控制面托管，provider 无需创建）——符合 CCE 事实 | ✅（平台侧） |
| 节点池 SG | `CCEManagedMachinePool.spec.securityGroups`（max 5，显式引用；为空时自动绑定集群托管 node SG） | ✅ |
| 控制器支持指定/自动创建 SG | ✅ `network/manager.go` `ReconcileSecurityGroup`（List → Create，幂等）+ 声明式 ingress/egress 规则（`spec.network.securityGroup`） | ✅ |

### 4.3 API Server 访问

| 文档要求 | 实现 | 状态 |
|---|---|---|
| 公网访问 | `spec.endpointAccess.public` + EIP/ELB 绑定（已真机验证） | ✅ |
| 私网访问 | CCE 默认暴露 VPC 内部端点（`public=false` 时 kubeconfig server 为内网 IP，README 已注明需 bastion） | 🟡（平台默认，无显式私网管理） |
| 公网+私网两者 | 🟡 无显式「两者」开关语义 |
| **VPC Endpoint** | ❌ 未实现（华为云 VPC Endpoint 打通 CCE 私网 API Server） | ❌ |

**缺口**：
- **P2** — VPC Endpoint 私网访问管理；公网/私网/两者三态显式语义。

### 4.4 辅助资源创建者与生命周期（CAPA 一致性对照）

按 CAPA 的「创建者 / 触发者」模型逐项核对 CCE provider：网络基础设施（VPC/子网/NAT/EIP/安全组）与节点/插件生命周期对齐；IAM Agency 自动创建亦已对齐。

| 资源 | CAPA（EKS）创建者 | CCE provider 现状 | 一致性 |
|---|---|---|---|
| VPC | CAPA 控制器创建（或用户提供） | `network/manager.go` `ReconcileVpc`/`ensureVpc`（或 BYO/adopt，owned tag 标记） | ✅ |
| 子网 | CAPA 控制器创建（或用户提供） | `ReconcileSubnets`/`ensureSubnets` | ✅ |
| 路由表 / Internet Gateway | CAPA 控制器创建 | N/A——华为云 VPC 创建即含默认路由，无独立 IGW/路由表资源需管理 | ✅（平台差异） |
| NAT 网关 | CAPA 控制器创建 | `ensureNatGateway`（含 EIP + 每节点子网 SNAT 规则） | ✅ |
| NAT 出站 EIP | CAPA 控制器创建 | `createEip`（`<cluster>-nat-eip`，带 owned tag 供 GC 回收） | ✅ |
| 节点/自定义安全组 | CAPA 控制器创建（若未指定） | ✅ `ReconcileSecurityGroup` 创建托管 node SG + 规则；节点池 `spec.securityGroups` 为空时自动绑定 | ✅ 一致 |
| 控制面安全组（`eks-cluster-sg-*`） | EKS 服务自动创建 | N/A——CCE 控制面托管，不暴露安全组 | ✅（平台差异） |
| 集群/节点组 IAM 角色 | CAPA 创建（或用户提供 ARN 跳过） | ✅ `internal/services/iam` `EnsureAgency`（List → Create，IAM v5）按 `spec.agencyTrustPolicy` 自动创建信任委托 | ✅ 一致 |
| API Server ELB/NLB | EKS 服务自动创建（CAPA 触发） | CCE 服务自动创建（provider 仅传 `endpointAccess.public`） | ✅ |
| API Server 公网 EIP | N/A（AWS NLB 自带公网 IP） | CCE 公网 ELB 绑定 EIP（平台自动；`hack/bind-eip` 为手动脚本，生产不直接绑定） | ✅（平台差异） |
| 节点 EC2/ASG | EKS 服务自动创建（CAPA 触发） | CCE 节点池自动创建（provider 触发 `CreateNodePool`） | ✅ |
| Fargate Profile / EKS addon | EKS 服务自动创建（CAPA 触发） | CCE addon（`CreateAddonInstance`）、PodIdentityAssociation | ✅ |

**删除生命周期**：`DeleteNetwork` 按依赖顺序聚合清理 SNAT → NAT → EIP → 安全组 → 子网 → VPC（BYO 规格为 no-op），与 CAPA 的 describe-based 删除对齐；CCE 集群删除时平台自动回收 ELB/EIP/节点。

---

## 五、节点池管理

**结论：🟡 部分实现（核心 CRUD/扩缩容/迁移/驱逐齐全；弹性伸缩受 feature gate 限制、CA 集成非托管）**

### 5.1 创建与更新

| 文档要求 | 实现 | 状态 |
|---|---|---|
| 异步轮询 | 节点池任务轮询 + `status.nodePoolID`/`conditions` 落库 | ✅ |
| ScaleNodePool 扩缩容 | `spec.replicas` 变更 → `ScaleNodePool`（README「scale by changing replicas」） | ✅ |
| 规格变更迁移 | `spec.updateConfig.maxUnavailable` 滚动更新（`UpgradeNodePool`） | ✅ |
| 删除驱逐 | 删除时驱逐节点 + finalizer 保证先删节点池再删集群 | ✅ |

### 5.2 弹性伸缩

- `spec.autoscaling`（enable/min/max）✅ 已建模，**但受 `NodePoolAutoscaling` feature gate 控制（Alpha，默认 false，`internal/features/features.go`）**。
- Cluster Autoscaler 集成：🟡 走 CCE 原生弹性伸缩（HPA/CA 由 CCE 托管），非 provider 部署 CA。

### 5.3 节点初始化

- `spec.preInstall` / `spec.postInstall` / `spec.waitPostInstallFinish` ✅（machine pool types + controller + cce.go 均命中）。
- 标签/污点：`spec.labels` / `spec.taints`（max 20）✅；启动脚本经 pre/postInstall。

**缺口**：
- **P2** — `NodePoolAutoscaling` 默认关：文档未提及需 feature gate，应在用户文档标注并评估是否转 Beta/默认开。

---

## 六、集群生命周期管理

**结论：✅ 已实现（创建/更新/删除/状态监控齐全，conditions 命名有差异）**

### 6.1 创建 ✅（幂等，先查后建）

### 6.2 更新

| 文档要求 | 实现 | 状态 |
|---|---|---|
| 控制平面升级 | `spec.version` 变更 → CCE 升级工作流（pre-check → 滚动升级 → post-check），`UpgradeReady` condition | ✅ |
| 配置变更 | spec 漂移检测 + 重调（coalesced spec change 测试覆盖） | ✅ |
| 插件升级 | `spec.addons`（9 文件命中，control plane types + controller + interfaces） | ✅ |

### 6.3 删除 ✅（先删节点池 → 再删 CCE 集群 → 清理 kubeconfig Secret → finalizer 释放；README「Clean Up Resources」）

### 6.4 状态监控

- `GET /clusters/{id}` 轮询 + `status.conditions` 落库 ✅。
- **conditions 命名差异**：文档 `ClusterReady/ControlPlaneReady/NetworkReady/NodePoolsReady` ↔ 仓库实际 `CredentialsReady`、`CCEClusterReady`（`ClusterAvailable`）、`KubeconfigReady`、`AddonsConfigured`、`PodIdentityAssociationsConfigured`、`LoggingConfigured`、`AccessPoliciesConfigured`、`UpgradeReady`（README「Expected conditions」）。
  - 仓库命名更细粒度（对齐 CAPA condition 命名习惯），但**无 `NetworkReady`/`NodePoolsReady` 两个文档明确要求的 condition**。

**缺口（P2）**：conditions 命名与文档对齐表需在用户文档提供；评估是否补 `NetworkReady`/`NodePoolsReady` 语义（当前网络/节点池状态隐含于 `CCEClusterReady` 与 machine pool conditions）。

---

## 七、错误处理与重试策略

**结论：🟡 部分实现（错误映射表、可重试分类齐全；退避为固定延迟而非指数退避）**

| 文档要求 | 实现 | 状态 |
|---|---|---|
| 错误映射表 | `internal/services/errors/errors.go`：26+ 官方 CCE 错误码（`CCE.01400001` 无效请求、`CCE.01400002` 子网不存在、`CCE.01400005` 容器网段冲突、`CCE.01400007~11` 配额类、`APIGW.0308` 限流等）+ `sdkerr.ServiceResponseError` 分类 | ✅ |
| 可重试错误分类 | `IsThrottled`(429)/`IsConflict`(409)/`IsQuotaExceeded`/`IsPermissionDenied`/`IsNotFound`/`IsScaleNoOp` | ✅ |
| **指数退避** | `controllers/requeue.go`：**固定延迟**——throttled=1min、quota=5min、permission=30min、默认=defaultRequeue；`resultAfterError` 对 throttled/quota 返回延迟 requeue+nil error（避免 error 风暴/覆盖延迟） | 🟡（非指数） |
| 不可重试 → Failed + 事件 | 非限流/配额错误透传为 reconcile error + `recordEvent`（`EventTypeWarning`） | ✅ |

**缺口（P2）**：文档要求「指数退避」，当前为固定延迟重试。限流 1min 固定延迟在华为云 10 次/分钟写限流下可工作（真机验证通过），但严格对齐「指数退避」需引入 `wait.ExponentialBackoff` 或自适应退避。

---

## 八、测试策略

**结论：🟡 部分实现（单测/envtest/mock 齐全；官方 CAPI e2e 框架缺失）**

| 文档要求 | 实现 | 状态 |
|---|---|---|
| 单元测试（gomock/testify） | `*_test.go`：controllers（含 requeue/coalesced 变更/升级/删除）、services、webhooks、`errors_test.go`、`throttle_test.go` | ✅ |
| 集成测试（沙箱/mock） | `controllers/suite_test.go` envtest + `test/fakes/fakes.go`（fake service/validator/manager） | ✅ |
| e2e | 真机冒烟（`docs/cce-verification-findings.md` Q1–Q14：创建→Available、扩缩容、kubeconfig 轮换、删除清理、EIP、限流） | 🟡（冒烟而非官方 e2e 框架） |
| CI | CI 单测+静态检查（README build passing badge） | 🟡（无正式 CI 配置证据/定期冒烟） |

**缺口（P2）**：官方 CAPI e2e 框架（`test/e2e`）未落地；真机冒烟依赖账户余额不可持续自动化。

---

## 九、文档与用户体验

**结论：✅ 已实现**

| 文档要求 | 实现 | 状态 |
|---|---|---|
| clusterctl CLI：generate cluster | `config/samples/cluster-template.yaml` + `metadata.yaml`/`infrastructure-components.yaml` 打包（`clusterctl init` 验证通过） | ✅ |
| clusterctl get kubeconfig | ✅（真机验证） | ✅ |
| clusterctl delete cluster | ✅（`clusterctl delete --infrastructure cce`） | ✅ |
| 用户文档：快速开始 | README「Quick Start」+ `scripts/deploy-kind.sh` 一键 | ✅ |
| 示例 YAML | `cluster-template.yaml`（含 OS 候选列表） | ✅ |
| FAQ | README「FAQ / Troubleshooting」12+ 条（限流/余额/标签/CA 解析/代理/OS 等） | ✅ |

---

## 十、其他注意事项

**结论：🟡 部分实现**

| 文档要求 | 实现 | 状态 |
|---|---|---|
| 版本兼容性 | `metadata.yaml`（CAPI v1.14.0 契约）、`clusterctl` 版本锁定 | ✅ |
| 多区域支持 | ✅ `NewClient(regionID, ak, sk)` + `clientCache sync.Map`（key 含 region+ak+sk），`--gc-region` 支持 | ✅ |
| 结构化日志 | ✅ logr/controller-runtime 结构化日志 | ✅ |
| Prometheus 指标 | 🟡 仅默认 controller-runtime metrics server（`cmd/main.go`），**无自定义业务指标**（集群/节点池数量、API 调用时延/失败率、凭证年龄等） | ❌ |
| 安全审计日志 | 🟡 CCE 控制面审计经 `spec.controlPlaneLogging`（audit 类型）；provider 自身操作审计日志未显式实现 | 🟡 |
| 社区协作 | ✅ MIT-0 许可证、DCO（`git commit -s`）、`CONTRIBUTING.md`、`CODE_OF_CONDUCT`、华为云解决方案开发者套件治理 | ✅ |

**缺口**：
- **P2** — 自定义 Prometheus 业务指标（文档 §10.3 明确要求）。
- **P2** — provider 操作审计日志。

---

## 差距汇总（按优先级）

| 优先级 | 维度 | 缺口 |
|---|---|---|
| P2 | §四 | VPC Endpoint 私网访问；公网/私网/两者三态显式语义 |
| P2 | §二 | `spec.region` 位置差异（文档挂 ControlPlane，实际在 CCECluster）需文档标注 |
| P2 | §五 | `NodePoolAutoscaling` 默认关，需文档标注/评估转默认开 |
| P2 | §六 | conditions 命名对齐表 + 补 `NetworkReady`/`NodePoolsReady` 语义 |
| P2 | §七 | 退避为固定延迟，非文档要求的指数退避 |
| P2 | §八 | 官方 CAPI e2e 框架 + 正式 CI 配置 |
| P2 | §十 | 自定义 Prometheus 业务指标、provider 操作审计日志 |

**总体结论**：项目在架构（§一/§二）、集群生命周期（§六）、文档与 UX（§九）已高度对齐规划文档；三个 P1 生产级安全/自动化缺口（STS 临时凭证刷新、Agency 自动创建、安全组自动创建）均已补齐；其余为命名/文档对齐与可观测性增强（P2）。
