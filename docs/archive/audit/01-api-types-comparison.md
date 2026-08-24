# 01 — API 类型层字段级对标报告

> CCE Provider（`cloudnative-cluster-api-provider-cce`）vs CAPA EKS 托管模式（`/tmp/capa`）
>
> 基线：CAPA commit `67de5c2`（v2.10 trunk，CAPI v1.13.4）；CCE 采用 CAPI v1.14.0，单存储版本 `v1beta2`。
>
> 范围：本模块对照 **9 个 CCE CRD 类型**（对照 CAPA 对等/相邻类型）的 Spec/Status 字段级差异，含共享子类型与 Condition 常量。
>
> 方法：以**当前代码的真实 JSON tag + kubebuilder marker**为准（`grep -n 'json:"'` + `sed -n` 逐段读取），**不采信**既有对标文档；`/tmp/capa` 与 CCE 源码均已逐文件核对。

---

## 1. 执行摘要

| 严重度 | 数量 | 概要 |
|---|---|---|
| **P0** | 0 | 无阻断性缺失。CCE 已覆盖托管集群生命周期（建/删/扩/升级/addon/pod-identity/kubeconfig）核心 API 面 |
| **P1** | 4 | 网络 Spec 归属架构分歧、节点池模板不对等、RoleIdentity 字段面不足、Access 管理语义差异 |
| **P2** | 6 | Endpoint 私有端点缺失、日志形态差异、UpgradePolicy/ScalingConfig 缺失、启动模板/AMI 术语缺失、生命周期钩子语义差异、AdditionalTags 缺失 |
| **P3** | 若干 | 命名差异（eksClusterName vs clusterName）、ObservedGeneration 缺失、ExternalManagedControlPlane 无对等 |

**最重要的结构发现**（详见 §3.1/§3.3）：

1. **网络 Spec 归属不同**：CAPA 托管模式下 `AWSManagedCluster.Spec` 为**空结构体**，VPC/子网/CNI 网络全部在 `AWSManagedControlPlane.Spec.NetworkSpec`；而 CCE 将 VPC/子网/NAT 放在 `CCECluster.Spec.Network`（基础设施集群），把 Pod/Service CIDR 放在 `CCEManagedControlPlane.Spec.containerNetwork/serviceNetwork`。二者对 VPC 生命周期归属的划分完全不同，直接影响 ClusterClass 拓扑与 BYO/托管网络的语义。
2. **节点池模板不对等**：CCE 提供 `CCEManagedMachinePoolTemplate`，CAPA **没有** `AWSManagedMachinePoolTemplate`（托管节点池无模板类型）。CCE 在此处比 CAPA 更完整，但映射非标准。
3. **CAPA 独有类型（CCE 全缺）**：`AWSFargateProfile`、`AWSMachine`、`AWSMachineTemplate`、`AWSMachinePool`（自管理路径），以及 CNI/KubeProxy/Bastion/OIDC/IAM-Role 预置等能力字段。

---

## 2. CRD 清单对照

| CCE CRD | API 组 | CAPA 对等/相邻 | 说明 |
|---|---|---|---|
| `CCECluster` | `infrastructure.cluster.x-k8s.io/v1beta2` | `AWSManagedCluster`（托管）/ `AWSCluster`（自管理） | 基础设施集群；CCE 带 Network Spec |
| `CCEClusterTemplate` | `infrastructure/v1beta2` | `AWSManagedClusterTemplate` | ClusterClass 模板 |
| `CCEManagedControlPlane` | `controlplane/v1beta2` | `AWSManagedControlPlane` | 托管控制面 |
| `CCEManagedControlPlaneTemplate` | `controlplane/v1beta2` | `AWSManagedControlPlaneTemplate` | 控制面模板 |
| `CCEManagedMachinePool` | `infrastructure/v1beta2` | `AWSManagedMachinePool`（exp） | 托管节点池 |
| `CCEManagedMachinePoolTemplate` | `infrastructure/v1beta2` | **无** | CCE 独有（CAPA 无托管节点池模板） |
| `CCEClusterControllerIdentity` | `infrastructure/v1beta2` | `AWSClusterControllerIdentity` | 控制器身份 |
| `CCEClusterStaticIdentity` | `infrastructure/v1beta2` | `AWSClusterStaticIdentity` | 静态凭证身份 |
| `CCEClusterRoleIdentity` | `infrastructure/v1beta2` | `AWSClusterRoleIdentity` | 角色（agency/role）身份 |

CAPA 侧 CCE **完全无对等**的 CRD：`AWSFargateProfile`、`AWSMachine`、`AWSMachineTemplate`、`AWSMachinePool`（自管理节点池）。

> 注：`00-summary.md` 所述「11 个 CRD 类型」与实际不符——CCE 实际为 **9 个** CRD 类型（3 身份 + 6 托管/模板），详见本报告修正。

---

## 3. 逐类型字段级对标

图例：✅ 对等 · 🟡 部分对等（云能力差异）· ⚪ CCE 无此概念 · ➕ CCE 独有（CAPA 无）· 🔀 归属位置不同

### 3.1 CCECluster ↔ AWSManagedCluster（+ AWSCluster 网络）

#### Spec

