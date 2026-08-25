# 03 — Scope 层对标 CAPA 全托管模式（逐字段审计）

> 生成日期：2026-08-23
> 对标基准：CAPA（kubernetes-sigs/cluster-api-provider-aws）@ `/tmp/capa` commit `67de5c2`
> 本项目：cloudnative-cluster-api-provider-cce
> 审计范围：Scope 层（per-object scope 结构、云客户端面、凭证链、allowedNamespaces 语义、patch/Close 模式）
> 结论口径：本报告仅覆盖 **CCE 与 CAPA managed/EKS 模式** 的 scope 层；不覆盖 Karpenter / ROSA scope。

---

## §1 架构总览：一条根本性差异

| 维度 | CAPA | CCE |
|---|---|---|
| Scope 结构 | 4 个 per-object scope struct：`ClusterScope`、`ManagedControlPlaneScope`、`MachinePoolScope`、`GlobalScope` | **无任何 per-object scope struct** |
| `internal/scope` / `pkg/cloud/scope` 职责 | 承载 logger + client + patchHelper + CR 引用 + session + serviceLimiters + controllerName | **仅凭证解析**：`Credentials` / `ResolveCredentials` / `credentialsFromEnv` / `ResolveIdentity` / `checkAllowedNamespace`（`scope.go` 共 148 行） |
| patch helper | 内置于 scope：`patchHelper` 字段 + `Close()` / `PatchObject()`（含 condition 汇总 SetSummary） | 内联在 controller 的 Reconcile 里：`patch.NewHelper` + `defer patchHelper.Patch` |
| 云 session/限流 | `session.go`（482 行）：session 缓存 + provider 缓存 + `ChainCredentialsProvider` + `throttle.ServiceLimiters` | 无独立 session 层；client 缓存 `sync.Map`（region+ak+sk 为 key）+ 依赖 SDK 退避重试 |

**根本结论**：CCE 把 CAPA 的 scope 层职责**拆散**到 controller（patch helper、client 构造）与 `internal/scope`（仅凭证）两处。`internal/scope` 的包注释声称"Pattern follows CAPA pkg/cloud/scope and CAPHW pkg/scope (patch helper + Close() = PatchObject)"——**与当前代码事实不符**（该文件无任何 scope struct、无 patch helper、无 Close），属于误导性注释。

---

## §2 字段级对比（scope struct）

### CAPA `ClusterScope`（`pkg/cloud/scope/cluster.go:97`）

| 字段 | 类型 | 说明 |
|---|---|---|
| logger | `logger.Logger` | 结构化日志（.WithName/.WithValues） |
| client | `client.Client` | management cluster k8s client |
| patchHelper | `patch.Helper` | 对象 patch |
| Cluster | `*clusterv1.Cluster` | CAPI core Cluster |
| AWSCluster | `*infrav1.AWSCluster` | infra cluster CR |
| session | `aws.Config` | 复用 AWS SDK session |
| serviceLimiters | `throttle.ServiceLimiters` | 每服务限流 |
| controllerName | `string` | 控制器名（session key 一部分） |
| tagUnmanagedNetworkResources | `bool` | 是否给非托管网络打 tag |
| maxWaitActiveUpdateDelete | `client.ObjectKey` | 等待活跃更新/删除上限 |

关键方法：`Network()`、`VPC()`、`Subnets()`、`SetSubnets()`、`IdentityRef()`、`CNIIngressRules()`、`SecurityGroupOverrides()`、`Session()`、`ServiceLimiter()`、`ControllerName()`、`PatchObject()`、`Close()`。

### CAPA `ManagedControlPlaneScope`（`managedcontrolplane.go:117`）

| 字段 | 类型 | 说明 |
|---|---|---|
| logger / Client / patchHelper | — | 同 ClusterScope |
| Cluster | `*clusterv1.Cluster` | CAPI core Cluster |
| ControlPlane | `*ekscontrolplanev1.AWSManagedControlPlane` | EKS 控制面 CR |
| MaxWaitActiveUpdateDelete | `client.ObjectKey` | 更新等待上限 |
| session / serviceLimiters / controllerName | — | 同 ClusterScope |
| enableIAM | `bool` | IAM 角色开关 |
| allowAdditionalRoles | `bool` | 附加角色开关 |
| tagUnmanagedNetworkResources | `bool` | 同上 |

关键方法：`RemoteClient()`、`Network()`、`VPC()`、`Session()`、`ServiceLimiter()`、`IdentityRef()`。

