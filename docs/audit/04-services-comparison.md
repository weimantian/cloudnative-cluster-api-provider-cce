# CCE Provider Services 层对标 CAPA 审计报告

> 生成日期：2026-08-23
> 对标基准：CAPA（kubernetes-sigs/cluster-api-provider-aws）@ `/tmp/capa` commit `67de5c2`（v2.10 主干，CAPI v1.13.4）
> 本项目：cloudnative-cluster-api-provider-cce @ CAPI v1.14.0
> 审计范围：Services 层（CCE/Network/Errors 服务 vs CAPA 50+ 服务模块）
> 审计方法：逐文件核验当前代码，不依赖历史对比文档结论

---

## §1 审计方法

本报告对 CCE Provider 与 CAPA 的 **Services 层** 做逐模块能力对标。审计过程：

1. **枚举**：`ls`/`glob` 全量枚举两侧服务目录与源文件，确认文件清单与行数。
2. **签名提取**：`grep "^func"` 提取两侧全部公开方法签名，逐条对齐能力。
3. **机制核验**：对限流、等待、GC、校验、错误分类五类横切机制，直接读取实现源码（非文档、非注释摘要）。
4. **接口/测试性核验**：读取接口定义、mock 生成产物、测试文件清单。
5. **§10 交叉验证**：将 `docs/capa-alignment-summary-2026-08-22.md` 与 `docs/archive/capa-comparison-review-2026-08.md` 中关于 Services 层的断言，逐条回读到当前代码验证。

**范围界定**：本报告只覆盖 `internal/services/**`（CCE 侧）与 `/tmp/capa/pkg/cloud/services/**` + 相邻横切包（`pkg/cloud/throttle`、`pkg/cloud/awserrors`、`pkg/cloud/scope/session.go` 限流配置）。Controllers 层的 Reconcile 流程见 `02-controllers-reconcile-comparison.md`；Scope 层见 `03-scope-comparison.md`。

---

## §2 服务模块目录映射

### 2.1 CCE Provider（`internal/services/`，6 个非测试源文件）

| 文件 | 行数 | 职责 |
|---|---|---|
| `cce/cce.go` | ~1757 | CCE 集群/节点池/升级/addon/pod-identity/访问策略的 SDK 客户端实现（`Client`） |
| `cce/interfaces.go` | ~532 | `Service` 接口（38 方法）+ 全部输入/输出数据类型 |
| `errors/errors.go` | ~180 | CCE 官方错误码常量 + 7 个分类谓词 |
| `network/manager.go` | ~707 | VPC/NAT/EIP 托管网络编排（`Manager`） |
| `network/throttle.go` | ~81 | 客户端 token-bucket 限流（`throttleRoundTripper`） |
| `network/validator.go` | ~242 | 托管网络预检（CIDR/子网/ENI 校验） |

> 注：CCE 无独立 `wait`/`gc` 服务包——等待逻辑内联在 controllers（`requeue.go`、`pollUpgradeTask`）；GC 在 `controllers/gc.go` + Service 层 `List*/Delete*` 方法上。

### 2.2 CAPA（`/tmp/capa/pkg/cloud/services/`，18 个子模块）

| 模块 | 职责 |
|---|---|
| `interfaces.go` | 11 个按服务切分的接口（ASG/EC2/ELB/Network/SecurityGroup/ObjectStore/AWSNode/IAMAuthenticator/KubeProxy/Secret/MachinePoolReconcile） |
| `eks/` | EKS 托管控制面（`Service`/`NodegroupService`/`FargateService`，30+ 文件） |
| `network/` | VPC/子网/NAT/路由/网关/EIP/ENI（17 文件，`ReconcileNetwork`/`DeleteNetwork`） |
| `gc/` | 外部资源 GC（ELB/ELBv2/SecurityGroup tag 扫描清理） |
| `wait/` | 指数退避 + 可重试错误等待工具 |
| `autoscaling/`、`ec2/`、`elb/`、`securitygroup/` | 非托管资源（ASG/实例/LB/安全组） |
| `s3/`、`secretsmanager/`、`ssm/`、`sts/` | 对象存储/密钥/参数/STS |
| `userdata/`、`awsnode/`、`kubeproxy/`、`iamauth/`、`instancestate/` | 节点引导/系统组件/实例状态 |
| `mock_services/` | 12 个 mockgen 生成的接口 mock |

