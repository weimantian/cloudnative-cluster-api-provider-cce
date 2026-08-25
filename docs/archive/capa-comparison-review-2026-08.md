# CCE Provider 对标 CAPA 全面对比审视报告

> 日期：2026-08-20
> 对标基准：CAPA（kubernetes-sigs/cluster-api-provider-aws）当前主干（v1beta2 存储 + v1beta1 服务 + 转换 webhook）。
> 本项目：cloudnative-cluster-api-provider-cce（PoC 验证版）。
> 证据来源：本项目全部控制器/服务层/webhook 代码逐文件审查；CAPA 架构事实经外部研究核实（源码 + 官方文档）；CAPI v1.14 核心行为直接查证模块缓存源码（`core/reconcilers/machinepool/`）。
> 与 `docs/capa-parity-gap-analysis.md` 的关系：该文档大方向准确，本报告在其之上补充了**实现级 P0 问题**（第三节）与 **ClusterClass 缺失**等遗漏项，并复核了其"已实现"标注的实况。

## 结论速览

**骨架对齐良好，三个 P0 级实现缺陷 + 一批架构差距。** 生命周期主链路（创建 -> 就绪 -> 扩缩 -> 升级 -> 删除）、CRD 契约（v1beta2）、身份模型、addons/pod-identity/logging 声明式差量同步均已落地且有真实云冒烟验证；但控制器的事件驱动机制有硬伤（扩缩容无触发路径）、删除路径与 identityRef 不兼容、RoleIdentity 语义未真正落地。对标范围上，本项目实质对标的是 **CAPA 的 EKS managed 子集**，且缺 ClusterClass（Template CRD）支持。

---

## 一、总体定位

| 维度 | CAPA | 本项目 |
|---|---|---|
| 模式 | 双模式：EC2 自管（kubeadm）+ EKS 托管 | 仅托管（CCE Standard/Turbo），等价 EKS 模式 |
| CRD 数 | ~20 种（含 Template×5、bootstrap×4、Fargate、ROSA） | 6 种（3 主对象 + 3 身份） |
| feature gates | 14 个 | 1 个（NodePoolAutoscaling，Alpha 默认关） |
| 成熟度 | 生产级（CNCF 孵化，E2E 门禁齐全） | PoC 验证版 |

## 二、对象模型（API 面）

| CAPI 契约 | CAPA | 本项目 | 差距 |
|---|---|---|---|
| InfrastructureCluster | AWSCluster / AWSManagedCluster | CCECluster（薄壳：region+network） | ✅ 等价 |
| ControlPlane（托管） | AWSManagedControlPlane | CCEManagedControlPlane | ✅ 对齐（见能力矩阵） |
| InfrastructureMachinePool | AWSManagedMachinePool / AWSMachinePool(ASG) | CCEManagedMachinePool | 🟡 部分 |
| 身份×3 | AWSClusterController/Static/RoleIdentity | CCECluster 同名三类 | ✅ 面上对齐（实现缺陷见下） |
| ClusterClass 模板 | AWSClusterTemplate、AWSManagedControlPlaneTemplate 等 5 种 | **无任何 Template CRD** | 🔴 **不支持 ClusterClass**——现代 CAPI 用户（拓扑编排、`clusterctl move`、多集群批量）会直接被挡 |
| Fargate | AWSFargateProfile | 无（CCE 超节点/Autopilot 远期） | 🔴 远期 |
| 转换 webhook | v1beta1↔v1beta2 | 单版本 v1beta1 | 🟡 |

另注：`CCEManagedMachinePool.spec` 无 `providerIDList`（CAPI v1beta2 MachinePool 契约字段），`clusterctl describe` 无法展示逐节点 provider ID。

## 三、控制器实现质量——本次审查发现的三个 P0 问题

CAPA 的 reconciler 普遍使用 `Owns()`/`Watches()`/`WatchesRawSource()` 联动关联对象；本项目三个 controller 均只有 `For(自身)`（已 grep 证实零 watch），由此产生：

### P0-1：扩缩容没有触发路径（功能性缺陷）

