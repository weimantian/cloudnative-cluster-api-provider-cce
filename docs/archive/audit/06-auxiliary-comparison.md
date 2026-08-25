# 06 — 辅助工程化对标 CAPA 全托管模式（逐行审计）

> 生成日期：2026-08-23
> 对标基准：CAPA（kubernetes-sigs/cluster-api-provider-aws）@ `/tmp/capa` commit `67de5c2`（v2.10 主干，CAPI v1.13.4）
> 本项目：cloudnative-cluster-api-provider-cce @ CAPI v1.14.0
> 审计范围：GC / kubeconfig rotation / credentials resolution / requeue helpers / event recording / feature gates / RBAC / clusterctl packaging / CRD & webhook manifest generation
> 结论口径：本报告仅覆盖 **CCE 与 CAPA managed/EKS 模式** 的辅助工程化层；ROSA 相关的 kubeconfig rotation 仅作对照参考，不视为 CCE 需对齐项。

---

## §1 方法

本报告对 9 个辅助模块逐行核验 CCE 当前代码与 CAPA 对应实现：

| 模块 | CCE 文件（当前代码） | CAPA 对应实现 |
|---|---|---|
| GC | `controllers/gc.go`（261 行） | `pkg/cloud/services/gc/cleanup.go` + `cmd/clusterawsadm/{cmd/gc,gc}` |
| Kubeconfig rotation | `controllers/kubeconfig_rotation.go`（76 行） | ROSA-only（`controlplane/rosa/controllers/rosacontrolplane_controller.go`） |
| Credentials resolution | `internal/scope/scope.go`（148 行） | `pkg/cloud/scope/session.go`（482 行） |
| Requeue helpers | `controllers/requeue.go`（46 行） | 无独立 helper（controller-runtime 退避 + conditions） |
| Event recording | `controllers/events.go`（21 行） | `util/record.Eventf`（CAPI core） |
| Feature gates | `internal/features/features.go`（65 行） | `feature/feature.go`（118 行）+ `feature/gates.go` |
| RBAC | `config/rbac/role.yaml`（91 行） | `config/rbac/role.yaml`（274 行） |
| clusterctl packaging | `metadata.yaml`、`PROJECT`、`config/{default,crd,webhook}` | `metadata.yaml`、`PROJECT`、`config/*` |
| CRD/Webhook 生成 | `config/crd/kustomization.yaml`、`config/webhook/manifests.yaml` | 同左 |

所有结论基于 `read` 到的当前文件原文，不采信 archive 文档的口径。

---

## §2 GC（孤儿资源回收）

### CCE 实现（`controllers/gc.go`）

`GarbageCollector` 是一个**独立周期扫描器**（ticker-based），非删除路径钩子：

- **触发方式**：`Start(ctx)` 立即 `sweep` 一次，然后每 `Interval` tick 一次；实现 `LeaderElectionRunnable`（只在 leader 上跑）。
- **凭证**：账户级 env 凭证（`ResolveCredentials(ctx, client, "", "")` → `CLOUD_SDK_AK/SK`），非 per-cluster Secret/identity。
- **扫描对象（Phase 1）**：`ListClusters` 枚举全部 CCE 集群，`ownedClusterName` 解析 owned tag（`cluster-api-provider-cce.cluster.<name>=owned`），找到「有 owned tag 但 Cluster CR 已不存在」的孤儿集群。
- **删除方式**：`DeleteCluster` 级联选项镜像控制面删除路径 —— `DeleteEVS=true` / `DeleteENI=true` / `DeleteELB=true` / `OnDemandNodePolicy="delete"` / `PeriodicNodePolicy="reset"`。
- **扫描对象（Phase 2）**：`sweepEips` / `sweepVolumes` / `sweepVpcs` / `sweepNatGateways`，删除**不在 DeleteCluster 级联覆盖范围**的独立资源（如 managed NAT EIP、VPC），按 owned tag 白名单过滤。
- **错误处理**：per-resource 聚合，单个删除失败不中断整轮。
- **开关**：`ExternalResourceGC` feature gate（Alpha，默认 false）+ `--gc-region`。

### CAPA 实现

分两处：