### 2.3 横切包

| 关注点 | CCE | CAPA |
|---|---|---|
| 限流 | `internal/services/network/throttle.go` | `pkg/cloud/throttle/throttle.go` + `pkg/cloud/scope/session.go` 配置 |
| 错误分类 | `internal/services/errors/errors.go` | `pkg/cloud/awserrors/errors.go` |

### 2.4 映射结论

CCE 是 **纯托管（managed）** provider，对标 CAPA 的 **EKS managed mode** 子集。CAPA 的 `ec2/elb/securitygroup/autoscaling/s3/secretsmanager/ssm/sts/userdata/awsnode/kubeproxy/iamauth/instancestate` 模块在 CCE 中 **无对等物**（这些由 CCE 平台服务端托管，provider 不直接管理节点/网络附属资源）。CCE 的 6 个服务文件映射到 CAPA 的 `eks` + `network` + `gc` + `wait` + `throttle` + `awserrors` 六类能力。

---

## §3 逐服务能力对比

### 3.1 集群控制面（CCE `cce.go` ↔ CAPA `eks/`）

CCE `Service` 接口（`interfaces.go:434` 起，38 方法）覆盖以下控制面能力，逐条映射 CAPA `eks/` 对应实现：

| CCE 方法 | CAPA 对应 | 状态 |
|---|---|---|
| `ShowCluster` | `describeEKSCluster` (`cluster.go:872`) | ✅ 对等 |
| `CreateCluster` | `createCluster` (`cluster.go:434`) | ✅ 对等 |
| `DeleteCluster` | `deleteCluster` + `deleteClusterAndWait` (`cluster.go:251/281`) | ✅ 对等（CCE 用级联选项，见 3.6） |
| `GetClusterKubeconfig` | `reconcileKubeconfig`/`createCAPIKubeconfigSecret` (`config.go`) | ✅ 对等 |
| `ShowQuotas` | —（CAPA 无运行时配额查询） | ➕ CCE 独有 |
| `ListClusters` | —（GC 用；CAPA 无集群列表） | ➕ 供 GC |
| `CreateNodePool`/`ScaleNodePool`/`UpdateNodePool`/`DeleteNodePool`/`ListNodePools` | `NodegroupService` (`eks.go`/`nodegroup.go`) | ✅ 对等 |
| `UpgradeNodePool` | `reconcileNodegroupVersion` (`nodegroup.go:320`) | ✅ 对等 |
| `ListNodes`/`ListNodesWithStatus`/`ResetNode` | —（CAPA 非托管走 EC2；CCE 托管节点平台 API） | ✅ 对等（CCE 平台能力） |
| `GetUpgradeInfo`/`StartUpgrade`/`ShowUpgradeTask` | `reconcileClusterVersion` (`cluster.go:784`) | ✅ 对等 |
| `Create/Update/List/DeleteAddonInstance` | `reconcileAddons` (`addons.go`) | ✅ 对等 |
| `Create/List/DeletePodIdentityAssociation` | `reconcilePodIdentityAssociations` (`pod_identities.go`) | ✅ 对等 |
| `Create/Update/List/DeleteAccessPolicy` | `reconcileAccessEntries`/`reconcileAccessPolicies` (`accessentry.go`) | ✅ 对等（CCE AccessPolicy 语义 ≈ EKS AccessEntry+Policy） |
| `UpdateClusterLogConfig`/`ShowClusterLogConfig` | `reconcileLogging` (`cluster.go:652`) | ✅ 对等 |

**CAPA 有、CCE 无（`eks/` 内）**：`reconcileOIDCProvider`/`reconcileTrustPolicy`（`oidc.go`）、`reconcileControlPlaneIAMRole`/`deleteControlPlaneIAMRole`（`roles.go`）、`reconcileSecurityGroups`（`securitygroup.go`）、`reconcileIdentityProvider`（`identity_provider.go`）、Fargate 全套。这些对应 CAPA 的 OIDC 信任策略、IAM 角色托管、安全组、Fargate——CCE 平台以不同方式承接（OIDC 走 kube-apiserver 参数、角色走 agency identityRef、安全组/超节点走平台托管），**非 provider 缺失，而是云能力差异**（与 `01-api-types` 结论一致）。

