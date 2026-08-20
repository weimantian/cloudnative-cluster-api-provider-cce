# 与 CAPA(EKS 托管模式)能力对标差距分析

> 对标基准:CAPA(Cluster API Provider AWS)EKS managed 模式,源码 `/tmp/capa`(67de5c2)。
> 本 Provider 现状:cloudnative-cluster-api-provider-cce(PoC 验证版)。
> 说明:CCE 与 EKS 是不同云,本分析标注"CCE 云能力映射"--差距项分「云有对应能力待实现」「云无直接对应需替代」「EKS 特有、CCE 场景可裁剪」。
> 状态更新(2026-08):P0/P1/P2 补齐已完成(Scope patchHelper/身份 webhook/多 AZ/竞价/ClusterClass 模板/e2e/v1beta2 存储版+转换 webhook/并发 flag/事件),各行已同步;feature gates 扩充经对标核定为不适用(判定准则见阶段 2 第 10 条)。

## 一、总览

| 能力维度 | CAPA EKS | CCE Provider 现状 | 差距 |
|---|---|---|---|
| CRD 面 | 6 类(ManagedControlPlane/ManagedCluster/ManagedMachinePool/FargateProfile/身份×3/bootstrap×2) | 9 类(3 主对象 + 3 ClusterClass 模板 + 3 身份;v1beta1+v1beta2 双版本) | 🟡 缺 Fargate/bootstrap(CCE 托管网池无需 bootstrap) |
| 控制面生命周期 | 创建/删除/版本升级/endpoint 写回 | ✅ 同 | ✅ 对齐 |
| 控制面配置能力 | addons、日志、KMS 加密、IAM 认证模式、OIDC、身份提供者、pod identity、access entry、控制面扩容、SecondaryCIDR | addons ✅ / pod-identity ✅ / 日志 ✅;版本/网络/endpoint/billing/agency;余 KMS/认证模式/access entry 等待补 | 🟡 部分 |
| 节点组 | 扩缩容、spot、AMI、启动模板、labels/taints、remoteAccess、磁盘、滚动更新、节点修复、多 AZ、生命周期钩子 | flavor/os/磁盘/SG/taints/labels/autoscaling/绝对值扩缩容/滚动更新(UpgradeNodePool) | 🟡 部分 |
| 凭证身份 | ControllerIdentity/StaticIdentity/RoleIdentity + allowedNamespaces | ✅ 三类身份 CRD(CCEClusterController/Static/RoleIdentity,Role 用委托 agency 替代 AssumeRole)+ identityRef + allowedNamespaces 校验 | ✅ 已实现 |
| 特性开关 | 13 个 feature gate | 2 个(NodePoolAutoscaling/AutoControllerIdentityCreator) | ⚪ 不适用:CAPA gate 的是整模式/新 CRD 面/主动云侧行为(EKS/ROSA/MachinePool/GC/IAM),其声明式 spec 能力(addons/OIDC/access entries/日志)均不 gate;现有 2 gate 已覆盖全部行为分叉与主动行为(判定准则见阶段 2 第 10 条) |
| conditions 全集 | 控制面/节点池/Fargate/身份/网络/bootstrap 多组 | 10 个(Network/Credentials/Cluster/Kubeconfig/Upgrade/Addons/PodIdentity/Logging/NodePool/Scaling) | 🟡 少但覆盖主流程 |
| 架构 | Scope 模式 + 服务接口工厂 + 错误聚合 + GC + tag 所有权 | ✅ Scope patchHelper(defer 单次全对象落盘)+ 服务接口工厂 + tag 所有权 + SDK client 缓存 + 并发 flag + K8s events;无 GC | 🟡 仅剩 GC |

---

## 二、控制面(CCEManagedControlPlane vs AWSManagedControlPlane)