1. **`pkg/cloud/services/gc/cleanup.go` — 删除路径驱动**：`ReconcileDelete` 由 `awscluster_controller` 在集群删除时调用。读 `ExternalResourceGCAnnotation`（缺省 "true"）决定是否 GC；`ExternalResourceGCTasksAnnotation` 可指定任务子集（`loadBalancer`/`targetGroup`/`securityGroup`）。`defaultGetResources` 用 **Resource Group Tagging API** 按 `serviceTag`（`ClusterAWSCloudProviderTagKey`）+ `ResourceLifecycleOwned` 枚举资源，`collectFuncs`/`cleanupFuncs` 分阶段收集+清理。
2. **`cmd/clusterawsadm/gc/gc.go` — CLI 工具**：通过 annotation 对集群 Enable/Disable/Configure GC。

### 差异结论

| 维度 | CAPA | CCE |
|---|---|---|
| 架构 | 删除路径驱动（ReconcileDelete）+ CLI 管理 | 独立周期扫描器（LeaderElectionRunnable ticker） |
| 枚举机制 | AWS Resource Group Tagging API | 直接 `ListClusters` + `ListEips`/`ListVolumes`/`ListVpcs`/`ListNatGateways` |
| 覆盖范围 | ELB/NLB、TargetGroup、SecurityGroup（服务型 LB 产物） | 孤儿集群 + EIP/EVS/VPC/NAT（计费资源） |
| 触发 | 集群删除时 | 定时 tick |
| 开关 | annotation 逐集群 opt-out | feature gate + `--gc-region` |

两者针对的资源类型不同（AWS 服务型 LB vs 华为云计费资源），无对等冲突。CCE 缺少的是 **annotation 级逐集群 opt-out**（CAPA `ExternalResourceGCAnnotation`）——CCE 目前是全局 gate，无 per-cluster 开关。

---

## §3 Kubeconfig rotation

### CCE 实现（`controllers/kubeconfig_rotation.go`）

- `kubeconfigRefreshThresholdDays = 30`（证书剩余 <30 天即刷新；证书申请 365 天）。
- `kubeconfigNeedsRefresh`：Secret 缺失 / `value` 缺失 / 解析失败 → 一律 true（重新拉取）。
- `kubeconfigClientCertExpiry`：`clientcmd.Load` 解析 kubeconfig，取第一个含 `ClientCertificateData` 的 authInfo，容忍 base64-PEM / 裸 DER，`x509.ParseCertificate` 取 `NotAfter`。
- 由 `CCEManagedControlPlane` 控制器在 reconcile 时调用，主动刷新 kubeconfig Secret。

### CAPA 实现

**仅 ROSA 模式**（`controlplane/rosa/controllers/rosacontrolplane_controller.go`）：`reconcileExternalAuthBootstrapKubeconfig` 轮询 break-glass credential，写 `ROSA...CredentialExpiryAnnotation`，到期前刷新 bootstrap kubeconfig 与 CAPI 契约 kubeconfig。

**EKS managed 模式无 kubeconfig rotation**（EKS kubeconfig 由 `aws eks update-kubeconfig` 按需生成，无客户端证书到期概念）。

### 差异结论

CCE 的 kubeconfig rotation 是 EKS 模式**没有的对等物**，属 CCE 加分项。CCE 的证书到期驱动（30 天阈值）与 ROSA 的 break-glass 到期驱动机制不同，但目标一致（到期前主动刷新）。无缺失。

---

## §4 Credentials resolution

### CCE 实现（`internal/scope/scope.go`，148 行）

- `ResolveCredentials(ctx, client, namespace, secretName)`：`secretName == ""` 时走 `credentialsFromEnv`；**显式指定 Secret 名但 Secret 缺失 → 报错**（不做静默回退，防 typo 误跑全局账户）。
- `credentialsFromEnv`：`CLOUD_SDK_AK` / `CLOUD_SDK_SK`。
- `ResolveIdentity(ctx, client, namespace, identityRef)`：三种身份 ——
  - `CCEClusterControllerIdentity` → env 凭证（controller default）。
  - `CCEClusterStaticIdentity` → 从 **硬编码 namespace `cloudnative-cluster-api-provider-cce-system`** 读 Secret（`accessKey`/`secretKey`）。
  - `CCEClusterRoleIdentity` → env 凭证 + `AgencyName`（agency 经 identityRef 传入 CreateCluster）。
  - `identityRef == nil` → env 凭证。
