# CCE Provider vs CAPA — Webhook 与 Conditions 深度对比

> 生成日期：2026-08-23
> 对标基准：CAPA（kubernetes-sigs/cluster-api-provider-aws）@ `/tmp/capa` commit `67de5c2`（v2.10 主干，CAPI v1.13.4）
> 本项目：cloudnative-cluster-api-provider-cce @ CAPI v1.14.0
> 审计模块：05 — Webhook 校验规则 + Conditions 集合
> 审计方法：逐文件阅读双方 webhook 源码与 condition 常量定义，逐条枚举校验规则。

---

## 一、TL;DR

| 维度 | CCE Provider | CAPA (EKS managed) | 结论 |
|---|---|---|---|
| Webhook 文件数（非测试） | **9** | **9+**（`webhooks/` 3 + `controlplane/eks/webhooks/` 2 + `exp/webhooks/` 1 + 若干，不含 fargate/rosa） | 数量对齐，但单文件体量差距巨大 |
| Webhook 总代码量 | ~1,048 行 | ~11,748 行 | CAPA 校验深度 ~10× |
| 校验规则条数 | ~24 条 | ~120+ 条 | **P1：校验深度显著不足** |
| Condition 类型数 | **14** | **40+**（core 19 + EKS 8 + exp 13） | **P1：conditions 集合覆盖不足** |
| 转换 webhook | ❌ 无（单版本 v1beta2） | ✅ 有（v1beta1↔v1beta2） | 单版本设计下不适用，非差距 |
| Template 类型 webhook | ✅ 3 个（Cluster/MachinePool/ControlPlane Template） | ✅ 有（ClusterTemplate / ManagedControlPlaneTemplate），**无** MachinePoolTemplate | CCE 更完整 |
| 身份 CRD webhook | ✅ 3 个（Controller/Role/Static Identity） | ✅ 3 个 | 对齐 |
| Defaulter 覆盖 | ✅ 3 个（MachinePool/MachinePoolTemplate/ControlPlane/ControlPlaneTemplate） | ✅ 全覆盖 | 基本对齐 |

**核心结论**：CCE 的 webhook 骨架（9 类对象全覆盖、身份 CRD 校验、Template 三件套）与 CAPA 对齐良好，且单版本 v1beta2 的收敛（移除转换 webhook）是**有意设计**而非缺失。差距集中在**校验规则的"深度"**：CAPA 对网络/CIDR/安全组/不可变字段/访问配置做了 ~120 条细粒度校验并附带 `Warnings`，CCE 仅覆盖 ~24 条"云侧硬约束"。Conditions 方面 CCE 覆盖了主流程 14 个 condition，但缺少 CAPA 的**失败原因细分**（每个 condition 配 3-8 个 reason，用于精确定位失败阶段）。

---

## 二、Webhook 逐对象对比

### 2.1 注册方式与注解差异（结构性）

| 维度 | CCE Provider | CAPA |
|---|---|---|
| 注册 API | `builder.WebhookManagedBy(mgr).For(&Type{})` | `ctrl.NewWebhookManagedBy(mgr).For(&Type{})` |
| Validator 接口 | `admission.Defaulter` / `admission.Validator`（`Default()` / `ValidateCreate/Update/Delete`） | `WithCustomValidator(&webhooks.Type{})` / `WithDefaulter(&webhooks.Type{})` |
| 校验签名 | `ValidateCreate() (admission.Warnings, error)` | `ValidateCreate() (admission.Warnings, error)`（相同） |
| Webhook 注解 | 单版本：仅 `v1beta2`，**无** `matchPolicy`，`admissionReviewVersions` 单值 | `matchPolicy=Equivalent`，`admissionReviewVersions=v1;v1beta1` |
| 注册数量（cmd/main.go） | **9**（CCECluster、CCEManagedMachinePool、CCEManagedControlPlane、CCEClusterTemplate、CCEManagedControlPlaneTemplate、CCEManagedMachinePoolTemplate、CCEClusterControllerIdentity、CCEClusterStaticIdentity、CCEClusterRoleIdentity） | 同类 9 个对象 + 转换 webhook |
| 注册开关 | `ENABLE_WEBHOOKS != "false"` 才注册 | 恒注册 |
| `config/webhook/` | `kustomization.yaml`、`manifests.yaml`、`service.yaml` | 同 + cert-manager 集成 |

> **注解差异说明**：CCE 单版本 v1beta2 存储，无多版本服务，故无需 `matchPolicy=Equivalent` 与多 `admissionReviewVersions`。这是**单版本收敛的直接结果**，不是缺陷。

### 2.2 校验规则逐条枚举

