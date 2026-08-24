# 02 — Controllers Reconcile 流程对标 CAPA 全托管模式（逐行审计）

> 生成日期：2026-08-23
> 对标基准：CAPA（kubernetes-sigs/cluster-api-provider-aws）@ `/tmp/capa` commit `67de5c2`（v2.10 主干，CAPI v1.13.4）
> 本项目：cloudnative-cluster-api-provider-cce @ CAPI v1.14.0
> 审计范围：4 个主 controller 的 Reconcile 流程 + 6 个辅助模块（gc / kubeconfig_rotation / credentials / requeue / setup / events）
> 结论口径：仅覆盖 CCE 与 CAPA managed/EKS 模式；ROSA / Karpenter / 自管 ASG/Machine controller 不在本报告范围

---

## §1 方法

本报告对 4 个主 controller + 6 个辅助模块逐行核验当前 CCE 代码与 CAPA 对应实现：

| 模块 | CCE 文件（当前代码，行数） | CAPA 对应实现 |
|---|---|---|
| Cluster controller | `controllers/ccecluster_controller.go`（379 行） | `controllers/awsmanagedcluster_controller.go`（非托管：`awscluster_controller.go`，21840 字节） |
| Managed ControlPlane | `controllers/ccemanagedcontrolplane_controller.go`（871 行） | `controlplane/eks/controllers/awsmanagedcontrolplane_controller.go`（25304 字节） |
| Managed MachinePool | `controllers/ccemanagedmachinepool_controller.go`（593 行） | `exp/controllers/awsmanagedmachinepool_controller.go`（14898 字节） |
| Identity controller | `controllers/cceclustercontrolleridentity_controller.go`（87 行） | `exp/controlleridentitycreator/awscontrolleridentity_controller.go` |
| GC | `controllers/gc.go`（261 行） | `pkg/cloud/services/gc/cleanup.go` + `controllers/awscluster_controller.go` 删除路径 |
| Kubeconfig rotation | `controllers/kubeconfig_rotation.go`（76 行） | ROSA-only；EKS managed 模式无对等（见 06 报告 §3） |
| Credentials | `controllers/credentials.go`（35 行） + `internal/scope/scope.go` | `pkg/cloud/scope/session.go`（482 行） |
| Requeue | `controllers/requeue.go`（46 行） | 无独立 helper（controller-runtime + conditions） |
| Setup | `controllers/setup.go`（~61 行） | `controllers/` setup + `feature.Gates` |
| Events | `controllers/events.go`（21 行） | `util/record.Eventf`（CAPI core） |

所有结论基于 `read` 的当前文件原文，不采信 archive 文档。

---

## §2 关键常量与基础设施

### CCE 常量

| 名称 | 值 | 文件 |
|---|---|---|
| `CCEClusterFinalizer` | `"ccecluster.infrastructure.cluster.x-k8s.io"` | `ccecluster_controller.go` |
| `ControlPlaneFinalizer` | `"ccemanagedcontrolplane.controlplane.cluster.x-k8s.io"` | `ccemanagedcontrolplane_controller.go` |
| `MachinePoolFinalizer` | `"ccemanagedmachinepool.infrastructure.cluster.x-k8s.io"` | `ccemanagedmachinepool_controller.go` |
| `defaultRequeue` | `30 * time.Second` | `controllers/requeue.go` |
| `kubeconfigRefreshThresholdDays` | `30`（证书 365 天） | `controllers/kubeconfig_rotation.go` |

### CAPA Finalizers

| 名称 | 形态 |
|---|---|
| AWSCluster | `awscluster.infrastructure.cluster.x-k8s.io` |
| AWSManagedCluster | `awsmanagedcluster.infrastructure.cluster.x-k8s.io` |
| AWSManagedControlPlane | `awsmanagedcontrolplane.controlplane.cluster.x-k8s.io` |
| AWSManagedMachinePool | `awsmanagedmachinepool.infrastructure.cluster.x-k8s.io` |

**结论**：CCE 与 CAPA 的 finalizer 命名格式完全一致（`{kind}.{group}.cluster.x-k8s.io`），属 CAPA Provider Contract 标准模式。✅ 对齐。

---

## §3 Reconcile 路径对比

### 3.1 CCECluster Controller