### CAPA `MachinePoolScope`（`machinepool.go:47`）

| 字段 | 类型 | 说明 |
|---|---|---|
| logger / client / patchHelper | — | 同 ClusterScope |
| capiMachinePoolPatchHelper | `patch.Helper` | 额外 CAPI MachinePool patch helper |
| Cluster | `*clusterv1.Cluster` | |
| MachinePool | `*clusterv1.MachinePool` | |
| InfraCluster | `scope.EC2Scope` | 复用 EC2 scope（网络） |
| AWSMachinePool | `*expinfrav1.AWSMachinePool` | |

### CAPA `GlobalScope`（`global.go:54`）

| 字段 | 类型 | 说明 |
|---|---|---|
| session | `aws.Config` | 全局（GC 等）复用 session |
| serviceLimiters | `throttle.ServiceLimiters` | |
| controllerName | `string` | |

### CCE —— **无 scope struct**

`internal/scope/scope.go` 只有：

```go
type Credentials struct { AccessKey, SecretKey string }

func ResolveCredentials(ctx, c, namespace, secretName) (*Credentials, error)
func credentialsFromEnv() (*Credentials, error)
func ResolveIdentity(ctx, c, namespace, identityRef) (*Credentials, string, error)
func checkAllowedNamespace(ctx, c, allowed, namespace, identityName) error
```

Controller 侧内联状态（无统一封装）：

- `CCEClusterReconciler`：`client` + `Recorder` + `NetworkValidatorFactory` + `NetworkServiceFactory`
- `CCEManagedControlPlaneReconciler`：`client` + `Recorder` + `ServiceFactory`
- `CCEManagedMachinePoolReconciler`：`client` + `Recorder` + `ServiceFactory`

> CCE 无 `ManagedNodeGroupScope` 等价物。CAPA 侧 `grep "type ManagedNodeGroupScope"` 亦无结果——CAPA 的 managed node group 由 EKS service 层（`pkg/cloud/services/eks`）处理，不设独立 scope；CCE 的 node pool 同理由 `cce` service 处理。此点两边一致，不构成差距。

---

## §3 云客户端面（client count per scope）

### CAPA — 13 个 AWS SDK 服务客户端（`pkg/cloud/scope/clients.go`）

| # | 服务 | ServiceID |
|---|---|---|
| 1 | autoscaling | autoscaling |
| 2 | ec2 | ec2 |
| 3 | eks | eks |
| 4 | elb | elasticloadbalancing |
| 5 | elbv2 | elasticloadbalancingv2 |
| 6 | eventbridge | eventbridge |
| 7 | iam | iam |
| 8 | resourcegroupstaggingapi | resourcegroupstaggingapi |
| 9 | s3 | s3 |
| 10 | secretsmanager | secretsmanager |
| 11 | sqs | sqs |
| 12 | ssm | ssm |
| 13 | sts | sts |

`AWSClients` struct 实际承载：`ELB`、`SecretsManager`、`ResourceTagging`、`ASG`、`EC2`、`ELBV2`。

### CCE — 5 个华为云 SDK 服务（无独立 scope，分布两处）

| # | 服务 | 使用位置 |
|---|---|---|
| 1 | CCE v3（`cce/v3`） | `internal/services/cce/cce.go` `Client` |
| 2 | EIP v2（`eip/v2`） | `cce.Client` + `network.Manager` |
| 3 | EVS v2（`evs/v2`） | `cce.Client` |
| 4 | VPC v2（`vpc/v2`） | `cce.Client` + `network.Manager` |
| 5 | NAT v2（`nat/v2`） | `cce.Client` + `network.Manager` |

- `internal/services/cce/cce.go`：`type Client struct`（第 41 行）持有 5 个 SDK client；`NewClient(regionID, ak, sk)`（第 59 行）用 `sync.Map` 按 `region+ak+sk` 缓存。
- `internal/services/network/manager.go`：`Manager{vpc, nat, eip}`；`NewManager(regionID, ak, sk)` 每次新建（无缓存），供 controller 内联调用。

**差异量化**：CCE 云客户端 5 个（CCE/EIP/EVS/VPC/NAT）vs CAPA 13 个（含 ELB/ELBV2/S3/SSM/STS/EventBridge/SQS/IAM/RGAPI/SecretsManager）。缺口主要对应 CAPA 的 ELB（负载均衡）、IAM/STS（角色凭证）、SecretsManager、S3（IRSA pod identity）、SSM、EventBridge、SQS 等 CCE 用不同机制实现（CCE 用 ELB 由平台托管、pod identity 用 CCE PodIdentityAssociation、凭证用 AK/SK Secret + agency）。

