# CCE Provider 对标 CAPA 全托管模式 — 全字段逐行审计报告（汇总）

> 生成日期：2026-08-23
> 对标基准：CAPA（kubernetes-sigs/cluster-api-provider-aws）@ `/tmp/capa` commit `67de5c2`（v2.10 主干，CAPI v1.13.4）
> 本项目：cloudnative-cluster-api-provider-cce @ CAPI v1.14.0，单存储版本 v1beta2
> 审计方法：6 个独立 deep agent 并行，逐文件 / 逐函数 / 逐字段核验**当前代码**（不采信 archive 文档结论）

> ⚠️ **本报告基于 2026-08-23 当前代码状态**。本项目已有 4 份历史对比文档（`docs/capa-alignment-summary-2026-08-22.md` + archive 3 份）。本次审计**不重复发现历史已实现项**，而是：
> 1. 验证 archive 报告声称 ✅ 的每一项在当前代码里**真实到位**
> 2. 找出**当前代码**真正缺失的能力（v1beta2 单版本收敛后是否引入新差距）
> 3. 按"全字段逐行"颗粒度补充 archive 报告未覆盖的细节

---

## 阅读指引

本汇总由 6 个独立模块审计构成：

| 文件 | 模块 | 审计范围 | 状态 |
|---|---|---|---|
| [01-api-types-comparison.md](./01-api-types-comparison.md) | API 类型层 | 9 个 CRD 类型的 Spec/Status 字段级对标（487 行） | ✅ |
| [02-controllers-reconcile-comparison.md](./02-controllers-reconcile-comparison.md) | Controllers | 4 个 controller 的 Reconcile 路径 + 6 个辅助模块（292 行） | ✅ |
| [03-scope-comparison.md](./03-scope-comparison.md) | Scope 层 | 架构级差异 + 云客户端面 + 凭证链（240 行） | ✅ |
| [04-services-comparison.md](./04-services-comparison.md) | Services 层 | CCE 6 文件 vs CAPA 18 模块 + 横切包（339 行） | ✅ |
| [05-webhook-conditions-comparison.md](./05-webhook-conditions-comparison.md) | Webhook + Conditions | 9 个 webhook 校验规则 + 14 vs 40+ conditions（237 行） | ✅ |
| [06-auxiliary-comparison.md](./06-auxiliary-comparison.md) | 辅助工程化 | GC / kubeconfig rotation / feature gates / RBAC（282 行） | ✅ |

---

## 一、TL;DR

> **审计结论**：CCE Provider 与 CAPA EKS managed mode 的核心能力已对齐（无 P0 阻塞），但存在 3 个**结构性差异**（CCE 无 scope struct / 无独立 Addon+AccessEntry CRD / 不支持 IRSA）和 1 个**深度差距**（Webhook 校验密度 ~10× 不足）。其余差异属"CCE 云能力映射"或"设计选择"，非真差距。

### 关键数字

| 维度 | CCE | CAPA | 倍率 |
|---|---|---|---|
| CRD 类型数 | **9**（3 身份 + 6 托管/模板） | 13+（含 Fargate/Machine/MachineTemplate） | 0.7× |
| Scope struct 数 | **0** | 4（Cluster/CPM/MP/Global） | 0× |
| Service 源文件数 | **6** | 50+（18 模块） | 0.12× |
| Webhook 文件数 | **9** | 9+ | 1× |
| Webhook 校验规则数 | **~24** | ~120+ | 0.2× |
| Condition 类型数 | **14** | 40+（core 19 + EKS 8 + exp 13） | 0.35× |
| Feature gates 数 | **3** | 13+ | 0.23× |
| RBAC 行数 | **91** | 274 | 0.33× |

### 优先级分布

| 优先级 | 数量 | 概要 |
|---|---|---|
| **P0（阻塞）** | **0** | 无阻断性缺失。CCE 已覆盖托管集群生命周期核心 API + controller 流程 |
| **P1（功能差距）** | **9** | 见 §二 |
| **P2（结构性改进）** | **11** | 见 §二 |
| **P3（命名/注释）** | **若干** | 详见各模块报告 |