| CCE 字段（json tag） | 类型 | CAPA 字段（json tag） | 类型 | 判定 |
|---|---|---|---|---|
| `region` | `string` | `region`（在 `AWSManagedControlPlane.Spec`） | `string` | 🔀 region 在 CAPA 托管模式下属于控制面 Spec，不在集群 |
| `network.vpc` | `*common.VPC` | `network.vpc`（在 `AWSManagedControlPlane.Spec.NetworkSpec`） | `VPCSpec` | 🔀 归属不同 + 字段面差异（见 §3.8） |
| `network.subnets` | `[]*common.Subnet` | `network.subnets`（同上） | `Subnets` | 🔀 归属不同 + 字段面差异 |
| `network.natGateway` | `*common.NatGatewaySpec` | —（NAT 在 CAPA 由子网路由隐式表达，Status 有 `natGatewaysIPs`） | — | ⚪ CCE 显式 NAT 三态，CAPA 托管无 NAT Spec |
| — | — | `identityRef`（`AWSManagedControlPlane.Spec`） | `*AWSIdentityReference` | 🔀 CCE 集群级身份在控制面/集群经 `identityRef` 链（见 §3.7） |

> `AWSManagedCluster.Spec` 在 CAPA 托管模式为**空结构体**（无任何 `json` 字段）；其网络与身份全部下沉到 `AWSManagedControlPlane`。CCE 选择把 VPC/子网/NAT 留在 `CCECluster`，是二者最大的结构分歧。

#### Status

| CCE 字段 | 类型 | CAPA 字段 | 类型 | 判定 |
|---|---|---|---|---|
| `ready` | `bool` | `ready` | `bool` | ✅ |
| `conditions` | `[]metav1.Condition` | `conditions` | `Conditions` | ✅ |
| `clusterID` | `string` | —（CAPA 无集群 ID 概念，EKS 名称即标识） | — | ➕ CCE 独有 |
| `initialization` | `ClusterInitializationStatus` | — | — | ➕ |
| `provisioned`（废弃） | `bool` | — | — | ➕ 遗留 |
| — | — | `controlPlaneEndpoint` | `APIEndpoint` | 🔀 CCE 的 endpoint 在 `CCEManagedControlPlane.Status`，不在集群 |
| — | — | `failureDomains` | `FailureDomains` | ⚪ CCE 无 FailureDomains（未暴露 AZ 故障域到集群层） |

**结论**：CCECluster 是「富 Spec + 薄 Status」，AWSManagedCluster 是「空 Spec + 薄 Status」。CCE 把网络放在基础设施集群是合理选择，但与 CAPA 托管模式（网络在控制面）不可直接互换 ClusterClass 拓扑。

---

### 3.2 CCEClusterTemplate ↔ AWSManagedClusterTemplate

| 字段 | CCE | CAPA | 判定 |
|---|---|---|---|
| `spec.template.spec` | `CCEClusterSpec`（嵌入） | `AWSClusterTemplateSpec` → `AWSClusterSpec`（嵌入） | ✅ 结构对等 |
| `spec.template.metadata` | `ObjectMeta` | `ObjectMeta` | ✅ |

> 模板均为「外层 `Spec.Template` 嵌入主类型 Spec」的标准 ClusterClass 形态，字段随主类型差异（§3.1）。无额外独立字段。

---

### 3.3 CCEManagedControlPlane ↔ AWSManagedControlPlane

#### Spec（核心字段）

| CCE 字段 | 类型 | CAPA 字段 | 类型 | 判定 |
|---|---|---|---|---|
| `clusterName` | `string` | `eksClusterName` | `string` | ✅（命名差异 P3） |
| `version` | `string` | `version` | `*string` | ✅ |
| `flavor` | `string` | `controlPlaneScalingConfig` | `*ControlPlaneScalingConfig` | 🟡 CCE 用 flavor 表达控制面规格；CAPA 用 autoscaling 配置（tier），语义近似但形态不同 |
| `category` | `string` | — | — | ➕ CCE 独有（CCE Turbo/标准分类） |
| `identityRef` | `*corev1.ObjectReference` | `identityRef` | `*AWSIdentityReference` | 🟡 类型不同：CCE 用 K8s 原生 ObjectReference，CAPA 用自定义 `AWSIdentityReference`（kind+name） |
| `agencyName` | `string` | `roleName` / `rolePath` / `roleAdditionalPolicies` / `rolePermissionsBoundary` | 多个 | ⚪ CCE 单 agencyName；CAPA 整套 IAM Role 预置参数（CCE 无 IAM Role 概念） |
| `containerNetwork` | `ContainerNetworkSpec` | `network`（NetworkSpec） | `NetworkSpec` | 🔀 见 §3.8：CCE 容器网段 vs CAPA VPC+CNI |
| `serviceNetwork` | `ServiceNetworkSpec` | — | — | ➕ CCE 显式 service CIDR；CAPA 由 EKS 托管 |
| `ipv6enable` | `*bool` | `network.vpc.ipv6` | `IPv6` | 🟡 CCE 顶层开关；CAPA 在 VPC 内嵌 IPv6 |
| `enableAutopilot` | `*bool` | —（Autopilot/Fargate 是独立 `AWSFargateProfile` CRD + 无 autopilot 字段） | — | 🟡 CCE 用开关；CAPA 用独立 FargateProfile CRD |
| `endpointAccess` | `EndpointAccessSpec` | `endpointAccess` | `EndpointAccess` | 🟡 见下（CCE 缺 private 字段） |
| `billing` | `BillingSpec` | — | — | ➕ CCE 独有（按需/包年包月） |
| `addons` | `[]AddonSpec` | `addons` | `*[]Addon` | ✅（字段面见下） |
| `podIdentityAssociations` | `[]PodIdentityAssociationSpec` | `podIdentityAssociations` | `[]PodIdentityAssociation` | ✅ |
| `logging` | `*ControlPlaneLoggingSpec` | `logging` | `*ControlPlaneLoggingSpec` | 🟡 形态差异（见下） |
| `accessPolicies` | `[]AccessPolicySpec` | `accessConfig` + `accessEntries` | `*AccessConfig` + `[]AccessEntry` | 🟡 语义差异（见 §3.9） |
| `encryptionConfig` | `*EncryptionConfigSpec` | `encryptionConfig` | `*EncryptionConfig` | ✅ |
| `authentication` | `*AuthenticationSpec` | `accessConfig.authenticationMode` | `EKSAuthenticationMode` | 🟡 CCE 独立字段（rbac/authenticating_proxy）+ authenticatingProxy CA/cert/key；CAPA 用 AuthenticationMode + `iamAuthenticatorConfig`（mapRoles/mapUsers/backendMode） |
| `customSan` | `[]string` | — | — | ➕ CCE 独有（API server SAN） |
| `controlPlaneEndpoint` | `*APIEndpoint` | `controlPlaneEndpoint` | `APIEndpoint` | ✅ |