| CAPA 能力 | CAPA 字段/机制 | CCE 现状 | CCE 云能力映射 | 差距 |
|---|---|---|---|---|
| 版本升级 | Version + 滚动/强制策略 | ✅ 升级工作流(CreateUpgradeWorkFlow→PreCheck→UpgradeCluster→轮询) | 原地升级 inPlaceRollingUpdate | ✅ |
| endpoint access | EndpointAccess(public/private)+ 子网限制 | ✅ public + cidrs 公网白名单(publicAccess.cidrs) | CCE 私网端点常开(无开关),公网支持白名单网段 | ✅ 对齐(CCE 语义) |
| 网络 | NetworkSpec(VPC/subnet)+ SecondaryCIDR | ✅ VPC/subnet 引用校验 | CCE 单容器网段(vpc-router/eni) | 🟡 无 secondary CIDR 概念(CCE 无) |
| 日志 | Logging(control plane logs) | ✅ 已实现(`spec.logging` → `UpdateClusterLogConfig`/`ShowClusterConfig` 差量同步 + `LoggingConfigured` condition) | CCE 有 `UpdateClusterLogConfig`(4.1.16)/`ShowClusterConfig` | ✅ 已实现 |
| KMS 加密 | EncryptionConfig(KMS) | ❌ | CCE 磁盘加密(diskEncryption/系统盘加密) | 🟡 部分(节点盘加密) |
| IAM 认证模式 | AccessConfig(API/ConfigMap/API_AND_CONFIG_MAP) | ❌ | CCE 认证模式(authenticatingProxy/认证代理) | 🟡 待调研 |
| OIDC provider | OIDCProviderStatus | ❌ | CCE 无 EKS 式 OIDC(有 pod-identity 替代) | 🟡 用 pod-identity 替代 |
| 身份提供者 | IdentityProviderStatus | ❌ | CCE 认证代理/OIDC 需调研 | 🟡 |
| pod identity | EKSPodIdentityAssociationConfigured | ✅ 已实现(spec.podIdentityAssociations[] 声明式差量 + `PodIdentityAssociationsConfigured` condition) | CCE pod-identity(4.11.6 CreatePodIdentityAssociation/4.11.7 List/4.11.10 Delete) | ✅ 已实现 |
| access entry | AccessEntry | ❌ | CCE 访问策略(4.11.1 CreateAccessPolicy) | 🔴 待实现 |
| addons | EKSAddonsConfigured + Addon CRD | ✅ 已实现(spec.addons[] 声明式差量:create/upgrade/delete + `AddonsConfigured` condition) | CCE 插件(4.4 CreateAddonInstance/UpdateAddonInstance/ListAddonInstances/DeleteAddonInstance) | ✅ 已实现 |
| Fargate | FargateProfile CRD | ❌ | CCE Autopilot/超节点(ListHyperNodes 4.2.18) | 🔴 远期 |
| 控制面扩容 | ControlPlaneScalingConfig | ❌ | CCE 集群规格 flavor(cce.s1.small 等) | 🟡 以 flavor 表达 |

---

## 三、节点池(CCEManagedMachinePool vs AWSManagedMachinePool)

| CAPA 能力 | CAPA 字段 | CCE 现状 | CCE 云映射 | 差距 |
|---|---|---|---|---|
| 扩缩容 | Scaling{MinSize,MaxSize} | ✅ 绝对值 ScaleNodePool | ScaleNodePool/autoscaling | ✅ |
| 竞价/spot | CapacityType(spot/on-demand) | ✅ spot/spotPrice -> ExtendParam.marketType=spot(仅 billingMode=0,webhook 校验) | CCE 竞价实例(NodeExtendParam.marketType/spotPrice,已核 SDK) | ✅ 已实现 |
| AMI/镜像 | AMIVersion/AMIType | ✅ OS 字段 | CCE os(如 Huawei Cloud EulerOS 2.0) | ✅ |
| 启动模板 | AWSLaunchTemplate | ❌ | CCE 节点模板(nodeTemplate 固定字段,无自定义模板) | 🟡 CCE 无对等(裁剪) |
| labels/taints | Labels/Taints | ✅ + taint/label 同步策略(refresh) | 同 | ✅ |
| remoteAccess | RemoteAccess(SSH key) | ✅ sshKey | 同 | ✅ |
| 磁盘 | DiskSize | ✅ rootVolume + dataVolumes[](多数据卷) | 同 | ✅ 已实现 |
| 滚动更新 | UpdateConfig | ✅ 已实现(`spec.updateConfig.maxUnavailable` → 属性漂移时调 `UpgradeNodePool` 同步存量节点,对标 CAPA UpdateConfig 滚动更新) | 节点池同步 UpgradeNodePool(4.3.7) | ✅ 已实现 |
| 节点修复 | NodeRepairConfig | ❌ | CCE 节点自愈(需调研) | 🟡 |
| 多 AZ | AvailabilityZones + subnetType | ✅ extensionScaleGroups[](扩展伸缩组,各组独立 flavor/AZ) | CCE 多 AZ 走扩展伸缩组(nodeTemplate.az 单值,已核 SDK) | ✅ 已实现 |
| 生命周期钩子 | LifecycleHooks | ❌ | CCE 无对等(裁剪) | ⚪ 裁剪 |

---

## 四、凭证与身份

