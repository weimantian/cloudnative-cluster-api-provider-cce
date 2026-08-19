# 与 CAPA(EKS 托管模式)能力对标差距分析

> 对标基准:CAPA(Cluster API Provider AWS)EKS managed 模式,源码 `/tmp/capa`(67de5c2)。
> 本 Provider 现状:cloudnative-cluster-api-provider-cce(PoC 验证版)。
> 说明:CCE 与 EKS 是不同云,本分析标注"CCE 云能力映射"——差距项分「云有对应能力待实现」「云无直接对应需替代」「EKS 特有、CCE 场景可裁剪」。

## 一、总览

| 能力维度 | CAPA EKS | CCE Provider 现状 | 差距 |
|---|---|---|---|
| CRD 面 | 6 类(ManagedControlPlane/ManagedCluster/ManagedMachinePool/FargateProfile/身份×3/bootstrap×2) | 3 类(CCECluster/CCEManagedControlPlane/CCEManagedMachinePool) | 🔴 缺 Fargate/身份/bootstrap |
| 控制面生命周期 | 创建/删除/版本升级/endpoint 写回 | ✅ 同 | ✅ 对齐 |
| 控制面配置能力 | addons、日志、KMS 加密、IAM 认证模式、OIDC、身份提供者、pod identity、access entry、控制面扩容、SecondaryCIDR | 仅版本/网络/endpoint/billing/agency | 🔴 大面积缺失 |
| 节点组 | 扩缩容、spot、AMI、启动模板、labels/taints、remoteAccess、磁盘、滚动更新、节点修复、多 AZ、生命周期钩子 | flavor/os/磁盘/SG/taints/labels/autoscaling/绝对值扩缩容 | 🟡 部分 |
| 凭证身份 | ControllerIdentity/StaticIdentity/RoleIdentity + allowedNamespaces | ✅ 三类身份 CRD(CCEClusterController/Static/RoleIdentity,Role 用委托 agency 替代 AssumeRole)+ identityRef + allowedNamespaces 校验 | ✅ 已实现 |
| 特性开关 | 13 个 feature gate | 1 个(NodePoolAutoscaling) | 🔴 |
| conditions 全集 | 控制面/节点池/Fargate/身份/网络/bootstrap 多组 | 7 个(Network/Credentials/Cluster/Kubeconfig/Upgrade/NodePool/Scaling) | 🟡 少但覆盖主流程 |
| 架构 | Scope 模式 + 服务接口工厂 + 错误聚合 + GC + tag 所有权 | 服务接口工厂有;无 Scope patch helper、无 GC、无 tag 所有权 | 🟡 |

---

## 二、控制面(CCEManagedControlPlane vs AWSManagedControlPlane)

| CAPA 能力 | CAPA 字段/机制 | CCE 现状 | CCE 云能力映射 | 差距 |
|---|---|---|---|---|
| 版本升级 | Version + 滚动/强制策略 | ✅ 升级工作流(CreateUpgradeWorkFlow→PreCheck→UpgradeCluster→轮询) | 原地升级 inPlaceRollingUpdate | ✅ |
| endpoint access | EndpointAccess(public/private)+ 子网限制 | 🟡 仅 `endpointAccess.public` | CCE 有 publicAccess/EIP 绑定(UpdateClusterEip) | 🟡 缺 private 细节 |
| 网络 | NetworkSpec(VPC/subnet)+ SecondaryCIDR | ✅ VPC/subnet 引用校验 | CCE 单容器网段(vpc-router/eni) | 🟡 无 secondary CIDR 概念(CCE 无) |
| 日志 | Logging(control plane logs) | ❌ | CCE 有 `UpdateClusterLogConfig`(4.1.16) | 🔴 待实现 |
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
| 竞价/spot | CapacityType(spot/on-demand) | ❌ | CCE 竞价实例(billingMode 扩展/竞价节点池) | 🟡 待调研 |
| AMI/镜像 | AMIVersion/AMIType | ✅ OS 字段 | CCE os(如 Huawei Cloud EulerOS 2.0) | ✅ |
| 启动模板 | AWSLaunchTemplate | ❌ | CCE 节点模板(nodeTemplate 固定字段,无自定义模板) | 🟡 CCE 无对等(裁剪) |
| labels/taints | Labels/Taints | ✅ + taint/label 同步策略(refresh) | 同 | ✅ |
| remoteAccess | RemoteAccess(SSH key) | ✅ sshKey | 同 | ✅ |
| 磁盘 | DiskSize | ✅ rootVolume/dataVolumes(单数据卷) | 同 | 🟡 仅 1 数据卷 |
| 滚动更新 | UpdateConfig | 🟡 升级走集群级原地滚动 | 节点池同步 UpgradeNodePool(4.3.7,未实现) | 🟡 待补 UpgradeNodePool |
| 节点修复 | NodeRepairConfig | ❌ | CCE 节点自愈(需调研) | 🟡 |
| 多 AZ | AvailabilityZones + subnetType | 🟡 单 availabilityZone | CCE az 单值 | 🟡 多 AZ 需调研 |
| 生命周期钩子 | LifecycleHooks | ❌ | CCE 无对等(裁剪) | ⚪ 裁剪 |