#### CAPA 独有 Spec 字段（CCE 无）

| CAPA 字段 | 说明 | 判定 |
|---|---|---|
| `network`（VPC/Subnets/CNI） | CCE 拆到 CCECluster | 🔀 |
| `secondaryCidrBlock` | EKS Secondary CIDR | ⚪ CCE 用 `containerNetwork.cidrs[]` 表达多容器网段（等价但形态不同） |
| `partition` | AWS 分区（aws-cn 等） | ⚪ 无对等 |
| `sshKeyName` | 控制面跳板密钥 | ⚪ CCE 无控制面 SSH 概念 |
| `additionalTags` | 附加标签 | ⚪ CCE 无附加标签字段（标签走 VPC/子网/NAT owned tag） |
| `bastion` | 跳板机 | ⚪ |
| `tokenMethod` / `associateOIDCProvider` | EKS token 获取方式 / 自动关联 OIDC provider | ⚪ |
| `oidcIdentityProviderConfig` | 声明式 OIDC IdP 配置 | ⚪（CCE OIDC 依赖 kube-apiserver 参数，非 CRD 字段） |
| `vpcCni` / `kubeProxy` | CNI/KubeProxy 附加组件配置 | ⚪ CCE 无 CNI 插件可配性（CCE 自带 CNI） |
| `bootstrapSelfManagedAddons` | 引导自管理 addon | ⚪ |
| `restrictPrivateSubnets` | 限制私有子网 | ⚪ |
| `upgradePolicy` | 升级策略（type/version） | ⚪ CCE 升级经 version diff + 工作流，无显式策略字段 |
| `imageLookupFormat/Org/BaseOS` | AMI 镜像查找 | ⚪ CCE 无 AMI 概念 |
| `roleAdditionalPolicies` 等 IAM 字段 | IAM 权限 | ⚪ |

#### Status

| CCE 字段 | CAPA 字段 | 判定 |
|---|---|---|
| `ready` | `ready` | ✅ |
| `initialized` | `initialized` | ✅ |
| `conditions` | `conditions` | ✅ |
| `clusterID` | — | ➕ |
| `controlPlaneEndpoint` | `controlPlaneEndpoint` | ✅ |
| `version` | `version` | ✅ |
| `kubeconfigSecretName` | —（CAPA 用固定 Secret 命名约定） | ➕ |
| `upgradeTaskID` | — | ➕（CCE 升级任务追踪） |
| `initialization` / `controlPlaneInitialized`（废弃） | — | ➕ |
| — | `networkStatus` | ⚪ CCE 网络状态在 CCECluster |
| — | `failureDomains` | ⚪ |
| — | `bastion` | ⚪ |
| — | `oidcProvider` | ⚪ |
| — | `externalManagedControlPlane` | ⚪（CCE 无外部托管控制面模式） |
| — | `addons`（`[]AddonState`） | 🟡 CCE addon 状态未回填到 Status |
| — | `identityProviderStatus` | ⚪ |
| — | `observedGeneration` | ⚪ CCE 缺 observedGeneration（P3） |
| — | `failureMessage` | 🟡 CCE 经 conditions 表达失败，无独立 failureMessage |

---

### 3.4 CCEManagedControlPlaneTemplate ↔ AWSManagedControlPlaneTemplate

| 字段 | CCE | CAPA | 判定 |
|---|---|---|---|
| `spec.template.spec` | `CCEManagedControlPlaneSpec`（嵌入） | `AWSManagedControlPlaneSpec`（嵌入） | ✅ 结构对等 |

> 字段随 §3.3 主类型差异。无额外独立字段。

---

### 3.5 CCEManagedMachinePool ↔ AWSManagedMachinePool

#### Spec（核心字段）

