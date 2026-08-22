# Cluster API Provider AWS (CAPA) 源码架构分析报告

> 分析对象:`/tmp/capa`(shallow clone,最新 commit `67de5c2`,release 2.x 系列,CAPI v1.13.4 / controller-runtime v0.24.1 / k8s.io/apimachinery v0.36.1,见 `go.mod:59,67,69`)
> 用途:华为云 CCE Cluster API Provider(下称 CCE Provider)架构参考。
> 说明:所有文件路径相对 `/tmp/capa`,行号基于当前 checkout。

---

## 0. 总体印象(先读这一段)

CAPA 是 CAPI 生态里最成熟的基础设施 provider 之一,同时支持两条完全不同的产品线:

1. **自建模式(Unmanaged/EC2)**:AWSCluster + AWSMachine + AWSMachineTemplate(infra),配合 CAPI 的 KubeadmControlPlane + KubeadmConfig(控制面由 CAPI kubeadm 控制器管理),CAPA 只负责"云资源"(VPC/子网/安全组/ELB/EC2/ASG/用户数据注入)。
2. **EKS 托管模式(Managed)**:AWSManagedCluster(infra)+ AWSManagedControlPlane(controlplane,在 `controlplane/eks/` 单独一个 module)+ AWSManagedMachinePool + EKSConfig/NodeadmConfig(bootstrap),控制面完全由 AWS 托管,写回 endpoint/kubeconfig secret。

架构核心是 **Scope 模式 + 服务接口层 + 工厂注入**:每次 reconcile 构建一个 Scope(把 CAPI 对象 + AWS SDK session + patch helper 打包),服务层只依赖接口(`pkg/cloud/scope/ec2.go` 等的 `EC2Scope`/`SGScope`/`ELBScope`),控制器通过可替换的 service factory 字段注入具体实现,单元测试用 mock 注入(`pkg/cloud/services/mock_services/`)。

下面按 12 个主题展开。

---

## 1. 仓库顶层结构与各目录职责

| 路径 | 职责 |
|---|---|
| `main.go` | 二进制入口:flag、scheme、feature gate、manager 装配、按 feature gate 注册控制器与 webhook |
| `controllers/` | 自建模式控制器:AWSCluster / AWSMachine / AWSMachineTemplate / AWSManagedCluster / ROSACluster,以及 `disabled_controllers.go` |
| `controlplane/eks/` | EKS 托管控制面:AWSManagedControlPlane 的 types / controller / webhooks(独立于 controllers/) |
| `controlplane/rosa/` | ROSA(OpenShift 托管)控制面 |
| `bootstrap/eks/` | EKS 节点引导:EKSConfig、NodeadmConfig 两种 bootstrap provider(types + controllers + `internal/userdata` 模板) |
| `exp/` | experimental API 与控制器:MachinePool、AWSManagedMachinePool、Fargate、ROSAMachinePool;`exp/api/`、`exp/controllers/`、`exp/webhooks/`、`exp/instancestate/`、`exp/controlleridentitycreator/` |
| `api/` | 自建模式 CRD 类型,v1beta1(旧)+ v1beta2(存储版/hub) |
| `pkg/cloud/` | 核心抽象层:`interfaces.go`(Session/ClusterScoper)、`scope/`(各类 Scope)、`services/`(AWS 服务实现)、`identity/`(凭证 provider)、`tags/`、`throttle/`(限流)、`endpoints/`(自定义 endpoint)、`awserrors/`(AWS 错误分类)、`metrics/`、`logs/`、`filter/`(EC2 filter 构造)、`converters/` |
| `webhooks/` | 自建模式 webhook(AWSCluster/AWSMachine/Template/三类 Identity) |
| `iam/api/` | `clusterawsadm` 的 IAM API 类型(v1beta1) |
| `cmd/clusterawsadm/` | 独立 CLI:CloudFormation 引导 IAM 角色/策略、AMI 查询、credentials bootstrap |
| `config/` | kustomize 布局:crd / default / manager / rbac / webhook / certmanager |
| `feature/` | feature gates 定义与默认值 |
| `test/e2e/` | Ginkgo v2 + clusterctl e2e 套件(unmanaged/managed/conformance)+ shared 工具 + 模板 flavors |
| `templates/` | `cluster-template*.yaml`,供 clusterctl 生成集群的 flavor 模板 |
| `metadata.yaml`、`clusterctl-settings.json`、`tilt-provider.json` | clusterctl / tilt 合约文件 |

**入口装配逻辑(`main.go`)**:
- `init()`(83-97)把所有 group 的 v1beta1 和 v1beta2 scheme 都注册(自建 infra、EKS controlplane、EKS bootstrap、exp、ROSA),这是"新旧版本并存、转换 webhook 保底"的基础。
- `main()`(137-361):解析 flag → `controllers.ValidateNamesAndDisable(disabledControllers)`(143)校验可禁用的控制器组 → 建 manager(202-223,含 WebhookServer、EventBroadcaster burst=100 避免事件被过滤)→ `endpoints.ParseFlag(serviceEndpoints)`(251)解析自定义服务 endpoint → `setupReconcilersAndWebhooks`(257)→ 若 `feature.Gates.Enabled(feature.EKS)` 则 `setupEKSReconcilersAndWebhooks`(258-260)→ 注册 readyz/healthz。
- `setupReconcilersAndWebhooks`(363-478):默认启用 Unmanaged 组的 AWSMachine(368)/AWSCluster(381)/AWSMachineTemplate(395);feature gate 控制 MachinePool(408)、EventBridgeInstanceState(426)、AutoControllerIdentityCreator(438);最后注册全部自建 webhook(450-477)。
- `setupEKSReconcilersAndWebhooks`(480-589):**EKS 开启时 sync-period 上限 10 分钟**(485-488,因为每次 resync 会重新签发 AWS 认证 token,token 有效期 15 分钟);EKSEnableIAM 与 EKSAllowAddRoles 的依赖校验(490-497);注册 AWSManagedControlPlane(500)/EKSConfig(516)/NodeadmConfig(524)/AWSManagedCluster(533)/Fargate(542,需 EKSFargate gate)/AWSManagedMachinePool(561,需 MachinePool gate)控制器与 webhook。
- 关键 flag(591-731):`awscluster-concurrency=5`、`awsmachine-concurrency=10`、`instance-state-concurrency=5`、`sync-period=10m`、`wait-infra-period=1m`、`max-wait-managed-resources=30m`(等待托管 AWS 资源就绪的上限)、`disable-controllers`、`watch-filter`、`namespace`。

---

## 2. CRD 类型体系(api/ 与 exp/api/)

### 2.1 版本策略:v1beta2 为存储版(hub),v1beta1 为 spoke

- `api/v1beta2/` 各类型标注 `+kubebuilder:storageversion`(如 `awscluster_types.go:388`),`api/v1beta1/` 与 `api/v1beta2/` 之间通过 `conversion.go` + `zz_generated.conversion.go`(生成)实现转换,v1beta2 是 hub;`main.go:47-95` 同时注册两版。
- CRD 清单由 `config/crd/kustomization.yaml` 的 `commonLabels` 打合约标签(**关键机制**,见 §10):`cluster.x-k8s.io/v1beta1: v1beta1_v1beta2`,含义是"这个 provider 的 CRD 同时服务 v1beta1 与 v1beta2 两个 CRD 版本",clusterctl 据此匹配 contract。

### 2.2 自建模式(AWSCluster / AWSMachine / AWSMachineTemplate)

**AWSClusterSpec**(`api/v1beta2/awscluster_types.go:35-114`):
- `NetworkSpec`(VPC/子网/CNI/安全组覆盖/附加 ingress 规则,见 §7)
- `Region`、`Partition`(默认 "aws")
- `SSHKeyName`、`ControlPlaneEndpoint`(host:port)
- `AdditionalTags`(叠加到所有 AWS 资源的额外标签)
- `ControlPlaneLoadBalancer` + `SecondaryControlPlaneLoadBalancer`(`AWSLoadBalancerSpec`,199-285:Name/Scheme(internet-facing|internal)/CrossZoneLoadBalancing/Subnets/HealthCheckProtocol/LoadBalancerType(classic|elb|alb|nlb|disabled)/IngressRules/DNSResolutionCheck/AdditionalListeners 等;类型可禁用,禁用时走外部 LB 等待逻辑)
- `ImageLookupFormat/Org/BaseOS`(AMI 命名模板,支持 `{{.BaseOS}}`、`{{.K8sVersion}}`)
- `Bastion`(Enabled/AllowedCIDRBlocks/InstanceType/AMI)
- `IdentityRef`(指向三类身份,见 §6)
- `S3Bucket`(Ignition 引导数据桶,344-384)

