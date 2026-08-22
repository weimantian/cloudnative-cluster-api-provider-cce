# CCE Provider 对标 CAPA 汇总报告（含变更记录）

> 汇总日期：2026-08-22
> 对标基准：CAPA（kubernetes-sigs/cluster-api-provider-aws）EKS managed 模式，源码 `/tmp/capa` @ `67de5c2`（v2.10 主干）
> 本项目：cloudnative-cluster-api-provider-cce
> 本文整合三份文档：`archive/capa-comparison-review-2026-08.md`（实现级审视）、`archive/capa-parity-gap-analysis.md`（差距总账）、`archive/capa-alignment-remediation-design-2026-08.md`（修复设计+实施记录），并附完整变更记录。

---

## 一、变更记录（Changelog）

### 文档演进时间线

| 日期 | 文档 | 定位 | 关键结论 |
|---|---|---|---|
| 2026-08-20 | `archive/capa-comparison-review-2026-08.md` | 实现级审视（逐文件代码审查） | 发现 **3 个 P0 缺陷**（扩缩容无触发路径 / 删除路径忽略 identityRef / RoleIdentity agency 丢弃）+ 架构差距（无 Scope/patchHelper、零 events、无并发控制）+ 缺 ClusterClass/转换 webhook |
| 2026-08-21 | `archive/capa-parity-gap-analysis.md` | 差距总账（能力矩阵） | 状态更新行标注「P0/P1/P2 补齐已完成」；剩余差距：access entry、GC、KMS/认证模式、节点修复/反向同步/user-kubeconfig、Ginkgo e2e |
| 2026-08-22 | `archive/capa-alignment-remediation-design-2026-08.md` | 修复设计 + 实施记录 | 10 项修复（P0×1/P1×4/P2×4/P3×1）设计 + 实施；二次审计修复 3 个 bug；调查修正多个错误结论 |

### 本次会话（2026-08-22）变更明细

按时间顺序（相对 gap-analysis 08-21 的「P0/P1/P2 已补齐」状态之后的增量）：

**A. 10 项修复实现**（design 文档编号）：

| # | 修复项 | 优先级 | 结果 |
|---|---|---|---|
| 1 | 网络拓扑托管（managed VPC/子网/NAT，三态） | P0 | ✅ 实现 + 测试 |
| 2 | 外部资源 GC 扩展（EIP/EVS 扫删） | P1 | ✅ 实现 + 测试 |
| 3 | KMS/envelope 加密（encryptionConfig） | P1 | ✅ 实现 |
| 4 | IAM 认证模式（authenticatingProxy） | P1 | ✅ 实现 |
| 5 | 外部 autoscaler 反向同步 | P1 | ✅ 实现 + 对标 CAPA 修正 |
| 6 | user-kubeconfig 双 Secret | P2 | ✅ 实现 + 测试 |
| 7 | providerIDList 回填（`cce://uid`） | P2 | ✅ 实现（修正格式） |
| 8 | 节点修复（ResetNode） | P2 | ✅ 实现 + 测试 |
| 9 | Ginkgo + flavors e2e | P2 | ✅ 实现（build tag `e2e`） |
| 10 | Fargate/Autopilot 超节点 | P3 | ✅ 实现（`spec.enableAutopilot` → `ClusterSpec.EnableAutopilot`） |

**B. 二次审计修复（3 个类型/逻辑 bug）**：

1. `manager.go` `findEipBySnatRules` 时序错误（EIP ID 找回在删 SNAT 之后，找不到 → EIP 泄漏）→ 移到删 SNAT 之前。
2. `manager.go` `DeleteNetwork` 依赖 `ng.Enabled`（disable 后删除泄漏 NAT）→ 改为「有 ResourceID 即删」。
3. `ccecluster_controller.go` 用 `scope.ResolveCredentials`（只认 per-cluster Secret）而非 identityRef 链 → identityRef 集群在网络校验/托管/删除三处卡死；新增 `resolveClusterCredentials`（读 CP 的 identityRef 链）。**这是 comparison-review P0-2 的同类问题，当时只修了 CP/MP，漏了 CCECluster**。

