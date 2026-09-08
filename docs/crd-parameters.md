# my-cluster.yaml 配置参数参考

> `my-cluster.yaml` 由 `clusterctl generate cluster` 生成，是一份多文档 YAML，依次为：
> `Cluster`（CAPI 编排）→ `CCECluster` → `CCEManagedControlPlane` → `MachinePool` ×N → `CCEManagedMachinePool` ×N。
> 下面按 kind 列出各 `spec` 的**全部字段**（字段名与 CRD / `kubectl explain` 一致；说明摘自 API 源码注释）。

> `Cluster` / `MachinePool` 是 Cluster API 核心标准类型（非本 provider），常用字段见文末。


### `CCECluster` — 集群外壳（region / 网络）

| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `region` | (必填)  string| Region is the Huawei Cloud region (e.g. cn-north-4). Required. |
| `network` |  common.NetworkSpec| Network references the VPC/subnets the CCE cluster consumes. CCE requires a VPC to exist before cluster creation. |

### `CCEManagedControlPlane` — 托管控制面（CCE 集群参数）

| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `clusterName` | (必填)  string| ClusterName of the owning CCE cluster. |
| `version` |  string| Version of Kubernetes, e.g. "v1.30.0". Empty means CCE latest. |
| `category` |  string| Category of the CCE cluster: CCE (Standard) or Turbo. 默认 `=Turbo`。 |
| `flavor` |  string| Flavor of the cluster (official enum e.g. cce.s1.small ... cce.s2.xlarge). |
| `containerNetwork` |  ContainerNetworkSpec| ContainerNetwork of the cluster. |
| `serviceNetwork` |  ServiceNetworkSpec| ServiceNetwork of the cluster. |
| `ipv6enable` |  *bool| Ipv6Enable enables IPv6 dual-stack for the cluster (maps to CCE spec.ipv6enable). Requires a VPC with IPv6 enabled; the service network IPv6CIDR must be set as well. |
| `enableAutopilot` |  *bool| EnableAutopilot creates an Autopilot (Serverless) cluster instead of a standard CCE/Turbo cluster (maps to CCE spec.enableAutopilot). Autopilot requires category Turbo and a compatible flavor (cce.autopilot.cluster). |
| `customSan` |  []string| CustomSan entries for the API server certificate. |
| `endpointAccess` |  EndpointAccessSpec| EndpointAccess controls public API server access. |
| `agencyName` |  string| AgencyName used by the cluster (1.27+; empty uses the system agency). |
| `agencyTrustPolicy` |  string| AgencyTrustPolicy is the IAM v5 trust-policy JSON document used to auto-create the trust agency (信任委托) referenced by the role identity (identityRef -> CCEClusterRoleIdentity.spec.agencyName) when that agency does not already exist. Mirrors CAPA auto-creating the cluster IAM role: a non-empty policy + a role identity triggers EnsureAgency (List -> Create when absent); an existing agency is adopted (never overwritten). When the identity has no agency (controller/static identity), creation is skipped. The document must declare "Version": "5.0". |
| `identityRef` |  *corev1.ObjectReference| IdentityRef references a CCECluster*Identity (Controller/Static/Role). Empty means the controller default identity (CLOUD_SDK_AK/SK env). |
| `billing` |  BillingSpec| Billing controls billing mode: 0=on-demand, 1=subscription. |
| `addons` |  []AddonSpec| Addons are the CCE addon instances to manage (declarative set; the controller installs missing ones, upgrades version drift, and removes those no longer listed — mirrors CAPA EKS addons). |
| `podIdentityAssociations` |  []PodIdentityAssociationSpec| PodIdentityAssociations bind Kubernetes ServiceAccounts to Huawei Cloud agencies (the CCE equivalent of EKS Pod Identity). Declarative set: create missing, delete removed. |
| `logging` |  *ControlPlaneLoggingSpec| Logging configures control-plane log collection (mirrors CAPA EKS Logging). Maps to CCE UpdateClusterLogConfig / ShowClusterConfig. |
| `accessPolicies` |  []AccessPolicySpec| AccessPolicies declare CCE access policies (the CCE equivalent of EKS access entries). Declarative set: create missing, update drift, remove those no longer listed. |
| `encryptionConfig` |  *EncryptionConfigSpec| EncryptionConfig controls etcd secret encryption (mirrors CAPA EKS EncryptionConfig). Mode Default leaves etcd unencrypted; KMS enables envelope encryption with a KMS key configured at the account level. Immutable after creation. |
| `authentication` |  *AuthenticationSpec| Authentication controls the API server authentication mode (mirrors CAPA EKS AccessConfig.AuthenticationMode). Default rbac; authenticating_proxy delegates auth to an external proxy (requires a CA + client cert + key). Immutable after creation. |
| `controlPlaneEndpoint` |  *clusterv1.APIEndpoint| ControlPlaneEndpoint is the API server endpoint (host:port) of the managed control plane. It is backfilled by the controller once the CCE cluster is available and is read by CAPI to populate Cluster.spec.controlPlaneEndpoint — the CAPI control-plane contract reads spec.controlPlaneEndpoint, not status. |
| `additionalTags` |  common.Tags| AdditionalTags is an optional set of tags to add to the CCE cluster (maps to CCE clusterTags / ResourceTag), in addition to the provider owned tag cluster-api-provider-cce.cluster.<clusterName>=owned that is always added. The owned tag wins on key collision. Mirrors CAPA's spec.additionalTags. Tag updates on an already-created cluster are reconciled via the CCE BatchCreateClusterTags API (see requirements FR-1.9). |