**AWSClusterStatus**(314-322):`Ready`、`Network`(NetworkStatus)、`FailureDomains`、`Bastion`、`Conditions`。

**Conditions**(`api/v1beta2/conditions_consts.go`,共 20 余个,全部以 `*Ready` 命名并配 `*FailedReason`):`VpcReady`/`SubnetsReady`/`InternetGatewayReady`/`EgressOnlyInternetGatewayReady`/`CarrierGatewayReady`/`NatGatewaysReady`/`RouteTablesReady`/`VpcEndpointsReady`/`SecondaryCidrsReady`/`ClusterSecurityGroupsReady`/`BastionHostReady`/`LoadBalancerReady`(还有 WaitForDNSName/WaitForExternalControlPlaneEndpoint/WaitForDNSNameResolve 等中间原因)、机器侧 `InstanceReady`(WaitingForClusterInfrastructure/WaitingForBootstrapData/InstanceProvisionStarted/...)、`SecurityGroupsReady`、`ELBAttached`、`S3BucketReady`、身份侧 `PrincipalCredentialRetrieved`/`PrincipalUsageAllowed`(22-37)。

**AWSMachineSpec**(`api/v1beta2/awsmachine_types.go:79-292`,字段极多,是"机型配置面"的集大成):
- 标识:`ProviderID`/`InstanceID`
- 镜像:`AMI`(AMIReference)/`ImageLookupFormat/Org/BaseOS`
- 计算:`InstanceType`(必填)、`CPUOptions`、`MarketType`(OnDemand/Spot/CapacityBlock)、`SpotMarketOptions`、`Tenancy`、`HostID`/`DynamicHostAllocation`(专用宿主机)、`CapacityReservationID/Preference`、`PlacementGroupName/Partition`
- 网络:`PublicIP`、`ElasticIPPool`(BYO 公网 IP 池)、`Subnet`(AWSResourceReference,可按 ID 或 filter)、`AdditionalSecurityGroups`、`SecurityGroupOverrides`、`NetworkInterfaces`/`NetworkInterfaceType`(ENI/EFA)、`AssignPrimaryIPv6`、`PrivateDNSName`
- 存储:`RootVolume`/`NonRootVolumes`(Volume)
- 引导:`CloudInit`(302-327:InsecureSkipSecretsManager/SecretCount/SecretPrefix/SecureSecretsBackend(secrets-manager|ssm-parameter-store))、`Ignition`、`UncompressedUserData`
- 权限:`IAMInstanceProfile`
- 该校验上还用了 CEL 规则(77-78:capacityReservationId 与 Spot 互斥)。

**AWSMachineTemplate**:标准模板型 CRD(指向 AWSMachineSpec 的 `spec.template.spec`),由 `controllers/awsmachinetemplate_controller.go` 处理,并支持模板 "mutate"(ClusterClass 场景)。

**AWSManagedCluster**(`api/v1beta2/awsmanagedcluster_types.go`):EKS 模式的 infra 引用对象,极简——Spec 基本为空,Status 持有 `FailureDomains`;控制器(controllers/awsmanagedcluster_controller.go:58-98)从 owner Cluster 拿到 ControlPlaneRef 读 AWSManagedControlPlane 并同步 failure domains。

### 2.3 EKS 托管模式(重点看 controlplane/eks 与 exp/api 的 v1beta2)

**AWSManagedControlPlaneSpec**(`controlplane/eks/api/v1beta2/awsmanagedcontrolplane_types.go:36+`):
- `EKSClusterName`(缺省由 ns/name 生成)、`IdentityRef`
- `NetworkSpec`(复用 infrav1.NetworkSpec)、`SecondaryCidrBlock`(pod 网段,100.64.0.0/10 或 198.19.0.0/16 内)、`Region`/`Partition`
- `Version`(EKS 版本,正则校验)、`RoleName`(+RoleAdditionalPolicies/RolePath/RolePermissionsBoundary,受 EKSEnableIAM / EKSAllowAddRoles gate 控制)
- `Logging`、`EncryptionConfig`(KMS)
- `AdditionalTags`、`IAMAuthenticatorConfig`
- `EndpointAccess`(public/private 访问)、`RestrictPrivateSubnets`、`AccessConfig`(EKS 认证模式 API/ConfigMap/API_AND_CONFIG_MAP + bootstrapClusterCreatorAdminPermissions)、`BootstrapSelfManagedAddons`、`UpgradePolicy`、`ControlPlaneScalingConfig`
- `ControlPlaneEndpoint`、ImageLookup 系列

**AWSManagedControlPlaneStatus**(375-420):`Network`、`FailureDomains`、`Bastion`、`OIDCProvider`(OIDCProviderStatus)、`ExternalManagedControlPlane`(默认 true,告诉 CAPI 控制面是外部的)、`Initialized`、`Ready`、`FailureMessage`、`Conditions`、`Addons`、`IdentityProviderStatus`、`Version`、`ObservedGeneration`。conditions 见 `controlplane/eks/api/v1beta2/conditions_consts.go`(EKSControlPlaneReady/Creating/Updating、IAMControlPlaneRolesReady、IAMAuthenticatorConfigured、EKSAddonsConfigured、EKSIdentityProviderConfigured、EKSPodIdentityAssociationConfigured)。

**AWSManagedMachinePoolSpec**(`exp/api/v1beta2/awsmanagedmachinepool_types.go:93+`):
- `EKSNodegroupName`、`AvailabilityZones` + `AvailabilityZoneSubnetType`(public|private|all)、`SubnetIDs`
- `RoleName`(+附加策略/Path/PermissionsBoundary)、`AMIVersion`、`AMIType`(AL2_x86_64 默认,含 Bottlerocket/AL2023/Windows 等一长串枚举,162)、`Labels`/`Taints`、`DiskSize`、`InstanceType`
- `Scaling`(224-227:MinSize/MaxSize)、`RemoteAccess`(SSH)、`CapacityType`、`AWSLaunchTemplate`(自定义启动模板,含 AMI/实例类型等)、`NodeRepairConfig`、`UpdateConfig`、`ProviderIDList`
- Status(243+):`Ready`、`Replicas`、`LaunchTemplateID`/`LaunchTemplateVersion`、`FailureReason`/`FailureMessage`、Conditions(IAMNodegroupRolesReady/EKSNodegroupReady/LaunchTemplateReady 等)。

**EKSConfig / NodeadmConfig**(`bootstrap/eks/api/v1beta2/eksconfig_types.go`、`nodeadmconfig_types.go`):bootstrap 侧类型,Spec 含 KubeletExtraArgs/ContainerRuntime/DNSClusterIP/PreBootstrapCommands/PostBootstrapCommands/Files/Users/DiskSetup/Mounts/PauseContainer 等,Status 只有 `DataSecretName`/`Ready` + `DataSecretAvailableCondition`。NodeadmConfig 是新的(面向 nodeadm),EKSConfig 是老的(面向 `/etc/eks/bootstrap.sh`)。

### 2.4 CRD 生成

所有 CRD 由 kubebuilder 标记生成到 `config/crd/bases/`;`config/crd/kustomization.yaml` 把 26 个 CRD 全部列进 resources,并通过 patches 挂 conversion webhook(`patches/webhook_in_*.yaml`)、cert-manager CA 注入(`patches/cainjection_in_*.yaml`)、全局 identity 的合约标签(`patches/label_in_*.yaml`)。

---

## 3. 控制器实现

### 3.1 AWSCluster 控制器(`controllers/awscluster_controller.go`,549 行)

**Reconcile 骨架**(143-216):Get AWSCluster → `awsCluster.Default()`(169,先 defaulting 再建 scope,避免反复 patch)→ `util.GetOwnerCluster`(172)拿到 CAPI Cluster(未设置 OwnerRef 则跳过)→ paused 检查(184)→ `scope.NewClusterScope`(189,见 §4)→ `defer clusterScope.Close()`(203-207,任何路径都落盘)→ 有 DeletionTimestamp 走 `reconcileDelete`(211),否则 `reconcileNormal`(215)。