---

## §4 凭证链（credential resolution）对比

### CAPA `session.go`（482 行）凭证链

1. `sessionForRegion`：按 region 缓存 session（`sync.Map`）。
2. `sessionForClusterWithRegion`：按 `region-controllerName-infraClusterName-namespace` 缓存（`getSessionName`），带 provider hash 缓存。
3. `buildProvidersForRef`：按 identityRef Kind 递归构建 provider 链——
   - `ControllerIdentityKind`：校验 singleton name，返回空 provider 列表（落到 controller principal）。
   - `ClusterStaticIdentityKind`：读 Secret，构造 static provider，并给 Secret 打 ownerRef。
   - `ClusterRoleIdentityKind`：校验 allowedNamespaces → 若有 `SourceIdentityRef` 递归解析 → 构造 `NewAWSRolePrincipalTypeProvider`（assume-role 链）。
4. `ChainCredentialsProvider.Retrieve`：依次尝试 providers，取首个非空。
5. `newServiceLimiters`：为 EC2/ELB/ELBV2/RGAPI/SecretsManager 配置 per-operation RefillRate/Burst（EC2 有 RunInstances 2/5、StartInstances 2/5 等细粒度限流）。

### CCE `scope.go`（148 行）凭证链

1. `ResolveCredentials`：`secretName==""` → env（`CLOUD_SDK_AK`/`CLOUD_SDK_SK`）；否则读 `<ns>/<secretName>` Secret（keys `accessKey`/`secretKey`），缺失即报错（**不静默回退**，避免跨租户风险）。
2. `ResolveIdentity`：按 identityRef Kind——
   - nil → env creds。
   - `CCEClusterControllerIdentity` → 校验 allowedNamespaces → env creds。
   - `CCEClusterStaticIdentity` → 校验 allowedNamespaces → 读 `capi-cce-system` 命名空间下 Secret（**硬编码命名空间**）。
   - `CCEClusterRoleIdentity` → 校验 allowedNamespaces → env creds + `agencyName`（**无 SourceIdentityRef 递归、无 provider 链**）。
   - default → 报错。
3. 无限流结构；依赖 SDK 自带 backoff + controller 退避重试（README 记载 APIGW.0308 429 场景）。

**差异点**：
- CAPA RoleIdentity 支持 `SourceIdentityRef` 递归 assume-role 链；CCE RoleIdentity 仅"env creds + agency name"，无链式解析。
- CAPA StaticIdentity Secret 命名空间取自 `system.GetManagerNamespace()`；CCE 硬编码 `capi-cce-system`。
- CAPA 有 per-service throttle limiters；CCE 无。

---

## §5 allowedNamespaces 语义对比（⚠️ 语义反转）

| 条件 | CAPA `isClusterPermittedToUsePrincipal`（session.go:409） | CCE `checkAllowedNamespace`（scope.go:125） |
|---|---|---|
| 资源为 cluster-scoped | 直接放行（bypass） | 无此分支 |
| `allowedNamespaces == nil`（未设置） | **拒绝**（返回 false） | **放行**（"any namespace"） |
| 空 struct `{}`（显式设置但为空） | **放行所有命名空间** | **拒绝**（"no namespace"） |
| NamespaceList 命中 | 放行 | 放行 |
| Selector 命中 | 放行 | 放行 |
| Selector 为空 | 拒绝 | 拒绝 |

**这是本层最严重的安全相关差距（P0）**：

1. **nil 语义反转**：CCE 把"未设置 `allowedNamespaces`"解释为"任何命名空间可用"；CAPA 解释为"任何命名空间都不可用"（默认拒绝）。意味着在 CCE 中，用户一旦**忘记设置** `allowedNamespaces`，该 identity 即对全集群命名空间开放，违背最小权限。
2. **注释误导**：`scope.go` 与 `identity_types.go` 均注释"nil pointer means any namespace (CAPA contract)"——**与 CAPA 实际契约相反**（CAPA 的 nil=deny，空 struct=allow）。这是一个事实性错误注释，会诱导后续维护者按错误契约写代码。
3. **缺 cluster-scoped bypass**：CAPA 对 cluster-scoped 资源跳过 allowedNamespaces 检查；CCE 无此逻辑（CCE 当前无 cluster-scoped identity 使用场景，但语义上不完整）。