| CAPA | CCE 现状 | 差距 |
|---|---|---|
| AWSClusterControllerIdentity(默认凭证) | ✅ 等价 + AutoControllerIdentityCreator gate 自动创建 default 单例 + allowedNamespaces 校验 | ✅ 对齐 |
| AWSClusterStaticIdentity(AK/SK Secret) | per-cluster `<cluster>-credentials` Secret | ✅ 等价 |
| AWSClusterRoleIdentity(跨账户 AssumeRole) | ✅ agencyName 经 identityRef 解析传入 CreateCluster(显式 spec.agencyName 优先) | ✅ 等价(CCE 委托语义) |
| allowedNamespaces 校验 | ✅ 三类身份均校验(ControllerIdentity 一并生效)+ 身份 CRD 校验 webhook | ✅ 已实现 |

---

## 五、架构对标(CAPA 优点 → CCE 差距)

| CAPA 模式 | CCE 现状 | 差距 |
|---|---|---|
| Scope 模式(patchHelper + defer Close 统一落盘) | ✅ 已实现(CAPI patch.NewHelper + defer 单次全对象 patch,3 controller 收口,散落写入清零) | ✅ |
| 服务接口 + 工厂注入 | ✅ ServiceFactory(测试注入 fake) | ✅ |
| 错误聚合删除(kerrors.NewAggregate) | 🟡 单资源顺序删除 | 🟡 |
| 依赖计数删除 | 🟡 finalizer 顺序耦合已修(等 CP 先删) | ✅ 近似 |
| 外部资源 GC(tag 扫描) | ❌ | 🔴 待补(防残留 EIP/EVS) |
| tag 所有权模型(owned/shared + 云标签) | 🟡 已补 owned tag(创建集群/节点池自动打 `cluster-api-provider-cce.cluster.<name>=owned`(键用 `.` 分隔,CCE 标签键不允许 `/`),映射 CCE clusterTags/userTags) | ✅ 打标已实现;GC 按 tag 扫描清理遗留 EIP 待补(需 TMS 标签服务) |
| 限流中间件(token bucket) | 🟡 退避 requeue(无主动限流) | 🟡 |
| 多版本 v1beta1/v1beta2 + 转换 webhook | ✅ v1beta2 存储(Hub)+ v1beta1 服务 + Convertible(JSON 往返)+ /convert webhook(kustomize 补丁启用 Webhook 策略) | ✅ 已实现 |
| e2e(Ginkgo + clusterctl flavors) | ✅ env 门控真实生命周期 e2e(建->Ready->删->消失,零新依赖)+ smoke + envtest | 🟡 Ginkgo/clusterctl flavors 形态未跟进(现有测试已覆盖同流程) |

---

## 六、分阶段补齐建议

### 阶段 1(对标核心,近期)
1. **CCE 插件(addons)管理**:✅ 已实现--Service 四方法(Create/Update/List/DeleteAddonInstance)+ spec.addons[] 声明式差量 + `AddonsConfigured` condition。
2. **pod-identity / 访问策略**:✅ pod-identity 已实现(声明式差量 + `PodIdentityAssociationsConfigured` condition);访问策略(AccessPolicy)仍在剩余差距清单。
3. **GC 服务**:删除后按 tag 扫描清理遗留 EIP/EVS/ELB(对标 CAPA gc.NewService)。
4. **tag 所有权模型**:✅ 已实现--owned tag(`cluster-api-provider-cce.cluster.<name>=owned`)+ CCE clusterTags/userTags 映射;GC 按 tag 扫描仍待补(依赖 TMS)。

### 阶段 2(对标增强)
5. **身份 CRD**:✅ 已实现(CCEClusterControllerIdentity/StaticIdentity/RoleIdentity + allowedNamespaces,commit 687e1f1)。
6. **滚动更新**:✅ 已实现(UpgradeNodePool 同步节点池 + `spec.updateConfig.maxUnavailable` 滚动策略)。
7. **日志/配置**:✅ 已实现(UpdateClusterLogConfig + ShowClusterConfig + `LoggingConfigured` condition)。
8. **多 AZ 节点池**:✅ 已实现--SDK 核实 nodeTemplate.az 为单值,多 AZ 经 `extensionScaleGroups`(扩展伸缩组,各组独立 flavor/AZ)落地。
9. **spot/竞价节点**:✅ 已实现--`spot`+`spotPrice` 映射 ExtendParam.marketType=spot(SDK 已核:仅 billingMode=0 生效,不传价默认按需价,webhook 拒绝订阅+竞价组合)。
10. **feature gates 扩充**:⚪ 不适用(已决策,依据 CAPA 实际用法)。CAPA 的 13 个 gate 全部位于 main.go 控制器注册层,门控的是「整模式」(EKS/ROSA)、「新 CRD 面」(MachinePool/Fargate)、「基础设施依赖」(EventBridge)、「权限升级」(EKSEnableIAM)、「主动云侧行为」(GC/打标)——而其声明式 spec 能力(addons/OIDC/access entries/日志/KMS)均不 gate。本 provider 的 addons/pod-identity/logging/滚动更新同为声明式 spec 驱动,加 gate 反致 gate 关闭时静默忽略用户声明的 spec,属反模式。现有 2 个 gate(NodePoolAutoscaling=行为分叉、AutoControllerIdentityCreator=主动创建对象)已覆盖全部应 gate 场景。**将来何时该加 gate**:① 注册整套新 controller/CRD kind(如 Autopilot 模式);② provider 主动做 spec 之外的云侧操作(如 GC 扫删、改写 IAM/委托);③ 存在两套互斥行为语义。纯 spec 声明式能力永不 gate。