**reconcileNormal**(357-435),即"网络创建顺序":
1. 加 finalizer `awscluster.infrastructure.cluster.x-k8s.io` 并立即 Patch(370-375,防孤儿资源)
2. `networkSvc.ReconcileNetwork()`(382,内部顺序见 §7)
3. `sgService.ReconcileSecurityGroups()`(387)
4. `ec2Service.ReconcileBastion()`(393)
5. 可选 EventBridge 实例状态事件(399-405)
6. `reconcileLoadBalancer`(407,见下)
7. `s3Service.ReconcileBucket`(413,Ignition 场景)
8. 按私有子网的 AZ 写 FailureDomains(419-431,ControlPlane=true 仅当该 AZ 在 ELB AZ 列表中)
9. `awsCluster.Status.Ready = true`(433)

**reconcileLoadBalancer**(306-355):LB 类型为 disabled 时改等外部 endpoint(`checkForExternalControlPlaneLoadBalancer`,504-528,15s 重试);否则 `elbService.ReconcileLoadbalancers`;等 `Status.Network.APIServerELB.DNSName` 非空(330-334,15s requeue);默认做 DNS 解析校验(337-345);最后把 ELB DNSName 写进 `Spec.ControlPlaneEndpoint`(349-352)——这就是 kubeadm 控制面节点 join 用的 API server 地址。

**reconcileDelete**(218-304):
- `dependencyCount`(530-548)按 `cluster.x-k8s.io/cluster-name` 标签数 AWSMachine,>0 则 20s 后 requeue(240-243)
- 无依赖后按序删:S3 bucket → ELB → bastion → 安全组 →(可选)外部资源 GC(`gc.NewService`,286-291,按 tag 扫描删除 NLB/ALB 等)→ `networkSvc.DeleteNetwork`(293)
- **所有删除错误收集成 `allErrs` 用 `kerrors.NewAggregate` 一次性返回**(268-299),因为资源间存在依赖(如 SG 依赖 ELB),尽量多删再报错
- 最后 `controllerutil.RemoveFinalizer`(302)

**SetupWithManager**(437-454):`For(&AWSCluster{})` + `predicates.ResourceHasFilterLabel`(watch-filter)+ `predicates.ResourceIsNotExternallyManaged`;额外 Watch CAPI `Cluster`,用 `requeueAWSClusterForUnpausedCluster`(456-502)在 Cluster 解除 paused 时重排队,并过滤 `cluster.x-k8s.io/externally-managed` 注解对象。

### 3.2 AWSMachine 控制器(`controllers/awsmachine_controller.go`,1368 行)

**reconcileNormal**(523-702)状态机:
1. `machineScope.HasFailed()` 则跳过并清理 bootstrap secret(534-544)
2. 等 `Cluster.Status.Initialization.InfrastructureProvisioned`(546-550,置 WaitingForClusterInfrastructure)
3. 等 `Machine.Spec.Bootstrap.DataSecretName` 就绪(553-557,WaitingForBootstrapData)——用户数据由 CAPI 的 kubeadm bootstrap 控制器先生成
4. `findInstance`(562):优先按 Spec.ProviderID 查,否则按 tags 查(GetRunningInstanceByTags)
5. 无实例则加 finalizer(576-582,注意"先读 AWS 再加 finalizer"的注释)并 `createInstance`(601,内部见下)
6. `SetProviderID/SetInstanceID`(639-640,providerID 格式 `aws:///<az>/<i-xxx>`,见 `scope/providerid.go`)
7. 按 `instance.State` 的 switch(655-687):Pending→SetNotReady+requeue;Stopped→错误条件;Running→`SetReady()`;Terminated→机器池实例属正常(缩容),普通机器则报 UnexpectedTermination
8. Running 后 `deleteBootstrapData`(690-701,引导 secret 用完即删;若走 Secrets Manager/SSM 则删云端 secret,见 771-796)
9. 控制面机器额外做 `reconcileLBAttachment`(1029)注册/注销 ELB 目标

**createInstance**(pkg/cloud/services/ec2/instances.go:121-341):
- 从 AWSMachine.Spec 组装 `infrav1.Instance`,标签用 `infrav1.Build(BuildParams{...}.WithCloudProvider(...).WithMachineName(...))`(136-142)
- AMI 三级解析(152-188):`Spec.AMI.ID` 优先;EKS 托管且未配置 lookup 时用 `eksAMILookup`(按 K8s 版本查 EKS 优化 AMI);否则 `defaultAMIIDLookup`(按 ImageLookupFormat 模板+组织过滤)
- `findSubnet`(348+):Machine.FailureDomain → 机器子网 ID/filter → AZ → 默认第一个私有子网
- 用户数据 gzip+base64(211-218);核心安全组 `GetCoreSecurityGroups`(221-225);SSH key 优先级 机器→集群→default(234-251)
- `runInstance`(586)真正调 EC2 RunInstances;创建后立即回写 providerID/instanceID(306-307),并给实例的 ENI 补同样的标签(318-337)

### 3.3 EKS 托管模式控制器

**AWSManagedControlPlaneReconciler**(`controlplane/eks/controllers/awsmanagedcontrolplane_controller.go`):
- Reconcile(216-307):Get CP → GetOwnerCluster → paused → `NewManagedControlPlaneScope`(247,EnableIAM/AllowAdditionalRoles 传入)→ defer 里 SetSummary 并 `PatchObjectWithOptions`(289-297,成功时写 ObservedGeneration)→ delete/normal 分流
- reconcileNormal(309-398):等 Cluster.InfrastructureRef 定义(312);**当 infraRef 不是本 CP 时等 `InfrastructureProvisioned`**(320-326,兼容旧版单一 kind 用法);加 finalizer(330);然后依次:ReconcileNetwork → ReconcileSecurityGroups → ReconcileBastion → **`ekssvc.ReconcileControlPlane`**(358)→ `awsnodeService.ReconcileCNI`(362,注入/更新 aws-node DaemonSet 环境变量,`pkg/cloud/services/awsnode/cni.go:46` 起)→ kube-proxy(367)→ 可选 EventBridge(371)→ `authService.ReconcileIAMAuthenticator`(378,把 aws-iam-authenticator ConfigMap 写回集群)→ 写 failure domains(384-388)→ 若 ObservedGeneration 落后则 `RequeueAfter: WaitInfraPeriod` 重试(390-395)
- reconcileDelete(400-453):dependencyCount(475,数 AWSManagedMachinePool 等依赖)→ `ekssvc.DeleteControlPlane`(423)→ bastion → SG → 可选 GC → network → 移除 finalizer(450)

**EKS Service 层**(`pkg/cloud/services/eks/eks.go:35-74`):
`ReconcileControlPlane` = ① `reconcileControlPlaneIAMRole`(EKSEnableIAM 时创建控制面 IAM 角色,roles.go)→ ② `reconcileCluster` → ③ `reconcileAddons` → ④ `reconcileIdentityProvider` → ⑤ `reconcilePodIdentityAssociations`,每个步骤对应一个 condition。

**reconcileCluster**(`pkg/cloud/services/eks/cluster.go:50-152`)——EKS 生命周期核心:
1. `describeEKSCluster`;不存在则 `createCluster`(60-64)
2. **存在性校验 tag**(66-79):必须带 `kubernetes.io/cluster/<name>=owned`(兼容旧 tag `sigs.k8s.io/cluster-api-provider-aws/cluster/<name>`),否则报"不是本 provider 拥有的集群"
3. `setStatus`(81,194-248):按 EKS status(Creating/Active/Updating/Failed/Deleting)映射 `ControlPlane.Status.Ready/FailureMessage/Version`,并处理"自动升级出标准支持"的告警(223-235)
4. **等待**:Creating/Updating 时 `waitForClusterActive`(86-91,546-567,`WaitUntilClusterActive` 带 `MaxWaitActiveUpdateDelete` 上限)
5. **写回 endpoint**(100-105):`ControlPlane.Spec.ControlPlaneEndpoint = {Host: *cluster.Endpoint, Port: 443}`
6. 之后依次:reconcileSecurityGroups(107)→ **reconcileKubeconfig**(111,见下)→ 附加 kubeconfig(115)→ reconcileClusterVersion(119,升级要走 minor 逐个升)→ reconcileClusterConfig(123)→ AccessConfig/AccessEntries(127-133)→ Logging(135)→ 加密配置(139)→ tags(143)→ **reconcileOIDCProvider**(147,创建 OIDC provider 供 IRSA)