| CCE 字段 | 类型 | CAPA 字段 | 类型 | 判定 |
|---|---|---|---|---|
| `clusterName` | `string` | —（CAPA 经 OwnerRef 关联） | — | ➕ CCE 显式 clusterName |
| `nodePoolName` | `string` | `eksNodegroupName` | `string` | ✅（命名差异 P3） |
| `flavor` | `string` | `instanceType` | `*string` | ✅ 语义对等（CCE flavor vs AWS instanceType） |
| `os` | `string` | `amiVersion` / `amiType` | `*string` / `*ManagedMachineAMIType` | 🟡 CCE OS 字段；CAPA AMI 体系（version+type） |
| `rootVolume` | `*common.NodeVolume` | `diskSize` | `*int32` | 🟡 CCE 结构化 rootVolume（size+type）；CAPA 单 diskSize int |
| `dataVolumes` | `[]common.NodeVolume` | — | — | ➕ CCE 多数据盘 |
| `sshKey` | `string` | `remoteAccess.sshKeyName` | `*string` | 🔀 CCE 顶层；CAPA 在 remoteAccess 内 |
| `availabilityZone` | `string` | `availabilityZones` | `[]string` | 🟡 CCE 单 AZ + extensionScaleGroups 多 AZ；CAPA 多 AZ 数组 + subnetType |
| `replicas` | `int32` | `scaling.minSize`/`maxSize` | `*int32` | 🔀 CCE 直接 replicas；CAPA 经 Scaling 结构 |
| `providerIDList` | `[]string` | `providerIDList` | `[]string` | ✅ |
| `billingMode` | `int32` | — | — | ➕ CCE 独有 |
| `spot` / `spotPrice` | `bool` / `string` | `capacityType` | `*ManagedMachinePoolCapacityType` | 🟡 CCE spot+价格；CAPA capacityType（onDemand/spot） |
| `extensionScaleGroups` | `[]ExtensionScaleGroupSpec` | `availabilityZones` + `availabilityZoneSubnetType` | 数组 | 🟡 CCE 扩展伸缩组（多 AZ）；CAPA AZ 数组 |
| `taints` | `[]string` | `taints` | `Taints` | 🟡 CCE 字符串数组；CAPA 结构化 `Taints`（key/value/effect） |
| `labels` | `map[string]string` | `labels` | `map[string]string` | ✅ |
| `securityGroups` | `[]string` | `remoteAccess.sourceSecurityGroups` | `[]string` | 🔀 归属不同 |
| `autoscaling` | `AutoscalingSpec` | `scaling` | `*ManagedMachinePoolScaling` | ✅ 语义对等（enable/min/max） |
| `updateConfig` | `UpdateConfigSpec` | `updateConfig` | `*UpdateConfig` | ✅（maxUnavailable） |
| `nodeRepair` | `*NodeRepairSpec` | `nodeRepairConfig` | `*NodeRepairConfig` | ✅（enabled） |
| `ecsGroupId` / `faultDomain` / `dedicatedHostId` | `string` | — | — | ➕ CCE 独有（ECS 置放/宿主机） |
| `preInstall` / `postInstall` | `string` | `lifecycleHooks` | `[]AWSLifecycleHook` | 🟡 CCE 初始化脚本 vs CAPA ASG 生命周期事件（语义不同，前文已定性） |
| `waitPostInstallFinish` | `*bool` | — | — | ➕ CCE 独有 |
| — | — | `awsLaunchTemplate` | `*AWSLaunchTemplate` | ⚪ CCE 无启动模板概念（NodeSpec 即对等） |
| — | — | `subnetIDs` | `[]string` | ⚪ CCE 子网在集群级 Network |
| — | — | `roleName` / `rolePath` / `roleAdditionalPolicies` / `rolePermissionsBoundary` | IAM 字段 | ⚪ |
| — | — | `additionalTags` | `infrav1.Tags` | ⚪ CCE 无附加标签 |

#### Status

| CCE 字段 | CAPA 字段 | 判定 |
|---|---|---|
| `ready` | `ready` | ✅ |
| `replicas` | `replicas` | ✅ |
| `availableReplicas` | — | ➕ CCE 独有 |
| `nodePoolID` | — | ➕（CCE 节点池 ID；CAPA 无对应） |
| `lastAppliedSecurityGroups` | — | ➕ CCE 差量追踪 |
| `lastAppliedAutoscaling` | — | ➕ CCE 差量追踪 |
| `conditions` | `conditions` | ✅ |
| — | `launchTemplateID` / `launchTemplateVersion` | ⚪ |
| — | `failureReason` / `failureMessage` | 🟡 CCE 经 conditions |

---

### 3.6 CCEManagedMachinePoolTemplate ↔（CAPA 无对等）

| 字段 | 值 |
|---|---|
| `spec.template.spec` | `CCEManagedMachinePoolSpec`（嵌入） |

> **CAPA 无 `AWSManagedMachinePoolTemplate`**（`/tmp/capa/exp/api/v1beta2/` 无任何 `*template*` 文件）。CCE 在此处比 CAPA 更完整，但这意味着：采用 CCE 的 ClusterClass 拓扑无法直接对齐 CAPA 的托管节点池（CAPA 托管节点池不自带模板，通常经 `AWSMachinePool`/`MachinePool` 组合表达）。

---

### 3.7 身份三件套（Controller / Static / Role）

#### ControllerIdentity

| 字段 | CCE | CAPA | 判定 |
|---|---|---|---|
| `allowedNamespaces.namespaceList` | `[]string`（`namespaceList`） | `[]string`（`list`） | ✅（tag 名不同 P3） |
| `allowedNamespaces.selector` | `metav1.LabelSelector` | `metav1.LabelSelector` | ✅ |

> 双方 ControllerIdentity 均**无凭证字段**（依赖环境 ambient 凭证 / agency），结构对等。

#### StaticIdentity

| 字段 | CCE | CAPA | 判定 |
|---|---|---|---|
| `secretRef` | `string` | `string` | ✅ |
| `allowedNamespaces` | `*AllowedNamespaces` | `*AllowedNamespaces` | ✅ |

> 完全对等。

#### RoleIdentity（**P1**）