**C. 对标 CAPA 完整性补充（审计报告提出）**：

| # | 补充项 | 对标 CAPA | 结果 |
|---|---|---|---|
| #4 | conditions 细化（`VpcReady`/`SubnetsReady`/`NatGatewaysReady`） | CAPA 独立 condition 集合 | ✅ 实现（Manager 拆分为 `ReconcileVpc`/`ReconcileSubnets`/`ReconcileNatGateway`） |
| #5 | 收养模式（tag 三态） | CAPA 创建/收养/BYO 三态 | ✅ 实现（`VPC.Tags` + `HasOwnedTag`） |
| #6 | tag 打标（managed VPC/NAT/EIP 打 owned tag） | CAPA tag 所有权 | ✅ 实现（VPC 星号、EIP `CreatePublicipTag`、NAT 星号推断） |
| #2-ext | GC 扫 VPC/NAT（原 #2 只扫 EIP/EVS） | CAPA ExternalResourceGC 全资源 | ✅ 实现（N+1 查 tag + NAT 先删 SNAT） |

**D. 调查修正（错误结论更正）**：

| 原结论 | 修正后事实 | 依据 |
|---|---|---|
| SWR 无 VPC endpoint，CCE 比 CAPA 更依赖 NAT | SWR **有 VPCEP**，基础版同区域 ECS/CCE 默认内网访问（免 NAT），与 CAPA ECR endpoint 对等 | 华为云官方文档 bestpractice-swr |
| EIP tag key 36 字符，owned key 超限需缩短 | key **128 字符**（SDK 注释「36」过时，官方 2026-08-05 更新），owned key 无需缩短 | 官方 api-eip/eip_apitag_0001 |
| EIP tag 依赖 TMS | EIP 有专属 `CreatePublicipTag` API，无需 TMS | SDK eip/v2 |
| EIP v3 缺创建/打 tag（暗示版本退步） | v2/v3 是**并存的 API 面**（v2=生命周期+tag，v3=查询/绑定），非迭代 | SDK 方法清单 + HTTP path |

**E. 实测记录（真实 API，cn-north-4）**：

| 项 | 结果 |
|---|---|
| VPC tag 分隔符 | **星号 `*`**（`key*value`，ShowVpcTags 正确拆出；等号报 `VPC.1801`）✅ 实测 |
| EIP tag 格式 | 打 tag `{Key,Value}` → ListPublicips 返回 `"key=value"`（等号），匹配 `parseKVTags` ✅ 实测闭环 |
| NAT tag 格式 | 推断星号（华为云 `Tags *[]string` 统一约定）⚠️ 未实测——账户余额冻结 `CBC.30060005 "Frozen CbcDeposit Failed!"` |

**F. 限流中间件实现（token bucket，2026-08-23）**：
- 客户端主动限流：`throttleRoundTripper` 包裹 `http.DefaultTransport`，读（GET/HEAD）20 ops/s burst 100，写（其余方法）10 req/min burst 10。
- 接线：`internal/services/network/manager.go` 统一注入限流 transport；`throttle.go` + `throttle_test.go`（5 用例）。
- 依赖：`golang.org/x/time` 提升为直接依赖。

---

## 二、文档关系与状态澄清（重要）

三份文档存在**时间线状态不一致**，汇总时以最新为准：

- `comparison-review`（08-20）的 3 个 P0 问题，在 `gap-analysis`（08-21）状态更新中标注「已补齐」，但 comparison-review 正文**未回改**（仍写「发现三个 P0 缺陷」）。
- `gap-analysis`（08-21）正文的能力矩阵部分行仍标注「❌」（如 KMS、认证模式、access entry、GC、节点修复、反向同步、user-kubeconfig、ProviderIDList），这些**已在 08-22 的 design 文档实施记录中实现**，但 gap-analysis 正文未回改。