**createCluster**(434-544):组装 `eks.CreateClusterInput`——`Version`(spec 版本转 EKS 格式,493-501)、`Logging`、`AccessConfig`、`EncryptionConfig`、`ResourcesVpcConfig`(441-446,RestrictPrivateSubnets 时只放私有子网)、`RoleArn`(488)、`Tags`(含云 provider owned tag,481)、`KubernetesNetworkConfig`(465-475,IPv6 时 IpFamily=ipv6)、`BootstrapSelfManagedAddons`、`UpgradePolicy`、`ControlPlaneScalingConfig`;用 `wait.WaitForWithRetryable` 包裹(530-540)对 ResourceNotFound 重试。

**kubeconfig 写回**(`pkg/cloud/services/eks/config.go:55` 起):用 EKS token(前缀 `k8s-aws-v1.`,15 分钟有效,45-47)生成 kubeconfig,写入名为 `<cluster>-kubeconfig` 的 Secret(`kubeconfig.GenerateSecretWithOwner`,266),同时生成面向用户的 `<cluster>-user-kubeconfig`(88+)。

**AWSManagedMachinePoolReconciler**(`exp/controllers/awsmanagedmachinepool_controller.go`):
- Reconcile(96-195):AWSManagedMachinePool → owner MachinePool → Cluster → **ControlPlane,且必须 `Status.Ready`**(153-157,否则 EKSNodegroupReady=WaitingForEKSControlPlane)→ 建 ManagedControlPlaneScope(141)+ ManagedMachinePoolScope(159)→ defer Close(176-188)→ delete/normal 分流
- reconcileNormal(197-266):加 finalizer → 若配了 `AWSLaunchTemplate` 先 `ReconcileLaunchTemplate`(237,走通用 ec2 launch template 逻辑,含 enclave/edge zone 校验 217-225)→ `ekssvc.ReconcilePool`(261)
- reconcileDelete(268+):`ReconcilePoolDelete`(eks.go:134-161:describe → 不存在则返回 → `deleteNodegroupAndWait` → 删 IAM role)

**NodegroupService**(`pkg/cloud/services/eks/nodegroup.go`):
- `createNodegroup`(193-273):CreateNodegroupInput 组装——ScalingConfig、ClusterName/NodegroupName、Subnets(来自 scope.SubnetIDs)、NodeRole(roleArn)、Labels/Tags、RemoteAccess、UpdateConfig、AmiType/DiskSize/InstanceTypes/Taints/CapacityType、LaunchTemplate(252-257,来自 Status.LaunchTemplateID/Version)、NodeRepairConfig
- `reconcileNodegroup`(513):describe → 不存在创建 → 等 ACTIVE → 幂等更新
- `reconcileNodegroupVersion`(320-372):对比 spec Version/AMIVersion/LaunchTemplateVersion 与现网,**K8s 只能 minor 逐个升**(360-362),AMI 用 UpdateNodegroupVersion.ReleaseVersion(364-366),LT 用 LaunchTemplate 字段(353-357)
- `deleteNodegroupAndWait`(275-318):`WaitUntilNodegroupDeleted` 带 MaxWaitActiveUpdateDelete

### 3.4 AWSManagedCluster 控制器(controllers/awsmanagedcluster_controller.go:58-98)

极轻量:取 owner Cluster → 读 ControlPlaneRef 对应的 AWSManagedControlPlane → paused 检查 → 同步 failure domains / readiness,主要作用是满足 CAPI 对 "InfrastructureRef 对象" 的契约。

---

## 4. Scope 模式(CAPA 最核心的设计)

### 4.1 概念

**Scope = "一次 reconcile 的上下文对象"**:把 ① 相关 CAPI/Infra 对象引用、② 该集群的 AWS SDK session(带限流器)、③ patch helper(状态落盘)、④ 日志、⑤ 控制器名 打包成一个 struct。每次 Reconcile 开头 `NewXxxScope(params)` 创建,`defer scope.Close()` 保证任何返回路径都把 spec/status 改动 patch 回 API server。

### 4.2 接口分层(`pkg/cloud/interfaces.go`)

- `Session`(35-38):`Session() aws.Config` + `ServiceLimiter(service string) *throttle.ServiceLimiter`
- `ScopeUsage`(41-44):`ControllerName()`——用于 metrics/UserAgent 标注
- `ClusterObject`(47-49):condition setter(来自 CAPI 的 conditions 接口)
- `ClusterScoper`(52-95):**所有集群级 Scope 的最低公共契约**——Name/Namespace/InfraClusterName/Region/KubernetesClusterName(对 EKS 与 CAPI 集群名不同)、InfraCluster()、ClusterObj()、UnstructuredControlPlane()、IdentityRef()、ListOptionsLabelSelector()、APIServerPort()、AdditionalTags()、SetFailureDomain()、PatchObject()、Close()、MaxWaitDuration()
- `SessionMetadata`(98-109):只取建 session 所需的最小信息(Namespace/InfraClusterName/InfraCluster/IdentityRef/ControllerName),身份解析只依赖它(见 §6)

### 4.3 具体 Scope

| Scope | 文件 | 用途 |
|---|---|---|
| `ClusterScope` | `scope/cluster.go` | AWSCluster 控制器;持有 Cluster+AWSCluster+session+patchHelper;Network/VPC/Subnets/SecurityGroups/ControlPlaneLoadBalancer 等访问器(113-210);`PatchObject`(256-306)会先 SetSummary 汇总 Ready 条件(按 managed VPC 增删 IGW/NAT/RouteTables/VPCEndpoints/Bastion 条件)再 patch |
| `MachineScope` | `scope/machine.go` | AWSMachine 控制器;IsControlPlane/IsMachinePoolMachine/Role(113-137)、GetProviderID/SetProviderID(149-160)、SetInstanceState/SetReady(168-185)、**GetRawBootstrapDataWithFormat(283-300,读 Machine.Spec.Bootstrap.DataSecretName 的 Secret "value"+"format")**、PatchObject(302-330,汇总 InstanceReady/SecurityGroupsReady/ELBAttached) |
| `ManagedControlPlaneScope` | `scope/managedcontrolplane.go` | EKS 控制面;RemoteClient(136,连到 EKS 集群的 client)、复用 ClusterScoper 全部契约 + EKS 特有字段(EndpointAccess/Logging/EncryptionConfig...) |
| `ManagedMachinePoolScope` | `scope/managednodegroup.go` | EKS 节点池;NodegroupName()(330)、SubnetIDs()(217)、ControlPlaneSubnets()(212)、EnableIAM()/AllowAdditionalRoles()、NodegroupReadyFalse/IAMReadyFalse 条件助手、PatchCAPIMachinePoolObject(286,同步 CAPI MachinePool 的 replicas) |
| 其他 | `ec2.go`(EC2Scope)、`sg.go`(SGScope)、`elb.go`(ELBScope)、`fargate.go`、`s3.go`、`ignition.go`、`launchtemplate.go`、`providerid.go` | 服务层"按需取接口"的最小视图 |

### 4.4 核心机制:session 构建与缓存(`scope/session.go`)

- `sessionForClusterWithRegion`(90-159):① `getProvidersForCluster`(381-389)按 `IdentityRef` 解析出凭证 provider 链(见 §6);② provider 按 `Hash()` 缓存(`providerCache`,101-118,凭证变化才重建);③ 先用第一个 provider `Retrieve` 验证可取到凭证(132-141,失败置 PrincipalCredentialRetrieved=Unknown 并删缓存);④ `config.LoadDefaultConfig(WithRegion, WithCredentialsProvider(chainProvider))` 建 AWS SDK v2 config(148);⑤ 按 `region-controller-cluster-namespace` 缓存 session + service limiters(`sessionCache`,121-124)
- `sessionForRegion`(71-88):无身份时的默认 config(IRSA/环境变量/instance profile 走 SDK 默认链)
- `ChainCredentialsProvider`(461-482):按顺序取第一个能返回非空 AK/SK 的 provider
- `newServiceLimiters`(165-223):每个服务一个 token-bucket 限流器(EC2 自定义:Describe/Get 20rps/100burst、RunInstances 2rps/5burst 等),以 middleware 注入 SDK 客户端(见 §5)