---

## 二、P0/P1 差距清单（跨模块聚合）

### P1 跨模块差距（按修复价值排序）

| # | 差距 | 模块 | 现状 | 修复成本 | 价值 |
|---|---|---|---|---|---|
| **1** | **Webhook 校验深度不足** | Webhook | CCE ~24 条 vs CAPA ~120+ 条 | 中 | **高**（前置拦截错误，用户体验质变） |
| **2** | **CCE 无独立 Addon CRD** | API + Controllers | addon 内嵌 `spec.addons[]` | 高 | 中（业务驱动） |
| **3** | **CCE 无独立 AccessEntry CRD** | API + Controllers | access policy 内嵌 | 高 | 中 |
| **4** | **CCE 无 scope struct**（结构性） | Scope + Controllers | patch helper / session 内联 | **极高** | 中（重构收益不显性） |
| **5** | **CCE 不支持 IRSA** | Credentials | 仅 AK/SK + agency | 高 | 中（需客户驱动） |
| **6** | **CCE 无 wait package** | Services + Controllers | 固定 5s `time.Sleep` 轮询 | 低 | **高**（low cost, high value） |
| **7** | **CCE throttle 只包 network clients**（cluster API 未限流） | Services | 仅 VPC/NAT/EIP 走限流 transport | 低 | **高**（429 风险） |
| **8** | **Conditions 失败原因细分不足** | Webhook + Controllers | 14 condition 共用 8 reason | 中 | 中（定位失败阶段） |
| **9** | **CCE 无 GlobalScope**（GC 走 env 凭证） | Auxiliary | `controllers/gc.go` | 低 | 低 |

### P2 结构性改进（按实施优先级排序）

| # | 差距 | 模块 | 修复成本 |
|---|---|---|---|
| 1 | `ClusterResourceSet` 未采用 | API | 中 |
| 2 | `ServiceFactory` 抽象缺失 | Controllers | 中 |
| 3 | Kubeconfig rotation 实现简化（CCE 证书 365d vs CAPA EKS on-demand） | Auxiliary | —（设计选择） |
| 4 | 节点修复形态不同（provider 主动 vs EKS auto-repair） | Controllers | —（设计选择） |
| 5 | 生命周期钩子语义不同（init 脚本 vs ASG LifecycleHook） | API | —（设计选择） |
| 6 | CCE 身份 webhook 缺单例名约束 + spec 不可变 | Webhook | 低 |
| 7 | CCE GC 是独立 ticker vs CAPA 删除路径驱动 | Auxiliary | —（设计选择） |
| 8 | CCE cluster controller 承担 VPC/NAT 编排（与 CAPA 拓扑不同） | API + Controllers | —（设计选择） |
| 9 | CCE 无 ASG/EC2/ELB/SecurityGroup/S3/SM/SSM/STS 等非托管模块 | Services | —（CCE 是纯托管） |
| 10 | CAPA mock_services（12 个 mockgen）vs CCE go test native + fakes | Services | 中 |
| 11 | 命名差异（eksClusterName vs clusterName 等） | API | 低 |

---

## 三、3 个**根本性架构差异**

### 差异 #1：CAPI v1.14 vs CAPA v2.10（CAPI v1.13.4）

- CCE 用 CAPI v1.14.0，CAPA 对比基线用 CAPI v1.13.4（CAPI 早 1 个 minor）。
- CAPI v1.14 引入的 `ClusterResourceSet` / 一些 status 字段变化在 CAPA 67de5c2 中可能未完全反映。
- **建议**：下次审计时升级 CAPA 到最新 main 分支（v2.5+ / v2.10+），避免基线陈旧。

### 差异 #2：CCE 无 scope struct（结构性）

详见 [03 报告](./03-scope-comparison.md) §1。