---

## 四、凭证与身份

| CAPA | CCE 现状 | 差距 |
|---|---|---|
| AWSClusterControllerIdentity(默认凭证) | env `CLOUD_SDK_AK/SK` 兜底 | 🟡 等价 |
| AWSClusterStaticIdentity(AK/SK Secret) | per-cluster `<cluster>-credentials` Secret | ✅ 等价 |
| AWSClusterRoleIdentity(跨账户 AssumeRole) | ❌ 无委托角色链 | 🟡 CCE 有委托(agencyName)+ 企业项目,可扩展 |
| allowedNamespaces 校验 | ❌ | 🟡 待补(多租户隔离) |

---

## 五、架构对标(CAPA 优点 → CCE 差距)

| CAPA 模式 | CCE 现状 | 差距 |
|---|---|---|
| Scope 模式(patchHelper + defer Close 统一落盘) | ❌ 无(已删死代码,改用逐分支 Status().Update) | 🟡 可重引入 scope 收口 |
| 服务接口 + 工厂注入 | ✅ ServiceFactory(测试注入 fake) | ✅ |
| 错误聚合删除(kerrors.NewAggregate) | 🟡 单资源顺序删除 | 🟡 |
| 依赖计数删除 | 🟡 finalizer 顺序耦合已修(等 CP 先删) | ✅ 近似 |
| 外部资源 GC(tag 扫描) | ❌ | 🔴 待补(防残留 EIP/EVS) |
| tag 所有权模型(owned/shared + 云标签) | 🟡 已补 owned tag(创建集群/节点池自动打 `sigs.k8s.io/cluster-api-provider-cce/cluster=<name>=owned`,映射 CCE clusterTags/userTags) | ✅ 打标已实现;GC 按 tag 扫描清理遗留 EIP 待补(需 TMS 标签服务) |
| 限流中间件(token bucket) | 🟡 退避 requeue(无主动限流) | 🟡 |
| 多版本 v1beta1/v1beta2 + 转换 webhook | ❌ 单 v1beta1 | 🟡 CAPI 合约要求 v1beta1;转换可后补 |
| e2e(Ginkgo + clusterctl flavors) | 🟡 smoke(build tag)+ envtest,e2e 占位 | 🔴 e2e 待补 |

---

## 六、分阶段补齐建议

### 阶段 1(对标核心,近期)
1. **CCE 插件(addons)管理**:Service 增加 CreateAddonInstance/ListAddonTemplates/UpdateAddonInstance/DeleteAddonInstance;CP spec 增加 `addons[]`(name+version),condition `AddonsConfigured` —— 对标 EKS addons。
2. **pod-identity / 访问策略**:Service 增加 CreatePodIdentityAssociation/ListPodIdentityAssociations + CreateAccessPolicy/ListAccessPolicy;对标 EKS pod identity / access entry。
3. **GC 服务**:删除后按 tag 扫描清理遗留 EIP/EVS/ELB(对标 CAPA gc.NewService)。
4. **tag 所有权模型**:统一 BuildParams 给 CCE 集群/节点池打 owned tag,幂等寻址 + GC 基础。

### 阶段 2(对标增强)
5. **身份 CRD**:CCEClusterControllerIdentity/StaticIdentity + allowedNamespaces(对标三类身份)。
6. **滚动更新**:实现 UpgradeNodePool(同步节点池)+ UpdateNodePool 的扩展伸缩组,补节点池级滚动策略。
7. **日志/配置**:UpdateClusterLogConfig + ShowClusterConfig,暴露控制面日志开关。
8. **多 AZ 节点池**:availabilityZone 扩展为 []string(需 CCE 支持多 AZ 节点池调研)。
9. **spot/竞价节点**:billingMode 扩展竞价实例(调研 CCE 竞价节点池)。
10. **feature gates 扩充**:为上述能力加 gate(默认关),对标 CAPA 13 gate 的隔离策略。

### 阶段 3(远期/裁剪)
11. **Fargate/Autopilot**:评估 CCE Autopilot/超节点在 CAPI 的建模(远期)。
12. **e2e**:Ginkgo + clusterctl flavors + 预检配额(对标 CAPA test/e2e)。
13. **多版本转换 webhook**:v1beta2 或 v1beta1 存储版演进时补齐。

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

**已对齐核心**:托管集群生命周期(创建/删除/版本升级/kubeconfig/节点池 CRUD/扩缩容/属性同步)、CAPI v1beta2 契约、服务接口工厂、幂等与错误分类、真实云冒烟——这已覆盖 CAPA EKS managed 的**主骨架**。

**主要差距**(按价值排序):① CCE 插件管理;② pod-identity/访问策略;③ 资源 GC + tag 所有权;④ 身份 CRD;⑤ 节点池滚动更新(UpgradeNodePool);⑥ 多 AZ/竞价节点;⑦ e2e 与 feature gate 体系。

上述差距中,①②③⑤⑥ 在 CCE 云均有对应 API(此前官方 API 参考 PDF 已确认),属"待实现"而非"云能力缺失";④ 是通用架构能力;⑦ 是工程化投入。