- `checkAllowedNamespace`：`allowed == nil` = 任意 namespace；`NamespaceList` 精确匹配；`Selector` 走 LabelSelectorAsSelector 匹配 namespace 标签。三类身份 + webhook 均强制校验。

### CAPA 实现（`pkg/cloud/scope/session.go`，482 行）

- `sessionForClusterWithRegion`：构建 `aws.Config` + `throttle.ServiceLimiters`。
- `ChainCredentialsProvider`（`NewChainCredentialsProvider` / `Retrieve`）：自定义 provider 链。
- `buildProvidersForRef`：**递归解析 `RoleIdentity.Spec.SourceIdentityRef`**（第 275–276 行递归调用），支持身份链。
- `isClusterPermittedToUsePrincipal`：nil=deny、empty=allow 的语义。
- 每服务限流（`newServiceLimiters`）+ session 缓存。

### 差异结论

| 能力 | CAPA | CCE |
|---|---|---|
| SourceIdentityRef 身份链 | ✅ 递归解析 | ❌ 无（`RoleIdentity` 无 SourceIdentityRef 概念） |
| ChainCredentialsProvider | ✅ | ❌（仅 env / Secret 二选一） |
| 每服务限流（scope 层） | ✅ `throttle.ServiceLimiters` | 🟡 依赖 clientCache + SDK 退避（非 scope 层） |
| allowedNamespaces 语义 | ✅ | ✅（nil=any 语义一致） |
| Static identity Secret namespace | 经 identityRef 指定 | 🟡 **硬编码 `cloudnative-cluster-api-provider-cce-system`** |

`SourceIdentityRef` 身份链是 CAPA 特有（IAM role assume-role），CCE 的 agency 模型无此概念，属「不适用」，但 **static identity Secret 硬编码 namespace** 是 CCE 可改进点（CAPA 不硬编码）。

---

## §5 Requeue helpers

### CCE 实现（`controllers/requeue.go`，46 行）

- `requeueAfterForError`：`IsThrottled` → 1m；`IsQuotaExceeded` → 5m；`IsPermissionDenied` → 30m；否则 `defaultRequeue`。
- `resultAfterError`：throttled / quota 错误 → 返回 `ctrl.Result{RequeueAfter: ...}` + **nil error**（避免 controller-runtime 退避覆盖延迟、且错误不被记为 reconcile 失败）；其余错误透传。

### CAPA 实现

无独立 requeue helper；依赖 controller-runtime 默认指数退避 + `util/conditions` 记录状态。

### 差异结论

CCE 的「按错误分类差异化退避」是 CAPA 没有的加分项，与 archive §4.5 声称一致。无缺失。

---

## §6 Event recording

### CCE 实现（`controllers/events.go`，21 行）

`recordEvent(rec, obj, eventtype, reason, messageFmt, args...)`：nil-recorder guard（测试中不设 recorder 时不 panic），否则 `rec.Eventf`。

### CAPA 实现

使用 CAPI core `sigs.k8s.io/cluster-api/util/record` 的 `record.Eventf`，配合 conditions 变化自动发事件。

### 差异结论

CCE 用 k8s 原生 `client-go/tools/record`，CAPA 用 CAPI `util/record` 封装（语义更丰富）。功能对等，无缺失。

---

## §7 Feature gates

### CCE（`internal/features/features.go`，3 个）

| Gate | 默认 | 阶段 |
|---|---|---|
| `NodePoolAutoscaling` | false | Alpha |
| `AutoControllerIdentityCreator` | false | Alpha |
| `ExternalResourceGC` | false | Alpha |

### CAPA（`feature/feature.go`，13 个）

| Gate | 默认 | 阶段 |
|---|---|---|
| `EKS` | true | Beta |
| `EKSEnableIAM` | false | Beta |
| `EKSAllowAddRoles` | false | Beta |
| `EKSFargate` | false | Alpha |
| `EventBridgeInstanceState` | false | Alpha |
| `MachinePool` | true | Beta |
| `MachinePoolMachines` | false | Alpha |
| `AutoControllerIdentityCreator` | **true** | Alpha |
| `BootstrapFormatIgnition` | false | Alpha |
| `ExternalResourceGC` | **true** | Beta |
| `AlternativeGCStrategy` | false | Beta |
| `TagUnmanagedNetworkResources` | true | Alpha |
| `ROSA` | false | Alpha |