| CCE 字段 | CAPA 字段 | 判定 |
|---|---|---|
| `agencyName` | `roleARN` | 🟡 CCE agency（华为云委托）vs CAPA RoleArn（AWS IAM 角色 ARN）——云能力对等，字段名/语义不同 |
| `allowedNamespaces` | `allowedNamespaces` | ✅ |
| — | `sessionName` | ⚪ |
| — | `durationSeconds`（900–43200） | ⚪ |
| — | `inlinePolicy`（JSON 内联策略） | ⚪ |
| — | `policyARNs`（托管策略 ARN 列表） | ⚪ |
| — | `externalID` | ⚪ |
| — | `sourceIdentityRef`（链式身份，`AWSIdentityReference`） | ⚪ |

> **P1-3**：CCE RoleIdentity 仅有 `agencyName` + `allowedNamespaces`。CAPA 的 `AWSRoleSpec`（RoleArn/SessionName/DurationSeconds/InlinePolicy/PolicyARNs）+ `ExternalID` + `SourceIdentityRef` 中，`sessionName`/`durationSeconds`/`inlinePolicy`/`policyARNs` 属 AWS-IAM 特有（CCE 无对等概念，可判「无对等」而非缺失），但 **`sourceIdentityRef` 链式身份（凭证回退链）CCE 确实缺失**——CAPA 支持 identity → 另一 identity 的跨账户角色链，CCE 仅有 agency 单跳。此为真实差距。

---

### 3.8 共享子类型：网络（CCE `common` vs CAPA `infrav1`）

#### VPC

| CCE `common.VPC` | 类型 | CAPA `VPCSpec` | 类型 | 判定 |
|---|---|---|---|---|
| `id` | `string` | `id` | `string` | ✅ |
| `name` | `string` | `name` | `string` | ✅ |
| `cidr` | `string` | `cidrBlock` | `string` | ✅（命名 P3） |
| `resourceID` | `string` | — | — | ➕ |
| `description` | `string` | — | — | ➕ |
| `tags` | `Tags` | `tags` | `Tags` | ✅ |
| — | — | `secondaryCidrBlocks` | `[]VpcCidrBlock` | ⚪ CCE 无 VPC 级辅 CIDR |
| — | — | `ipamPool` | `*IPAMPool` | ⚪ |
| — | — | `ipv6` | `*IPv6` | ⚪（CCE 的 IPv6 在控制面 `ipv6enable`） |
| — | — | `internetGatewayID` / `carrierGatewayID` | `*string` | ⚪ |

#### Subnet

| CCE `common.Subnet` | 类型 | CAPA `SubnetSpec` | 类型 | 判定 |
|---|---|---|---|---|
| `id` / `name` / `cidr` | `string` | `id` / `name` / `cidrBlock` | `string` | ✅ |
| `vpcID` | `string` | —（Subnet 从属 VPC，经列表关联） | — | ➕ |
| `availabilityZone` | `string` | `availabilityZone` | `string` | ✅ |
| `type` | `SubnetType`（node/eni） | `isPublic` / `isIPv6` | `bool` | 🟡 CCE node/eni 二分 vs CAPA public/ipv6 布尔 |
| `neutronSubnetID` | `string` | — | — | ➕（华为 neutron 子网 ID） |
| `resourceID` | `string` | `resourceID` | `string` | ✅ |
| — | — | `routeTableID` | `*string` | ⚪ |
| — | — | `poolID` / `egressOnlyInternetGatewayID` | `*string` | ⚪ |
| — | — | `ipamPool` / `ipv4CidrBlock` / `netmaskLength` | 多个 | ⚪ |

#### NAT / CNI / 安全组 / 入站规则

| 能力 | CCE | CAPA | 判定 |
|---|---|---|---|
| NAT 网关 | `common.NatGatewaySpec`（`spec`/`resourceID`/`eipResourceID`）三态 | 无 NAT Spec（托管模式 NAT 由子网路由隐式，Status 有 `natGatewaysIPs`） | ⚪/➕ CCE 显式 NAT |
| CNI 配置 | 无（CCE 自带 CNI） | `CNISpec` | ⚪ |
| 安全组覆盖 | 无 | `securityGroupOverrides` | ⚪ |
| 入站规则 | 无 | `additionalControlPlaneIngressRules` / `additionalNodeIngressRules` / `nodePortIngressRuleCidrBlocks` | ⚪ |
| ELB/LoadBalancer | 无 | `APIServerELB` / `SecondaryAPIServerELB` / 大量 ELB 字段 | ⚪ |

> **P1-1**：CCE `NetworkSpec` 仅 3 个字段（VPC/Subnets/NatGateway），且归属在 `CCECluster`。CAPA `NetworkSpec` 归属在 `AWSManagedControlPlane`，字段面远大于 CCE（CNI/安全组覆盖/入站规则/IPAM/IPv6-VPC/路由表/ELB）。多数 CAPA 字段属 AWS 网络模型特有（无对等可判），但**「网络归属位置」与「NAT 显式建模」两点的架构差异需要显式决策**（是否对标 CAPA 把网络移入控制面）。

#### 卷 / 标签 / 子网类型

| 类型 | CCE | CAPA | 判定 |
|---|---|---|---|
| `Tags` | `map[string]string` | `map[string]string`（`infrav1.Tags`） | ✅ |
| `NodeVolume` | `{size int32, type string}` | `diskSize *int32`（无 type 概念） | 🟡 CCE 结构化 + 多盘；CAPA 单盘单值 |
| `SubnetType` | `node` / `eni` | （Subnet 用 isPublic/isIPv6 表达） | 🟡 |

---