### 阶段 3(远期/裁剪)
11. **Fargate/Autopilot**:评估 CCE Autopilot/超节点在 CAPI 的建模(远期)。
12. **e2e**:✅ 已实现--env 门控真实生命周期测试(E2E_MANAGEMENT_KUBECONFIG + CCE_* 变量,建集群->控制面 Ready->节点池 Ready->删除->消失),零新增依赖;Ginkgo/clusterctl flavors 形态作为后续可选。
13. **多版本转换 webhook**:✅ 已实现--v1beta2 存储版本(Hub)+ v1beta1 服务(Convertible,JSON 往返无损)+ /convert webhook + CRD kustomize 补丁(Webhook 策略);deploy-kind.sh 已同步注入转换 webhook caBundle;另修复 identity CRD 未入 kustomization 的存量 bug。

---

## 六b、细节对齐点(逐项核实)

| 对标点 | CAPA | CCE 现状 | 结论 |
|---|---|---|---|
| 控制面 initialized 合约 | `Status.Initialized`(kubeconfig 写回后置位;CAPI 1.14 contract 读 `status.initialized`) | ✅ `status.initialized` 已实现且被 CAPI 识别 | ✅ 对齐 |
| ExternalManagedControlPlane | CAPA 内部字段(CAPI 1.13 时代),CAPI 1.14 改用 `status.initialized` | 无(无需,CAPI 1.14 不读) | ⚪ 无需 |
| kubeconfig 双 Secret | `<cluster>-kubeconfig`(CAPI 消费)+ `<cluster>-user-kubeconfig`(用户) | 仅 `<cluster>-kubeconfig` | 🟡 缺 user-kubeconfig |
| 节点组三通道升级 | K8s 版本 / AMI 版本 / 启动模板版本 差量 | 仅集群级 K8s 升级;节点池 OS 升级(Q11b)独立 | 🟡 缺 AMI/LT 通道(CCE 以 OS 镜像升级替代) |
| 自动升级告警 | UpgradePolicy=standard 被 AWS 自动升级 → Ready=false+FailureMessage | 无;CCE 维护周期 24 个月,过期需升级 | 🟡 可加"版本 EOS 告警" |
| 外部 autoscaler 反向同步 | `replicas-managed-by` 注解 → 反向同步 ASG DesiredCapacity | autoscaling Alpha gate(仅正向映射) | 🟡 缺反向同步 |
| ProviderIDList 回填 | `Status.Replicas` + `Spec.ProviderIDList` | `Status.Replicas(currentNode)` + `AvailableReplicas(activeNode)`,无 providerID | 🟡 可回填 CCE nodeID |

## 七、结论

**已对齐核心**:托管集群生命周期(创建/删除/版本升级/kubeconfig/节点池 CRUD/扩缩容/属性同步)、CAPI v1beta2 契约(原生 v1beta2 存储版本 + 转换 webhook)、ClusterClass 模板三件套、身份三件套(allowedNamespaces/自动创建/CRD webhook)、Scope patchHelper/事件/并发/SDK client 缓存、多 AZ/竞价/多数据卷/公网 CIDR 白名单、服务接口工厂、幂等与错误分类、真实云冒烟 + env 门控 e2e——已覆盖 CAPA EKS managed 的主骨架与大部分外围。

**剩余差距**(按价值排序):① CCE 访问策略(AccessPolicy,对标 access entry);② 资源 GC 按 tag 扫描清理遗留 EIP/EVS/ELB(需 TMS);③ KMS/envelope 加密与 IAM 认证模式;④ 节点修复/外部 autoscaler 反向同步/user-kubeconfig 等细节;⑤ Ginkgo+clusterctl flavors 形态的 e2e。

上述差距中,①③ 在 CCE 云均有对应 API,属"待实现"而非"云能力缺失";② 依赖 TMS 标签服务;④⑤ 是工程化投入。feature gates 扩充经对标核定为不适用(判定准则见阶段 2 第 10 条)。