### 4.5 Scope 的价值(对 CCE Provider 的启示)

1. 控制器代码不直接碰 SDK:reconcile 只编排 Scope + 服务接口
2. **Patch 集中化**:所有 spec/status 变更经 Scope 的 patchHelper,天然解决并发写冲突
3. **session 缓存 + 限流**集中管理,避免每个 reconcile 重建凭证
4. 接口最小化:每个服务只声明它需要的方法(EC2Scope 只有 VPC/Subnets/SecurityGroups/Bastion...),服务实现可被 mock 替换
5. 条件汇总集中在一处(SetSummary),控制器不用各自维护 Ready 汇总逻辑

---

## 5. services 层:接口定义与实现方式

### 5.1 接口定义(`pkg/cloud/services/interfaces.go`,199 行)

按"控制器使用场景"切分,而非按 AWS 服务切分:
- `EC2Interface`(94-131):机器生命周期(InstanceIfExists/CreateInstance/TerminateInstanceAndWait/GetRunningInstanceByTags)+ 安全组(GetCoreSecurityGroups/UpdateInstanceSecurityGroups)+ 标签(UpdateResourceTags)+ 启动模板(GetLaunchTemplate/CreateLaunchTemplate/LaunchTemplateNeedsUpdate,110-118)+ bastion(119-120)+ EIP/专用宿主机
- `NetworkInterface`(164-167):只有 `DeleteNetwork`/`ReconcileNetwork` 两个方法
- `SecurityGroupInterface`(171-174):`DeleteSecurityGroups`/`ReconcileSecurityGroups`
- `ELBInterface`(151-160):Reconcile/Delete + 实例注册/注销(RegisterInstanceWithAPIServerELB/Deregister...)
- `ASGInterface`(73-90):机器池的 ASG 生命周期 + InstanceRefresh + LifecycleHooks
- `MachinePoolReconcileInterface`(136-139):`ReconcileLaunchTemplate`(把 ec2svc/objectStoreSvc 等作为参数传入,便于 mock 分层)
- `SecretInterface`(143-147):用户数据 secret 的 Create/Delete/UserData(Secrets Manager/SSM 后端)
- `ObjectStoreInterface`(177-184):S3 桶与对象(Ignition)
- `AWSNodeInterface`/`IAMAuthenticatorInterface`/`KubeProxyInterface`(187-198):EKS 集群内 DaemonSet/ConfigMap 注入
- 常量:TemporaryResourceID、AnyIPv4CidrBlock=0.0.0.0/0、AnyIPv6CidrBlock=::/0、NAT64CidrBlock(33-42)

### 5.2 实现方式

每个子包 `service.go` 提供 `NewService(scope)` 构造器,struct 持有 scope 接口 + 具体 AWS 客户端:
- `ec2/service.go`、`network/service.go`、`securitygroup/service.go`、`elb/service.go`、`eks/service.go`、`autoscaling/service.go`、`s3/service.go`、`ssm/service.go`、`secretsmanager/service.go`、`iamauth/service.go`、`awsnode/service.go`、`kubeproxy/service.go`、`gc/service.go`(外部资源回收,compose.go/ec2.go/loadbalancer.go 按 tag 扫描清理)、`userdata/`(bastion 用户数据模板)、`wait/`(WaitForWithRetryable 带指数退避)、`common/`(共享工具)
- **客户端工厂在 `scope/clients.go`**(44-336):`NewEC2Client/NewELBClient/NewEKSClient/NewIAMClient/...` 统一做四件事:① 注入日志与 ClientLogMode(按日志级别);② 注入 metrics middleware(`awsmetrics.WithMiddlewares(scopeUser.ControllerName(), target)` 带对象引用,方便按 GVR 打指标);③ 注入 UserAgent `aws.cluster.x-k8s.io/<version>`(pkg/cloud/metrics:114-116);④ 注入 endpoint resolver(自定义 endpoint,`endpoints` 包)与 throttle 限流 middleware。所有客户端集中在 `AWSClients` struct(339-346)
- 错误处理:`pkg/cloud/awserrors`(IsNotFound/IsFailedDependency/ParseSmithyError),控制器据此决定 requeue 还是报错
- **Mock 体系**:`pkg/cloud/services/mock_services/`(ec2_interface_mock.go/network_interface_mock.go/...)是手写接口 mock,供 controller 单测注入;`pkg/cloudtest/` 提供 fake EC2 客户端

### 5.3 注入方式(控制器侧)

控制器 struct 里声明 **service factory 字段**(可测试替换点),如 `awscluster_controller.go:82-90`:
```go
ec2ServiceFactory     func(scope.EC2Scope) services.EC2Interface
networkServiceFactory func(scope.ClusterScope) services.NetworkInterface
elbServiceFactory     func(scope.ELBScope) services.ELBInterface
securityGroupFactory  func(scope.ClusterScope) services.SecurityGroupInterface
```
`getEC2Service`(94-99):factory 非空则用之,否则 `ec2.NewService(scope)`。单测里注入 mock factory 即可零 AWS 依赖测编排逻辑。

---

## 6. 凭证与身份(pkg/cloud/identity + 身份 CRD)

### 6.1 三类身份 CRD(`api/v1beta2/awsidentity_types.go`)

| 类型 | scope | Spec 关键字段 |
|---|---|---|
| `AWSClusterStaticIdentity`(98-106) | Cluster | `SecretRef`(指向含 AccessKeyID/SecretAccessKey/SessionToken 的 Secret)+ AllowedNamespaces |
| `AWSClusterRoleIdentity`(134+) | Cluster | `RoleArn`、`ExternalID`、`SessionName`、`InlinePolicy`、`DurationSeconds`、`SourceIdentityRef`(**可链式假设角色**,跨账户)、AllowedNamespaces |
| `AWSClusterControllerIdentity`(180-182) | Cluster | 单例(名字必须是 `default`,`awscluster_types.go:31`;webhook 强制:`webhooks/awsclustercontrolleridentity_webhook.go:61-64`),代表"控制器自身凭证"(IRSA/环境变量) |

所有身份共享 `AllowedNamespaces`(40-51):`list` + `selector` 两种限制方式,空 = 允许所有 namespace,nil = 不允许任何。

### 6.2 凭证 provider(`pkg/cloud/identity/identity.go`)

- `AWSPrincipalTypeProvider`(41-47):`aws.CredentialsProvider` + `Hash()`(用于 session 缓存失效判断)+ `Name()`
- `AWSStaticPrincipalTypeProvider`(50-64,108-136):从 Secret 读 AK/SK/SessionToken,`credentials.NewStaticCredentialsProvider` + `aws.NewCredentialsCache`
- `AWSRolePrincipalTypeProvider`(96-105,138-184):`stscreds.NewAssumeRoleProvider`,支持 ExternalID/InlinePolicy/DurationSeconds/SessionName(67-93);`Retrieve` 时若配置了 `sourceProvider`,先取源凭证再 assume(165-174)——这就是"静态身份 → 角色"或"角色 → 角色"链

### 6.3 身份解析流程(`scope/session.go`)

`buildProvidersForRef`(225-297)按 `IdentityRef.Kind` 分派:
- `ControllerIdentityKind`:校验单例名 + AllowedNamespaces(355-379),返回**空 provider 列表**表示走默认凭证链(IRSA/环境变量)——这是生产上最常用的方式
- `ClusterStaticIdentityKind`:取身份 + 取 Secret(在 `system.GetManagerNamespace()` 即控制器命名空间),**并把 Secret 打上身份的 ownerRef 以便 clusterctl move**(313-353)
- `ClusterRoleIdentityKind`:取角色身份 → 校验 AllowedNamespaces(265-272)→ 递归处理 `SourceIdentityRef`(275-280)→ 追加到 provider 链
- 每步成功/失败会写 `PrincipalUsageAllowed`/`PrincipalCredentialRetrieved` condition(295-311)

### 6.4 其他