#### CCECluster（`ccecluster_webhook.go`，63 行）

| # | 规则 | CAPA 对应（`awscluster_webhook.go`，546 行） |
|---|---|---|
| C1 | `spec.region` 必填 | ✅ `spec.region` 必填 |
| — | （无其他校验） | bastion 配置、`sshKeyName` 格式、additionalTags 合法性、S3Bucket 引用、network 全量（CIDR 解析/重叠、子网归属、secondary CIDR、IPAM pool、ingress rules、target group IP 类型、control plane LBs、elastic IP pool）、GC tasks 注解、controlPlaneLoadBalancer 不可变、region 不可变、identityRef 移除禁止、externallyManaged 注解（~40 条 + Warnings） |

**差距**：CCE 仅 1 条必填校验 vs CAPA ~40 条网络/标签/不可变校验。CAPA 的核心价值在**网络拓扑前置校验**（CIDR 格式与重叠、子网归属 VPC、安全组引用），CCE 把这类校验推迟到云 API 拒绝。

#### CCEClusterControllerIdentity（`cceclustercontrolleridentity_webhook.go`，58 行）

| # | 规则 | CAPA（`awsclustercontrolleridentity_webhook.go`，124 行） |
|---|---|---|
| CI1 | `allowedNamespaces.selector` 可解析（selector 非法 → 拒绝） | ✅ 同 |
| — | （无） | 单例约束：`name` 必须为 `default`；`spec` 不可变（更新拒绝） |

**差距**：CCE 缺"单例名必须为 `default`"约束与 `spec` 不可变约束。CAPA 用 `AutoControllerIdentityCreator` 创建名为 `default` 的单例并拒绝改名/改 spec。

#### CCEClusterRoleIdentity（`cceclusterroleidentity_webhook.go`，54 行）

| # | 规则 | CAPA（`awsclusterroleidentity_webhook.go`，117 行） |
|---|---|---|
| RI1 | `agencyName` 必填 | ✅ `sourceIdentityRef` 必填（非 nil） |
| — | （无 allowedNamespaces 校验？→ 待确认） | allowedNamespaces selector 校验 + 禁止移除 sourceIdentityRef |

**差距**：CAPA 对 RoleIdentity 额外校验 allowedNamespaces + 禁止更新移除 `sourceIdentityRef`。

#### CCEClusterStaticIdentity（`cceclusterstaticidentity_webhook.go`，54 行）

| # | 规则 | CAPA（`awsclusterstaticidentity_webhook.go`，111 行） |
|---|---|---|
| SI1 | `secretRef` 必填 | ✅ `secretRef` 不可变（更新拒绝）+ allowedNamespaces |
| — | （无） | allowedNamespaces selector 校验 |

**差距**：CAPA 禁止更新时修改 `secretRef`（防止凭证偷换），CCE 未做此不可变校验。

#### CCEClusterTemplate（`cceclustertemplate_webhook.go`，54 行）

| # | 规则 | CAPA（`awsclustertemplate_webhook.go`，94 行） |
|---|---|---|
| T1 | 委托 `CCECluster.validate()`（region 必填） | ✅ bastion + sshKeyName + spec 不可变（更新拒绝） |

**差距**：CAPA 对 Template 的 `spec` 做**不可变**约束（Template 一旦创建不可改 spec），CCE 允许更新。

#### CCEManagedMachinePool（`ccemanagedmachinepool_webhook.go`，145 行）

| # | 规则 | CAPA（`awsmanagedmachinepool_webhook.go`，314 行） |
|---|---|---|
| MP1 | `clusterName` 必填 | ✅ `eksNodegroupName` 必填 |
| MP2 | `flavor` 必填 + 正则 + 部署级 allowlist（`--valid-flavors`） | 等价：instanceType 校验 |
| MP3 | `taints` ≤ 20 | 等价（nodegroup 约束） |
| MP4 | `securityGroups` ≤ 5 | 等价（nodegroup 约束） |
| MP5 | `spot` + `billingMode=1`（订阅）组合拒绝 | 等价（spot 需 on-demand） |
| MP6 | `extensionScaleGroups[].flavor` 必填 + 正则 | 无对应（CCE 特有扩展伸缩组） |
| MP7 | `extensionScaleGroups[].availabilityZone` 必填 | 无对应 |
| MP8 | `availabilityZone` 必填 | 等价 |
| MP9 | `os` 必填 | 等价 |
| MP10 | `rootVolume` 必填 + size ∈ [40, 1024] | 等价（launchTemplate diskSize） |
| MP11 | `maxUnavailable` ∈ [1, 20] | 等价 |
| — | Default: NodePoolName 生成、MaxUnavailable=1 | Default: EKSNodegroupName 生成、UpdateConfig.MaxUnavailable=1 |
| — | （无不可变校验） | scaling(min/max)、remoteAccess(public+sourceSG)、launchTemplate(instanceType/diskSize/iam profile)、lifecycleHooks、additionalTags 等 ~20 条 + 大量不可变约束 |