- `syncReplicasFromOwner` 的逻辑本身正确（从 CAPI MachinePool 拷贝 replicas 并调 ScaleNodePool），但它只在 CCEManagedMachinePool 自身被 reconcile 时执行。
- 已核实 CAPI v1.14 源码（模块缓存 `core/reconcilers/machinepool/`）：**核心 controller 不会把 `MachinePool.spec.replicas` 同步到 infra pool 的 spec**，且只在创建时对 infra pool 打一次 ownerRef，之后不再写它。
- 因此 `kubectl scale machinepool --replicas=5` 后：MachinePool 更新 -> 无人 watch -> infra pool 无 reconcile -> 扩缩容**无限期搁置**（直到某个无关事件触发）。稳态下 status 全量 `Update()` 写入为 no-op，不会产生自我唤醒事件。
- 测试掩盖了问题：`ccemanagedmachinepool_controller_test.go` 直接调用 Reconcile，绕过了触发链。
- **修复**：`SetupWithManager` 增加 `Watches(&clusterv1.MachinePool{}, handler.EnqueueRequestsFromMapFunc(映射到 infra pool))`（CAPA 的标准做法）。

### P0-2：删除路径忽略 identityRef（可能导致删除永久卡死）

- `reconcileDelete`（CP 与 MP 两个 controller）都只调 `ResolveCredentials(<cluster>-credentials)`，不查 `spec.identityRef`。
- 用 StaticIdentity/RoleIdentity 创建、且无 per-cluster Secret 的集群：删除时凭证解析报 NotFound -> 报错重试 -> **finalizer 永不移除，云资源与 CR 双双卡死**。
- **修复**：删除路径复用 normal 路径的凭证解析链。

### P0-3：CCEClusterRoleIdentity 的 agencyName 被丢弃

- `ccemanagedcontrolplane_controller.go:138`：`creds, _, err = scope.ResolveIdentity(...)`——agencyName 返回值直接丢给 `_`；`CreateClusterInput.AgencyName` 用的是 `cp.Spec.AgencyName`。
- 结果：RoleIdentity 目前**等价于环境变量凭证**，跨账户委托语义没有落地。CAPA 的 RoleIdentity 是真正调 STS AssumeRole 切换整套凭证链。
- **修复**：把 identity 解析出的 agency 传入 create input（或对 API 调用本身做委托切换）。

### 架构级差距（相对 CAPA 的 Scope 模式）

| CAPA 模式 | 本项目现状 |
|---|---|
| Scope 对象 + patchHelper，`defer scope.Close()` 统一落盘 | 无 Scope；39 处 `Status().Update()` 散落各分支（易冲突、易漏） |
| EventRecorder 记录 K8s events | **零事件**（grep 证实），用户只能看 conditions |
| Scope 内缓存 AWS session | 每次 reconcile `newCCEService` 重建 SDK client |
| `--aws-cluster-concurrency` 等并发控制 | 无 |
| SSA patch | 无 |
| ClusterCacheTracker（跨集群缓存） | 无（CCE 场景影响小） |

## 四、身份与凭证

| 能力 | CAPA | 本项目 |
|---|---|---|
| ControllerIdentity（默认凭证） | 单例 CR + `AutoControllerIdentityCreator` gate 自动创建 | CRD 存在，但**无自动创建逻辑**（CP 无 identityRef 时直接走 env） |
| StaticIdentity | Secret 引用 + allowedNamespaces | ✅ 等价（但 Secret namespace 硬编码 `capi-cce-system`，CAPA 用 controller 运行 namespace） |
| RoleIdentity | STS AssumeRole 真实换凭证，支持链式 sourceIdentityRef、durationSeconds | 🔴 agency 丢弃（P0-3），无链式解析 |
| allowedNamespaces | list+selector（OR） | ✅ 已实现同语义 |
| 凭证回退链 | identityRef -> ControllerIdentity -> IRSA | identityRef -> per-cluster Secret -> env（normal 路径）/ 仅 per-cluster Secret（delete 路径，P0-2） |