- **SSM/IRSA/WebIdentity**:凭证本身没有专门代码路径——`config.LoadDefaultConfig` 天然继承环境变量/IRSA/instance profile;SSM 只用作**用户数据的密文存储后端**(`SecureSecretsBackend=ssm-parameter-store`,awsmachine_types.go:44-53),不是凭证后端
- `AutoControllerIdentityCreator` feature gate(默认 true)自动创建 `default` 的 ControllerIdentity(`exp/controlleridentitycreator/`)
- `cmd/clusterawsadm` 提供 `bootstrap credentials` 子命令:CloudFormation 一次性创建 controller 所需的 IAM 角色/策略,是部署前置步骤
- 身份 webhook:role identity 校验 AllowedNamespaces.Selector 合法性(awsclusterroleidentity_webhook.go:65-68)

---

## 7. 网络设计

### 7.1 托管 VPC vs 自带 VPC

- `VPCSpec.IsUnmanaged/IsManaged`(`network_types.go:590-598`):`ID` 非空且没有 owned tag → unmanaged;unmanaged 时**不创建任何网络资源,只描述校验**,且必须显式给子网(`subnets.go:51` 起,reconcileSubnets 中 unmanaged VPC 无子网直接报错)
- 托管 VPC 默认 CIDR `10.0.0.0/16`(`network/vpc.go:44` `defaultVPCCidr`),可用 `SecondaryCidrBlocks`/`IPAMPool`/`IPv6` 扩展

### 7.2 创建顺序(`network/network.go:32-100`,condition 一一对应)

```
VPC(reconcileVPC) → SecondaryCIDRs → Subnets → InternetGateways → CarrierGateway(Wavelength)
→ EgressOnlyInternetGateway(IPv6) → NAT Gateways → RouteTables → VPCEndpoints
```
每步成功 MarkTrue 对应 condition,失败 MarkFalse 并 return。删除(`DeleteNetwork`,103-237)严格逆序:VPCEndpoints → RouteTables → NAT → EIP → IGW → CarrierGW → EgressOnlyIGW → orphaned ENI 清理(198-202)→ Subnets → SecondaryCIDR → VPC,每步更新 condition 为 Deleting/Deleted/DeletingFailed。

### 7.3 默认子网(`subnets.go:295` `getDefaultSubnets`)

- 无子网配置时,按 `AvailabilityZoneUsageLimit`(默认 3,`network_types.go:536-538`)在区域里取 AZ(`AvailabilityZoneSelection`:Ordered/Random),每个 AZ 建 1 公网 + 1 私有子网,公网/私有 CIDR 按 `SubnetSchema`(PreferPrivate 默认)切分(`subnets.go:336-378`)
- 子网模型(`SubnetSpec`,622-704):`ID`/`ResourceID`(托管时 AWS 生成的 ID 回填)、`IsPublic`、`IsIPv6`、`RouteTableID`、`NatGatewayID`(公网子网上的 NAT,供同 AZ 私有子网路由)、`ZoneType`(availability-zone/local-zone/wavelength-zone,边缘区域不能承载控制面/ELB/NAT,见 669-694 注释)
- 默认 NAT 每 AZ 一个(`natgateways.go`),公网 IP 汇总进 `Status.Network.NatGatewaysIPs`(cluster.go SetNatGatewaysIPs,342-349);删除顺序要求先删 NAT 再释放 EIP

### 7.4 Tagging 约定(`api/v1beta2/tags.go`)

- `BuildParams`/`Build`(217-269):统一生成标签集——`Name` 标签 + `Additional` + **owned 标签 `sigs.k8s.io/cluster-api-provider-aws/cluster/<clusterName>=owned|shared`**(158-163,207-209)+ 云 provider 标签 `kubernetes.io/cluster/<clusterName>=owned`(147-153,211-214,供 AWS 云控制器/ELB 服务发现)+ 角色标签(`NameAWSClusterAPIRole=sigs.k8s.io/cluster-api-provider-aws/role`,167)
- 角色值:apiserver/bastion/common/public/private(176-189);机器标签追加 `MachineName=<ns>/<name>`(192,242-246)
- `ResourceLifecycleOwned/Shared`(140-145):owned 资源删除集群时销毁,shared 保留——**外部资源 GC 就靠这套 tag 扫描**(`pkg/cloud/services/gc/`)
- 标签一致性由 `pkg/cloud/tags/tags.go` 的 `Builder` 保证(`Ensure` 幂等补齐),VPC 更新标签见 `network/vpc.go` reconcileVPC(用 `tags.New(&buildParams, tags.WithEC2(...))` + `WaitForWithRetryable`)

### 7.5 安全组(`securitygroup/securitygroups.go`)

- 角色:APIServerLB / LB / ControlPlane / Node / Bastion(controllers/awscluster_controller.go:71-76,defaultAWSSecurityGroupRoles;开 bastion 时追加)
- 规则要点(598-721):Node 组放行 NodePort(30000-32767,673)、kubelet 10250(681)、来自 ControlPlane/Node 组内互访、6443 由 LB 组;ControlPlane 组互访 + LB 组来源;LB 组开 6443(721);Bastion 组 SSH(643,705);CNI 规则可配(`NetworkSpec.CNI.CNIIngressRules`);支持 `SecurityGroupOverrides` 与 `AdditionalControlPlane/NodeIngressRules`、`NodePortIngressRuleCidrBlocks`
- 删除顺序注意:先删依赖方(ELB/实例)再删组,控制器用错误聚合容忍临时依赖(`awscluster_controller.go:268-299`)

### 7.6 公网/私有集群差异

- 控制面 LB Scheme:internet-facing(默认)vs internal(`AWSLoadBalancerSpec.Scheme`,211-214)
- EKS 侧:`EndpointAccess`(public/private)+ `RestrictPrivateSubnets`(EKS 只挂私有子网)+ bastion 跳板
- 私有子网出网靠 NAT;无 NAT 的纯私有场景由用户自带
- IPv6:VPC.IPv6 + EgressOnlyIGW + 双栈子网 + `AssignPrimaryIPv6`,EKS 侧 IpFamily=ipv6

---

## 8. 节点引导(userdata)

### 8.1 自建模式(kubeadm)

- **CAPA 不生成 kubeadm userdata**:引导数据由 CAPI 的 kubeadm bootstrap provider(CABPK,KubeadmConfig)生成,写入 Secret;CAPA 控制器只消费——`MachineScope.GetRawBootstrapDataWithFormat`(`scope/machine.go:283-300`)读 `Machine.Spec.Bootstrap.DataSecretName` 对应 Secret 的 `value` 与 `format` 字段
- 传输与存储(`CreateInstance` 内,instances.go:211-218 + SecretInterface):
  - 默认:userdata gzip + base64 直接放进 EC2 UserData(`UncompressedUserData` 可关压缩)
  - 安全模式(`CloudInit.InsecureSkipSecretsManager=false` 默认):把 userdata 拆块存入 AWS Secrets Manager 或 SSM Parameter Store(`SecureSecretsBackend`),EC2 UserData 里只放一个 boothook 脚本,实例启动后下载密文并**删除**云端 secret(`deleteEncryptedBootstrapDataSecret`,awsmachine_controller.go:771-796;SecretPrefix/SecretCount 记录分块)
  - Ignition 模式(需要 `BootstrapFormatIgnition` gate + `S3Bucket`):S3 存对象,`pkg/cloud/services/s3/` 负责桶与对象(含 presigned URL 选项,S3BucketSpec:366-373),支持 Karpenter 等第三方节点池通过额外 IAM role/前缀读取(`AdditionalIAMRoles`,328-342)
- 引导数据生命期:实例 Running 且注册后删除 Secret(awsmachine_controller.go:690-701)

### 8.2 EKS 模式(bootstrap/eks)

两种 bootstrap provider(都挂在 `cluster.Spec.ControlPlaneRef.kind == AWSManagedControlPlane` 上,eksconfig_controller.go:175-177):

**EKSConfig(老,面向 `/etc/eks/bootstrap.sh`)**
- `EKSConfigReconciler.Reconcile`(eksconfig_controller.go:73-152):等 owner(Machine/MachinePool)→ 等集群 → paused → defer 里 SetSummary(DataSecretAvailableCondition)→ joinWorker
- `joinWorker`(154-262):Machine 类型只生成一次(158-173,已有 DataSecretName 直接返回);等 `InfrastructureProvisioned`(179)与 `ControlPlaneInitialized`(188);组装 `userdata.NodeInput`(208-225,字段来自 EKSConfig.Spec: KubeletExtraArgs/ContainerRuntime/DNSClusterIP/PreBootstrapCommands/Files/Users...);`internal/userdata/node.go:30` 定义默认引导命令 `defaultBootstrapCommand = "/etc/eks/bootstrap.sh"`,生成 cloud-init 脚本:`/etc/eks/bootstrap.sh <cluster> --kubelet-extra-args ...`(node_test.go 展示了各参数的 CLI 映射)
- `storeBootstrapData`(299-334):创建/更新名为 `<config>.yaml` 的 Secret 并写 `Status.DataSecretName`