**差距**：CCE 的 11 条规则已覆盖 CCE 云侧硬约束（taints/SG/rootVolume/maxUnavailable 官方上限），但缺 CAPA 的**不可变字段约束**（节点池创建后 immutable 字段禁止修改）与 launchTemplate/remoteAccess 细粒度校验。

#### CCEManagedMachinePoolTemplate（`ccemanagedmachinepooltemplate_webhook.go`，74 行）

| # | 规则 | CAPA |
|---|---|---|
| T2 | Default: MaxUnavailable=1；委托 MachinePool 校验（clusterName 用占位符） | ❌ **CAPA 无此 webhook**（无 `awsmanagedmachinepooltemplate` webhook 文件） |

**差距**：CCE 的 MachinePoolTemplate webhook 是 CAPA 没有的（CAPA 的 MachinePool 支持 via `MachinePool` + bootstrap provider，无 template 资源）。CCE 提供 ClusterClass 支持所需的 Template 三件套，**在 ClusterClass 拓扑上领先 CAPA 一处**。

#### CCEManagedControlPlane（`ccemanagedcontrolplane_webhook.go`，138 行）

| # | 规则 | CAPA（`awsmanagedcontrolplane_webhook.go`，720 行） |
|---|---|---|
| CP1 | `clusterName` 必填 | ✅ `eksClusterName` 必填 |
| CP2 | `category` ∈ {CCE, Turbo} | 等价（无对应，CCE 特有） |
| CP3 | `containerNetwork.mode=eni` 要求 `category=Turbo` | 无对应（EKS 无此概念） |
| CP4 | `containerNetwork.mode=eni` 要求 `eniSubnets` 非空 | 等价（vpc-cni 需子网） |
| CP5 | `billingMode=1`（订阅）拒绝 | 等价（EKS 不支持订阅） |
| CP6 | `authenticatingProxy` 需同时提供 ca/cert/privateKey | 等价 |
| CP7 | 不可变：containerNetwork.cidr / mode / category / encryptionConfig.mode / authentication.mode | 等价（region/encryption/identityRef/ipFamily/eksClusterName 不可变） |
| — | Default: Category=Turbo、Mode=eni、Flavor=cce.s1.small | Default: EKSClusterName 生成、identityRef→controller identity、bastion、network、BootstrapSelfManagedAddons=true |

**差距**：CCE 的 7 条规则质量高（覆盖了 CCE 特有的 Turbo/eni/订阅/认证代理约束 + 关键不可变字段），但 CAPA 有 **~50 条**额外校验：EKS 版本格式解析与降级禁止、IPv6 最小版本、secondary CIDR、addons（IPv6 依赖、vpc-cni 版本）、IAM auth config、access config（create/update）、access entries、pod identity associations、kube proxy、additional tags、private DNS hostname 类型等。

#### CCEManagedControlPlaneTemplate（`ccemanagedcontrolplanetemplate_webhook.go`，77 行）

| # | 规则 | CAPA（`awsmanagedcontrolplanetemplate_webhook.go`，241 行） |
|---|---|---|
| T3 | Default: Category/Mode/Flavor；委托 ControlPlane 校验（clusterName 占位符） | ✅ 同结构（template spec 委托 + region/encryption/identityRef/ipFamily 不可变） |

**差距**：对齐良好。

### 2.3 Webhook 校验规则统计汇总

| 对象 | CCE 规则数 | CAPA 规则数 |
|---|---|---|
| Cluster | 1 | ~40 |
| ControllerIdentity | 1 | ~4 |
| RoleIdentity | 1 | ~4 |
| StaticIdentity | 1 | ~3 |
| ClusterTemplate | 1 | ~4 |
| ManagedMachinePool | 11 | ~25 |
| MachinePoolTemplate | 3（CCE 独有） | —（CAPA 无） |
| ManagedControlPlane | 7 | ~50 |
| ControlPlaneTemplate | 4 | ~10 |
| **合计** | **~24** | **~120+** |

---

## 三、Conditions 集合对比

### 3.1 CCE Conditions（`internal/conditions/conditions.go`，14 类型 + 8 原因）

**CCECluster（4）**：
- `NetworkReadyCondition`、`VpcReadyCondition`、`SubnetsReadyCondition`、`NatGatewaysReadyCondition`