- **CAPA**：`ClusterScope` / `ManagedControlPlaneScope` / `MachinePoolScope` / `GlobalScope` 4 个 per-object scope struct，承载 logger + client + patchHelper + CR + session + serviceLimiters + controllerName。
- **CCE**：`internal/scope/scope.go` 仅凭证解析（148 行），**无任何 scope struct**。patch helper 内联到 controller。
- **影响**：
  - CCE 没有 CAPA 那种聚合 getter（`scope.Network()` / `scope.VPC()`），在 controller 内每次取网络对象都走 Service 层。
  - CCE 没有 `Close()` / `PatchObject()` 统一落盘 + SetSummary 自动汇总。
  - **包注释误导**：`scope.go` 注释声称 "Pattern follows CAPA pkg/cloud/scope ... patch helper + Close() = PatchObject"，与代码事实不符（**P3-1**）。
- **评估**：重构成本极高（极高），重构收益不显性（CCE 当前 controller 已稳定运行）。**建议**：保持现状，但**修正注释**。

### 差异 #3：CCE 是纯托管（managed-only）

- CAPA 同时支持 EKS managed（CCE 对标目标）、ASG 自管、Karpenter、ROSA 等模式。
- CAPA 的 `ec2/elb/securitygroup/autoscaling/s3/secretsmanager/ssm/sts/userdata/awsnode/kubeproxy/iamauth/instancestate` 模块在 CCE 中**无对等物**（CCE 平台服务端托管）。
- CCE 仅需对标 CAPA 的 `eks` + `network` + `gc` + `wait` + `throttle` + `awserrors` 6 类能力。
- **结论**：CCE 的 6 个服务文件映射合理（详见 04 报告 §2.4），**非差距**。

---

## 四、与现有对比文档（archive）的差异

| 文档声称 | 实际（经本次审计核验） | 差异类型 |
|---|---|---|
| `capa-comparison-review-2026-08.md` L140：「身份 CRD 无 webhook」 | 实际**有 3 个**（已修复） | **archive 时效**（正文未回改） |
| `capa-parity-gap-analysis.md` L83：「转换 webhook ✅ 已实现」 | 实际**已移除**（单版本 v1beta2 收敛） | **archive 时效** |
| `capa-parity-gap-analysis.md` L18：「conditions 10 个」 | 实际 **14 个**（少算 4 个：VpcReady/SubnetsReady/NatGatewaysReady/AccessPoliciesConfigured） | **archive 漏数** |
| `capa-alignment-summary-2026-08-22.md` L79：「13 个 webhook 文件」 | 实际 **9 非测试 + 4 测试 = 13**（口径混淆） | **archive 误计** |
| `capa-alignment-summary-2026-08-22.md` §五：「NAT 默认建 vs 显式 enabled 已决策」 | 经审计实际 NAT 在 `CCECluster.Spec.network.natGateway`，已去掉 Enabled 字段 | ✅ 一致 |
| `capa-alignment-summary-2026-08-22.md` §A.1：P0/P1/P2 已补齐 | 经本次审计核验属实 | ✅ 一致 |

**结论**：archive 文档整体可信，但有几处**时效问题**（v1beta2 单版本收敛后未回改）和**漏数/误计**。建议：
1. 更新 `docs/capa-alignment-summary-2026-08-22.md`，将"9 个 webhook 文件"取代"13 个"
2. 删除 archive 中关于"身份 CRD 无 webhook"和"转换 webhook ✅"的过时断言
3. 修正 `00-summary.md`（本文件）中"11 个 CRD 类型"为"9 个"

---

## 五、修复路线（按优先级）

### 🔴 立即修复（1 周内）

| # | 项 | 模块 | 文件 | 价值 |
|---|---|---|---|---|
| 1 | 修正 `internal/scope/scope.go` 包注释（移除误导性 "follows CAPA" 声明） | Scope | `internal/scope/scope.go` | 防止误读 |
| 2 | 修正 `docs/audit/00-summary.md`（11 → 9 个 CRD 类型） | 文档 | `docs/audit/00-summary.md` | 准确性 |
| 3 | 更新 `docs/capa-alignment-summary-2026-08-22.md`（9 个 webhook + 删除 archive 旧断言） | 文档 | `docs/capa-alignment-summary-2026-08-22.md` | 准确性 |