| 步骤 | CCE | CAPA `AWSManagedCluster` |
|---|---|---|
| 入口签名 | `Reconcile(ctx, req)` → `ctrl.Result, error` | 同 |
| 预检 | `clusterutil.ShouldReconcile`（cluster paused 检查） | `clusterutil.ShouldReconcile` |
| 取对象 | `Get(ctx, req.NamespacedName, &CCECluster{})` | 同 |
| Patch Helper | `patch.NewHelper(obj, c.Client)` + `defer patchHelper.Patch(ctx, obj)`（每个 controller 一致） | 同（CAPA 在 scope 内） |
| 凭证解析 | `resolveClusterCredentials(ctx, c.Client, cce)`（identityRef 链；二次审计修复 P0-2 的同类问题） | scope.IdentityRef() → AK/SK |
| 网络 Reconcile | `reconcileNetwork(ctx, scope, cce)`（含 VPC/子网/NAT 托管） | 不在 cluster；下沉到 `ManagedControlPlane.ReconcileNetwork` |
| Conditions | `VpcReady`/`SubnetsReady`/`NatGatewaysReady`/`NetworkReady`（**CCE 独有细分**，CAPA 在控制面） | core `VpcReady`/`SubnetsReady` |
| Delete 路径 | finalizer 移除 → `cluster.Status.Ready=false` | 同 |

**根本差异**：CCE 把网络编排放在 CCECluster 控制器，CAPA 放在 ManagedControlPlane。CCECluster controller 实际承担了"基础设施层"职责（VPC/NAT 托管），这与 CAPA 的 AWSCluster（非托管路径）结构类似但语义不同。详见 01 报告 §3.1。

### 3.2 CCEManagedControlPlane Controller（871 行，CCE 最大）

| 步骤 | CCE | CAPA `AWSManagedControlPlane` |
|---|---|---|
| Reconcile 主分支 | `reconcileNormal` / `reconcileDelete` | 同（`ReconcileNormal` / `ReconcileDelete`） |
| 凭证 | identityRef → per-cluster Secret → env | session.k8sIdentity（基于 Kubernetes ServiceAccount） |
| 升级工作流 | `reconcileUpgrade`：CreateUpgradeWorkFlow → PreCheck → UpgradeCluster → 轮询 | `reconcileClusterUpgrade`：PreCondition → Start → waitFor |
| Addons | `reconcileAddons` 声明式差量（CRD layer） | `reconcileAddons` + `Addon CRD`（**CAPA 有独立 CRD `Addon`**） |
| PodIdentity | `reconcilePodIdentityAssociations` | `reconcileEKSPodIdentityAssociations` |
| Logging | `UpdateClusterLogConfig` + 差量 | `reconcileControlPlaneLogging` + TTL |
| KMS 加密 | `encryptionConfig.mode`（Default/KMS） | `EncryptionConfig{KMSProviderARN, resources}`（CAPA 字段更详细） |
| 认证模式 | `authentication.mode`（rbac/authenticating_proxy） | `AccessConfig.AuthenticationMode` + bootstrap config |
| Access policies | `reconcileAccessPolicies`（AccessPolicy） | `reconcileAccessEntries`（**CAPA 用 AccessEntry CRD**） |
| Kubeconfig | 双 Secret：`<cluster>-kubeconfig` + `<cluster>-user-kubeconfig`（证书 365d，过期前 30d 轮换） | 单 Secret `<cluster>-kubeconfig` |
| Autopilot | `spec.enableAutopilot` → `ClusterSpec.EnableAutopilot` 透传 | 独立 FargateProfile + ROSA |

**核心差异**：
1. **CCE 无 `Addon` 独立 CRD**——addon 配置内嵌在 `spec.addons[]`。CAPA 的 `Addon` CRD 允许按集群/命名空间独立管理。CAPI v1.14 引入的 `ClusterResourceSet` 也可承载，但 CCE 未采用。
2. **CCE 无 `AccessEntry` 独立 CRD**——access policy 内嵌在控制面。CAPA 的 AccessEntry 模型更细粒度（per-user/group），CCE 的 AccessPolicy 以 group 粒度+命名空间范围简化。
3. **Kubeconfig 双 Secret** 是 CCE 改进项（archive 已实施），CAPA 不做用户级 kubeconfig。

### 3.3 CCEManagedMachinePool Controller