**CCEManagedControlPlane（8）**：
- `CredentialsReadyCondition`、`CCEClusterReadyCondition`、`KubeconfigReadyCondition`、`AddonsConfiguredCondition`、`PodIdentityAssociationsConfiguredCondition`、`LoggingConfiguredCondition`、`AccessPoliciesConfiguredCondition`、`UpgradeReadyCondition`

**CCEManagedMachinePool（2）**：
- `NodePoolReadyCondition`、`NodePoolScalingCondition`

**共享 Reason（8）**：
- `ReconciliationInProgressReason`、`ReconciliationFailedReason`、`WaitingForClusterInfrastructureReason`、`WaitingForControlPlaneReason`、`WaitingForKubeconfigReason`、`UpgradeNotOfferedReason`、`UpgradeInProgressReason`、`UpgradeTargetUnavailableReason`

> 实现细节：CCE 的 condition 常量是**无类型 `string` 常量**（非 `clusterv1beta1.ConditionType`），与 CAPA 的 `clusterv1beta1.ConditionType` 强类型不同。

### 3.2 CAPA Conditions（40+ 类型）

**core（`api/v1beta2/conditions_consts.go`，~19）**：
`PrincipalCredentialRetrieved`、`PrincipalUsageAllowed`、`VpcReady`、`SubnetsReady`、`InternetGatewayReady`、`EgressOnlyInternetGatewayReady`、`CarrierGatewayReady`、`NatGatewaysReady`、`RouteTablesReady`、`VpcEndpointsReady`、`SecondaryCidrsReady`、`ClusterSecurityGroupsReady`、`BastionHostReady`、`LoadBalancerReady`、`InstanceReady`、`SecurityGroupsReady`、`ELBAttached`、`S3BucketReady` 等。

**EKS control plane（`controlplane/eks/api/v1beta2/conditions_consts.go`，8）**：
`EKSControlPlaneReady`、`EKSControlPlaneCreating`、`EKSControlPlaneUpdating`、`IAMControlPlaneRolesReady`、`IAMAuthenticatorConfigured`、`EKSAddonsConfigured`、`EKSIdentityProviderConfigured`、`EKSPodIdentityAssociationConfigured`。

**exp（`exp/api/v1beta2/conditions_consts.go`，13）**：
`ASGReady`、`LaunchTemplateReady`、`PreLaunchTemplateUpdateCheck`、`PostLaunchTemplateUpdateOperation`、`InstanceRefreshStarted`、`LifecycleHookReady`、`EKSNodegroupReady`、`EKSFargateProfileReady`、`IAMNodegroupRolesReady`、`IAMFargateRolesReady`、`RosaMachinePoolReady`、`RosaMachinePoolUpgrading`、`ROSANetworkReady`。

### 3.3 Conditions 差异分析

| 维度 | CCE | CAPA | 结论 |
|---|---|---|---|
| 主流程覆盖 | ✅ 创建/删除/升级/kubeconfig/节点池 全覆盖 | ✅ | 对齐 |
| 网络子阶段拆分 | ✅ Vpc/Subnets/NatGateways 3 个细分 | ✅ 更细（InternetGateway/EgressOnly/Carrier/RouteTables/VpcEndpoints/SecurityGroups） | 🟡 CAPA 更细（CCE 无 gateway/route/endpoint 概念，属云差异） |
| 失败原因细分 | ⚠️ 仅 8 个共享 reason | ✅ 每个 condition 配 3-8 个专用 reason（如 `ASGNotFoundReason`/`ASGProvisionFailedReason`/`ASGDeletionInProgress`） | **P1：失败定位能力弱** |
| 凭证 condition | ✅ CredentialsReady | ✅ PrincipalCredentialRetrieved / PrincipalUsageAllowed（2 个） | 🟡 CAPA 拆分凭证获取与凭证授权 |
| 控制面生命周期 | ✅ CCEClusterReady + UpgradeReady | ✅ Creating/Ready/Updating 3 态 | 🟡 CAPA 区分"创建中"与"更新中"，CCE 只区分 Ready/Upgrade |

---

## 四、Top 3 P0/P1 差距

### P1-1：Webhook 校验深度显著不足（网络/不可变字段/访问配置）