### 🟡 短期修复（1 个月内）

| # | 项 | 模块 | 文件 | 价值 / 成本 |
|---|---|---|---|---|
| 1 | **新增 `internal/wait/` 包**（复制 CAPA wait.go 指数退避） | Services | `internal/wait/wait.go` | 高 / 低 |
| 2 | **CCE 集群 API SDK client 加限流**（`pkg/cloud/sdk/cce` 包 `throttleRoundTripper`） | Services | `internal/services/cce/cce.go` NewClient | 高 / 低 |
| 3 | **CCE 身份 webhook 加 spec 不可变校验**（Controller/Role/Static 三类） | Webhook | `api/infrastructure/v1beta2/ccecluster*identity_webhook.go` | 中 / 低 |
| 4 | **CCE GC 加 per-cluster annotation opt-out**（对齐 CAPA ExternalResourceGCAnnotation） | Auxiliary | `controllers/gc.go` | 中 / 低 |

### 🟢 中期改进（季度）

| # | 项 | 模块 | 价值 / 成本 |
|---|---|---|---|
| 1 | Webhook 校验密度补齐（CCE ~24 → CAPA ~120+ 的 50% 覆盖率） | Webhook | 高 / 中 |
| 2 | Conditions 失败原因细分（14 condition 各自 3-8 reason） | Controllers | 中 / 中 |
| 3 | ServiceFactory 抽象层 | Controllers | 中 / 中 |
| 4 | 评估 CCEAddon / CCEAccessEntry 独立 CRD | API + Controllers | 视业务驱动 |
| 5 | 评估 ClusterResourceSet 采用 | API | 中 |

### ⚪ 长期 / 视需求

| # | 项 | 模块 | 价值 / 成本 |
|---|---|---|---|
| 1 | IRSA 支持 | Credentials | 中 / 高 |
| 2 | Scope struct 重构 | Scope + Controllers | 中 / 极高 |
| 3 | CAPA 基线升级到最新 main（CAPI v1.14+） | 审计方法 | 防止基线陈旧 |

---

## 六、附录

### A. 审计方法与限制

- **6 个独立 deep agent 并行**，每个负责一个模块。
- **逐文件 / 逐函数 / 逐字段**核验当前代码（不采信 archive）。
- **限制**：
  1. 部分 agent 因 read 工具输出截断，导致 02 报告的 reconcile 步骤细节较其他模块略粗（已尽量从已有事实重建）。
  2. CAPA 基线 `67de5c2` 用 CAPI v1.13.4，与 CCE 的 CAPI v1.14.0 差 1 个 minor，存在 API 演进差距（详见 §三.1）。
  3. CAPA 是单仓多模式（EKS / ASG / ROSA / Karpenter），本审计只覆盖 EKS managed 模式子集。

### B. 6 个 agent 输出文件清单

```
docs/audit/
├── 00-summary.md                              ← 本文件（汇总）
├── 01-api-types-comparison.md                 (33KB, 487 行)
├── 02-controllers-reconcile-comparison.md     (12KB, 292 行)
├── 03-scope-comparison.md                     (15KB, 240 行)
├── 04-services-comparison.md                  (23KB, 339 行)
├── 05-webhook-conditions-comparison.md        (18KB, 237 行)
└── 06-auxiliary-comparison.md                 (17KB, 282 行)
```

### C. CAPA 文件清单（参考点）

```
/tmp/capa/ (commit 67de5c2, CAPI v1.13.4, v2.10 trunk)
├── api/v1beta2/                            (AWSCluster, AWSManagedCluster, identity, templates)
├── controlplane/eks/
│   ├── api/v1beta2/                       (AWSManagedControlPlane)
│   ├── controllers/                       (awsmanagedcontrolplane_controller.go)
│   └── webhooks/                          (awsmanagedcontrolplane_webhook.go)
├── exp/
│   ├── api/v1beta2/                       (AWSManagedMachinePool, FargateProfile)
│   ├── controllers/                       (awsmanagedmachinepool_controller.go)
│   ├── controlleridentitycreator/         (awscontrolleridentity_controller.go)
│   └── webhooks/                          (awsmanagedmachinepool_webhook.go)
├── controllers/                           (awscluster_controller.go)
├── webhooks/                              (cluster + identity webhooks)
├── pkg/cloud/
│   ├── scope/                             (ClusterScope, ManagedControlPlaneScope, ...)
│   ├── services/                          (18 modules: ec2/elb/eks/network/gc/wait/...)
│   ├── throttle/                          (ServiceLimiters, middleware)
│   └── awserrors/                         (error classification)
├── feature/feature.go                     (13+ feature gates)
├── util/                                  (conditions/defaulting/paused/system)
└── main.go                                (registration)
```