### 3.2 网络托管（CCE `network/manager.go` ↔ CAPA `network/`）

CCE `ManagerInterface`（`manager.go:83-93`，4 方法）：

| CCE | CAPA | 状态 |
|---|---|---|
| `ReconcileVpc` | `reconcileVPC` (`vpc.go:49`) | ✅ 对等（含收养模式） |
| `ReconcileSubnets` | `reconcileSubnets` (`subnets.go:51`) | ✅ 对等 |
| `ReconcileNatGateway` | `reconcileNatGateways` (`natgateways.go:43`) | ✅ 对等（含 EIP+SNAT） |
| `DeleteNetwork` | `DeleteNetwork` (`network.go:103`) | ✅ 对等（依赖序 SNAT→NAT→EIP→子网→VPC，聚合错误） |

**CCE 裁剪**（CAPA `network/` 有，CCE 无）：`reconcileRouteTables`、`reconcileInternetGateways`、`reconcileCarrierGateway`、`reconcileEgressOnlyInternetGateways`、`associateSecondaryCidrs`、`reconcileVPCEndpoints`、ENI 清理。这些对应 AWS 专有网络拓扑（IGW/路由表/网关/端点）——CCE 托管网络模型无对等物，属于 **⚪ 裁剪**。

### 3.3 错误分类（CCE `errors/` ↔ CAPA `awserrors/`）

| 谓词 | CCE (`errors.go`) | CAPA (`awserrors/errors.go`) |
|---|---|---|
| 未找到 | `IsNotFound` (L84) | 60+ `*NotFound` 常量 |
| 冲突 | `IsConflict` (L96) | `ResourceExists`/`ResourceNotFound` |
| 权限拒绝 | `IsPermissionDenied` (L115) | `AuthFailure`/`UnauthorizedOperation` |
| 限流 | `IsThrottled` (L129) | `RequestLimitExceeded`/`Throttling` |
| 配额超限 | `IsQuotaExceeded` (L142) | —（AWS 无对等单码） |
| 错误码提取 | `ServiceResponseError` (L164) | `ParseSmithyError`/`Code` |
| 幂等扩展 | `IsScaleNoOp` (L177) | —（CCE 独有：扩缩容 no-op 判定） |

**结论**：CCE 分类谓词 7 个，覆盖 CAPA 全部语义，并多出 `IsQuotaExceeded`（CCE 独有配额码族，26 个 `CCE.014000xx` 常量）与 `IsScaleNoOp`。CCE 用华为官方错误码（`CCE.014xxxxx`），CAPA 用 AWS 错误码，映射等价。

### 3.4 限流（CCE `throttle.go` ↔ CAPA `throttle.go`）

| 维度 | CCE | CAPA |
|---|---|---|
| 机制 | `operationLimiter` 双桶（读/写） | `ServiceLimiter`→`OperationLimiter` 按操作正则匹配 |
| 读写分离 | ✅ GET/HEAD 为读，其余为写 | ✅ 按 Operation 名匹配（Describe/Get/List 为读） |
| 读速率 | 20 ops/s，burst 100 | 20/s，burst 100（generic）/ EC2 差异化 |
| 写速率 | 10 req/min（6s/token），burst 10 | 5/s burst 200（generic `.*`）/ EC2 `RunInstances` 2/s burst 5 |
| 注入方式 | `throttleRoundTripper` 包裹 `http.DefaultTransport` | middleware（`Finalize` 栈） |
| **作用范围** | **仅 VPC/NAT/EIP（network 客户端）** | **全 SDK 服务**（session.go 按 ServiceID 配置） |
| 响应自适应 | ❌ 无 | ✅ `ReviewResponse` 遇 `Throttling`/`RequestLimitExceeded` 重置 token |

**关键差异（见 §4）**：CCE 集群 API 客户端（`cce.go:NewClient`）**未**接入 `throttleRoundTripper`——`NewClient` 与 `buildAuxClients` 均用 `config.DefaultHttpConfig()`（无自定义 RoundTripper）。限流仅覆盖托管网络客户端。

### 3.5 等待/轮询（CCE 内联 ↔ CAPA `wait/`）

见 §5。