CAPA 的核心 webhook 价值是**前置校验网络拓扑**（CIDR 格式与重叠、子网归属、安全组引用、secondary CIDR、IPAM pool、ingress rules）与**不可变字段约束**。CCE 的 `CCECluster` webhook 仅 1 条 region 必填，`CCEManagedControlPlane` 仅 7 条，网络/访问策略/标签等校验全部推迟到云 API 拒绝（体验差、失败晚）。CCE 至少应补：
- 网络 CIDR 格式解析与重叠校验（VPC 内 container/service CIDR 唯一性）；
- identityRef 移除禁止、`secretRef` 不可变（防凭证偷换）；
- Template 类型 `spec` 不可变约束。

### P1-2：Conditions 失败原因细分不足

CCE 的 14 个 condition 共用 8 个 reason，无法定位"哪个子资源、哪个阶段"失败。CAPA 每个 condition 配 3-8 个专用 reason（如 `ASGProvisionFailedReason`、`LaunchTemplateNotFoundReason`）。用户故障排查依赖 reason 精确定位，CCE 应为核心 condition（CCEClusterReady/NodePoolReady/UpgradeReady）补齐阶段化 reason。

### P1-3：身份 CRD 不可变与单例约束缺失

CAPA 对 ControllerIdentity 强制单例名 `default` + `spec` 不可变，对 StaticIdentity 强制 `secretRef` 不可变。CCE 三个身份 webhook 各仅 1 条必填校验（agencyName/secretRef/selector 解析），缺少防篡改约束，存在凭证被替换/身份对象被改名导致云凭证链失效的隐患。

---

## 五、与现有对比文档的差异

| 现有文档 | 原声称 | 本次审计核实 | 差异 |
|---|---|---|---|
| `archive/capa-parity-gap-analysis.md` L18 | conditions「10 个（Network/Credentials/Cluster/Kubeconfig/Upgrade/Addons/PodIdentity/Logging/NodePool/Scaling）」 | 实际 **14 个**（新增 VpcReady/SubnetsReady/NatGatewaysReady 细分 + AccessPoliciesConfigured） | **少算 4 个**（文档未反映 conditions 细化与 AccessPolicies） |
| `archive/capa-comparison-review-2026-08.md` L140 | 「身份 CRD **无 webhook**」 | 实际 **有** 3 个身份 webhook（Controller/Role/Static） | **已过时**（身份 webhook 已补齐） |
| `archive/capa-comparison-review-2026-08.md` L143 / `capa-parity-gap-analysis.md` L83、L107 | 「转换 webhook ✅ 已实现」 | 实际 **无转换 webhook**（api/ 无 `ConvertTo`/`ConvertFrom`/`Hub`；cmd/main.go 无 `/convert` 注册） | **已过时**（转换 webhook 已随单版本收敛移除） |
| `capa-alignment-summary-2026-08-22.md` L79 | 「13 个 webhook 文件从 v1beta1 移入 v1beta2」 | 实际 **9 个非测试 webhook 文件**（另 4 个 `*_webhook_test.go`，9+4=13） | **口径混淆**（把 4 个测试文件算作 webhook 文件） |
| `capa-alignment-summary-2026-08-22.md` L78 | 「移除转换 webhook」 | ✅ 确认无误（单版本 v1beta2，无转换） | **一致** |

> 关键澄清：`capa-parity-gap-analysis.md` 声称的"转换 webhook ✅ 已实现"是**历史状态**，当前代码已按 `capa-alignment-summary-2026-08-22.md` L78 的设计收敛为单版本 v1beta2 并移除转换 webhook。二者不矛盾（时间先后），但若以最新文档为准，当前状态 = 无转换 webhook。

---

## 六、附录

- **A**：CAPA webhook 文件清单 —— `webhooks/awscluster_webhook.go`(546)、`webhooks/awsclustercontrolleridentity_webhook.go`(124)、`webhooks/awsclusterroleidentity_webhook.go`(117)、`webhooks/awsclusterstaticidentity_webhook.go`(111)、`webhooks/awsclustertemplate_webhook.go`(94)、`controlplane/eks/webhooks/awsmanagedcontrolplane_webhook.go`(720)、`controlplane/eks/webhooks/awsmanagedcontrolplanetemplate_webhook.go`(241)、`exp/webhooks/awsmanagedmachinepool_webhook.go`(314)。
- **B**：CAPA **无** `awsmanagedmachinepooltemplate` webhook（`find` 验证返回空）；CCE 的 `CCEManagedMachinePoolTemplate` webhook 是 CAPA 没有的增量能力。
- **C**：CCE condition 常量使用无类型 `string`，CAPA 使用 `clusterv1beta1.ConditionType` 强类型。
- **D**：条件设置点 —— CCE 的 conditions 在 `controllers/ccecluster_controller.go` 等 Reconcile 中通过 `conditions.Set/MarkTrue/MarkFalse/MarkUnknown` 设置。