### `CCEManagedMachinePool` — 节点池

| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `clusterName` | (必填)  string| ClusterName of the owning CCE cluster (matches Cluster name). |
| `nodePoolName` | (必填)  string| NodePoolName is the CCE node pool name. |
| `flavor` | (必填)  string| Flavor is the ECS instance flavor of the nodes (nodeTemplate.flavor). |
| `os` |  string| OS image family of the nodes (nodeTemplate.os), e.g. "Huawei Cloud EulerOS 2.0". Required by the webhook (official CreateNodePool requires os unless a private image is used). |
| `rootVolume` |  *common.NodeVolume| RootVolume of the nodes (nodeTemplate.rootVolume). Pointer so an empty value is omitted; required by the webhook (size 40-1024 GiB). |
| `dataVolumes` |  []common.NodeVolume| DataVolumes of the nodes (nodeTemplate.dataVolumes). |
| `sshKey` |  string| SSHKey used to access the nodes (nodeTemplate.sshKey). |
| `availabilityZone` |  string| AvailabilityZone of the nodes. Required by the webhook (CCE does not support random AZ via API). |
| `replicas` |  int32| Replicas is the desired node count (maps to the node pool expected count). It is normally driven by the owning MachinePool.spec.replicas. |
| `providerIDList` |  []string| ProviderIDList is the list of provider IDs of the nodes in the pool, populated by the controller so Cluster API can fill MachinePool.status.nodeRefs (and the deprecated readyReplicas). Each entry has the form huaweicloud:///<serverId>, matching the spec.providerID of the corresponding workload node. The controller owns this field. |
| `billingMode` |  int32| BillingMode: 0=on-demand, 1=subscription. |
| `spot` |  bool| Spot requests spot (竞价) instances for the node pool. Only effective when billingMode=0 (on-demand); maps to nodeTemplate.extendParam. marketType=spot. |
| `spotPrice` |  string| SpotPrice is the maximum hourly price the user is willing to pay for spot instances. Empty = the on-demand price is used as the spot price. Only effective when spot is set and billingMode=0. |
| `extensionScaleGroups` |  []ExtensionScaleGroupSpec| ExtensionScaleGroups extends the node pool into additional availability zones (CCE 扩展伸缩组). Each entry carries its own flavor and AZ; the base nodeTemplate.az remains the primary AZ. |
| `taints` |  []string| Taints applied to the nodes (max 20 per official constraint). |
| `labels` |  map[string]string| Labels applied to the nodes. |
| `securityGroups` |  []string| SecurityGroups to bind to the node pool (Turbo >= 1.21, max 5 per official constraint). |
| `autoscaling` |  AutoscalingSpec| Autoscaling maps to the CCE node pool autoscaling (NodePoolNodeAutoscaling). Only honored when the NodePoolAutoscaling feature gate is enabled (Alpha, off by default); otherwise scaling is driven solely by CAPI MachinePool replicas (questionnaire Q3, FR-2.6). |
| `updateConfig` |  UpdateConfigSpec| UpdateConfig controls how spec changes are rolled onto existing nodes (CCE 同步节点池 UpgradeNodePool, the analogue of CAPA's UpdateConfig / rolling update). Node attributes such as securityGroups, taints, labels and OS only apply to newly created nodes, so the controller calls UpgradeNodePool to synchronise them onto running nodes. |
| `nodeRepair` |  *NodeRepairSpec| NodeRepair enables node auto-repair (mirrors CAPA NodeRepairConfig. Enabled). CCE has no EKS-style auto-repair switch, so the provider detects Abnormal/Error nodes and resets them via CCE ResetNode. |
| `ecsGroupId` |  string| EcsGroupId is the ECS server group ID (云服务器组) for the nodes (nodeTemplate.ecsGroupId). Used to place nodes according to the group's affinity/anti-affinity policy. |
| `faultDomain` |  string| FaultDomain is the fault domain (故障域) for the nodes (nodeTemplate.faultDomain). Enables single-AZ multi-fault-domain placement. |
| `dedicatedHostId` |  string| DedicatedHostId is the dedicated host (专属主机) ID for the nodes (nodeTemplate.dedicatedHostId). Only effective for dedicated-host flavors. |
| `preInstall` |  string| PreInstall is the base64-encoded script run before node installation (nodeTemplate.extendParam["alpha.cce/preInstall"]). Mirrors the CCE node lifecycle preInstall hook; the value must already be base64-encoded. |
| `postInstall` |  string| PostInstall is the base64-encoded script run after node installation (nodeTemplate.extendParam["alpha.cce/postInstall"]). Mirrors the CCE node lifecycle postInstall hook; the value must already be base64- encoded. |
| `waitPostInstallFinish` |  *bool| WaitPostInstallFinish blocks pod scheduling until the post-install script finishes (nodeTemplate.waitPostInstallFinish). Mirrors CCE's NodeLifecycleConfig waitPostInstallFinish. |
| `additionalTags` |  common.Tags| AdditionalTags is an optional set of tags to add to the CCE node pool (maps to CCE userTags / UserTag), in addition to the provider owned tag cluster-api-provider-cce.cluster.<clusterName>=owned that is always added. The owned tag wins on key collision. Mirrors CAPA's spec.additionalTags. |

### `MachinePool` — CAPI 标准（驱动节点池）
| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `clusterName` | string (必填) | 所属 Cluster |
| `replicas` | int32 | 期望节点数；`kubectl scale` 改此值驱动 CCE 节点池扩缩容 |
| `template.spec.clusterName` | string (必填) | 同顶层 clusterName |
| `template.spec.version` | string | Kubernetes 版本（如 v1.35.0）|
| `template.spec.bootstrap.dataSecretName` | string | bootstrap Secret 引用（托管节点池：空 Secret）|
| `template.spec.infrastructureRef` | ObjectReference (必填) | 指向同名的 CCEManagedMachinePool |

> `Cluster` 常用：`metadata.name`、`spec.infraRef`（→CCECluster）、`spec.controlPlaneRef`（→CCEManagedControlPlane）、`spec.clusterNetwork`。
> CAPI 完整字段：`kubectl explain cluster` / `kubectl explain machinepool`。

## 嵌套子结构字段

**`containerNetwork`（容器网络）**


| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `mode` | string | Mode: overlay_l2 | vpc-router | eni (eni implies Turbo). |
| `cidr` | string | CIDR of the container network (immutable after creation for tunnel mode). |
| `cidrs` | []string | CIDRs lists additional container network segments (secondary CIDR). Maps to model.ContainerNetwork.Cidrs; each entry becomes a model.ContainerCidr. Empty = single-CIDR cluster. |
| `eniSubnets` | []string | ENISubnets for eni mode (Turbo). |



**`serviceNetwork`（服务网络）**


| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `cidr` | string | CIDR of the service network. |
| `ipv6CIDR` | string | IPv6CIDR of the service network (required when Ipv6Enable is true). Maps to model.ServiceNetwork.IPv6CIDR. |



**`endpointAccess`（API 访问）**


| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `public` | bool | Public enables public API server access. |
| `private` | bool | Private controls private (VPC-internal) API server access. CCE always exposes a VPC-internal endpoint and cannot disable it (platform-managed control plane), so this field defaults to true and false is rejected by the webhook. It exists for CAPA parity (CAPA EndpointAccess exposes public/private). |
| `cidrs` | []string | CIDRs is the public API server access whitelist (mapped to the CCE PublicAccess.cidrs). Only sent when public access is enabled; empty means the platform default ["0.0.0.0/0"]. CCE always exposes a VPC-internal (private) endpoint regardless of this field. |



**`billing`（计费）**


| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `mode` | int32 | Mode: 0=on-demand, 1=subscription. |



**`authentication`（API 认证）**


| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `mode` | string | Mode: "rbac" (default) or "authenticating_proxy". |
| `authenticatingProxy` | *AuthenticatingProxySpec | AuthenticatingProxy config (required when Mode=authenticating_proxy). |



**`logging`（控制面日志）**


| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `ttlInDays` | int32 | TTLInDays is the log retention in days (official range 0-30). |
| `logs` | []ControlPlaneLogSpec | Logs lists the components to collect. |



**`accessPolicies[].accessPolicySpec`（CCE 访问策略）**


| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `name` | string | Name of the access policy (unique within the account). |
| `policyType` | string | PolicyType is the permission level. |
| `principalType` | string | PrincipalType is the IAM principal kind: user, group or agency. |
| `principalIds` | []string | PrincipalIds are the IAM user/group/agency IDs the policy applies to. |
| `namespaces` | []string | Namespaces the policy applies to (["*"] = all). |



**`rootVolume` / `dataVolumes[]`（节点磁盘，common.NodeVolume）**


| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `size` | int32 | Size in GiB. |
| `type` | string | Type of the volume, e.g. GPSSD / SSD / SAS. |



**`updateConfig`（滚动更新）**


| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `maxUnavailable` | int32 | MaxUnavailable is the maximum number of nodes made unavailable per rolling batch (official range [1,20]; default 1). |



**`autoscaling`（节点池自动伸缩）**

| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `enable` | bool | Enable turns autoscaling on for the node pool. |
| `minNodeCount` | int32 | MinNodeCount is the minimum node count when autoscaling is enabled. |
| `maxNodeCount` | int32 | MaxNodeCount is the maximum node count when autoscaling is enabled. |

**`network`（common.NetworkSpec — CCECluster 网络外壳）**


**NetworkSpec**

| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `vpc` | VPC | VPC referenced (BYO) or created (managed) for the cluster. |
| `subnets` | []Subnet | Subnets referenced (BYO) or created (managed) by the cluster. When empty in managed mode, a default node subnet is derived from the VPC CIDR. |
| `natGateway` | *NatGatewaySpec | NatGateway enables managed node egress (managed mode only): a NAT gateway + EIP + SNAT rule per managed subnet. Ignored in BYO mode. |
| `securityGroup` | *SecurityGroupSpec | SecurityGroup declares a managed node security group the provider creates (with the declared ingress/egress rules) in the managed VPC and binds to nod |


**VPC（VPC）**

| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `id` | string | ID of an existing VPC. If set, the VPC is referenced and not managed. |
| `name` | string | Name of the VPC to create (only used when ID is empty). |
| `cidr` | string | CIDR of the VPC to create (only used when ID is empty). |
| `resourceID` | string | ResourceID records the created VPC resource ID (provider-managed). |
| `description` | string | Description of the VPC to create. |
| `tags` | Tags | Tags attached to the VPC. The provider owned tag (cluster-api-provider-cce.cluster.<name>=owned) marks an existing VPC as ADOPTED (managed, including  |


**Subnet（子网）**

| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `id` | string | ID of an existing subnet. If set, the subnet is referenced and not managed. |
| `name` | string | Name of the subnet to create (only used when ID is empty). |
| `cidr` | string | CIDR of the subnet to create (only used when ID is empty). |
| `vpcId` | string | VPCID of the VPC the subnet belongs to (only used when creating). |
| `availabilityZone` | string | AvailabilityZone of the subnet (only used when creating). |
| `type` | SubnetType | Type of the subnet: node (default) or eni (Turbo container subnet). Managed Turbo clusters derive the ENI neutron_subnet_id from subnets of Type eni. |
| `neutronSubnetId` | string | NeutronSubnetID records the neutron subnet ID (provider-managed; the CCE eniNetwork API requires the neutron ID, not the network ID). |
| `resourceID` | string | ResourceID records the created subnet resource ID (provider-managed). |


**NatGatewaySpec（NAT，可选）**

| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `spec` | string | Spec of the NAT gateway: "1" (small, default), "2", "3", "4". |
| `resourceID` | string | ResourceID records the created NAT gateway ID (provider-managed). |
| `eipResourceID` | string | EIPResourceID records the created NAT EIP ID (provider-managed). |


**SecurityGroupSpec（安全组，可选）**

| 字段 (json) | 类型 | 说明 |
|---|---|---|
| `name` | string | Name of the security group to create. Empty = "<clusterName>-node". |
| `ingress` | []SecurityGroupRuleSpec | Ingress rules applied to the security group (direction=ingress). |
| `egress` | []SecurityGroupRuleSpec | Egress rules applied to the security group (direction=egress). |
| `resourceID` | string | ResourceID records the created security group ID (provider-managed). |