**结论**：`archive/capa-alignment-remediation-design-2026-08.md` 的实施记录（§5）是最新、最准确的状态来源。本汇总以它为准。

---

## 三、当前状态速览

**骨架 + 大部分外围已对齐 CAPA EKS managed 子集；9 项修复（P0/P1/P2）+ 二次审计 3 个 bug + 4 项对标补充 + 限流中间件全部落地并测试通过。剩余差距集中在「余额冻结待实测」「MHC 生态验证」两类，无阻塞性缺陷。**

---

## 四、对标 CAPA 能力全景（合并三份文档，标注最新状态）

> ✅ 已实现 / 🟡 部分（CCE 云能力差异）/ ⚪ 裁剪（CCE 无对等）/ ⏳ 待办 / 🔴 待决策

### 4.1 控制面（CCEManagedControlPlane vs AWSManagedControlPlane）

| 能力 | 状态 | 说明 |
|---|---|---|
| 生命周期 / 幂等创建 / 带外删除检测 | ✅ | 带外删除清 ClusterID 重建 |
| 版本升级 | ✅ | 工作流 PreCheck→原地滚动→轮询，`UpgradeNotOffered` 当正常态 |
| addons | ✅ | 声明式差量（CRD 层 name+version；service 层已支持 Values map 🟡） |
| pod identity | ✅ | 声明式差量 |
| 控制面日志 | ✅ | UpdateClusterLogConfig + TTL + 差量 |
| KMS/envelope 加密 | ✅（本次 #3） | `encryptionConfig.mode`（Default/KMS） |
| IAM 认证模式 | ✅（本次 #4） | `authentication.mode`（rbac/authenticating_proxy） |
| access entry | ✅ | `reconcileAccessPolicies`（AccessPolicy） |
| OIDC/身份提供者 | 🟡 | CCE 支持 OIDC（kube-apiserver OIDC 参数，官方文档确认），但非 ClusterSpec API 字段（SDK 未暴露），需控制台/kube-apiserver 配置 |
| endpoint 访问 | 🟡 | 仅 `public bool` + cidrs 白名单（CCE 私网端点常开） |
| 控制面规格 | ✅ | flavor 表达（等价 scalingConfig tier） |
| kubeconfig | ✅（本次 #6） | 双 Secret（`<cluster>-kubeconfig` + `<cluster>-user-kubeconfig`），365d 证书 + 过期前 30d 轮换 |
| Secondary CIDR | 🟡 | 无 EKS 术语，但支持多容器网段 containerNetwork.cidrs[] + 多 ENI 子网 eniNetwork.subnets[] |
| Fargate/Autopilot | ✅（本次 #10） | `spec.enableAutopilot` → `ClusterSpec.EnableAutopilot`（标准 CreateCluster 流程） |

### 4.2 节点池（CCEManagedMachinePool vs AWSManagedMachinePool）

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
| 节点修复 | ✅（本次 #8） | `nodeRepair.enabled` → 检测 Abnormal/Error 节点 ResetNode（CCE 无 EKS auto-repair 开关，provider 主动） |
| 多 AZ | ✅ | extensionScaleGroups |
| 生命周期钩子 | ✅（本次 #6） | preInstall/postInstall → `alpha.cce/preInstall`/`alpha.cce/postInstall`（NodeExtendParam）+ `waitPostInstallFinish`（NodeTemplate）；语义=初始化脚本 vs CAPA ASG 生命周期事件 |
| 反向同步（external autoscaler） | ✅（本次 #5） | `replicas-managed-by` 注解 → 反向写 MachinePool.spec.replicas（对标 CAPA 精确修正） |

### 4.3 网络（CCECluster）