### 3.6 GC（CCE `controllers/gc.go`+Service ↔ CAPA `gc/`）

见 §6。

### 3.7 校验（CCE `validator.go` ↔ CAPA webhook/scope）

见 §7。

---

## §4 限流与重试

### 4.1 CAPA 实现

- **配置**（`scope/session.go:170-220`）：`ServiceLimiters map[string]*ServiceLimiter`，按 AWS ServiceID 注册。generic 服务：`Describe/Get/List` 20/s burst 100 + `.*` 5/s burst 200；EC2 额外为 `RunInstances`/`StartInstances` 设 2/s burst 5。
- **匹配**（`throttle.go:Match`）：用 `regexp` 对 SDK 操作名做前缀匹配。
- **自适应**（`throttle.go:ReviewResponse`）：响应错误码为 `Throttling`/`RequestLimitExceeded` 时 `ResetTokens()`，立即清空桶。
- **注入**（`throttle.go:WithServiceLimiterMiddleware`）：插入 middleware `Finalize` 栈，全 SDK 请求生效。

### 4.2 CCE 实现

- **机制**（`network/throttle.go`）：`operationLimiter` 双桶，读 `rate.NewLimiter(20, 100)`，写 `rate.NewLimiter(rate.Every(6s), 10)`。
- **注入**（`manager.go:NewManager`）：仅 `Manager`（VPC/NAT/EIP）用 `httpConfig.WithHttpRoundTripper(newThrottleRoundTripper(...))`。
- **重试退避**（`controllers/requeue.go`）：错误分类驱动 `RequeueAfter`——throttle 1min、quota 5min、permission 30min；throttle/quota **不返回 error**（`resultAfterError`），避免 controller-runtime backoff 覆盖延迟。

### 4.3 差距

| # | 差距 | 严重度 |
|---|---|---|
| G4-1 | **CCE 集群 API（`cce.go`）无客户端限流**，仅依赖服务端 429（`APIGW.0308`/`CCE.01429002/003`）后由 `requeueAfterForError` 退避 1min。相比 CAPA 全 SDK 主动限流，CCE 在集群 CRUD 高峰期会把限流压力完全推给服务端。 | **P1** |
| G4-2 | CCE 限流无「响应自适应重置 token」机制（CAPA `ReviewResponse`）。收到 429 后不会主动降速，而是靠 controller 退避。 | P2 |
| G4-3 | 读写分离粒度：CCE 按 HTTP 方法（GET/HEAD vs 其余），CAPA 按操作名正则（可区分 RunInstances 等重操作）。CCE 粒度较粗但够用（托管网络只有 Create/Delete/List）。 | P3 |

> **已达标项**：错误分类退避（NotFound/Conflict/Throttle/Quota/PermissionDenied/ScaleNoOp 六类 + 差异化退避）已实现且与 CAPA 语义等价；`resultAfterError` 正确避免 controller-runtime backoff 覆盖自定义延迟。

---

## §5 等待/轮询辅助

### 5.1 CAPA `wait/`

- `NewBackoff()`（`wait.go:26`）：`Duration: 1s, Factor: 1.71, Steps: 10, Jitter: 0.4` → 指数退避总时长约 5 分钟。
- `WaitForWithRetryable(backoff, condition, retryableErrors...)`（`wait.go:45`）：`wait.ExponentialBackoff` + 可重试错误码匹配（含 smithy 错误解析），重试期满返回实际错误。
- 消费方：`eks/cluster.go` 的 `waitForClusterActive`/`deleteClusterAndWait`、`nodegroup.go` 的 `waitForNodegroupActive`/`deleteNodegroupAndWait`。

### 5.2 CCE 实现（无独立 wait 包）

- **NAT 网关轮询**（`network/manager.go:40` `pollInterval = 5s`）：`time.Sleep(5s)` 固定间隔同步轮询（`manager.go:557`）。
- **升级任务轮询**（`ccemanagedcontrolplane_controller.go:308` `pollUpgradeTask`）：`ShowUpgradeTask` 轮询 + `RequeueAfter`。
- **通用重试**：`controllers/requeue.go` 的 `RequeueAfter`（`defaultRequeue` / 错误分类退避），依赖 reconcile 重入而非 in-reconcile 等待。

### 5.3 差距