### 3.9 访问管理（AccessPolicies vs AccessConfig/AccessEntries）— **P1**

| CCE `AccessPolicySpec` | 类型 | CAPA `AccessEntry` + `AccessConfig` | 判定 |
|---|---|---|---|
| `name` | `string` | — | ➕ CCE 命名策略 |
| `policyType` | `string` | `accessScope.type` | 🟡 |
| `principalType` | `string` | `type`（`AccessEntryType`：Standard/EC2_Linux/…） | 🟡 |
| `principalIds` | `[]string` | `principalARN`（单 ARN） | 🟡 CCE 多 ID 列表 vs CAPA 单 ARN |
| `namespaces` | `[]string` | `accessScope.namespaces` | ✅ |

CAPA 侧 CCE 无的：`authenticationMode`（rbac/authenticating_proxy）、`bootstrapClusterCreatorAdminPermissions`、`kubernetesGroups`、`username`、`policyARN`、`accessPolicies`（ARN 引用）。

> **P1-4**：CCE 用「命名策略 + 主体类型 + 主体 ID 列表 + 命名空间」的**华为云 RBAC 策略模型**；CAPA 用「AccessEntry（principalARN + type + groups/username）+ AccessScope（type + namespaces）+ policyARN 引用」的**EKS Access Entry 模型**。二者语义不同但都能表达「谁+能访问什么命名空间」，属云能力差异。CCE 缺 CAPA 的 `authenticationMode` 独立字段（CCE 放在 `Authentication.Mode`，已覆盖）。

---

### 3.10 控制面日志 / 加密 / 认证（形态差异）

| 能力 | CCE | CAPA | 判定 |
|---|---|---|---|
| 日志 | `ControlPlaneLoggingSpec{ttlInDays, logs[]{name,type,enable}}` | `ControlPlaneLoggingSpec{enable, types[]}` | 🟡 CCE 带 TTL+逐组件；CAPA 开关+类型列表 |
| 加密 | `EncryptionConfigSpec{mode: Default/KMS}` | `EncryptionConfig{provider, resources[]}` | 🟡 CCE mode 二选一；CAPA provider+资源列表 |
| 认证 | `AuthenticationSpec{mode: rbac/authenticating_proxy, authenticatingProxy{ca,cert,privateKey}}` | `IAMAuthenticatorConfig` + `accessConfig.authenticationMode` | 🟡 CCE 认证代理带 CA/证书；CAPA IAM authenticator mapRoles/mapUsers/backendMode |
| Pod 身份 | `PodIdentityAssociationSpec{namespace, serviceAccount, agencyName}` | `PodIdentityAssociation{namespace, serviceAccount, roleARN}` | ✅ 语义对等（agencyName vs roleARN） |

---

## 4. CAPA 独有类型（CCE 全缺）

| CAPA CRD | 说明 | 影响 |
|---|---|---|
| `AWSFargateProfile` | Fargate/无服务器节点 | CCE 用 `enableAutopilot` 开关 + 超节点表达，无独立 CRD。若需对标需评估独立超节点 CRD |
| `AWSMachine` | 自管理 EC2 实例 | CCE 为纯托管 provider，无自管理机路径 |
| `AWSMachineTemplate` | 自管理机模板 | 同上 |
| `AWSMachinePool` | 自管理 ASG 节点池 | 同上 |

> 这三类属 CAPA 自管理（非托管）路径，CCE 定位纯托管 provider，判定为**「不适用」**而非缺失。`AWSFargateProfile` 是唯一与 CCE `enableAutopilot` 语义相交、但形态不同（CRD vs 布尔开关）的类型，属需决策项。

---

## 5. Condition 常量对标

### CCE（`internal/conditions/conditions.go`，纯字符串常量，无 JSON tag）

| Condition 类型 |
|---|
| `NetworkReadyCondition` · `VpcReadyCondition` · `SubnetsReadyCondition` · `NatGatewaysReadyCondition` · `CredentialsReadyCondition` · `CCEClusterReadyCondition` · `KubeconfigReadyCondition` · `AddonsConfiguredCondition` · `PodIdentityAssociationsConfiguredCondition` |

| Reason 常量 |
|---|
| `ReconciliationInProgressReason` · `ReconciliationFailedReason` · `WaitingForClusterInfrastructureReason` · `WaitingForControlPlaneReason` · `WaitingForKubeconfigReason` · `UpgradeNotOfferedReason` · `UpgradeInProgressReason` · `UpgradeTargetUnavailableReason` |

### CAPA（`api/v1beta2/conditions_consts.go` + eks/exp 同名文件）

| 类型/常量族 | 说明 |
|---|---|
| `VpcReadyCondition` / `SubnetsReadyCondition` / `SecurityGroupsReadyCondition` / `NatGatewaysReadyCondition` / `BastionHostReadyCondition` / `LoadBalancerReadyCondition` | 基础设施条件族（比 CCE 多 SecurityGroups/Bastion/LoadBalancer） |
| `ClusterSecurityGroupsReadyCondition` / `EKSControlPlaneReadyCondition` / `EKSAddonsConfiguredCondition` / `IAMControlPlaneRolesReadyCondition` / `IAMNodeGroupRolesReadyCondition` | EKS 托管条件族 |
| `WaitingForEKSControlPlaneReason` / `UpdatingEKSControlPlaneReason` / `DeletingEKSControlPlaneReason` / `EKSControlPlaneCreatingReason` 等 | 生命周期 reason 族 |

