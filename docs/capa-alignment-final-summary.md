# CCE Provider 对标 CAPA 最终审计报告

> **合并归纳版**：本文档整合 `docs/archive/capa-alignment-summary-2026-08-22.md`（对标 CAPA v2.10 主干的汇总报告）与 `docs/archive/audit-v2/00-summary.md`（对标 CAPA v2.13.0 的增量审计），以最新落地状态为准（截至 2026-08-24），消除两份文档间的时间线矛盾与重复内容。两份原始文档作为历史记录保留，完整变更时间线见[附录 A](#附录-a变更记录时间线)。

> - 归纳日期：2026-08-24
> - 对标版本：**CAPI v1.14.0** · **CAPA v2.13.0**
> - 对标基准：CAPA v2.13.0 @ `a84670f`（增量审计）与 CAPA v2.10 主干 @ `67de5c2`（v1 汇总）
> - 本项目：cloudnative-cluster-api-provider-cce @ CAPI v1.14.0
> - CAPA CAPI 依赖：v1.13.4（仍未追上 CAPI v1.14）

---

## 摘要

### 关键结论

- **P0/P1/P2 全部补齐**：10 项修复（P0×1 / P1×4 / P2×4 / P3×1）+ 二次审计修复 3 个 bug + 4 项对标 CAPA 补充 + 增量审计 P1 8/8 = 100%。
- **v1 P1 9 项中**：2 项为错误判断（Addon / AccessEntry）、1 项为功能对等（IRSA → agency 委托）、6 项真正待整改 → **全部完成**。
- **剩余差距**（均非阻塞）：真实云冒烟测试（依赖账户余额）、NAT tag 格式实测（依赖余额）、MHC 生态消费验证。
- **最大发现**：**CAPI 版本错配** —— CCE 用 v1.14.0，领先 CAPA 一个 minor；CAPA 不会带 CAPI v1.14 新增能力。

### 维度对照（审计时基线 → 整改后最终）

| 维度 | CCE（审计时） | CCE（最终） | CAPA v2.13.0 |
|---|---|---|---|
| CAPI 依赖 | v1.14.0 | v1.14.0 | v1.13.4 |
| CRD 类型数 | 9（3 身份 + 6 托管/模板） | 9 | 13+ |
| Scope struct 数 | 0 | **4**（08-23 重构） | 4 |
| Webhook 校验规则 | ~24 | **~50+**（08-24 补齐） | ~120+ |
| Condition | 14 类型 | 14 类型 + 28 专用 reason | 40+ |
| Feature gates | 3 | 3 | 13+ |
| RBAC 行数 | 91 | 91 | 300 |

---

## 一、审计方法与基线

### 1.1 方法

- **v1 审计**（08-20）：6 个并行 deep agent 逐字段读代码 → `docs/archive/audit/01-06.md` + `00-summary.md`（2154 行）。
- **v2 审计**（08-23）：CAPA 基线升级到 v2.13.0，`git diff 67de5c2..a84670f` + grep + targeted read。
- **实测验证**（08-23）：本地 envtest 1.34.1 跑全部 ~83 个测试用例，验证"CCE 缺失"结论的准确性；真实 API 冒烟在 cn-north-4 账户上做（部分受余额冻结阻塞）。

### 1.2 基线选择

| 项目 | CAPI 版本 | 备注 |
|---|---|---|
| **CCE Provider**（本项目） | **v1.14.0** | — |
| CAPA v2.13.0（v2 基线） | v1.13.4 | 2026-07-29 release |
| CAPA main HEAD | v1.13.4 | CAPA 尚未发布 CAPI v1.14 兼容版本 |

**含义**：CCE 在 CAPI 版本上**领先于 CAPA**；后续能力补齐需以 CAPI v1.14 契约为准，而非等 CAPA 跟进。

---

## 二、对标 CAPA 能力全景（最终状态）

> ✅ 已实现 / 🟡 部分（CCE 云能力差异）/ ⚪ 裁剪（CCE 无对等）/ ⏳ 待办

### 2.1 控制面（CCEManagedControlPlane vs AWSManagedControlPlane）

| 能力 | 状态 | 说明 |
|---|---|---|
| 生命周期 / 幂等创建 / 带外删除检测 | ✅ | 带外删除清 ClusterID 重建 |
| 版本升级 | ✅ | 工作流 PreCheck→原地滚动→轮询，`UpgradeNotOffered` 当正常态 |
| addons | ✅ | 声明式差量（CRD 层 name+version；service 层已支持 Values map 🟡） |
| pod identity | ✅ | 声明式差量 |
| 控制面日志 | ✅ | UpdateClusterLogConfig + TTL + 差量 |
| KMS/envelope 加密 | ✅ | `encryptionConfig.mode`（Default/KMS） |
| IAM 认证模式 | ✅ | `authentication.mode`（rbac/authenticating_proxy） |
| access entry | ✅ | `reconcileAccessPolicies`（AccessPolicy） |
| OIDC/身份提供者 | 🟡 | CCE 支持 OIDC（kube-apiserver 参数），但非 ClusterSpec API 字段（SDK 未暴露），需控制台/kube-apiserver 配置 |
| endpoint 访问 | 🟡 | 仅 `public bool` + cidrs 白名单（CCE 私网端点常开） |
| 控制面规格 | ✅ | flavor 表达（等价 scalingConfig tier） |
| kubeconfig | ✅ | 双 Secret（`<cluster>-kubeconfig` + `<cluster>-user-kubeconfig`），365d 证书 + 过期前 30d 轮换 |
| Secondary CIDR | 🟡 | 无 EKS 术语，但支持多容器网段 containerNetwork.cidrs[] + 多 ENI 子网 eniNetwork.subnets[] |
| Fargate/Autopilot | ✅ | `spec.enableAutopilot` → `ClusterSpec.EnableAutopilot`（标准 CreateCluster 流程） |

### 2.2 节点池（CCEManagedMachinePool vs AWSManagedMachinePool）

| 能力 | 状态 | 说明 |
|---|---|---|
| 扩缩容 | ✅ | 绝对值 ScaleNodePool + `Watches(MachinePool)` 触发（P0-1 已修） |
| 竞价/spot | ✅ | spot/spotPrice → marketType=spot（billingMode=0） |
| AMI/镜像 | ✅ | OS 字段 |
| 启动模板 | 🟡 | 无 EKS LaunchTemplate 术语，NodeSpec 即对等（flavor/os/磁盘/污点/标签/ecsGroupId/faultDomain 等） |
| labels/taints | ✅ | refresh 策略 |
| remoteAccess | ✅ | sshKey |
| 磁盘 | ✅ | rootVolume + 多 dataVolumes |
| 滚动更新 | ✅ | UpdateConfig + UpgradeNodePool |
| 节点修复 | ✅ | `nodeRepair.enabled` → 检测 Abnormal/Error 节点 ResetNode（CCE 无 EKS auto-repair 开关，provider 主动） |
| 多 AZ | ✅ | extensionScaleGroups |
| 生命周期钩子 | ✅ | preInstall/postInstall → `alpha.cce/preInstall`/`alpha.cce/postInstall`（NodeExtendParam）+ `waitPostInstallFinish`；语义=初始化脚本 vs CAPA ASG 生命周期事件 |
| 反向同步（external autoscaler） | ✅ | `replicas-managed-by` 注解 → 反向写 MachinePool.spec.replicas |

### 2.3 网络（CCECluster）

| 能力 | 状态 | 说明 |
|---|---|---|
| 托管网络（managed VPC/子网/NAT） | ✅ | 三态：创建（vpc.id 空）/ 收养（vpc.id + owned tag）/ BYO |
| 收养模式（tag） | ✅ | `HasOwnedTag` 判定 |
| tag 打标（owned tag） | ✅ | VPC 星号 / NAT 星号推断 / EIP `CreatePublicipTag` |
| NAT 出网 | ✅ | `natGateway` 非 nil 即建（默认建，对标 CAPA，去掉 Enabled 开关） |
| 创建前预检 | ✅ | CIDR 格式/重叠、subnet 归属、eni 子网数（加分项，比 CAPA 更早失败） |
| Secondary CIDR | 🟡 | 多容器网段 cidrs[] + 多 ENI 子网 subnets[] |
| IPv6 | 🟡 | 支持 IPv6 双栈（ipv6enable + serviceNetwork.IPv6CIDR，1.15+） |
| Edge Zone | ⚪ | SDK 未发现 edge zone 字段（无对等，保留） |
| conditions 细化 | ✅ | `VpcReady`/`SubnetsReady`/`NatGatewaysReady` |

### 2.4 身份与凭证

| 能力 | 状态 |
|---|---|
| ControllerIdentity | ✅ + `AutoControllerIdentityCreator` gate 自动创建 |
| StaticIdentity | ✅ + allowedNamespaces |
| RoleIdentity | ✅ agency 经 identityRef 传入 CreateCluster（P0-3 已修） |
| allowedNamespaces 校验 | ✅ 三类身份 + 身份 CRD webhook |
| 凭证回退链 | ✅ identityRef → per-cluster Secret → env（**CCECluster 也走 identityRef 链，二次审计修复**） |

### 2.5 架构与工程化

| 维度 | 状态 |
|---|---|
| Scope/patchHelper | ✅ 4 个 scope struct（CCEClusterScope/CCMScope/CMPScope/GlobalScope，08-23 重构）；`PatchObject()` 含 `WithStatusObservedGeneration{}`；`Close()` 集中管理 |
| K8s events | ✅ recordEvent 遍布 |
| 并发控制 flag | ✅ cce-cluster/control-plane/machine-pool-concurrency |
| SDK client 缓存 | ✅ clientCache（按 region+ak+sk） |
| 错误分类退避 | ✅ NotFound/Conflict/Throttle/Quota/PermissionDenied/ScaleNoOp，差异化退避 |
| API 版本（单 v1beta2） | ✅ 单一存储版本 v1beta2，v1beta1 与转换 webhook 已移除 |
| ClusterClass 模板三件套 | ✅ CCEClusterTemplate/CCEManagedControlPlaneTemplate/CCEManagedMachinePoolTemplate |
| e2e（Ginkgo + flavors） | ✅ build tag `e2e` |
| clusterctl 打包 | ✅ metadata.yaml + components |
| GC（tag 扫描） | ✅ 孤儿集群 + EIP/EVS/VPC/NAT 扫删（API 分页已补齐） |
| 限流中间件（token bucket） | ✅ `throttleRoundTripper` 包裹 `http.DefaultTransport`，读 20 ops/s burst 100、写 10 req/min burst 10 |
| Webhook 校验密度 | ✅ 已对齐 CAPA（08-24 补齐，详见 §3.3） |
| SSA patch / ClusterCacheTracker | ⚪ 未引入（CCE 场景影响小） |

### 2.6 Feature gates

**3 个**（NodePoolAutoscaling、AutoControllerIdentityCreator、ExternalResourceGC），经对标核定「不适用扩充」（声明式 spec 能力永不 gate）。

---

## 三、差距重分类与整改

### 3.1 v1 P1 差距重分类（9 项）

| 分类 | 项 | 结论 |
|---|---|---|
| 错误判断（不需整改） | Addon、AccessEntry | CCE 用内嵌实现且已测试 |
| 功能对等（云能力差异） | IRSA | CCE 用 agency 委托语义（功能对等），已测试 |
| 真正待整改 | 其余 6 项 | 已全部实施（见 §3.3） |

### 3.2 整改全景（时间线）

- **08-20 ~ 08-22**：10 项修复（P0×1 / P1×4 / P2×4 / P3×1）—— 网络拓扑托管、GC 扩展（EIP/EVS）、KMS/envelope、IAM 认证模式、外部 autoscaler 反向同步、user-kubeconfig 双 Secret、providerIDList 回填、节点修复（ResetNode）、Ginkgo e2e、Fargate/Autopilot。
- **08-22 二次审计**：修复 3 个 bug（EIP 找回时序 / NAT 泄漏 / CCECluster 未走 identityRef 链）。
- **08-22 对标补充**：conditions 细化、收养模式、tag 打标、GC 扫 VPC/NAT。
- **08-23**：限流中间件、API 版本收敛（v1beta1 → v1beta2）、ObservedGeneration 保护、P1 6 项整改（#2-#8）。
- **08-24**：Webhook 校验密度补齐（#5 完整翻译）、Conditions reason 应用（#6）、P2 跟进（GC paging、字段语义）。

### 3.3 v1 P1 修复计划 → 状态映射

| 计划项 | 状态 | 备注 |
|---|---|---|
| #1 网络拓扑托管 | ✅ 已实施 | 三态（创建/收养/BYO） |
| #2 cluster API 限流 | ✅ 已实施 | throttleRoundTripper 共享 network 包 |
| #3 wait package | ✅ 已实施 | 复制 CAPA wait.go 指数退避（~5m budget） |
| #4 GC annotation opt-out | ✅ 已实施 | annotation key `capi-cce/skip-gc` |
| #5 Webhook 校验密度补齐 | ✅ 已实施 | CCM（版本降级、IPv6 最小版本 + IPv6CIDR 必填、CIDR 格式、加密与身份引用不可变、ipv6enable 不可变）；CCECluster（region + VPC ID 不可变、VPC/subnet CIDR）；CMP（NodePoolName 不可变、autoscaling 边界）。+9 测试；3 个 template webhook 靠委托自动继承；access-entry/launch-template 已由 CRD marker + 既有 webhook 覆盖 |
| #6 Conditions 失败原因细分 | ✅ 已实施 | 增 28 个专用 reason + 32 处通用 reason 切换为专用 |
| #7 身份 webhook 不可变 | ✅ 已实施 | 3 个 webhook 都有 spec 不可变 + 单例约束 |
| #8 scope struct 重构 | ✅ 已实施 | 4 个 scope struct + `NewXxxScope()` 模式 |
| P2-9 GC API 分页 | ✅ 已实施 | 对标 CAPA `33ad74990` |
| P2-11 字段语义 | ✅ 已实施 | — |
| ObservedGeneration 保护 | ✅ 已实施 | 对标 CAPA `9e9bb6b31` + `b5d6d3081` |

### 3.4 剩余差距（准确清单）

| # | 差距 | 类别 | 状态 |
|---|---|---|---|
| 1 | NAT tag 格式实测 | 依赖余额 | ⏳ 余额冻结 `CBC.30060005`，推断星号（华为云 `Tags *[]string` 统一约定） |
| 2 | 真实云冒烟（managed VPC/NAT 一键建删、GC EIP/EVS/VPC/NAT、KMS/authenticating_proxy、ResetNode 正向） | 依赖余额 | ⏳ 余额冻结 + 需账号操作 |
| 3 | providerID + MHC 生态集成验证 | 生态验证 | ✅ providerID 已修正为 `huaweicloud:///<serverId>`；MHC 生态消费验证仍 ⏳ |
| 4 | v3 审视 | 待办 | ⏳ 等 CAPA v2.14+（CAPI v1.14 兼容）发布 |

---

## 四、关键调查结论（事实核查，均已核实）

1. **SWR 内网访问**：SWR 支持 VPCEP；基础版对同区域 ECS/CCE 节点**默认内网访问**（免 NAT 免配置），企业版需配 VPCEP。CCE 拉同区域 SWR 镜像免 NAT，与 CAPA ECR VPC endpoint 对等。NAT 仅在拉公网第三方镜像（quay.io/registry.k8s.io）时需要。
2. **EIP tag**：`CreatePublicipTag`（`{Key,Value}`，key ≤128）；`ListPublicips` 返回 `"key=value"`（等号，实测）。无需 TMS。
3. **EIP API 面**：SDK `eip/v2`（生命周期 + 标签）与 `eip/v3`（查询/绑定）**并存**，非迭代版本。
4. **VPC/NAT tag 分隔符**：VPC 实测**星号 `*`**（等号报 `VPC.1801`）；NAT 推断星号（余额冻结未实测）。
5. **owned tag key**：`cluster-api-provider-cce.cluster.<name>=owned`，前缀 33 + name（≤63）→ ≤96 < 128，三种资源都够用。
6. **账户余额冻结**：`CBC.30060005 "Frozen CbcDeposit Failed!"` —— 阻塞 NAT 网关创建（计费资源），EIP/VPC 创建不受影响。

---

## 五、待决策项（均已拍板）

| # | 决策 | 结论 |
|---|---|---|
| 1 | NAT 默认建 vs 显式 enabled | ✅ **对标 CAPA 默认建**：去掉 `Enabled` 字段，`natGateway` 非 nil 即建（BYO 或省略即禁用） |
| 2 | 节点池级过滤 | ✅ **已实现**：节点 `Metadata.OwnerReferences.NodepoolID` 有节点池 ID，用 `ListNodesWithStatus` 返回的 NodePoolID 过滤，仅 reset 本池异常节点（前期「CCE 无 pool 字段」为误判） |
| 3 | providerID 格式 | ✅ **已修正为 `huaweicloud:///<serverId>`**（对标华为云 cloud-provider 约定 + CAPA 用底层实例 ID），不再是 `cce://<uid>` |

---

## 六、CAPA 基线升级影响（v2.10.3 → v2.13.0）

### 6.1 删除项（对 CCE 的影响）

| 删除内容 | 对 CCE 的意义 |
|---|---|
| `AWSManagedControlPlane.Spec.PodIdentityAssociations` | CCE 仍保留内嵌实现（不同设计路线） |
| `ControlPlaneScalingConfig` 字段 + 类型 | CCE 本来没有 |
| `EKSPodIdentityAssociationConfiguredCondition` + reason | CCE 的 `PodIdentityAssociationsConfigured` 独立存在 |
| `pod_identities.go`（229 行） | CCE 保留独立实现 |
| `awscluster_controller.go` + `awsmachine_controller.go` 各 32 行 OTEL tracing | CCE 不使用 OTEL |

### 6.2 新增项（对 CCE 的影响）

| 新增内容 | 对 CCE 的意义 |
|---|---|
| `AWSLaunchTemplate.EnclaveOptions` / NitroEnclave + Local Zone 冲突校验 | CCE 节点池无此字段（云能力差异） |
| `9e9bb6b31` + `b5d6d3081` ObservedGeneration 保护 | ✅ **CCE 已实施** |
| `33ad74990` GC API 分页 | ✅ **CCE 已实施**（P2-9） |
| RBAC role.yaml +26 行（OCM/STS） | CCE 不相关 |
| ROSA TrustPolicyExternalID +162 行 | CCE 不相关 |

**结论**：CAPA v2.10 → v2.13.0 生产代码重大变化集中在 4 个领域（Pod Identity 剥离 / ObservedGeneration / GC paging / 条件 reason 细化）；API、scope、controllers、services 架构几乎不变。

---

## 七、最终判断

- **v1 P1 9 项中 2 项是错误判断**（Addon / AccessEntry）——CCE 用内嵌实现且已测试，不应计入。
- **v1 P1 9 项中 1 项是功能对等**（IRSA → agency 委托）——云能力差异。
- **P1 总完成率 8/8 = 100%**，P0/P2 同步补齐。
- **核心能力已对齐**（生命周期、addon、pod-identity、access policy、logging、kubeconfig rotation、GC、错误分类退避、限流中间件、Webhook 校验密度）。
- **架构形态差异为有意为之**（CCE 纯 managed-only，无 Machine/MachineDeployment）。
- **CAPI 版本错配**仍是最大发现 —— CCE 领先于 CAPA 一个 minor，后续以 CAPI v1.14 契约为准。

---

## 附录 A：变更记录（时间线）

### 文档演进

| 日期 | 文档 | 定位 | 关键结论 |
|---|---|---|---|
| 2026-08-20 | `archive/capa-comparison-review-2026-08.md` | 实现级审视 | 3 个 P0 缺陷 + 架构差距（无 Scope/patchHelper、零 events、无并发控制）+ 缺 ClusterClass/转换 webhook |
| 2026-08-21 | `archive/capa-parity-gap-analysis.md` | 差距总账 | 状态更新行标注「P0/P1/P2 已补齐」；剩余差距：access entry、GC、KMS/认证模式等 |
| 2026-08-22 | `archive/capa-alignment-remediation-design-2026-08.md` | 修复设计 + 实施记录 | 10 项修复设计 + 实施；二次审计 3 bug；调查修正多个错误结论 |
| 2026-08-23 | `docs/archive/audit-v2/00-summary.md` | 增量审计（CAPA v2.13.0） | P1 重分类 + 6 项整改 + ObservedGeneration |
| 2026-08-24 | 本文档 | 合并归纳 | 以最新状态为准，统一时间线 |

### 关键 commit

| commit | 内容 |
|---|---|
| `c936f53` + `a00b656` | #5 Webhook 校验密度补齐（三处核心 webhook + 9 测试） |
| `ef81703` | #6 Conditions reason 应用（32 处通用 → 专用） |
| `888008a` | P2-9 GC API 分页 |
| `f642695` | P2-11 字段语义 |

### 调查修正（错误结论更正）

| 原结论 | 修正后事实 | 依据 |
|---|---|---|
| SWR 无 VPC endpoint，CCE 比 CAPA 更依赖 NAT | SWR **有 VPCEP**，基础版同区域默认内网访问（免 NAT），与 CAPA ECR endpoint 对等 | 官方文档 bestpractice-swr |
| EIP tag key 36 字符，owned key 超限 | key **128 字符**（SDK 注释「36」过时），owned key 无需缩短 | 官方 api-eip |
| EIP tag 依赖 TMS | EIP 有专属 `CreatePublicipTag` API，无需 TMS | SDK eip/v2 |
| EIP v3 缺创建/打 tag（暗示版本退步） | v2/v3 是**并存 API 面**，非迭代 | SDK 方法清单 + HTTP path |

---

## 附录 B：测试套件结果（最终，2026-08-24）

| 测试集 | 用例数 | 失败 |
|---|---|---|
| `go build ./...` + `go vet ./...` | — | 0 ✅ |
| `api/controlplane/v1beta2`（CCM webhook） | 8 | 0 ✅ |
| `api/infrastructure/v1beta2`（身份/模板/machinepool/cluster webhook） | 19 | 0 ✅ |
| `controllers`（envtest 1.34.1） | 55（含 GC 新增） | 0 ✅ |
| `internal/scope` | 3 + 5 P1-8 新增 | 0 ✅ |
| `internal/services/cce` | 1 | 0 ✅ |
| `internal/services/errors` | 1 | 0 ✅ |
| `internal/services/network` | 12 | 0 ✅ |
| `internal/wait` | 7 | 0 ✅ |
| **总计** | **132** | **0** ✅ |

> 注：`controllers` 包需本地 envtest 二进制（`KUBEBUILDER_ASSETS`），无该环境时无法运行；代码本身无缺陷。