**亮点**：`ResolveCredentials` 对"显式引用的 Secret 缺失"报错而非静默回退 env——这个跨租户安全考量写得很清楚，值得保留。

## 五、网络

| 维度 | CAPA | 本项目 |
|---|---|---|
| 模式 | managed VPC（自动建 VPC/3AZ 子网/IGW/NAT/路由表）或 BYO | **仅 BYO**（引用既有 VPC/subnet） |
| 校验 | 创建时由 AWS API 拒绝 | **创建前预检**（CIDR 格式/重叠、subnet 归属 VPC、eni 子网数与分离建议，含 Warning 级）——比 CAPA 更早失败，体验更好 |
| Secondary CIDR | 支持（pod 网独立 CIDR） | 无（CCE 无直接对应） |
| IPv6 / Edge Zone | 支持 | 无 |

BYO-only 对 CCE 场景合理（EKS 模式下 CAPA 也以 BYO 居多），预检是加分项。

## 六、控制面能力矩阵（AWSManagedControlPlane vs CCEManagedControlPlane）

| 能力 | CAPA EKS | 本项目 | 备注 |
|---|---|---|---|
| 生命周期/幂等创建/带外删除检测 | ✅ | ✅ | 带外删除后清 ClusterID 重建，处理正确 |
| 版本升级 | 逐 minor 步进 | ✅ 工作流（PreCheck -> 原地滚动 -> 轮询），前缀匹配目标版本，`UpgradeNotOffered` 当正常态 | 实现质量不错 |
| addons | ✅（含 conflictResolution） | ✅ 声明式差量 | service 层已支持 `Values map`，CRD 层只有 name+version，🟡 |
| pod identity | ✅ | ✅ 声明式差量 | |
| 控制面日志 | ✅ CloudWatch | ✅ UpdateClusterLogConfig + TTL + 差量比较 | |
| KMS/envelope 加密 | ✅ | ❌（仅节点盘加密） | |
| 认证模式/access entry | ✅（authenticationMode、accessEntries） | ❌ | gap 分析列为待实现的 AccessPolicy |
| OIDC/身份提供者 | ✅（IRSA 基础） | ❌（pod-identity 替代） | |
| endpoint 访问 | public/private/子网级 | 仅 `public bool` | 🟡 |
| 控制面规格 | scalingConfig tier | flavor（cce.s1.small…） | 等价表达 |
| kubeconfig | 双 Secret（user + CAPI），token ~10min 刷新 | 单 Secret，365d 证书 + 过期前 30d 轮换（解析证书 notAfter） | CCE 方案轮换时机更精准，但长周期证书安全性弱于短 token，🟡 |

## 七、节点池能力矩阵

| 能力 | CAPA Managed NodeGroup | 本项目 | 备注 |
|---|---|---|---|
| 扩缩容 | min/max/desired | 绝对值 replicas | ✅（除 P0-1 触发问题） |
| spot/竞价 | capacityType=spot | ❌ | billingMode 仅 0/1，订阅(1)还被 webhook 拒绝 |
| 多 AZ | ✅ | 单 AZ 字符串 | 🟡 |
| 滚动更新 | updateConfig（数量/百分比） | ✅ UpgradeNodePool + maxUnavailable(1-20)，属性漂移触发 | 对标到位 |
| taints/labels 同步 | ✅ | ✅ refresh 策略 | |
| 数据卷 | diskSize | rootVolume + **仅 1 个 dataVolume**（>1 被 webhook 拒绝） | 🟡 |
| 启动模板/自定义 AMI | ✅ | ❌（CCE 无对等，OS 字符串表达） | 裁剪合理 |
| 反向同步（external autoscaler） | replicas-managed-by 注解 | ❌ | autoscaling gate 仅正向 |
| 节点修复/生命周期钩子 | NodeRepair/LifecycleHooks | ❌ | |
| 带外删除自愈 | - | ✅ 检测后重建 | 加分 |

## 八、错误处理与弹性——本项目的相对亮点