| # | 差距 | 严重度 |
|---|---|---|
| G5-1 | **无可复用的指数退避等待工具**（无 `NewBackoff`/`WaitForWithRetryable` 对等物）。删除/就绪等待依赖 `RequeueAfter` 重入或 `time.Sleep` 固定间隔，无 jitter、无重试错误码匹配、无超时语义。 | **P1** |
| G5-2 | `time.Sleep(5s)` 阻塞式轮询（`manager.go:557`）不可取消（未传 ctx），长轮询期间控制器无法响应取消。 | P2 |

> 注：CCE 的 `RequeueAfter` 模型（错误分类→延迟）本身是 controller-runtime 惯用法，与 CAPA 的 in-reconcile wait 是两种风格。差距在于 CCE **缺少一个像 `wait.WaitForWithRetryable` 那样带超时+可重试错误分类的通用轮询原语**，导致升级/删除轮询逻辑在控制器内散落、不可复用。

---

## §6 垃圾回收（GC）

### 6.1 CAPA `gc/`

- `Service`（`gc/service.go:43`）：持 ELB/ELBv2/ResourceGroupsTaggingAPI/EC2 客户端 + `cleanupFuncs`/`collectFuncs` 两个函数集合。
- **策略模式**：`addDefaultCleanupFuncs`（deleteLoadBalancers/deleteTargetGroups/deleteSecurityGroups）vs `addAlternativeCollectFuncs`（`WithGCStrategy(true)` 时用 provider-owned 精确扫描）。
- 入口：`ReconcileDelete`（`cleanup.go:43`）→ `deleteResources`（`cleanup.go:65`）→ collect 出 tag 匹配资源 → 逐个 cleanup。
- 覆盖资源：ELB/ELBv2 LoadBalancer、TargetGroup、SecurityGroup（`ec2.go`/`loadbalancer.go`）。

### 6.2 CCE 实现

- **孤儿集群 GC**（`controllers/gc.go`）：`GarbageCollector` 周期性扫 owned-tag 检测孤儿资源，`ServiceFactory func(regionID, ak, sk string) (cceService.Service, error)`。
- **Service 层扫删原语**（`cce/interfaces.go`）：`ListClusters`/`ListEips`/`ListVolumes`/`ListVpcs`/`ListNatGateways` + 对应 `DeleteEip`/`DeleteVolume`/`DeleteVpc`/`DeleteNatGateway`。
- **级联删除**（`cce.go:DeleteCluster`）：`DeleteClusterInput` 显式 7 个级联选项（`DeleteEVS/ENI/ELB/EFS/OBS/SFS/SFS30`）+ `OnDemandNodePolicy`/`PeriodicNodePolicy`——平台侧一次性级联清理。

### 6.3 对比结论

| 维度 | CAPA | CCE | 状态 |
|---|---|---|---|
| 孤儿资源扫描 | tag 扫描（ELB/ELBv2/SG） | owned-tag 扫描（EIP/EVS/VPC/NAT/集群） | ✅ 对等（资源面不同） |
| 可插拔策略 | `ResourceCollectFuncs`/`CleanupFuncs` 函数集合 | `ServiceFactory` | ✅ 对等 |
| 级联清理 | 依赖 GC 服务扫 tag（无平台级级联） | **`DeleteCluster` 7 个级联选项平台原生级联** | ➕ CCE 优势（更内聚） |
| 覆盖资源类型 | ELB/ELBv2/SG（AWS 附属） | EIP/EVS/VPC/NAT（CCE 附属） | 等价（云能力差异） |

**结论**：CCE GC 覆盖了 CAPA GC 的语义（孤儿资源 tag 扫描 + 可插拔工厂），并通过 `DeleteCluster` 平台级级联删除了 CAPA 需要 GC 服务事后补扫的附属资源。**无功能性缺口**；差异仅在资源类型集合（云能力决定）。

---

## §7 校验

### 7.1 CCE `network/validator.go`

- `ValidatorInterface.Validate(ctx, ValidateInput) ([]Issue, error)`（L62/95）。
- 校验项：CIDR 格式（`validCIDR` L239）、CIDR 重叠（`cidrsOverlap` L227）、子网归属、`maxENISubnets = 20`（L36）、`fetchNetwork`（L202）回读云侧网络。
- 消费方：托管网络创建前预检（创建前失败，比 CAPA 更早）。