**NodeadmConfig(新,面向 AWS nodeadm)**:`nodeadmconfig_controller.go` 结构完全平行(EKSConfig 的现代替代),userdata 模板在 `internal/userdata/nodeadm.go`。

### 8.3 bootstrap/ 目录作用总结

`bootstrap/` 是"自举 provider"的 CAPI 契约实现:它消费 Machine/MachinePool → 产出 `DataSecretName` Secret → 状态 `Ready` + `DataSecretAvailableCondition`。CAPA 同时提供 EKS 系列;kubeadm 系列来自 CAPI 主仓库(不在本仓库)。

---

## 9. Webhooks

### 9.1 自建模式(`webhooks/`)

- **AWSCluster**(awscluster_webhook.go,546 行):
  - `ValidateCreate`(62-86):Bastion、SSHKeyName、AdditionalTags、S3Bucket、Network、控制面 LB(方案/健康检查/immutability)校验
  - `ValidateUpdate`(94-180):**Region 不可变**(110-114)、`controlPlaneEndpoint` 一旦设置不可变(136-141)、**VPC ID 不可改**(144-154,防止重建 VPC)、`identityRef` 不可删除(157-162)、`cluster.x-k8s.io/externally-managed` 注解不可移除(164-169)、LB 字段 immutable 检查(128-134)、classic ELB 弃用告警(warning,176-179)
  - defaulting:`awscluster_defaults.go` 的 `Default()`(20-22)走 zz_generated.defaults.go
- **AWSMachine**(awsmachine_webhook.go):Create/Update 校验(机型、AMI、安全组引用、Spot 与容量预留互斥等)
- **身份 webhook**:ControllerIdentity 单例名强制(awsclustercontrolleridentity_webhook.go:61-64)、RoleIdentity 的 AllowedNamespaces selector 合法性(65-68)、StaticIdentity 校验
- 模板 webhook:AWSClusterTemplate/AWSMachineTemplate

### 9.2 EKS 模式

- `controlplane/eks/webhooks/awsmanagedcontrolplane_webhook.go`:`ValidateCreate`(86+)校验 EKSClusterName/版本/网络;`validate.go` 里还有 EKS 特定规则(如 SecondaryCidrBlock 范围)
- `exp/webhooks/awsmanagedmachinepool_webhook.go`:`ValidateCreate`(152+)校验 AMIType/Scaling/taints 等
- `exp/webhooks/validation.go`:共享校验函数(如 LifecycleHooks 字段成对校验)
- 所有 webhook 通过 `SetupWebhookWithManager` 注册(main.go:450-477,580-588),CRD 的 webhook 配置由 config/crd/patches 注入(kubebuilder 标记生成)

### 9.3 模式总结

- 每个 webhook 用 `ctrl.NewWebhookManagedBy(...).WithCustomValidator(w).WithCustomDefaulter(w)`(awscluster_webhook.go:43-48)
- "immutable 清单"是 CAPA webhook 的核心价值:Region/VPC ID/ControlPlaneEndpoint/LB 类型等在创建后被锁定,避免漂移

---

## 10. clusterctl 合约

### 10.1 metadata.yaml(集群 API 合约声明)

- `kind: Metadata` / `apiVersion: clusterctl.cluster.x-k8s.io/v1alpha3`,`releaseSeries` 把每个 major.minor 映射到 contract 版本(v1alpha2→v1alpha3→v1alpha4→v1beta1);当前 2.x 全部 contract: v1beta1
- clusterctl 用它判断 provider 版本与 CAPI 主版本是否兼容

### 10.2 CRD 合约标签(关键机制)

`config/crd/kustomization.yaml:2-5`:
```yaml
commonLabels:
  cluster.x-k8s.io/v1alpha3: v1alpha3
  cluster.x-k8s.io/v1alpha4: v1alpha4
  cluster.x-k8s.io/v1beta1: v1beta1_v1beta2
```
含义:CRD 上打 `cluster.x-k8s.io/v1beta1=v1beta1_v1beta2` 标签,clusterctl 据此知道该 provider 同时提供 v1beta1 和 v1beta2 两个 CRD 版本,做 contract 匹配与升级判断。**这是"多 API 版本 provider"的标配做法**。

### 10.3 infrastructure-components.yaml 生成

- `make release-manifests`(Makefile:615-655):用 kustomize 把 `config/` 整体 build 出单文件 `infrastructure-components.yaml`(含 CRD、manager Deployment、RBAC、webhook 配置),上传到发布桶,clusterctl init 直接拉取
- `clusterctl-settings.json`:`{"name":"infrastructure-aws","config":{"componentsFile":"infrastructure-components.yaml","nextVersion":"v1.0.0"}}`
- 布局:`config/crd/`(bases + webhook/cainjection/label patches)、`config/manager/`(Deployment)、`config/default/`(namespace、manager_webhook_patch、webhookcainjection_patch、credentials.yaml、manager_credentials_patch(把 AWS 凭证做成 secret 挂载)、manager_iam_patch、manager_role_aggregation_patch、manager_service_account_patch、metrics_service、probes)、`config/rbac/`、`config/webhook/`、`config/certmanager/`
- **注意 credentials.yaml**:默认部署形态把 AWS 凭证作为 Secret 注入(dev/tilt 场景),生产建议用 IRSA/角色身份

### 10.4 tilt-provider.json

```json
[{"name":"aws","config":{"image":"gcr.io/k8s-staging-cluster-api-aws/cluster-api-aws-controller",
  "live_reload_deps":["main.go","go.mod","go.sum","api","cmd","controllers","exp","pkg","controlplane/eks","bootstrap/eks"],"label":"CAPA"}}]
```
tilt 据此做源码热重载(改依赖路径内的 .go 即自动重建镜像)。

### 10.5 模板 flavors

`templates/cluster-template*.yaml` 提供 20+ flavor(默认、machinepool、eks、eks-managedmachinepool、clusterclass、ipv6、fargate、rosa 等),e2e 与用户 `clusterctl generate cluster` 共用同一批模板(e2e 的 flavors 在 `test/e2e/data/infrastructure-aws/*/kustomize_sources/` 由 `make generate-test-flavors` 从模板+kustomize 派生)。

---

## 11. 测试体系

### 11.1 单元测试

- 位置:与被测文件同目录,命名 `*_test.go`/`*_unit_test.go`(如 `controllers/awscluster_controller_unit_test.go`、`pkg/cloud/services/network/vpc_test.go`)
- 手段:controller-runtime fake client + **service factory 注入 mock**(`pkg/cloud/services/mock_services/` 的接口 mock,配合 `awscluster_controller.go:94-99` 的工厂注入);`pkg/cloudtest/` 提供 fake EC2 客户端;API 转换测试(`api/v1beta1/conversion_test.go`)用 fuzz/round-trip 验证 v1beta1↔v1beta2
- `config/crd/kustomization_test.go`:校验 kustomize 输出与 CRD 标签

### 11.2 e2e(test/e2e,Ginkgo v2 + clusterctl)

- 结构:`suites/unmanaged/`(gc_test、CAPI_clusterclass_test 等)、`suites/managed/`(eks_test、eks_ipv6、eks_access_entries、eks_pod_identities、upgrade 等 20+ 文件)、`suites/conformance/`;`shared/`(aws.go/identity.go/template.go/cluster.go/suite.go...)公共工具
- 配置:`test/e2e/data/e2e_conf.yaml`(provider 版本、镜像预加载、替换规则)、`e2e_eks_conf.yaml`;跑法:`make test-e2e`/`make test-e2e-eks`(Makefile:464-476),GINKGO_FOCUS 选择用例
- **快速起集群**:用 CAPI 官方 test framework(`framework.NewClusterProxy`、`clusterctl.ApplyClusterTemplateAndWait`)在 kind 管理集群上 `clusterctl init` + 应用 flavors 模板,再断言集群/机器池就绪
- **配额处理**:`shared/aws.go:1019-1033` `EnsureServiceQuotas` 用 servicequotas API 预检(如 VPC/实例数),避免测试中配额不足;`shared/aws.go` 还有 `AcquireResources` 文件锁做并行资源配额(gc_test.go:59)
- 特殊:e2e 会先跑 `clusterawsadm bootstrap`(CloudFormation 建 IAM),`-skip-cloudformation-deletion` 可加速