---

## §6 patch helper / Close() / conditions 汇总

| 能力 | CAPA | CCE |
|---|---|---|
| patch helper 位置 | scope 字段，跨 reconcile 复用 | controller Reconcile 内联 `patch.NewHelper` + `defer Patch` |
| `Close()` / `PatchObject()` | 有，`PatchObject` 先 `SetSummary`（step counter）再 patch owned conditions | **无**（conditions 由 `internal/conditions` 包单独处理） |
| 对象生命周期汇总 | scope 一次构造，贯穿整个 reconcile | 无统一对象；credentials + client 按需构造 |
| 会话/限流复用 | scope 持有 session + limiters | `sync.Map` client 缓存（无 limiter） |

---

## §7 与现有文档的差异（仅基于当前代码事实）

> 按审计约束，本报告不将 `docs/capa-alignment-summary-2026-08-22.md` 与 `docs/archive/` 作为事实基准，仅记录**当前代码内部自相矛盾或与 CAPA 源码不符**之处：

1. `internal/scope/scope.go` 包注释声称"Pattern follows CAPA pkg/cloud/scope ... (patch helper + Close() = PatchObject)"，但该文件**不含任何 scope struct / patch helper / Close()**。→ 注释需修正为"凭证解析 helpers"。
2. `internal/scope/scope.go:122-124` 与 `api/.../identity_types.go` 注释"nil pointer means any namespace (CAPA contract)"，与 CAPA `session.go` 实际契约（nil=deny, empty=allow）**相反**。
3. README「Identity management」声称"mirroring CAPA's three identities"——三种 identity 类型确实齐全（ControllerIdentity/StaticIdentity/RoleIdentity），但 RoleIdentity 缺 `SourceIdentityRef` 链式解析，未完全 mirror。

---

## §8 结论：差距清单（P0/P1/P2）

### P0（安全相关 / 阻塞性）

1. **allowedNamespaces nil 语义反转**（§5）：未设置 `allowedNamespaces` 时 CCE 放行所有命名空间，CAPA 默认拒绝。修复：`checkAllowedNamespace` 中 nil → 拒绝，空 struct `{}` → 放行（对齐 CAPA）；同步修正两处错误注释。
2. **缺 cluster-scoped 资源 bypass**（§5）：对齐 CAPA `isClusterScopedResource` 分支。
3. **无 per-service 限流（throttle.ServiceLimiters）**（§4）：CAPA 对 EC2/ELB/ELBV2/RGAPI/SecretsManager 配置细粒度 RefillRate/Burst；CCE 完全依赖 SDK 退避。高并发 reconcile 时可能触发华为云 APIGW.0308 429 限流风暴（当前仅靠 backoff 硬抗）。

### P1（功能差距 / 重要补充）

1. **RoleIdentity 无 SourceIdentityRef 链式 assume-role**（§4）：仅"env creds + agency name"，无法表达嵌套角色链。
2. **StaticIdentity Secret 命名空间硬编码** `capi-cce-system`（§4）：应取 manager namespace（对齐 CAPA `system.GetManagerNamespace()`），否则部署到非默认命名空间时失效。
3. **无 per-object scope struct**（§1/§2）：导致 session/limiter/logger/patchHelper 无法跨 reconcile 复用，controller 重复内联构造；也偏离包注释承诺的 CAPA 模式。

### P2（结构性改进）

1. `network.Manager` 每次 `NewManager` 新建 3 个 SDK client（无缓存），与 `cce.Client` 的 `sync.Map` 缓存策略不一致。
2. 缺 `GlobalScope` 等价物：GC sweeper 直接构造 client，无统一 session 复用。
3. 包注释 / 字段注释多处与实现不符（§7），建议统一修订。

---

## 附录：输出路径与依据

- **本报告输出路径**：`docs/audit/03-scope-comparison.md`
- 关键代码依据：
  - CCE `internal/scope/scope.go`（148 行）、`internal/scope/scope_test.go`
  - CCE `api/infrastructure/v1beta2/identity_types.go`
  - CCE `controllers/ccecluster_controller.go` / `ccemanagedcontrolplane_controller.go` / `ccemanagedmachinepool_controller.go`
  - CCE `internal/services/cce/cce.go`、`internal/services/cce/interfaces.go`、`internal/services/network/manager.go`
  - CAPA `pkg/cloud/scope/{cluster,managedcontrolplane,machinepool,global,clients,session}.go`