### 7.2 CAPA 对应

CAPA **无独立服务层 validator**——网络/字段校验分散在 webhook（见 `05-webhook-conditions-comparison.md`）与 scope 层。服务层无创建前预检。

### 7.3 结论

CCE 的 `validator.go` 是 **➕ 相对 CAPA 的加分项**（创建前预检，提前失败）。无差距。

---

## §8 服务接口与可测试性

### 8.1 接口切分

| 维度 | CCE | CAPA |
|---|---|---|
| 接口数量 | 3（`Service` 38 方法 / `ManagerInterface` 4 / `ValidatorInterface` 1） | 11（ASG/EC2/ELB/Network/SG/ObjectStore/AWSNode/IAMAuthenticator/KubeProxy/Secret/MachinePoolReconcile） |
| 切分粒度 | 单一大接口 `Service` | 按服务/职责切分的小接口 |
| 合理性 | CCE 是单一云 API，单接口可辩护；但 38 方法导致 mock 成本高 | 高内聚小接口，mock 精准 |

### 8.2 Mock 与测试

| 维度 | CCE | CAPA |
|---|---|---|
| 生成 mock | ❌ 无 mockgen 产物，无 mock 文件 | ✅ `mock_services/` 12 个 mockgen 生成 mock |
| 测试文件 | `cce_test.go`/`smoke_test.go`/`errors_test.go`/`manager_test.go`/`throttle_test.go`/`validator_test.go`（6 个） | 各模块 `_test.go`（Ginkgo 全家桶） |
| 测试风格 | go test 原生 + fakes + envtest | Ginkgo + mockgen + envtest |

### 8.3 结论

| # | 差距 | 严重度 |
|---|---|---|
| G8-1 | **无生成的接口 mock**（`Service` 38 方法无 mockgen mock），单测需手写 fake 或 envtest，隔离性弱于 CAPA。 | P2 |
| G8-2 | `Service` 单一大接口（38 方法）vs CAPA 11 个小接口；可测试性与可替换性弱。 | P2（架构倾向，非阻塞） |

---

## §9 差距汇总（P0–P3）

> 说明：本报告未发现 **P0（阻塞性）** 缺口——CCE Services 层对 CAPA EKS managed 子集的覆盖完整（控制面/网络/错误分类/GC/校验五类能力均到位）。以下按优先级列出非阻塞差距。

| 优先级 | # | 差距 | 位置 |
|---|---|---|---|
| **P1** | G4-1 | CCE 集群 API 无客户端限流（限流仅覆盖 network 客户端），依赖服务端 429 + controller 退避 | `cce.go:NewClient` |
| **P1** | G5-1 | 无可复用指数退避等待原语（无 `NewBackoff`/`WaitForWithRetryable` 对等物） | 等待逻辑散落 controllers/network |
| **P2** | G4-2 | 限流无响应自适应（收到 429 不主动降速，CAPA `ReviewResponse`） | `throttle.go` |
| **P2** | G5-2 | `time.Sleep(5s)` 阻塞轮询不可取消（未传 ctx） | `network/manager.go:557` |
| **P2** | G8-1 | 无生成接口 mock（mockgen 缺位） | `internal/services/**` |
| **P2** | G8-2 | `Service` 单一大接口（38 方法），切分粒度粗 | `interfaces.go` |
| **P3** | G4-3 | 限流粒度按 HTTP 方法而非操作名（CAPA 可按操作名精细限流） | `throttle.go` |

**已达标（无差距）**：
- 错误分类退避六类 + 差异化 RequeueAfter ✅（§4）
- 托管网络三态（创建/收养/BYO）+ 依赖序删除 + 聚合错误 ✅（§3.2）
- GC 孤儿扫描 + 可插拔工厂 + 平台级级联删除 ✅（§6，CCE 级联是优势）
- 创建前网络预检 ✅（§7，CCE 加分项）
- 集群/节点池/addon/pod-identity/升级/log/访问策略全能力对齐 ✅（§3.1）

---

## §10 既有对比文档断言交叉验证

将 `docs/capa-alignment-summary-2026-08-22.md`（下称「summary」）与 `docs/archive/capa-comparison-review-2026-08.md`（下称「review-0820」）中 Services 层相关断言回读当前代码。