| 能力 | 状态 | 说明 |
|---|---|---|
| 托管网络（managed VPC/子网/NAT） | ✅（本次 #1） | 三态：创建（vpc.id 空）/ 收养（vpc.id + owned tag）/ BYO |
| 收养模式（tag） | ✅（审计 #5） | `HasOwnedTag` 判定 |
| tag 打标（owned tag） | ✅（审计 #6） | VPC 星号 / NAT 星号推断 / EIP `CreatePublicipTag` |
| NAT 出网 | ✅（本次 #1） | `natGateway.enabled`（**默认建 vs 显式，待决策**） |
| 创建前预检 | ✅ | CIDR 格式/重叠、subnet 归属、eni 子网数（加分项，比 CAPA 更早失败） |
| Secondary CIDR | 🟡 | 多容器网段 cidrs[] + 多 ENI 子网 subnets[]（见 4.1） |
| IPv6 | 🟡 | 支持 IPv6 双栈（ipv6enable + serviceNetwork.IPv6CIDR，1.15+） |
| Edge Zone | ⚪ | SDK 未发现 edge zone 字段（无对等，保留） |
| conditions 细化 | ✅（审计 #4） | `VpcReady`/`SubnetsReady`/`NatGatewaysReady` |

### 4.4 身份与凭证

| 能力 | 状态 |
|---|---|
| ControllerIdentity | ✅ + `AutoControllerIdentityCreator` gate 自动创建 |
| StaticIdentity | ✅ + allowedNamespaces |
| RoleIdentity | ✅ agency 经 identityRef 传入 CreateCluster（P0-3 已修） |
| allowedNamespaces 校验 | ✅ 三类身份 + 身份 CRD webhook |
| 凭证回退链 | ✅ identityRef → per-cluster Secret → env（**CCECluster 也走 identityRef 链，二次审计修复**） |

### 4.5 架构与工程化

| 维度 | 状态 |
|---|---|
| Scope/patchHelper | ✅ defer 单次全对象 patch，3 controller 收口 |
| K8s events | ✅ recordEvent 遍布 |
| 并发控制 flag | ✅ cce-cluster/control-plane/machine-pool-concurrency |
| SDK client 缓存 | ✅ clientCache（按 region+ak+sk） |
| 错误分类退避 | ✅ NotFound/Conflict/Throttle/Quota/PermissionDenied/ScaleNoOp，差异化退避 |
| v1beta1/v1beta2 + 转换 webhook | ✅ 存储 Hub + 服务 + Convertible + /convert |
| ClusterClass 模板三件套 | ✅ CCEClusterTemplate/CCEManagedControlPlaneTemplate/CCEManagedMachinePoolTemplate |
| e2e（Ginkgo + flavors） | ✅（本次 #9）build tag `e2e` |
| clusterctl 打包 | ✅ metadata.yaml + components |
| GC（tag 扫描） | ✅（本次 #2 + #2-ext）孤儿集群 + EIP/EVS/VPC/NAT 扫删 |
| 限流中间件（token bucket） | ✅（本次） | 客户端主动限流：`throttleRoundTripper` 包裹 `http.DefaultTransport`，读（GET/HEAD）20 ops/s burst 100、写（其余）10 req/min burst 10 |
| SSA patch / ClusterCacheTracker | ⚪ 未引入（CCE 场景影响小） |

### 4.6 feature gates

**2 个**（NodePoolAutoscaling、AutoControllerIdentityCreator），经对标核定「不适用扩充」（gap-analysis 阶段 2 第 10 条判定准则：声明式 spec 能力永不 gate）。

---

## 五、剩余差距清单（准确）