| 步骤 | CCE | CAPA `AWSManagedMachinePool` |
|---|---|---|
| 扩缩容 | 绝对值 `ScaleNodePool` + `Watches(&MachinePool{})` 触发（archive P0-1 修复） | `ReconcileManagedMachinePool` 通过 ASG desired capacity |
| 触发源 | `Watches(&source.Kind{Type: &clusterv1.MachinePool{}}, handler.EnqueueRequestForOwner)` | CAPI core 通用 + owner reference |
| 升级 | `UpgradeNodePool` 同步节点池 | `UpgradeNodegroup` + update strategy |
| 节点修复 | `reconcileNodeRepair`：检测 Abnormal/Error 节点 `ResetNode`（CCE 主动，CAPA 走 EKS auto-repair） | EKS managed node group auto-repair |
| 多 AZ | `extensionScaleGroups[]`（各组独立 flavor/AZ） | `AWSManagedMachinePool.Spec.AvailabilityZones` |
| 生命周期钩子 | `preInstall/postInstall` → `alpha.cce/preInstall/postInstall` 注解（NodeExtendParam） | `LifecycleHookSpec`（ASG launch template hook） |
| 反向同步 | `replicas-managed-by` 注解 → 反向写 MachinePool.spec.replicas | 同（CAPA 精确修正） |

**核心差异**：
1. **CCE 节点修复是 provider 主动检测 + 主动 reset**；CAPA 走 EKS auto-repair（AWS 端自动）。语义不同但目标一致。
2. **生命周期钩子形态不同**：CCE 是节点初始化脚本（preInstall/postInstall），CAPA 是 ASG LifecycleHook（launching/terminating 事件）。语义有重叠但不可互换。

### 3.4 CCEClusterControllerIdentity Controller（87 行）

| 步骤 | CCE | CAPA `AWSControllerIdentity`（exp） |
|---|---|---|
| 单例 | name 可任意（无强制 default） | name **必须为 `default`**（单例） |
| 自动创建 | `AutoControllerIdentityCreator` feature gate 触发创建 default 单例 | 同（`AWSControllerIdentityCreator` feature gate） |
| Spec 不可变 | ❌ 无校验 | ✅ 禁止更新 spec |
| 凭证类型 | per-namespace（allowedNamespaces） | per-namespace + cluster-wide |

**核心差异**：CCE 缺"单例名 default"约束与 spec 不可变校验（详见 05 报告 §2.2）。

---

## §4 辅助模块逐一对标

### 4.1 `credentials.go`（CCE）

```go
// 凭证解析链（CCE 二次审计后）
1. spec.identityRef != nil → scope.ResolveIdentity → 解析 AllowedNamespaces → 获取 AK/SK
2. spec.identityRef == nil → per-cluster Secret `<cluster>-credentials`
3. Secret 不存在 → 环境变量 CLOUD_SDK_AK / CLOUD_SDK_AK_SK
```

| 维度 | CCE | CAPA |
|---|---|---|
| 凭证链顺序 | identityRef → per-cluster Secret → env | identityRef → session.k8sIdentity（IRSA） |
| IRSA（IAM Role for SA） | ❌ 不支持 | ✅ 主要模式（CAPI ServiceAccount token projection） |
| 凭证缓存 | `sync.Map`（region+ak+sk） | scope 内 session 缓存（按 controllerName） |
| AllowedNamespaces 校验 | `checkAllowedNamespace`（selector 解析） | 同 |
| 三类身份 | ControllerIdentity / StaticIdentity / RoleIdentity | 同 |
| RoleIdentity 实现 | agencyName 透传到 CreateCluster（华为云委托） | sourceIdentityRef + AssumeRole（AWS STS） |

### 4.2 `requeue.go`（CCE，46 行）

```go
// 错误分类 → 差异化退避
- IsThrottled()      → 1 min
- IsQuotaExceeded()  → 5 min
- IsPermissionDenied() / IsAuthFailure() → 30 min
- 默认               → 30 s
```

| 维度 | CCE | CAPA |
|---|---|---|
| 退避差异化 | ✅ 4 类错误差异化 | ❌ 无独立 helper（依赖 controller-runtime 默认 + conditions requeue） |
| 错误分类来源 | `internal/services/errors/errors.go`（7 谓词 + 26+ 官方 CCE 错误码） | `pkg/cloud/awserrors`（60+ AWS 错误码） |
| 默认退避 | 30s | controller-runtime 默认 5s（`RequeueAfter` 由 controller 显式指定） |

### 4.3 `events.go`（CCE，21 行）

```go
func recordEvent(rec record.EventRecorder, obj runtime.Object, eventType, reason, message string) {
    if rec == nil { return }
    rec.Eventf(obj, eventType, reason, message)
}
```