> **判定**：CCE 的条件细化（Vpc/Subnets/NatGateways/Network/Kubeconfig/Addons/PodIdentity）已对标 CAPA 核心网络+控制面条件族。差异：CCE 缺 `SecurityGroupsReady`、`BastionHostReady`、`LoadBalancerReady`（对应 CCE 无安全组覆盖/跳板/ELB 能力，§3.8）与 `IAMControlPlaneRolesReady`/`IAMNodeGroupRolesReady`（对应 CCE 无 IAM Role 预置）。这些缺项与 §3.8/§3.3 的能力缺口一一对应，属「能力裁剪的连带」，非独立条件缺陷。

---

## 6. 前置结论核查（`capa-alignment-summary-2026-08-22.md` §4/§5）

逐条核对既有文档 §4 的「✅ 已实现」宣称，与本报告当前代码字段证据对照：

| §4 宣称 | 代码证据 | 核查结果 |
|---|---|---|
| 4.1 生命周期/幂等/带外删除 ✅ | `clusterID`/`initialization`/`upgradeTaskID` status 支撑 | ✅ 一致 |
| 4.1 版本升级 ✅（UpgradeNotOffered 正常态） | `UpgradeNotOfferedReason` 常量存在 | ✅ 一致 |
| 4.1 addons ✅（name+version） | `AddonSpec{name, version}` | ✅ 一致（但 CAPA `Addon` 还有 Configuration/ConflictResolution/ServiceAccountRoleARN，CCE 未覆盖，文档未提） |
| 4.1 pod identity ✅ | `PodIdentityAssociations []PodIdentityAssociationSpec{namespace,serviceAccount,agencyName}` | ✅ 一致 |
| 4.1 KMS/envelope ✅ | `EncryptionConfigSpec{mode: Default/KMS}` | ✅ 一致 |
| 4.1 IAM 认证模式 ✅ | `AuthenticationSpec{mode, authenticatingProxy{ca,cert,privateKey}}` | ✅ 一致 |
| 4.1 access entry ✅ | `AccessPolicies []AccessPolicySpec` | 🟡 文档称「AccessPolicy」对标，但 CAPA 是 AccessEntry+AccessConfig 双结构，语义差异未在 §4 标注（见本报告 P1-4） |
| 4.1 OIDC 🟡 | 无 OIDC CRD 字段 | ✅ 一致（文档已标 🟡） |
| 4.1 endpoint 🟡（仅 public+cidrs） | `EndpointAccessSpec{public, cidrs}` | ✅ 一致（缺 private，文档未明示 private 缺项） |
| 4.1 控制面规格 ✅（flavor） | `flavor` | ✅ 一致（但 CAPA 是 `controlPlaneScalingConfig`，形态差异未标注） |
| 4.1 kubeconfig ✅（双 Secret） | `kubeconfigSecretName` | ✅ 一致 |
| 4.1 Secondary CIDR 🟡（cidrs[]） | `containerNetwork.cidrs[]` | ✅ 一致 |
| 4.1 Fargate/Autopilot ✅（enableAutopilot） | `enableAutopilot *bool` | ✅ 一致（CAPA 实为独立 FargateProfile CRD，文档未提此形态差异） |
| 4.2 扩缩容 ✅ | `replicas`/`autoscaling` | ✅ 一致 |
| 4.2 spot ✅ | `spot`/`spotPrice` | ✅ 一致 |
| 4.2 AMI/镜像 ✅（OS） | `os` | ✅ 一致（CAPA 是 amiType/amiVersion，文档未提） |
| 4.2 启动模板 🟡（NodeSpec 即对等） | 无 launchTemplate 字段 | ✅ 一致 |
| 4.2 labels/taints ✅ | `labels`/`taints` | ✅ 一致（taints 形态：CCE []string vs CAPA 结构化 Taints，文档未提） |
| 4.2 remoteAccess ✅（sshKey） | `sshKey` | ✅ 一致 |
| 4.2 磁盘 ✅（rootVolume+dataVolumes） | `rootVolume`/`dataVolumes` | ✅ 一致 |
| 4.2 滚动更新 ✅ | `updateConfig.maxUnavailable` | ✅ 一致 |
| 4.2 节点修复 ✅ | `nodeRepair.enabled` | ✅ 一致 |
| 4.2 多 AZ ✅ | `extensionScaleGroups` | ✅ 一致 |
| 4.2 生命周期钩子 ✅（preInstall/postInstall） | `preInstall`/`postInstall`/`waitPostInstallFinish` | ✅ 一致（语义=初始化脚本，文档已标注） |
| 4.2 反向同步 ✅ | （注解机制，非 CRD 字段） | ✅ 一致（CRD 层无字段，属 controller 行为） |
| 4.3 托管网络 ✅（三态） | `network.vpc.id` 三态 | ✅ 一致 |
| 4.3 收养模式 ✅ | （tag 判定，非字段） | ✅ 一致 |
| 4.3 NAT 出网 ✅ | `network.natGateway` | ✅ 一致 |
| 4.3 IPv6 🟡 | `ipv6enable` + `serviceNetwork.ipv6CIDR` | ✅ 一致 |
| 4.3 Edge Zone ⚪ | 无字段 | ✅ 一致 |
| 4.3 conditions 细化 ✅ | `VpcReady`/`SubnetsReady`/`NatGatewaysReady` | ✅ 一致 |
| 4.4 身份 ✅ | 三身份 + allowedNamespaces | ✅ 一致（但 RoleIdentity 缺 sourceIdentityRef 链式身份，文档未提——见 P1-3） |
| 4.6 feature gates 2 个 | — | ✅ 一致（CRD 层无关，无 API 字段差异） |