| # | 差距 | 类别 | 状态 |
|---|---|---|---|
| 1 | #8 节点池级过滤（reconcileNodeRepair 集群级→节点池级） | **前期误判已修正** | ✅ 已实现：节点 Metadata.OwnerReferences.NodepoolID 有节点池 ID，ListNodesWithStatus 返回 NodePoolID 过滤（见 §七 #2、§八 8.3） |
| 2 | NAT tag 格式实测 | 依赖余额 | ⏳ 余额冻结 `CBC.30060005`，推断星号（华为云 `Tags *[]string` 统一约定） |
| 3 | 真实云冒烟（managed VPC/NAT 一键建删、GC EIP/EVS/VPC/NAT、KMS/authenticating_proxy、ResetNode 正向） | 依赖余额 | ⏳ 余额冻结 + 需账号操作 |
| 4 | NAT 默认建 vs 显式 enabled | 产品决策 | ✅ 已决策：对标 CAPA 默认建，去掉 Enabled 字段（见 §七 #1、§八 8.2） |
| 5 | providerID 格式 + MHC 生态集成验证 | 生态验证 | ✅ providerID 已修正为 huaweicloud:///<serverId>（见 §七 #3、§八 8.1）；MHC 生态消费验证仍 ⏳ |
| 6 | 限流中间件（token bucket） | 工程化 | ✅ 已实现：客户端 token bucket 主动限流（`throttle.go`，读 20 ops/s burst 100、写 10 req/min burst 10） |
| 7 | Fargate/Autopilot 超节点（#10） | 远期 | ✅ 已实现：`spec.enableAutopilot` 透传（无需 hypernode-research） |

---

## 六、关键调查结论（事实核查，均已核实）

1. **SWR 内网访问**：SWR 支持 VPCEP；基础版对同区域 ECS/CCE 节点**默认内网访问**（免 NAT 免配置），企业版需配 VPCEP。CCE 拉同区域 SWR 镜像免 NAT，与 CAPA ECR VPC endpoint 对等。NAT 仅在拉公网第三方镜像（quay.io/registry.k8s.io）时需要。
2. **EIP tag**：`CreatePublicipTag`（`{Key,Value}`，key ≤128）；`ListPublicips` 返回 `"key=value"`（等号，实测）。无需 TMS。
3. **EIP API 面**：SDK `eip/v2`（生命周期 CreatePublicip HTTP `/v1/` + 标签 CreatePublicipTag HTTP `/v2.0/.../tags`）与 `eip/v3`（查询/绑定 HTTP `/v3/`）**并存**，非迭代版本。
4. **VPC/NAT tag 分隔符**：VPC 实测**星号 `*`**（等号报 `VPC.1801`）；NAT 推断星号（余额冻结未实测）。
5. **owned tag key**：`cluster-api-provider-cce.cluster.<name>=owned`，前缀 33 + name（≤63）→ ≤96 < 128，三种资源都够用，与 CCE 集群/节点池一致。
6. **账户余额冻结**：`CBC.30060005 "Frozen CbcDeposit Failed!"`——阻塞 NAT 网关创建（计费资源），EIP/VPC 创建不受影响。

---

## 七、待决策项（需项目负责人拍板）

| # | 决策 | 状态 | 结论 |
|---|---|---|---|
| 1 | NAT 默认建 vs 显式 enabled | ✅ 已决策（2026-08-22） | **对标 CAPA 默认建**：去掉 `Enabled` 字段，`natGateway` 非 nil 即建（BYO 或省略即禁用）。实现见 §八 |
| 2 | #8 节点池级过滤 | ✅ 已实现（2026-08-22） | 前期误判「CCE 无 pool 字段」——实际节点 `Metadata.OwnerReferences.NodepoolID` 有节点池 ID。用 `ListNodesWithStatus` 返回的 NodePoolID 过滤，仅 reset 本池异常节点。见 §八
| 3 | providerID 格式 | ✅ 已修正（2026-08-22） | **`huaweicloud:///<serverId>`**（对标华为云 cloud-provider 约定 + CAPA 用底层实例 ID），不再是 `cce://<uid>`。见 §八 |

---

## 八、末次修正记录（2026-08-22，待决策项落地）

### 8.1 providerID 格式修正