### 差异结论

CCE 只保留 3 个与自身能力对应的 gate，其余 CAPA gate（EKS*、Fargate、ROSA、Ignition 等）为 EKS/ROSA 特有，不适用。对齐项（`AutoControllerIdentityCreator`、`ExternalResourceGC`）两者都存在，但**默认值不同**：CAPA 默认 true（Beta），CCE 默认 false（Alpha）——这是保守选择的合理偏差，但若要求「开箱即用」对齐 CAPA，则 CCE 这两项应评估提 Beta + 默认 true。

---

## §8 RBAC（`config/rbac/role.yaml`）

### CCE（91 行，9 条 rule）

| apiGroup | resources | verbs |
|---|---|---|
| `""` | events | create, patch |
| `""` | secrets | create, delete, get, list, patch, update, watch |
| `cluster.x-k8s.io` | clusters, machinepools | get, list, watch |
| `cluster.x-k8s.io` | clusters/status | get |
| `controlplane.cluster.x-k8s.io` | ccemanagedcontrolplanes | create, delete, get, list, patch, update, watch |
| `controlplane.cluster.x-k8s.io` | ccemanagedcontrolplanes/status | get, patch, update |
| `infrastructure.cluster.x-k8s.io` | cceclustercontrolleridentities | create, get, list, watch |
| `infrastructure.cluster.x-k8s.io` | cceclusters, ccemanagedmachinepools | create, delete, get, list, patch, update, watch |
| `infrastructure.cluster.x-k8s.io` | cceclusters/status, ccemanagedmachinepools/status | get, patch, update |

### CAPA（274 行）

明显更广，额外包含：`configmaps`、`namespaces`、`customresourcedefinitions`、`tokenreviews`/`subjectaccessreviews`、`eksconfigs`、`nodeadmconfigs`、`awscluster*` 全家族、`machinepools`/`machines`/`machinesets` 全 verbs、events 的 get/list/watch 等。

### 差异结论

CCE RBAC 是**最小权限集**，覆盖当前 3 个 controller + webhook 所需。缺失项分两类：

1. **不适用**：eksconfigs / nodeadmconfigs / ROSA 等 CAPA 特有资源。
2. **值得关注**：`events` 缺 `get/list/watch`（CAPA 有全 verbs）；无 `cceclustertemplates` / `ccemanagedcontrolplanetemplates` / `ccemanagedmachinepooltemplates` / `cceclusterroleidentities` / `cceclusterstaticidentities` 的 RBAC rule（若 template 三件套 + 身份 CRD 已实现，则 RBAC 未同步覆盖，见 §11）。

---

## §9 clusterctl packaging

| 项 | CAPA | CCE |
|---|---|---|
| `metadata.yaml` releaseSeries | 至 2.13 | 0.1 |
| 契约版本 | v1alpha2 / v1alpha3 / v1alpha4 / v1beta1（多版本并存） | v1beta2（单版本） |
| `PROJECT` | 多 API group（eks + infrastructure + controlplane + iam） | 单 infrastructure group |

CCE 单版本 v1beta2 收敛（与 archive §4.5 一致），符合「新 provider 直接用最新契约」的合理选择。无缺失。

---

## §10 CRD / Webhook / Manifest 生成

### CCE

- `config/crd/kustomization.yaml`：9 个 CRD base（cceclusters、ccemanagedcontrolplanes、ccemanagedmachinepools + 3 个 template + 3 个 identity），`commonLabels: cluster.x-k8s.io/v1beta2: v1beta2`。
- `config/webhook/manifests.yaml`：**5 mutating + 9 validating = 14 个 webhook**。mutating：ccecluster、ccemanagedcontrolplane、ccemanagedcontrolplanetemplate、ccemanagedmachinepool、ccemanagedmachinepooltemplate；validating：ccecluster、cceclustercontrolleridentity、cceclusterroleidentity、cceclusterstaticidentity、cceclustertemplate、ccemanagedcontrolplane、ccemanagedcontrolplanetemplate、ccemanagedmachinepool、ccemanagedmachinepooltemplate。
- `config/default`：manager deployment + webhook + cert-manager。