---

## 12. 值得 CCE Provider 借鉴/规避的点

### 12.1 架构优点(建议照搬)

1. **Scope 模式**:一次 reconcile 一个上下文对象,持有对象引用 + session + patchHelper,`defer Close()` 统一落盘;服务层只依赖接口最小视图(EC2Scope/SGScope)。CCE Provider 建议同样拆 `ClusterScope`/`MachineScope`/`ManagedControlPlaneScope`/`NodePoolScope`,并把"写回状态"全部收敛到 scope 的 patch helper。
2. **接口化服务 + 工厂注入**:控制器里留 `xxxServiceFactory func(scope) services.XxxInterface` 字段,测试注入 mock(`mock_services/`)。这让编排逻辑可以零云依赖单测——CCE Provider 的 CCE API 客户端同样应该被接口包住。
3. **Condition 驱动进度 + SetSummary 集中汇总**:每个资源步骤(网络/安全组/LB/节点池)一个 `*Ready` condition,scope.PatchObject 统一汇总成 Ready + step counter;webhook 锁定 immutable 字段。对用户排障、clusterctl 状态机都极其重要。
4. **finalizer + 依赖计数 + 错误聚合删除**:先数依赖(AWSMachine 等)再删,删除收集所有错误一次返回,容忍资源间依赖;另有可选的外部资源 GC(按 tag 扫描 NLB/ALB)。CCE Provider 删除 ECS/CCE 集群资源时应照做。
5. **标签所有权模型**(owned/shared + 云 provider 标签):一切资源用统一 BuildParams 打标,既是幂等寻址(Describe by tag),也是 GC 和 BYO 资源判定的依据。
6. **多身份支持 + allowedNamespaces**:ControllerIdentity(默认凭证)/StaticIdentity(AK/SK)/RoleIdentity(AssumeRole 链,跨账户)统一抽象为 `AWSPrincipalTypeProvider`(带 Hash 做缓存失效),CCE 的 AK/SK、委托授权可复用这套模式。
7. **feature gate 隔离实验功能**:EKS/Fargate/MachinePool/ROSA 全部 gate 化,主流程默认只跑稳定功能;EKS 专属 flag(如 sync-period 上限)有交叉校验。
8. **限流与退避**:`throttle` token-bucket middleware + `wait.WaitForWithRetryable`,对 AWS API 配额友好;`max-wait-managed-resources` 统一控制托管资源等待上限。
9. **双 API 版本 + 转换 webhook**:v1beta1/v1beta2 并存、storageversion 标注、conversion 生成代码 + round-trip 测试,升级不破契约;CRD 用 `commonLabels` 打 `cluster.x-k8s.io/v1beta1: v1beta1_v1beta2` 合约标签。
10. **多模式复用同一套网络层**:EKS 与自建共享 NetworkSpec/network service(VPC/子网/IGW/NAT/安全组),只是 controlplane 实现不同;CCE 的 CCE 集群(托管)与自建(ECS+kubeadm)也应共享网络/安全组服务。

### 12.2 已知复杂性与坑(建议规避)

1. **EKS 模式处于 experimental 且有两套 bootstrap**:EKS 整体靠 `feature.EKS` gate;EKSConfig(老,`/etc/eks/bootstrap.sh`)与 NodeadmConfig(新)并存,迁移期要同时维护;EKS 开启时 `sync-period` 被强制 ≤10min(否则 EKS token 过期),主流程 flag 与 EKS flag 交叉校验复杂。CCE Provider 若同时支持 CCE 托管与自建,建议一开始就按"feature gate + 独立 module"布局,避免后期拆包。
2. **跨账户/委托授权的前置条件多**:RoleIdentity 假设角色要求源身份有 sts:AssumeRole 权限、目标账号信任策略、ExternalID、允许的命名空间等一长串条件,出错时只能靠 condition 定位;静态身份的 Secret 必须在控制器命名空间。CCE 的跨账号委托(如委托给子账号)应给出可操作的预检与错误提示。
3. **配额/限流是主要故障面**:EC2 Describe/RunInstances 限流、EKS 创建慢(等待 ACTIVE 可达分钟级)、删除依赖顺序(先删 ELB/ENI 再删 SG/子网)。CAPA 用限流中间件 + 重试 + 错误聚合缓解,但仍会出现"卡在某个 condition"。CCE Provider 应内置类似限流与"等待+requeue"模式,并对 quota 超限做专门错误分类。
4. **immutable 字段与漂移风险**:VPC ID/Region/ControlPlaneEndpoint 一旦设置不可变,但升级场景(如从无 LB 到有 LB、经典 ELB 迁移)要额外兼容逻辑(classic ELB 弃用告警就是例子);历史上还有"defaulting 前后反复 patch"的坑(awsmachine_controller 里 CNI 安全组遗留问题,awscluster_controller.go:164-169)。CCE Provider 在设计 Spec 时就要想清哪些字段进 immutable 清单、哪些要 defaulting 兼容旧对象。
5. **API 面过大带来的维护成本**:AWSMachineSpec 数十个字段 + deepcopy/conversion 生成 + 每个字段的 defaulting/validation,升级代价高;GC 与"未管理资源"标签判定(IsUnmanaged 依赖 tag)在用户误删 tag 时会误判。CCE Provider 应控制 Spec 面,优先覆盖高频字段,低频能力走"注解/外部 CRD"扩展。
6. **EKS 版本升级限制**:EKS 只能 minor 逐个升级,且存在 AWS 侧自动升级出标准支持导致 Ready=false 的逻辑(cluster.go:223-235);nodegroup 升级三通道(K8s 版本/AMI 版本/启动模板版本)并存,状态同步容易混乱。

---

## 对 CCE Provider 的借鉴清单(落地建议)

1. **目录骨架**仿照 CAPA:`controllers/`(自建)+ `controlplane/cce/`(托管控制面)+ `bootstrap/`(自举)+ `exp/`(实验)+ `pkg/cloud/scope|services|identity|tags|throttle|awserrors` + `config/`(kustomize)+ `templates/` + `metadata.yaml`。
2. **Scope 模式先行**:定义 `ClusterScoper`(含 PatchObject/Close/Session/IdentityRef)统一契约;每个控制器 `NewXxxScope` + `defer Close`。
3. **服务接口按使用场景切**:CCEClusterInterface(网络/安全组)、CCEMachineInterface(ECS 生命周期)、CCENodePoolInterface、SecretInterface(引导数据)、GCInterface;控制器 struct 保留 service factory 字段。
4. **条件命名与汇总照抄**:`VpcReady/SubnetsReady/NodePoolReady...` + scope 内 SetSummary + webhook immutable 清单。
5. **删除流程**:finalizer + 依赖计数 + 错误聚合 + 可选 GC(feature gate)。
6. **身份抽象**:`PrincipalTypeProvider`(Hash/Retrieve)支持"控制器默认凭证 / 静态 AK-SK / 委托角色链",带 allowedNamespaces 校验与 condition。
7. **标签/所有权**:统一 BuildParams(owned/shared + 云 provider 标签),幂等寻址与 GC 的基础。
8. **引导数据**:自建走 CABPK 生成的 Secret(`value`+`format`),托管模式自建 bootstrap provider(EKSConfig 风格:等 InfrastructureProvisioned/ControlPlaneInitialized 后生成 userdata 写 Secret)。
9. **clusterctl 合约**:`metadata.yaml` releaseSeries + `config/crd/kustomization.yaml` commonLabels(`cluster.x-k8s.io/v1beta1: v1beta1_v1beta2`)+ `infrastructure-components.yaml` 单文件发布 + `tilt-provider.json` 热重载 + 多版本转换 webhook。
10. **测试**:控制器单测用 factory 注入 mock;e2e 用 CAPI test framework + clusterctl + flavors,预检配额(仿 EnsureServiceQuotas)。

---

*报告基于 /tmp/capa @ 67de5c2 实际源码阅读整理;所有行号均为当前 checkout 对应行。*