### D. 本项目文件清单（被审计点）

```
cloudnative-cluster-api-provider-cce/
├── api/
│   ├── common/types.go                              (shared types: VPC, Subnet, NodeVolume, ...)
│   ├── infrastructure/v1beta2/                      (CCECluster, MachinePool, identity, templates)
│   └── controlplane/v1beta2/                        (CCEManagedControlPlane + template)
├── controllers/
│   ├── ccecluster_controller.go                     (CCECluster reconciler)
│   ├── ccemanagedcontrolplane_controller.go         (CCEManagedControlPlane reconciler, 871 行)
│   ├── ccemanagedmachinepool_controller.go          (CCEManagedMachinePool reconciler)
│   ├── cceclustercontrolleridentity_controller.go    (identity auto-creator)
│   ├── gc.go                                        (ExternalResourceGC orphan sweeper)
│   ├── kubeconfig_rotation.go                       (30-day proactive rotation)
│   ├── credentials.go                               (credential chain entry)
│   ├── requeue.go                                   (error-classified requeue)
│   ├── events.go                                    (event recorder wrapper)
│   └── setup.go                                     (controller registration)
├── internal/
│   ├── scope/scope.go                               (credentials resolution ONLY, 148 行)
│   ├── conditions/conditions.go                     (14 condition constants)
│   ├── features/features.go                         (3 feature gates)
│   └── services/
│       ├── cce/cce.go                               (CCE SDK client impl, 1757 行)
│       ├── cce/interfaces.go                        (Service interface, 38 方法)
│       ├── network/manager.go                       (VPC/NAT/EIP manager, 707 行)
│       ├── network/throttle.go                      (token bucket)
│       ├── network/validator.go                    (CIDR validator)
│       └── errors/errors.go                         (CCE error classification)
├── cmd/main.go                                      (manager setup)
├── config/
│   ├── crd/                                         (CRD manifests)
│   ├── rbac/role.yaml                               (91 行 RBAC)
│   ├── webhook/                                     (kustomization + manifests + service)
│   └── default/                                     (kustomization)
└── metadata.yaml, PROJECT                           (clusterctl packaging)
```

---

## 七、最终判断

**CCE Provider 与 CAPA EKS managed mode 的差距分布**：
- **核心能力**：✅ 对齐（生命周期、addon、pod-identity、logging、kubeconfig rotation、GC、错误分类退避、限流中间件）
- **架构形态**：🟡 差异（无 scope struct 是有意为之；CCE 是纯 managed-only）
- **API 字段深度**：🟡 部分（CCE 14 condition 比 CAPA 40+ 少；CCE webhook 校验密度比 CAPA 浅 ~10×）
- **云能力差异**：⚪ 非差距（CCE 不支持 IRSA、CCE GC 不扫 ELB 是云能力差异，非设计缺陷）

**建议优先级**：① 修正文档与注释（即时）→ ② 补齐 webhook 校验深度（中短期）→ ③ 评估独立 Addon/AccessEntry CRD 价值（中长期）→ ④ IRSA 支持（视客户需求）。

**无 P0 阻塞项**。本审计确认 `docs/capa-alignment-summary-2026-08-22.md` §A.1 声称的 P0/P1/P2 补齐全部到位，且新增了 archive 未覆盖的 3 个结构性差异（无 scope struct、addon/AccessEntry 内嵌、不支持 IRSA）和 1 个深度差距（webhook 校验密度）。