### CAPA

CRD 数量远多于 CCE（eks + infrastructure + controlplane + iam 多 group，20+ CRD）；webhook 按 group 拆分。

### 差异结论

CCE 的 CRD/webhook 生成是标准 kubebuilder 结构，14 个 webhook 与 9 个 CRD 对应。无缺失；`commonLabels` 版本标签正确（v1beta2）。

---

## §11 Findings summary

按优先级：

**P1 — RBAC 未覆盖已实现的 CRD 类型**
- 代码已实现 `CCEClusterTemplate` / `CCEManagedControlPlaneTemplate` / `CCEManagedMachinePoolTemplate`（template 三件套，archive §4.5 声称 ✅）与 `CCEClusterRoleIdentity` / `CCEClusterStaticIdentity`（身份 CRD），但 `config/rbac/role.yaml` 中**没有这些资源的 rule**。若 controllers/webhook 需对这些类型做 get/list/watch，当前 RBAC 会在部署后报 403。

**P1 — GC 无 per-cluster opt-out**
- CAPA 通过 `ExternalResourceGCAnnotation` 支持逐集群跳过 GC；CCE 只有全局 `ExternalResourceGC` gate + `--gc-region`，无 annotation 级开关。多租户下无法「只给部分集群关 GC」。

**P2 — Static identity Secret namespace 硬编码**
- `internal/scope/scope.go` 将 static identity Secret 固定读 `cloudnative-cluster-api-provider-cce-system` namespace，CAPA 不硬编码。跨 namespace 部署 static identity 会受限。

**P3 — `AutoControllerIdentityCreator` / `ExternalResourceGC` 默认值偏保守**
- CAPA 两者默认 true（Beta），CCE 默认 false（Alpha）。功能对等但「开箱即用」程度低。

---

## §12 Prior-doc claim verification

核验 `docs/capa-alignment-summary-2026-08-22.md` §4.5 / §4.6 / §5 的相关声称：

| Archive 声称 | 本次核验 | 结论 |
|---|---|---|
| §4.5「K8s events ✅ recordEvent 遍布」 | `controllers/events.go` 存在且 nil-guard | ✅ 属实 |
| §4.5「错误分类退避 ✅」 | `controllers/requeue.go` throttle/quota/permission 差异化 | ✅ 属实 |
| §4.5「GC（tag 扫描）✅」 | `controllers/gc.go` owned-tag 孤儿扫描 | ✅ 属实 |
| §4.5「clusterctl 打包 ✅」 | `metadata.yaml` + `config/default` 齐备 | ✅ 属实 |
| §4.5「Template 三件套 ✅」 | CRD/webhook 均存在 | ✅ 属实（但 RBAC 未覆盖，见 §11） |
| **§4.6「feature gates：2 个（NodePoolAutoscaling、AutoControllerIdentityCreator）」** | 当前 `features.go` 有 **3 个**（多 `ExternalResourceGC`） | ❌ **不准确**，应为 3 个 |
| §4.4「凭证回退链 ✅ identityRef → per-cluster Secret → env」 | `scope.go` 的 `ResolveIdentity` 只有 identityRef → env / static Secret，无「identityRef → per-cluster Secret → env」三层链 | 🟡 表述不精确（见下） |

**§4.4 澄清**：archive 声称「CCECluster 也走 identityRef 链」。当前代码事实是 `ResolveIdentity` 处理 identityRef 三类身份（controller→env / static→Secret / role→env+agency），而 per-cluster `<cluster>-credentials` Secret 走的是独立的 `ResolveCredentials`（`secretName != ""` 路径）。两者是**并列的两条路径**，而非「identityRef → Secret → env」的单链回退。若 archive 意指「CCECluster 现在也能通过 identityRef 解析凭证」，则该结论成立；若字面理解为三层回退链，则与代码不符。

**核心 discrepancy**：§4.6 的 gate 数量（2 → 3）是唯一硬性不准确点，其余 §4.5 声称均经逐行核验属实。