这块**超过**自有 gap 分析给出的评价，设计密度对标 CAPA wait/retry：

- 错误分类完整：NotFound / Conflict（含 CIDR 冲突码、CCE_CM.0410 实测码）/ Throttle / Quota（10 个配额码）/ PermissionDenied（刻意排除 403 状态冲突码避免误入长退避）/ ScaleNoOp（实测平台行为）；
- 差异化退避：throttle 1min、quota 5min、权限 30min，且 throttle/quota **不作为 error 返回**（避免 controller-runtime backoff 覆盖延迟）；
- 删除前强制 ShowCluster 确认 404 才摘 finalizer（防瞬态错误泄漏云资源）；
- CCE `DeleteCluster` 显式级联选项（deleteEvs/ENI/ELB/SFS/OBS）——**比 EKS 依赖 GC 服务扫 tag 的方案更内聚**，云原生支持级联清理，是 CCE 平台优势的正当利用。

## 九、Webhook

| 维度 | CAPA | 本项目 |
|---|---|---|
| 覆盖 | 14 组（含全部身份/Template） | 3 组（mutate+validate×3），身份 CRD **无 webhook**（CAPA 对身份有校验） |
| 校验深度 | 云侧约束 + 语义 | 官方硬约束齐：taints≤20、SG≤5、rootVolume 40-1024、maxUnavailable 1-20、AZ/OS 必填、flavor 正则+部署级允许列表、订阅计费显式拒绝（比静默失败好） |
| 不可变字段 | ✅ | ✅ containerNetwork CIDR/mode、category |
| 转换 webhook | ✅ | ❌ |

## 十、测试与工程化

| 维度 | CAPA | 本项目 |
|---|---|---|
| 单元/envtest | Ginkgo 全家桶 + mock services（生成式 mock） | go test 原生 + fakes + envtest；**覆盖了主要分支，但直接调 Reconcile，掩盖了 watch 缺失**（P0-1 的教训：测试应通过创建 owner 对象+更新事件驱动） |
| E2E | managed/unmanaged 双套件、升级/access entries/pod identity/GC 等十余场景 + Tilt + nightly | **占位**（skip） |
| 真实云验证 | （CI 付费池） | ✅ smoke（build tag）全流程 1042s PASS + 清理核查（0 残留）+ 详尽 findings 文档——PoC 阶段这点做得比多数同类项目扎实 |
| 可观测性 | events + OTel tracing | 无 events、无 tracing（默认 metrics） |
| clusterctl 打包 | ✅ | ✅（metadata.yaml + components，已在 kind+真实云验证） |

## 十一、优先级建议

**P0（正确性，先于一切功能）：**

1. 补 `Watches(&clusterv1.MachinePool{})`（修扩缩容触发）+ 补一个"事件驱动"的回归测试；
2. 删除路径接入 identityRef 凭证链；
3. RoleIdentity agency 传递到 CreateCluster（或明确降级该 CRD 语义并写入文档）。

**P1（对标补齐）：**

4. Template CRD 三件套（CCEClusterTemplate / CCEManagedControlPlaneTemplate / CCEManagedMachinePoolTemplate）-> ClusterClass；
5. endpointAccess 私网细粒度、多 AZ、竞价计费、dataVolumes 多卷；
6. K8s events + AutoControllerIdentityCreator。

**P2（工程化）：**

7. Scope/patchHelper 收口 status 写入、SDK client 缓存、并发 flag；
8. E2E（Ginkgo + CAPI test framework）、身份 CRD webhook、feature gate 体系扩充；
9. 转换 webhook（存储版演进时）。

## 十二、公平评价

本项目在有限投入下做对了最难的几件事：**契约正确**（v1beta2 initialization.provisioned/initialized、endpoint 类型细节、finalizer 顺序）、**幂等与自愈**（带外删除重建、scale no-op 识别）、**错误分类退避**、**真实云验证闭环**。P0 三项都是"最后一公里"的触发/链路问题，修复成本很低（估计各 <100 行），但修复前"对标 CAPA"在扩缩容这一核心卖点上尚不成立。