**§5 剩余差距清单核查**：

| # | 差距 | 核查结果 |
|---|---|---|
| 1 | 节点池级过滤 | ✅ 已实现（controller 行为，CRD 层无涉） |
| 2 | NAT tag 格式实测 | ⏳ 未验证（余额冻结，非代码差距） |
| 3 | 真实云冒烟 | ⏳ 非代码差距 |
| 4 | NAT 默认建 vs 显式 | ✅ 已决策（CRD `NatGatewaySpec` 无 `enabled` 字段，默认建已落） |
| 5 | providerID 格式 | ✅ 已修正（CRD 层 `providerIDList`，格式约定在 controller） |
| 6 | 限流中间件 | ✅ 已实现（非 API 类型层） |
| 7 | Fargate/Autopilot | ✅ `enableAutopilot` 已落 |

> **核查总结**：§4/§5 的「已实现」宣称在 API 类型层基本属实（无虚假「✅」）。但存在 **3 处文档未标注的字段面缺口**，本报告补充定性：
> 1. **RoleIdentity 缺链式身份**（`sourceIdentityRef`）——§4.4 未提。
> 2. **访问管理语义差异**（AccessPolicy vs AccessEntry+AccessConfig）——§4.1 未提。
> 3. **网络 Spec 归属分歧**（CCECluster vs AWSManagedControlPlane）——§4 未作架构层面标注。

---

## 7. 差距汇总（P0–P3）

### P0（阻断性）
无。

### P1（重要字段面差距）

| # | 差距 | 位置 | 说明 |
|---|---|---|---|
| P1-1 | **网络 Spec 归属分歧 + 字段面不足** | §3.1/§3.8 | CCE 网络在 `CCECluster.Spec.Network`（VPC/Subnets/NAT 三字段），CAPA 托管在 `AWSManagedControlPlane.Spec.NetworkSpec`（VPC/Subnets/CNI/安全组覆盖/入站规则/IPAM/IPv6-VPC/路由表）。多数 CAPA 字段属 AWS 特有可判无对等，但**归属位置**与**NAT 显式建模**需决策 |
| P1-2 | **托管节点池模板不对等** | §3.6 | CCE 有 `CCEManagedMachinePoolTemplate`，CAPA 无 `AWSManagedMachinePoolTemplate`。非标准映射，ClusterClass 拓扑无法直接对齐 |
| P1-3 | **RoleIdentity 缺链式身份** | §3.7 | CAPA `sourceIdentityRef`（identity→identity 跨账户链）CCE 缺失；`sessionName`/`durationSeconds`/`inlinePolicy`/`policyARNs`/`externalID` 属 AWS-IAM 特有（无对等）。仅 `sourceIdentityRef` 为真实能力缺口 |
| P1-4 | **访问管理语义差异** | §3.9 | CCE AccessPolicy（name/type/principalIds/namespaces）vs CAPA AccessConfig+AccessEntry（principalARN/type/groups/username/policyARN/AccessScope）。云能力差异，需文档明示不可直接对标 |

### P2（次要/形态差异）

| # | 差距 | 说明 |
|---|---|---|
| P2-1 | Endpoint 缺 `private` 字段 | CCE `EndpointAccessSpec{public, cidrs}`，CAPA `{public, publicCIDRs, private}`。CCE 私网端点常开，无显式开关 |
| P2-2 | 控制面日志形态差异 | CCE `{ttlInDays, logs[]{name,type,enable}}` vs CAPA `{enable, types[]}` |
| P2-3 | 缺 `upgradePolicy` / `controlPlaneScalingConfig` | CCE 升级经 version diff + 工作流（无策略字段），控制面规格用 `flavor`（CAPA 用 ScalingConfig tier） |
| P2-4 | 缺启动模板/AMI 术语 | CCE `flavor`+`os` 直接表达；CAPA `instanceType`+`amiType`+`amiVersion`+`awsLaunchTemplate` |
| P2-5 | 生命周期钩子语义差异 | CCE `preInstall`/`postInstall`（初始化脚本）vs CAPA `lifecycleHooks`（ASG 生命周期事件）——前文已定性，非等价 |
| P2-6 | 缺 `additionalTags` | 控制面/节点池均无附加标签字段（CCE 标签走 owned tag 机制） |

### P3（命名/一致性）

- `eksClusterName` vs `clusterName`、`eksNodegroupName` vs `nodePoolName`、`cidrBlock` vs `cidr`、`namespaceList` vs `list` 等命名差异。
- CCE Status 缺 `observedGeneration`（`AWSManagedControlPlane.Status` 有）。
- CCE 无 `externalManagedControlPlane` 标志（无外部托管控制面模式）。
- CCE addon 状态未回填 Status（CAPA 有 `AddonState` 列表）。

---

## 8. 附：待决策项（供项目负责人拍板）

1. **网络 Spec 归属**：是否对标 CAPA 将 VPC/子网移入 `CCEManagedControlPlane.Spec.Network`？当前 CCE 放 `CCECluster` 属合理自洽，但影响 CAPA 拓扑迁移工具兼容性。
2. **节点池模板**：`CCEManagedMachinePoolTemplate` 保留（比 CAPA 完整）还是裁剪以对齐 CAPA？
3. **RoleIdentity 链式身份**：是否补 `sourceIdentityRef` 支持 agency→agency 链？（华为云 IAM 委托是否支持链式需调研）
4. **Fargate/Autopilot 形态**：`enableAutopilot` 布尔开关 vs 独立超节点 CRD 的长期演进选择。