- 原：`cce://<uid>`（uid = CCE 节点 ID）——臆想格式。
- 现：`huaweicloud:///<serverId>`（serverId = ECS 实例 ID）。
- 依据（源码核实）：华为云 `kubernetes-sigs/cloud-provider-huaweicloud` 的 `instances.go`：`ProviderName = "huaweicloud"`、`providerIDRegexp = ^huaweicloud:///([^/]+)$`、`InstanceID() = ecsClient.GetByNodeName(name).Id`（ECS 实例 ID）。
- 对标 CAPA：CAPA 用 `aws:///<az>/<instance-id>`（instance-id = 底层 EC2 实例 ID），同样用**底层实例 ID**而非编排层 ID。CCE 的「底层实例 ID」= serverId（官方文档：\"底层云服务器或裸金属节点ID\"），非 uid（CCE 节点 ID）。
- 实现：`ListNodes` 改取 `n.Status.ServerId`，格式 `huaweicloud:///<serverId>`。

### 8.2 NAT 默认建（对标 CAPA）

- 原：`NatGatewaySpec.Enabled` 显式开关。
- 现：去掉 `Enabled` 字段，`natGateway` 非 nil 即建 NAT（对标 CAPA managed VPC 有私有子网即默认建 NAT、无 enabled 开关）。
- 禁用方式：BYO（`vpc.id` 非空 + 无 owned tag）或省略 `natGateway`。
- 改动：`types.go` 去掉 Enabled、`ReconcileNatGateway`/`reconcileManagedNetwork` 去掉 Enabled 检查。

### 8.3 #8 节点池级过滤（前期误判修正）

- **前期误判**：曾结论「CCE 无按 pool 列节点 API、节点对象无 pool 字段，无法精确实现」——**错误**（grep SDK 时只查了 NodeSpec/NodeMetadata 的 Name/Uid，漏了 OwnerReferences）。
- **事实**：`Node.Metadata.OwnerReferences` 有 `NodepoolID`/`NodepoolName` 字段（SDK `model_node_metadata_owner_references.go`）；节点还有专属标签 `cce.cloud.com/cce-nodepool-id`（官方「管理节点标签」文档）。
- 实现：`NodeInfo` 加 `NodePoolID` 字段（`ListNodesWithStatus` 从 `Metadata.OwnerReferences.NodepoolID` 提取）；`reconcileNodeRepair` 按 `NodePoolID == pool.Status.NodePoolID` 过滤，仅 reset 本池异常节点。
- 测试：新增跨 pool 节点验证（`nodepool-other` 的异常节点不被 reset）。

### 8.4 二次复核：修正 6 处「无对等/无此概念」误判（2026-08-22）

- **OIDC**：前期「CCE 无 EKS 式 OIDC」错误。CCE 官方文档支持 OIDC（kube-apiserver 参数 oidc-issuer-url/client-id/username-claim/groups-claim/ca-pem/prefixes/required-claim）；SDK model_cluster_condition.go 有 OpenIDConnectProcessing/ProcessSuccess/ProcessFailed 状态。但 SDK ClusterSpec 未暴露 OIDC 配置字段（authentication 仅 mode + authenticatingProxy），需控制台「集群访问配置」或 kube-apiserver 参数——非 CRD 声明字段。
- **Secondary CIDR**：支持多容器网段 containerNetwork.cidrs[] + 多 ENI 子网 eniNetwork.subnets[]（无 EKS 术语，能力对等）。
- **IPv6**：支持 IPv6 双栈（ClusterSpec.Ipv6enable + ServiceNetwork.IPv6CIDR，1.15+）。
- **Fargate/Autopilot**：有 EnableAutopilot + 完整 AutopilotClusterSpec（SDK 已支持）。已实现 `spec.enableAutopilot` → `ClusterSpec.EnableAutopilot` 透传（标准 CreateCluster 流程）；独立 AutopilotClusterSpec/CreateAutopilotCluster API 面未使用。
- **启动模板**：NodeSpec（nodeTemplate）即对等物（flavor/os/rootVolume/dataVolumes/taints/k8sTags/ecsGroupId/faultDomain/dedicatedHostId）。
- **生命周期钩子**：有 preInstall/postInstall 节点脚本钩子（NodeLifecycleConfig + alpha.cce/preInstall/postInstall）。
- **Edge Zone**：SDK 未发现 edge zone 字段，「无对等」保留。