### 10.1 验证为 ✅ 的断言（与代码一致）

| 断言来源 | 断言内容 | 代码证据 | 结论 |
|---|---|---|---|
| summary L72 | 「客户端主动限流：`throttleRoundTripper` 包裹 `http.DefaultTransport`，读（GET/HEAD）20 ops/s burst 100，写（其余）10 req/min burst 10」 | `throttle.go:32-44` 精确一致 | ✅ 属实 |
| summary L73 | 「`internal/services/network/manager.go` 统一注入限流 transport」 | `manager.go:NewManager` `WithHttpRoundTripper(newThrottleRoundTripper(...))` | ✅ 属实（且准确限定为 network 客户端） |
| summary L174 | 「错误分类退避 NotFound/Conflict/Throttle/Quota/PermissionDenied/ScaleNoOp，差异化退避」 | `errors.go` 7 谓词 + `requeue.go` 六类退避 | ✅ 属实 |
| summary L179 | 「GC（tag 扫描）孤儿集群 + EIP/EVS/VPC/NAT 扫删」 | `controllers/gc.go` + `List/Delete{Eip,Volume,Vpc,NatGateway}` | ✅ 属实 |
| summary L52 | 「GC 扫 VPC/NAT（N+1 查 tag + NAT 先删 SNAT）」 | Service 层 `ListVpcs`/`ListNatGateways` + `DeleteNatGateway`（SNAT→NAT 序） | ✅ 属实 |
| review-0820 L149 | 「单元测试 go test 原生 + fakes + envtest」 | `cce_test.go`/`manager_test.go` 等 6 测试文件 | ✅ 属实 |

### 10.2 需要修正/补充的断言

| 断言来源 | 断言内容 | 本审计结论 | 差异 |
|---|---|---|---|
| summary L72 措辞 | 隐含「限流中间件」为全服务通用 | **实际仅 network（VPC/NAT/EIP）客户端接入**；`cce.go:NewClient` 用 `DefaultHttpConfig()` 无 RoundTripper | summary 未明示范围，但措辞「统一注入」易被读作全服务。本审计标注为 **G4-1（P1）**——集群 API 客户端限流缺位 |
| review-0820 L149 | 「测试覆盖主要分支，但直接调 Reconcile，掩盖 watch 缺失」 | 本报告 §8 补充：除 watch 外，**无生成 mock（mockgen 缺位）** 是独立可测试性缺口（G8-1） | review 未提及 mock 缺位 |
| summary L174 与 L180 | 分别列「错误分类退避」「限流中间件」为独立 ✅ | 均属实，但两文档均未指出 **限流与错误分类分属两处**：限流是客户端主动（仅 network），错误分类是服务端被动（全服务）。 | 补充澄清 |

### 10.3 结论

两份既有文档关于 Services 层的 **正面断言（限流参数、注入点、错误分类、GC、测试风格）均与当前代码一致，无虚假 ✅**。本审计的净增量是：

1. **澄清限流作用域**：限流中间件仅覆盖 network 客户端，集群 API 客户端（`cce.go`）未限流（→ 新 P1 G4-1）。
2. **补一个 P1**：缺可复用指数退避等待原语（G5-1）。
3. **补两个 P2**：限流响应自适应缺位（G4-2）、`time.Sleep` 不可取消（G5-2）。
4. **补可测试性差距**：无 mockgen mock（G8-1）、单一大接口（G8-2）。

---

## 附录：本报告对照文件清单

**CCE 侧（审计对象）**：
- `internal/services/cce/cce.go`、`internal/services/cce/interfaces.go`
- `internal/services/errors/errors.go`
- `internal/services/network/manager.go`、`throttle.go`、`validator.go`
- `controllers/gc.go`、`controllers/requeue.go`、`controllers/ccemanagedcontrolplane_controller.go`（pollUpgradeTask）

**CAPA 侧（基准）**：
- `pkg/cloud/services/interfaces.go`、`eks/*.go`、`network/*.go`、`gc/*.go`、`wait/wait.go`、`mock_services/`
- `pkg/cloud/throttle/throttle.go`、`pkg/cloud/awserrors/errors.go`、`pkg/cloud/scope/session.go`