极简 wrapper，仅做 nil-recorder 守卫。CAPA 直接 `rec.Eventf(...)`（无独立 helper）。

### 4.4 `setup.go`（CCE）

```go
// 注册 4 个 controller 到 manager
type ControllerConcurrency struct {
    ClusterConcurrency, ControlPlaneConcurrency, MachinePoolConcurrency int
}
func SetupControllers(...) {
    // 每个 controller 用 ctrl.NewControllerManagedBy(mgr).For(&Type{}).Complete(r)
}
```

| 维度 | CCE | CAPA |
|---|---|---|
| 并发控制 | ✅ `cce-cluster/control-plane/machine-pool-concurrency` flag | ✅ `--cluster-concurrency` + 各 controller flag |
| Watch 范围 | 主对象 + 部分 Identity 监听 | 主对象 + Identity + MachinePool 全套 |
| `ForEach` 注册 | ❌ 单一控制器 | ✅ `forEachCRD` 模式（条件注册） |

### 4.5 `gc.go`（CCE，261 行）

详见 06 报告 §2。**结论速览**：CCE 是独立 ticker 周期扫描器（LeaderElectionRunnable），CAPA 是删除路径驱动 + CLI 工具。两者覆盖的资源类型不同（CCE 计费资源 vs CAPA 服务型 LB），无对等冲突。

### 4.6 `kubeconfig_rotation.go`（CCE，76 行）

详见 06 报告 §3。**结论速览**：CCE 在控制面控制器内主动刷新 kubeconfig Secret（证书 365d，过期前 30d）。CAPA EKS managed 无对等（EKS kubeconfig 按需生成）；ROSA-only。

---

## §5 关键结构差异：CCE 无 scope struct

详见 03 报告 §1。**关键事实**：
- CAPA：`ClusterScope` / `ManagedControlPlaneScope` / `MachinePoolScope` / `GlobalScope` 4 个 per-object scope struct，承载 logger + client + patchHelper + CR + session + serviceLimiters + controllerName。
- CCE：**无 scope struct**。`internal/scope/scope.go` 仅凭证解析（148 行）。patch helper 内联在 controller 的 `Reconcile` 里。
- **影响**：
  - CCE 没有 CAPA 那种 `scope.Network()` / `scope.VPC()` 这种聚合 getter；CCE 在 controller 内每次取网络对象都走 Service 层。
  - CCE 没有 `Close()` / `PatchObject()` 统一落盘 + SetSummary 自动汇总；CCE 各自 `defer patchHelper.Patch`。
  - 包注释 `Pattern follows CAPA pkg/cloud/scope ...` 与代码事实不符（误导性）。

---

## §6 P0/P1/P2 差距清单

### P0（功能性缺陷 / 阻塞性）
**无新 P0**。archive 报告的 P0 缺陷（扩缩容触发路径 / 删除路径忽略 identityRef / RoleIdentity agency 丢弃）已全部修复，详见 capa-alignment-summary §A.1。

### P1（功能性差距）

| # | 差距 | 现状 | 文件 | 修复成本 |
|---|---|---|---|---|
| 1 | **CCE 无独立 Addon CRD**（CAPA 有） | addon 配置内嵌 `spec.addons[]`，无法独立 lifecycle | `ccemanagedcontrolplane_types.go` | 中（新建 `CCEAddon` CRD + controller） |
| 2 | **CCE 无独立 AccessEntry CRD**（CAPA 有） | access policy 内嵌 `spec.accessPolicies[]` | `ccemanagedcontrolplane_types.go` | 中（新建 `CCEAccessEntry` CRD + controller） |
| 3 | **CCE 不支持 IRSA**（CAPA 默认模式） | 仅 AK/SK + agency | `scope.go` `credentials.go` | 高（需要 ServiceAccount token projection + STS） |
| 4 | **CCE 无 wait package**（CAPA 有） | 固定 5s `time.Sleep` 在 manager.go 轮询；无指数退避 | `internal/services/network/manager.go` + controllers | 低（新增 `internal/wait/` 复制 CAPA pattern） |
| 5 | **CCE 无 GlobalScope**（CAPA 有） | GC 走 env 凭证而非复用 controller session | `controllers/gc.go` | 低 |
| 6 | **CCE 无 scope struct**（结构性） | patch helper / session / serviceLimiters 内联 | 全 controllers | 高（重构，详见 03） |

### P2（结构性改进）

| # | 差距 | 现状 | 文件 | 修复成本 |
|---|---|---|---|---|
| 1 | **CAPI v1.14 `ClusterResourceSet` 未采用** | addon / access policy 内嵌而非 ResourceSet | `ccemanagedcontrolplane_types.go` | 中 |
| 2 | **CCE 节点修复是 provider 主动检测** | 与 EKS auto-repair 语义不同 | `controllers/ccemanagedmachinepool_controller.go` | —（设计选择，无需改） |
| 3 | **CCE 生命周期钩子用 init 脚本** | 与 CAPA ASG LifecycleHook 形态不同 | `ccemanagedmachinepool_types.go` | —（设计选择） |
| 4 | **CCE 无 ServiceFactory 抽象层** | 每个 controller 直接构造 SDK client | controllers | 中（重构） |
| 5 | **CCE requeue.go 单一文件** | CAPA 无独立 helper 但通过 controller-runtime conditions 分散处理 | `controllers/requeue.go` | 低（保持现状也可） |

### P3（命名/注释/装饰性）

| # | 差距 | 现状 | 文件 |
|---|---|---|---|
| 1 | `internal/scope/scope.go` 包注释误导 | 称"follows CAPA pattern (patch helper + Close() = PatchObject)" | `internal/scope/scope.go` |
| 2 | `00-summary.md` 提到"11 个 CRD 类型" | 实际 9 个（详见 01 报告 §2 注） | `docs/audit/00-summary.md` |

---

## §7 与现有对比文档（archive）的差异

| 文档声称 | 实际 | 备注 |
|---|---|---|
| `capa-comparison-review-2026-08.md` L140：「身份 CRD 无 webhook」 | 实际**有 3 个**（已修复，正文未回改） | archive 时效问题 |
| `capa-parity-gap-analysis.md` L83：「转换 webhook ✅ 已实现」 | 实际**已移除**（单版本 v1beta2 收敛） | 同上（archive 时效） |
| `capa-alignment-summary-2026-08-22.md` §A.1：P0/P1/P2 已补齐 | 经本审计核验属实 | ✅ |
| `capa-alignment-summary-2026-08-22.md` §五：「NAT 默认建 vs 显式 enabled」已决策 | 经审计实际 NAT 字段在 CCECluster.Spec.network.natGateway（已去掉 Enabled） | ✅ |

---

## §8 修复路线（建议优先级）

### 短期（1 个月内）
- [ ] 修复 §P3-1（误导性包注释）
- [ ] 新增 `internal/wait/` 包（low cost, high value，详见 §6 P1-4）
- [ ] 引入 `GlobalScope` 重构 GC（low cost，详见 §6 P1-5）

### 中期（季度）
- [ ] 评估 `CCEAddon` 独立 CRD 价值（业务驱动 vs 重构成本，详见 §6 P1-1）
- [ ] 评估 `CCEAccessEntry` 独立 CRD 价值（同上）
- [ ] 评估 `ClusterResourceSet` 采用（详见 §P2-1）

### 长期 / 视需求
- [ ] IRSA 支持（仅当客户要求）
- [ ] Scope struct 重构（高 cost，重构收益需评估）

---

## §9 待补查项

- CAPA `awsmanagedcluster_controller.go`（EKS 托管专用，7350 字节）——本报告以 `awsmanagedcontrolplane` 为对照基准；managed cluster 控制器在 CAPA 中较为简化（核心逻辑在控制面）。
- `internal/scope/scope_test.go`（覆盖度核验）
- CAPA `controllerutil` / `patches` helper 模式是否被 CCE 复制

---

## §10 报告总结

CCE Provider 在 Controllers 层的 **4 个主 controller 与 6 个辅助模块** 与 CAPA EKS managed mode 的整体对齐度良好：finalizer 命名一致、patch helper 模式一致、并发控制与 events 完整、requeue 差异化优于 CAPA（CCE 有 4 类错误差异化退避，CAPA 无独立 helper）。

**主要差距集中在 3 类**：
1. **CRD 不对等**：CCE 无独立 Addon / AccessEntry CRD（与 CAPA EKS managed 比），但 CCE 有 MachinePoolTemplate（CAPA 无）。
2. **认证模式不对等**：CCE 不支持 IRSA（依赖 AK/SK + agency），是云能力差异而非设计缺陷。
3. **架构不对等**：CCE 无 scope struct（详见 03），无 wait package（详见 06），无 GlobalScope（GC 走 env 凭证）。

**无 P0 阻塞项**。P1 差距 6 项，修复成本从中到高，需结合产品决策。