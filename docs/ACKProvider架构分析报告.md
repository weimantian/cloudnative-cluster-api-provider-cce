# 阿里云 Cluster API Provider(alibabacloud-provider-for-Cluster-API)源码分析报告

> 分析对象:克隆于 /tmp/capal(浅克隆,master 分支,最新提交 `349837c` "Merge pull request #2 from undefine1028/support-controlplane-custom-san",2025-07-08)。本报告所有文件路径相对 /tmp/capal,行号基于实际读取的源码。此仓库是我们开发华为云 CCE Cluster API Provider 的范本,以下按 12 个小节给出关键文件、代码事实与结论,文末附"直接借鉴清单"与"能力差距/可改进点"。

---

## 1. 仓库总体规模与成熟度

### 1.1 README.md 内容(README.md:1-197)
- 定位(README.md:3):阿里云官方(AliyunContainerService 组织)按 Cluster API 规范开发的 provider,只实现 **ACK 托管集群(managed cluster)的创建与删除**,支持自定义节点数、机型等参数。
- 开发环境(README.md:10-17):go 1.22.3、kubernetes v1.25.1、kubebuilder 3.14.0、terraform 1.7.3、terraform-aliyun-provider 1.223.2、clusterctl v1.6.3。**注意 terraform 是运行时依赖**,镜像里要打包 terraform 二进制和 alicloud provider 插件(见 1.3)。
- 部署方式(README.md:124-162):`kubectl apply -f config/crd/bases/` 装 CRD;在 `config/manager/provider_config.yaml` 填入 `access_key/secret_key`(必填,缺失则程序启动失败);`make docker-build` + `make deploy`。
- 使用方式(README.md:164-197):apply `config/samples/ack-test.yaml`(一个 Cluster + AliyunManagedCluster + AliyunManagedControlPlane + MachinePool + AliyunManagedMachinePool 的完整示例);`clusterctl describe cluster` / `clusterctl get kubeconfig` 可用(README.md:70-91),说明实现了 CAPI 对象合约。

### 1.2 go.mod 依赖(go.mod:1-27)
- 核心:`sigs.k8s.io/cluster-api v1.7.1`(go.mod:25)、`sigs.k8s.io/controller-runtime v0.17.3`(go.mod:26)、`k8s.io/* v0.29.3`。
- 关键架构依赖:`github.com/crossplane/crossplane-runtime v1.14.0-rc...`(go.mod:13)与 `github.com/crossplane/upjet v1.1.0`(go.mod:14)。**本项目把 crossplane/upjet 的 Terraform 驱动模式直接内嵌进 CAPI provider**:云资源(集群/节点池/VPC/交换机)都以 crossplane 风格 CRD 暴露,由 upjet 生成控制器调用 `terraform apply/destroy`(内部再调 ACK OpenAPI)。这是本项目最独特的架构选择,详见 §3、§4。
- 云 SDK:`github.com/alibabacloud-go/ess-20220222/v2`(go.mod:10,弹性伸缩 ESS SDK,仅用于查询节点池实例)、`darabonba-openapi/v2`、`tea` 等。
- 测试:`ginkgo/v2 v2.17.1`、`gomega v1.32.0`(go.mod:16-17)。

### 1.3 规模与成熟度(基于代码事实 + GitHub API 实测)
- GitHub 元数据(2025-07 抓取):created 2025-03-21,stargazers 4,forks 2,open issues 2,无正式 GitHub Release(releases API 返回空数组),仅有 git tag `v1.0.0-beta.2`(2025-07-08,即最新提交)。→ **早期实验性项目,并非生产级**;版本号停在 v1.0.0-beta 也印证这一点。
- 代码规模:api/ 下 4 个 group、共约 20 个类型文件;internal/ 下约 25 个 go 文件;config/ 为标准 kubebuilder 布局。Dockerfile 中把 `terraform 1.2.1` 与 `terraform-provider-alicloud_v1.223.2` 打进镜像(Dockerfile:31-34),镜像会显著变大且受 provider 版本漂移影响。
- 项目结构总览:
  - `cmd/main.go` — 组装所有控制器/webhook/upjet 控制器。
  - `api/` — `infrastructure/v1beta2`(AliyunManagedCluster、AliyunManagedMachinePool)、`controlplane/v1beta2`(AliyunManagedControlPlane)、`cs/v1alpha1`(upjet 生成的 ManagedKubernetes、KubernetesNodePool、VPC、Vswitch)、`alibabacloud/v1beta1`(ProviderConfig 等,复用 crossplane 类型)、`common/`(VPC/VSwitch 通用结构)。
  - `internal/controller/` — 手写 CAPI 控制器(controlplane、infrastructure)+ upjet 生成控制器(cs/*)+ 手写清理控制器(cs/vpc、cs/vswitch)+ providerconfig。
  - `internal/clients/` — 凭据注入(terraform setup builder)与 ESS SDK 封装。
  - `internal/config/` — upjet provider 配置(schema、external name、字段裁剪)。
  - `config/` — CRD/manager/rbac/webhook/samples/证书管理。
  - `test/e2e` — ginkgo 冒烟测试。

---

## 2. CRD 类型体系

### 2.1 总览:实际类型名(与题目猜测不同)
- **没有 AliyunCluster / AliyunMachine / AliyunMachinePool(经典模式)**,只有 Managed 系列:
  - `AliyunManagedCluster`(infrastructure.cluster.x-k8s.io/v1beta2)
  - `AliyunManagedControlPlane`(controlplane.cluster.x-k8s.io/v1beta2)
  - `AliyunManagedMachinePool`(infrastructure.cluster.x-k8s.io/v1beta2)
- 另有 4 个 `cs.alibabacloud.com/v1alpha1` 的"中间资源"(upjet 生成,cluster-scoped,见 `config/crd/bases/cs.alibabacloud.com_*.yaml:19`):`ManagedKubernetes`、`KubernetesNodePool`、`VPC`、`Vswitch`。它们对用户透明,由 CAPI 控制器代建,承载与阿里云的真实交互。
- 还有 crossplane 风格的 `ProviderConfig / ProviderConfigUsage / StoreConfig`(alibabacloud.alibabacloud.com/v1beta1)管凭据。

### 2.2 AliyunManagedCluster(api/infrastructure/v1beta2/aliyunmanagedcluster_types.go)
- Spec(行 29-37):**只有一个字段** `ControlPlaneEndpoint clusterv1.APIEndpoint`。注释(行 33-35)明确说明:该字段必须放在 spec 下,因为 cluster-api 的 `cluster_controller_phases.go → reconcileInfrastructure()` 会用 `UnstructuredUnmarshalField` 读取它。
- Status(行 40-46):`Ready bool`、`FailureDomains`。
- CAPI 合约角色:**InfraCluster**(cluster.spec.infrastructureRef)。控制器只做两件事:把 ControlPlane 的 endpoint 同步进 spec、把 status.ready 置 true(见 §3.2)。

### 2.3 AliyunManagedControlPlane(api/controlplane/v1beta2/aliyunmanagedcontrolplane_types.go)
- Spec(行 31-55)关键字段:
  - `ClusterName`(ACK 集群名)、`Region`(如 cn-hangzhou,必填)、`Version`(如 1.28.3-aliyun.1,空则用最新)、`ClusterSpec`(ack.pro.small 等)、`ClusterDomain`(默认 cluster.local)、`ResourceGroup`、`DeletionProtection`、`TimeZone`、`AdditionalTags`。
  - `Network`(行 57-71):`Vpc`/`VSwitches`(common 类型,见 §2.5)、`PodCIDR`、`ServiceCIDR`、`CustomSan`(apiserver 证书 SAN)、`NatGateway bool`、`SecurityGroup{ID, Create, Enterprice}`。
  - `Addons`、`Logging{Enable,TTL,Components}`、`CNI{Disable}`、`EncryptionConfig{ProviderKey}`、`KubeProxy{ProxyMode}`、`EndpointAccess{Public}`(是否给 apiserver 配公网 IP)。
  - `ControlPlaneEndpoint`(CAPI 回写点)。
- Status(行 93-109):`Ready`、`Initialized`(kubeconfig 已生成)、`FailureDomains`、`FailureReason/Message`、`ExternalManagedControlPlane`、`Version`、`Conditions`、`NetworkStatus{SecurityGroup, ApiServerSlbID, NatGatewayID}`、`Addons`、**`ClusterID`**(ACK 集群 ID,行 108)。
- 对接 ACK 的方式:本身不直接持有 endpoint/kubeconfig,而是:
  - `Status.ClusterID = *managedKubernetes.Status.AtProvider.ID`(控制器 setStatus,aliyunmanagedcontrolplane_controller.go:429);
  - endpoint 从 `ManagedKubernetes.Status.AtProvider.Connections` 解码(行 422-441,`ManagedKubernetesStatusConnection` 结构见 api/cs/v1alpha1/related_types.go:25);
  - kubeconfig 由控制器用 ACK 下发的 CA/client cert/key 现场拼装(见 §3.1)。
- CAPI 合约角色:**ControlPlane**(cluster.spec.controlPlaneRef)。

### 2.4 AliyunManagedMachinePool(api/infrastructure/v1beta2/aliyunmanagedmachinepool_types.go)
- Spec(行 31-41):`ClusterID`、`AckNodePoolName`、`ResourceGroupID`、`ScalingGroup`、`ProviderIDList`(控制器回填)、`Region`。
- `ScalingGroup`(行 43-56):`VSwitches`、`InstanceTypes []*string`(多规格)、`DesiredSize *float64`、`Password/KeyName`(二选一)、`SystemDiskCategory/Size/PerformanceLevel`、`DataDisks`、`ImageType`、`SecurityGroupIDs`、`KubernetesConfig`。
- `KubernetesConfig`(行 58-65):`RuntimeName/RuntimeVersion`、`UserData`、`Tags`、`Labels`、`Taints`。
- Status(行 77-104):`Ready`、**`Replicas int32`**(注释:按实际启动节点数设置,cluster-api 据此回写 MachinePool.Status.Replicas)、`AckNodePoolID`、`State`、`FailureReason/Message`、`Conditions`、`NodeStatus{DesiredNodes, FailedNodes, HealthyNodes, SpotNodes...}`(行 92-104,来自 ACK 节点池统计)。
- CAPI 合约角色:**InfraMachinePool**(MachinePool.spec.template.spec.infrastructureRef)。

### 2.5 通用网络类型(api/common/)
- `VPC`(api/common/vpc.go:24-31):`ID`(已有实例)、`Name`、`ResourceID`(自建成功后回填的云上 id)、`UID`(本地 CR 的 uid)、`CIDRBlock`、`Description`;`Equal()` 允许信息回填(行 33-37)。
- `VSwitch`(api/common/vswitch.go:33-48):同样 `ID`(非空=已有实例)/`Name`/`ResourceID`/`UID`/`ZoneID`/`CIDRBlock`/`Description`。`GetIDList()`(行 65-83)把"已有或已建好"的 vswitch 收集成 ACK 需要的 `[]*string`,若创建中导致 id 未回填则返回错误(触发重试);`Equal()` 用于 webhook 判断不可变字段。
- 设计意图:同一个结构既能表达"引用已有云资源"也能表达"自建资源"(建好后回填 ResourceID/UID),是双模式复用的关键。

### 2.6 upjet 中间资源(cs/v1alpha1)
- `ManagedKubernetes`(zz_managedkubernetes_types.go):`Spec.ForProvider` 几乎覆盖 alicloud_cs_managed_kubernetes 全部参数(Addons、ClusterSpec、ClusterDomain、PodCidr、ServiceCidr、NewNATGateway、CustomSan、WorkerVswitchIds、SecurityGroupID、ProxyMode、Timezone、Tags、ResourceGroupID、DeletionProtection、SlbInternetEnabled、Version...)(行 118-470 区间);`Status.AtProvider`(行 226+)含 `ID`(集群 id)、`CertificateAuthority`、`Connections`、`SecurityGroupID`、`SlbID`、`VPCID`、`Version`、`Addons`、`NATGatewayID` 等。
- `KubernetesNodePool`(zz_kubernetesnodepool_types.go):`ForProvider` 含 ClusterID、Name、VswitchIds、InstanceTypes、ScalingConfig、SystemDisk*、SecurityGroupIds、RuntimeName/Version、UserData、DataDisks、Tags/Labels/Taints、KMSEncryptedPassword 等;`Status.AtProvider`(行 435+)含 `ID`(**格式 `cluster_id:nodepool_id`**,见控制器 strings.Split 处)、`ScalingGroupID`、`DesiredSize`、`ScalingConfig`、`SecurityGroupIds`、`Instances` 等。
- 这类 CRD 都带 crossplane 的 CEL 校验(如 `spec.forProvider.workerVswitchIds` 必填,zz_managedkubernetes_types.go:574-576)与 `crossplane.io/external-name` 标注。

---

## 3. 控制器实现

### 3.1 控制器清单与职责(internal/controller/)
| 控制器 | 文件 | 职责 |
|---|---|---|
| AliyunManagedControlPlaneReconciler | controlplane/aliyunmanagedcontrolplane_controller.go | CAPI 控制面主流程:ProviderConfig→VPC→VSwitch→ManagedKubernetes→状态回写→kubeconfig;删除编排 |
| (同上 kubeconfig 部分) | controlplane/aliyunmanagedcontrolplane_controller_kubeconfig.go | 生成 CAPI kubeconfig Secret |
| (同上 ProviderConfig 部分) | controlplane/aliyunmanagedcontrolplane_providerconfig.go | 按 region 自动创建 Secret + ProviderConfig |
| AliyunManagedClusterReconciler | infrastructure/aliyunmanagedcluster_controller.go | 同步 endpoint 到 spec、置 status.ready |
| AliyunManagedMachinePoolReconciler | infrastructure/aliyunmanagedmachinepool_controller.go | 节点池主流程:等控制面→SDK client→VSwitch→KubernetesNodePool→状态回写(含 ESS 查询 providerID);删除编排 |
| (同上工具) | infrastructure/aliyunmanagedmachinepool_controller_utils.go | 找属主 MachinePool、password Secret 管理 |
| (同上索引/映射) | infrastructure/aliyunmanagedmachinepool_indexer.go | 索引器 + 事件映射(MachinePool/ControlPlane/NodePool→AliyunPool) |
| upjet: managedkubernetes/kubernetesnodepool | cs/managedkubernetes/zz_controller.go、cs/kubernetesnodepool/zz_controller.go | **upjet 生成**:对 cs 中间 CR 跑 terraform apply/destroy(内部调 ACK OpenAPI) |
| upjet + 手写: vpc/vswitch | cs/vpc/zz_controller.go + vpc_controller.go、cs/vswitch/zz_controller.go + vswitch_controller.go | upjet 负责同步;手写 Reconciler 只做"删除超时强制移除 crossplane finalizer"的兜底(vpc_controller.go:46-71) |
| providerconfig | controller/providerconfig/config.go | crossplane 标准 ProviderConfig 控制器(usage 追踪,复用 crossplane-runtime 的 `providerconfig.NewReconciler`) |

### 3.2 AliyunManagedCluster 控制器(infrastructure/aliyunmanagedcluster_controller.go)
非常简单(行 60-117):Get 自身→`util.GetOwnerCluster` 找属主 Cluster→读 `cluster.spec.controlPlaneRef` 指向的 AliyunManagedControlPlane→把 `controlPlane.Spec.ControlPlaneEndpoint` 写入 `aliyunManagedCluster.Spec.ControlPlaneEndpoint`(行 103,这是 cluster 进入 Provisioned 的判断条件)→`Status.Ready=true`、FailureDomains 同步(行 109-110)。Watch 控制面变化(行 132-139)以尽快更新 endpoint。

### 3.3 创建 ACK 托管集群的 reconcile 流程(controlplane/aliyunmanagedcontrolplane_controller.go)
主流程(行 70-239):
1. Get 自身,`util.GetOwnerCluster` 找 Cluster;Cluster 的 InfrastructureRef.Kind 匹配时,若 `cluster.Status.InfrastructureReady` 为 false 则等 20s 重试(行 124-135)。
2. 加 finalizer(行 137-141)。
3. `reconcileProviderConfig`(行 143-157):确保凭据 Secret 与 ProviderConfig 存在(详见 §5)。
4. `common.ReconcileVPC`(行 159-182):拿到 vpcID(已有或自建)。
5. `common.ReconcileVSwitch`(行 184-208):拿到 vswitchID 列表。
6. `reconcileManagedKubernetes`(行 210-227):创建/更新 ManagedKubernetes CR。
7. `setStatus`(行 229):把 ManagedKubernetes 的 conditions/AtProvider 映射到 AliyunManagedControlPlane.Status。
8. Ready 且 endpoint 有效时 `reconcileKubeconfig`(行 230-235)。
9. 函数尾部用 defer 统一做 `Status().Update` + `Update`(行 101-114)。

**reconcileManagedKubernetes(行 241-343)的幂等设计**:
- 以 `cluster.Name` 作为 ManagedKubernetes 的资源名(行 250-253),先 `Get`,IsNotFound 则创建(行 256-263),否则视为已存在。
- 创建时(行 265-316):构造 CR,ownerRef 指向 AliyunManagedControlPlane(行 271-280),`ForProvider` 从 AliyunManagedControlPlane.Spec 逐字段映射(行 288-310:Name=ClusterName、ClusterSpec、Version、ClusterDomain、ServiceCidr、PodCidr、NewNATGateway、CustomSan、WorkerVswitchIds、SecurityGroupID、Addons、ProxyMode、Timezone、Tags、ResourceGroupID、DeletionProtection、SlbInternetEnabled=EndpointAccess.Public)。注意 **WorkerVswitchIds 用的是控制面 vswitch**,node 池的 vswitch 在 §3.4。
- 已存在时(行 318-339):只更新**可更新字段**(Name、Version、DeletionProtection、Addons、Tags、Logging 相关),其余字段不可变(webhook 保证);并打 `AliyunManagedControlPlaneUpdatingCondition`。

**等待就绪与状态回写(setStatus,行 346-449)**:
- 先读 crossplane conditions:TypeSynced、TypeAsyncOperation、TypeLastAsyncOperation 有 Message 则打失败 condition 并写 FailureMessage(行 354-395)——**但注意这些不影响 Ready 判定**。
- `TypeReady != True` 则直接 return(行 397-402),Requeue 由事件驱动。
- Ready 后:清 Creating/Updating condition(行 407-420);从 `Status.AtProvider.Connections` 解码出 endpoint 写入 `Spec.ControlPlaneEndpoint`(行 422-441,注释:port 是 int 类型,ACK 返回字符串,由 Decode 转换);回填 ClusterID、Version、NetworkStatus{SecurityGroup,ApiServerSlbID}(行 429-435);VPC 的 ResourceID 回填(行 442-444);打 `AliyunManagedControlPlaneReadyCondition`(行 446)。

**kubeconfig 生成(kubeconfig 文件:40-184)**:
- 用 `ManagedKubernetes.Status.AtProvider.CertificateAuthority`(Decode 成 `CertificateAuthority{ClusterCert,ClientCert,ClientKey}`,related_types.go:79)拼出 `api.Config`,Server 为 `https://host:port`(行 95-119),用户名为 `<cluster>-capi-admin`(行 176-184),调用 cluster-api 的 `kubeconfig.GenerateSecretWithOwner` 创建 `<cluster>-kubeconfig` Secret(行 130),最后 `Status.Initialized = true`(行 78)。这与 CAPI 的 kubeconfig 合约完全一致,所以 `clusterctl get kubeconfig` 可用。

**删除流程(reconcileDelete,行 451-512)**:先删 ManagedKubernetes(行 459-481,删除中则 20s 重试)→ `ReconcileRemoveVSwitch`(行 484-494)→ `ReconcileRemoveVPC`(行 497-505)→ 移除自身 finalizer(行 508)。`DeleteTimeout`(默认 300s,cmd/main.go:128)用于判定删除卡死。

### 3.4 MachinePool ↔ 节点池映射(infrastructure/aliyunmanagedmachinepool_controller.go)
主流程(行 85-244):
1. Get 自身→ `getOwnerMachinePool`(行 100,通过 OwnerReferences 找 MachinePool,注释说明是借鉴 CAPA v2.4.0 的做法,不建 uid 索引)。
2. `util.GetClusterFromMetadata` 找 Cluster;`annotations.IsPaused` 支持暂停(行 118-121)。
3. 读 ControlPlane,`!Status.Ready` 则打 `WaitingForAliyunManagedControlPlane` condition 并返回(行 145-164)——**节点池强依赖控制面 ready 拿到 clusterId**。
4. `setupSDKClient`(行 168-178,详见 §5)。
5. 加 finalizer(行 179-183)。
6. 读 ManagedKubernetes 拿 `Status.AtProvider.VPCID`(行 186-197),`common.ReconcileVSwitch` 为节点池自建/复用 vswitch(行 200-220)。
7. `reconcileKubernetesNodePool`(行 222-238)→ `setStatus`(行 240)。

**reconcileKubernetesNodePool(行 246-385)**:
- 先回填派生字段:`aliyunPool.Spec.Region = controlPlane.Spec.Region`、`Spec.ClusterID = controlPlane.Status.ClusterID`、`DesiredSize = MachinePool.Spec.Replicas`(行 270-274)——**replicas 的唯一来源**。
- 幂等:以 `aliyunPool.Name` 为 KubernetesNodePool 名,Get 判存在(行 257-268)。
- 创建时(行 276-339)映射:`ClusterID`、`Name=AckNodePoolName`、`VswitchIds`、`InstanceTypes`、`ScalingConfig{MaxSize=MinSize=DesiredSize}`、`SystemDisk*`、`SecurityGroupIds`、`RuntimeName/Version`、`UserData`、`DataDisks`、`Tags/Labels/Taints`。**重要坑(行 304-313,代码注释)**:terraform-provider-alicloud 从 1.218.0 升到 1.223.2 后,`DesiredSize` 与 `ScalingConfig` 同时出现会冲突,所以**弃用 DesiredSize,改用 `ScalingConfig{max=min=desired}` 实现固定数量**。ImageType 仅在非空时赋值(行 330-332)。
- `ensurePasswordAndKeyName`(行 333-335,实现在 utils.go:99-121):KeyName 优先;否则把 `Spec.ScalingGroup.Password` 写进一个 `<pool>-password` Secret,`ForProvider.PasswordSecretRef` 引用它(utils.go:123-172,upjet 会把 secret 内容传给 terraform)。代码注释指出 KMSEncryptedPassword 与 KeyName 冲突被弃用(行 301-302)。
- 更新时(行 340-382):更新 Name/VswitchIds/KeyName/InstanceTypes/ScalingConfig/磁盘/Runtime/UserData/Tags/Labels/Taints;注释(行 363-365)提醒:Tags/Labels/Taints 变更**只影响新增节点,不影响已有 ECS**。

**setStatus(行 418-522)**:
- 与 §3.3 相同的 crossplane conditions 搬运(行 424-465)。
- `AtProvider.ID` 形如 `clusterId:nodePoolId`,用 `strings.Split(":")` 拆出节点池 id 写入 `Status.AckNodePoolID`(行 476-479)。
- `Status.Replicas` 从 `AtProvider.DesiredSize` 或 `ScalingConfig[0].MinSize` 取(行 480-485)。
- 安全组:spec 未指定时把 `AtProvider.SecurityGroupIds` 回写进 spec(行 487-489)。
- **providerID 收集(行 491-518)**:用 ESS SDK `DescribeScalingInstances`(ScalingGroupId 来自 `AtProvider.ScalingGroupID`)列出实例,拼成 `region.instance-id` 格式写入 `Spec.ProviderIDList`(行 509-511),供 cluster-api 的 MachinePool 控制器做 Node 关联。这是本项目唯一直接用阿里云 SDK 的地方。

**删除(行 524-576)**:删 KubernetesNodePool → 等其 DeletionTimestamp → `ReconcileRemoveVSwitch` → 移除自身 finalizer。

### 3.5 事件映射与索引器(indexer 文件)
- 因为 ManagedKubernetes/KubernetesNodePool 是 **cluster-scoped**(config/crd/bases/...:19),OwnerReferences 里没有 namespace,`Owns()` 无法正确映射请求(控制器注释,aliyunmanagedcontrolplane_controller.go:527-530),所以全部手写 `handler.EnqueueRequestsFromMapFunc` + 字段索引器:
  - `IndexAliyunControlPlaneByUID`、`IndexAliyunPoolByUID`、`IndexNodePoolByAliyunPoolUID`(indexer 文件:39-57)。
  - `mapMachinePoolToAliyunPool`(行 60-97):校验 InfrastructureRef 的 GroupKind 是 AliyunManagedMachinePool 后再入队。
  - `mapAliyunControlPlaneToAliyunPool`(行 101-151):控制面 ready 时通过 `cluster.x-k8s.io/cluster-name` label 列出 MachinePool 批量唤醒。
  - `mapNodePoolToAliyunPool`(行 154-196):节点池状态变化时按 ownerRef.UID 反查 AliyunPool。
- SetupWithManager 里 Watch 了 MachinePool、AliyunManagedControlPlane、KubernetesNodePool(controller 文件:597-625)。

### 3.6 cs/ 下的控制器分工
- `cs/managedkubernetes`、`cs/kubernetesnodepool`、`cs/vpc`、`cs/vswitch` 各有一个 upjet 生成的 `zz_controller.go`(如 zz_controller.go:36-81):用 `tjcontroller.NewConnector(... o.Provider.Resources["alicloud_cs_managed_kubernetes"] ...)` 把每个 CR 交给 terraform workspace 执行,`managed.WithFinalizer(terraform.NewWorkspaceFinalizer(...))`(zz_controller.go:62),带 3 分钟超时(行 63)、10 分钟轮询(由 cmd/main.go:135 的 --poll 控制)。
- 手写的 `vpc_controller.go`/`vswitch_controller.go`:只处理 DeletionTimestamp 且超过 DeleteTimeout 的 CR,直接移除 crossplane finalizer 强制清理(行 47-71),防止删除卡死泄漏云资源。

---

## 4. Scope 与客户端模式

### 4.1 没有经典 CAPI 的 Scope 结构
与 CAPA(AWS)的 `AWSScope` 不同,这里**没有统一 Scope 对象**,分三层:
1. **CAPI 控制器层**:reconciler 内嵌 `client.Client`(如 aliyunmanagedcontrolplane_controller.go:50-55),直接用 controller-runtime 操作 K8s 对象。
2. **云资源层(crossplane/upjet)**:所有云资源交互收敛到 cs CR;每个 CR 的 `ResourceSpec.ProviderConfigReference`(crossplane 标准字段)指向 ProviderConfig,upjet 控制器据此取凭据并执行 terraform。
3. **SDK 层(仅查询用)**:`internal/clients/aliyun-sdk.go` 封装 ESS SDK,供节点池 status 查询实例。

### 4.2 internal/clients/ 的封装
- `aliyun-sdk.go`:全局变量 `AliyunCreds`(行 31,进程级缓存)、`EssEndpointList`(行 32)。启动时 `EssEndpointList.Init()`(行 62-84)从 `https://api.aliyun.com/meta/v1/products/Ess/endpoints.json` 拉取 region→endpoint 映射,失败即 `os.Exit(1)`(cmd/main.go:171-175)。`CreateSDKClient(regionID)`(行 106-123)用 AK/SK + endpoint 构造 ESS client。
- `alibabacloud.go`:`TerraformSetupBuilder`(行 38-83)是 upjet 的 `SetupFn`:读取 CR 的 ProviderConfigReference → Get ProviderConfig → `ProviderConfigUsageTracker.Track`(行 57,记录 usage)→ `resource.CommonCredentialExtractor`(行 62,支持 Secret/InjectedIdentity/Environment/Filesystem 多种来源)→ JSON 解析出 access_key/secret_key/region(行 66-81)注入 terraform provider 配置。**这是连接"凭据"与"terraform 执行"的枢纽**。

### 4.3 internal/config/ 的作用
upjet provider 的"代码生成期配置"(运行时由 cmd/main.go:287 `config.GetProvider()` 加载):
- `provider.go`:embed `schema.json`(alicloud provider 全量 schema)与 `provider-metadata.yaml`;`WithIncludeList(ExternalNameConfigured())`(行 30)只启用 4 个资源;`WithRootGroup("alibabacloud.com")`。
- `external_name.go`:4 个资源统一 `config.IdentifierFromProvider`(行 13-16),即云上 id 直接作为 external-name。
- `cs/config.go`:裁剪与托管模式冲突的 terraform schema 字段——`managedKubernetesRemovedAttrs`(行 22-64)删掉 `worker_*`、`password`、`key_name`、`taints`、`kube_config` 等一批字段(这些是"带 worker 的托管集群"旧用法),`nodePoolRemovedAttrs`(行 66-71)删 `node_count`、`platform` 等;并给 alicloud_vpc/alicloud_vswitch 删 `name`/`availability_zone`(行 114-121)。

---

## 5. 凭证管理

### 5.1 启动凭据(必填,缺失即退出)
- 位置:`config/manager/provider_config.yaml:20-31` 定义 Secret(默认 ns/name/key 见 cmd/main.go:130-132:`cluster-api-provider-aliyun-system` / `cluster-api-provider-aliyun-aliyun-tf-creds` / `credentials`),内容为 JSON `{"access_key":"","secret_key":""}`。
- 启动校验(cmd/main.go:225-246):用 client-go 直接 Get 该 Secret,`CredentialSecret.Decode`(controlplane_providerconfig.go:47-74)解析 JSON 并校验 access_key/secret_key 非空,失败 `os.Exit(1)`(README.md:134 也强调"必填,否则程序启动失败")。

### 5.2 按 region 自动生成 ProviderConfig(reconcileProviderConfig,controlplane_providerconfig.go:78-153)
- 对每个 region:检查 Secret `aliyun-<region>`(行 84-115),不存在则用启动凭据 + region 字段创建(行 93-114)。
- 检查集群级 ProviderConfig(`name=<region>`,行 117-123),不存在则创建:Spec.Credentials = `{Source: Secret, secretRef: {ns, name, key}}`(行 134-147)。
- 于是 upjet 资源统一引用 `ProviderConfigReference.Name = <region>`(见 aliyunmanagedcontrolplane_controller.go:163-167 的 ResourceSpec 构造)。
- 校验:`CredentialSecret.Decode` 只检查空字段(行 62-67),region 不要求用户填(行 68-71,注释:由 Spec.Region 注入)。

### 5.3 机器池的凭据路径
- `setupSDKClient`(machinepool controller 行 387-416):若全局 `clients.AliyunCreds` 已就绪则跳过;否则读 ManagedKubernetes 的 ProviderConfigReference → Get ProviderConfig → `resource.CommonCredentialExtractor` → JSON unmarshal 进全局 `AliyunCreds`(行 412-414)。
- **局限**:凭据是**进程级全局单例**,多 region 由 endpoint 区分,但不支持"同一 region 内不同 AK/SK 的机器池"。

---

## 6. 网络:VPC / 交换机管理

### 6.1 自动创建 vs 复用已有(common/vpc.go、common/vswitch.go)
- VPC(ReconcileVPC,vpc.go:41-155):
  - `VPC.ID != ""` → 直接用已有 VPC(行 48-51);`ResourceID+UID` 非空 → 视为自建已完成,直接返回(行 52-55)。
  - `Name == ""` → 什么都不建,返回空(行 57-60,可行场景)。
  - 否则以 `Name` 为 VPC CR 名,Get 判存在(行 71-90);若存在但 OwnerReferences 属主不是自己 → 报错 `"vpc is already exist"`(行 82-88,因为同名资源在阿里云可建但 terraform 会报错);`AtProvider.ID` 回填成功则记录 ResourceID/UID(行 94-99);读 crossplane conditions 判断错误(行 101-116);创建中则返回 `"vpc not ready"` 触发重试(行 118)。
  - 创建:构造 VPC CR,ownerRef 指向调用方(ControlPlane 或 MachinePool),`ForProvider{VPCName, CidrBlock, Description}`(行 122-145)。
- VSwitch(ReconcileVSwitch,vswitch.go:45-161):与 VPC 同构;先 `GetIDList()` 快速判断是否全部就绪(行 52-55);逐个处理未完成的 vswitch,自建时 `ForProvider{VswitchName, CidrBlock, VPCID, ZoneID, Description}`(行 138-147);多个 vswitch 的错误聚合返回(行 57-159)。

### 6.2 删除与兜底
- `ReconcileRemoveVPC/VSwitch`(vpc.go:157-194、vswitch.go:163-210):跳过 `ID` 指定的已有资源(不删别人的网络);对自建 CR 发 Delete,DeletionTimestamp 超时(DeleteTimeout)返回 `ErrorTimeout`(vswitch.go:39)由上层容忍(controlplane controller 行 487-504 跳过 ErrorTimeout 继续推进)。
- 兜底:vpc/vswitch 的手写 reconciler 在超时后强制移除 crossplane finalizer(vpc_controller.go:46-71)。

### 6.3 已知约束(todo 注释)
- 同一集群的 control plane 与 machine pool **无法使用同名 vswitch**(vswitch.go:84 注释)。
- 不管理安全组规则、不管理路由表、不管理 NAT 网关细节(NatGateway 只透传 bool)、不管理 SLB/SLB(ACK 自动创建,只回读 SlbID)。
- 网络 CIDR 的校验全部在 webhook(见 §9),运行时不做。

---

## 7. 引导与节点接入

- **托管节点池不需要 userdata/bootstrap**:示例中 `MachinePool.spec.template.spec.bootstrap.dataSecretName: ""`(config/samples/ack-test.yaml:77-78);节点由 ACK 托管节点池(ESS 弹性伸缩组)自动创建并 join 集群,`KubernetesConfig.UserData` 只是透传给 ACK 的可选字段(machinepool controller 行 321、362)。
- **没有 self-managed ECS 模式**:无 AliyunMachine/AliyunMachinePool 类型、无 bootstrap provider、无 kubeadm、无 ignition;整个 api/ 只有 Managed 系列(grep 确认)。
- 节点身份关联:通过 `Spec.ProviderIDList`(格式 `region.instance-id`)与 cluster-api 的 MachinePool/Node 对齐(machinepool controller 行 491-518);`AckNodePoolID` 供排障使用。
- 对 CAPI 而言,`MachinePool.Status.Replicas` 由 AliyunManagedMachinePool.Status.Replicas 提供,伸缩直接改写 ACK 节点池的期望数量。

---

## 8. features(feature gate)

- `internal/features/features.go` 只有两个 **crossplane 的** feature flag(行 12-22):
  - `EnableAlphaExternalSecretStores`(External Secret Stores,alpha);
  - `EnableBetaManagementPolicies`(Management Policies,beta,复用 crossplane 定义)。
- 用法(cmd/main.go:294-322):`--enable-external-secret-stores`(默认 false)开启时创建默认 `StoreConfig`(行 300-317);`--enable-management-policies`(默认 true,行 145-146)启用后给 upjet 控制器加 `managed.WithManagementPolicies()`(zz_controller.go:71-73)。
- **没有 CAPI 风格的 feature gate**(例如 AWS 的 MachinePool/EventBridge 之类),也没有集群升级/自动伸缩等能力开关。

---

## 9. Webhooks 与校验

### 9.1 AliyunManagedControlPlane(api/controlplane/v1beta2/aliyunmanagedcontrolplane_webhook.go)
- Defaulting(行 63-107):ClusterName 为空则用 `<namespace>_<name>`(超长用 blake2b 生成 32 位 base36 摘要,related_types.go:66-102)生成;ClusterDomain 默认 `cluster.local`;ClusterSpec 默认 `ack.standard`;ProxyMode 默认 `ipvs`;**强制把 nginx-ingress-controller addon 置为 disabled**(行 92-106,因为 ACK 默认装 ingress 与 CAPI 冲突)。
- ValidateCreate(行 115-250):clusterName 格式/长度(related_types.go:39-57,不可 `-`/`_` 开头、最长 63);Region 必填;ClusterSpec 枚举 `{ack.standard, ack.pro.small}`(行 127-131);ProxyMode 枚举 `{iptables, ipvs}`;vswitch 数量 1-5(行 155-160);vswitch id 与其他字段互斥(行 162-192);**vpc/vswitch 四象限联合校验**(行 195-237:空 vpc 只能用已有 vswitch;已有 vpc 可已有/自建 vswitch;自建 vpc 只能自建 vswitch;vpc id 与 name 不可同时出现);CIDR 合法性;AdditionalTags 校验(行 241,tags.go:34-88,限制 20 对、禁 aliyun/acs: 前缀)。
- ValidateUpdate(行 253-358):**大量字段 immutable**——Region、ClusterSpec、ClusterDomain、ResourceGroup、TimeZone、Vpc、VSwitches、NatGateway、SecurityGroup 三字段、CNI.Disable、EncryptionConfig、KubeProxy.ProxyMode(行 269-349);ServiceCIDR/PodCIDR 的 immutable 校验被注释(行 305-314,todo)。
- ValidateDelete 空实现(行 361-366)。

### 9.2 AliyunManagedMachinePool(api/infrastructure/v1beta2/aliyunmanagedmachinepool_webhook.go)
- Defaulting(行 75-99):SystemDiskCategory 默认 `cloud_efficiency`、Size 默认 120、PerformanceLevel 默认 PL1(行 53-72 定义了枚举与范围);ImageType 默认 `AliyunLinux3`。
- ValidateCreate(行 107-232):AckNodePoolName 必填 + 名称校验;vswitch 数量 1-8;vswitch id/name+cidr 互斥;InstanceTypes 必填;系统盘枚举/范围(40-500GB);数据盘枚举/范围(40-32768);**KeyName 与 Password 必须且只能二选一**(行 212-221)。
- ValidateUpdate(行 235-380):SecurityGroupIds(集合比较)、ImageType immutable;系统盘/数据盘重新校验;vswitch 同名成员 cidr/zoneId immutable(行 338-347);KeyName/Password 二选一(行 360-369,注释:keyName 改 password 时 apply 会报错)。
- ValidateDelete 空实现。

### 9.3 注册
- 仅 2 个类型有 webhook(PROJECT:30-46 只有这两个声明了 webhooks);cmd/main.go:264-275 用 `ENABLE_WEBHOOKS != "false"` 控制开关。config/webhook/manifests.yaml 定义了 4 个 admission webhook(2 mutating + 2 validating,failurePolicy=Fail)。证书走 cert-manager(config/default/kustomization.yaml:26-30、certmanager/)。cs 中间 CR 的校验靠 CEL(XValidation)而非 webhook。

---

## 10. 测试

- `test/e2e/e2e_test.go` + `e2e_suite_test.go`:ginkgo v2 + gomega,Ordered 结构。内容**只有 kubebuilder 默认冒烟**:装 prometheus-operator 与 cert-manager → 建 ns → `make docker-build` 构建镜像 → kind load → `make install` 装 CRD → `make deploy` → 轮询确认 controller pod Running(e2e_test.go:57-119)。**没有真实创建 ACK 集群、扩容、删除的端到端场景**,也没有 mock 云 API 的集成测试。
- 单元测试:`internal/controller/controlplane/aliyunmanagedcontrolplane_controller_test.go`、`infrastructure/*_test.go`(envtest suite,见 suite_test.go)、`api/.../webhook_test.go`、`webhook_suite_test.go`。
- `test/utils/utils.go`:kind 相关 helper(kind 镜像 load、kubectl 执行)、prometheus/cert-manager 安装。
- Makefile:test 目标走 envtest(行 70),无专门的 e2e/release 流水线目标(前面 grep 到的 target 只有 manifests/generate/test/lint/docker-build/docker-buildx/install/deploy)。

---

## 11. clusterctl 集成

- **不支持 clusterctl init**:config/ 下没有 `metadata.yaml`,也没有 `components.yaml`(find 确认),缺少 CAPI provider 打包所需的 metadata 与 components 清单;README 也只教 `kubectl apply -f config/crd/bases/` + `make deploy`(README.md:31-34)。
- 兼容性声明仅限运行时契约:因为 CRD 遵守 CAPI 合约(AliyunManagedCluster.spec.controlPlaneEndpoint、status.ready;AliyunManagedControlPlane 的 conditions/finalizer;MachinePool 关联),`clusterctl describe cluster` 与 `clusterctl get kubeconfig` 可用(README.md:70-91)。
- config/crd/patches/ 下是 kubebuilder 的常规 patch(cainjection、webhook、capi_in_* 标注),不构成 clusterctl 打包。

---

## 12. 局限性分析与对 CCE 的启示

### 12.1 能力边界(基于代码事实)
1. **只覆盖"创建/删除 + 有限更新"生命周期**:可更新字段仅 Name/Version/DeletionProtection/Addons/Tags/Logging(machinepool 侧为节点池部分字段);无升级编排(Version 只是透传给 ACK,升级由 ACK 侧完成,无 KCP 式滚动、无升级期间条件管理)。
2. **只支持托管模式**:无 AliyunMachine/AliyunMachinePool,无自管节点、无 kubeadm 引导;节点接入完全依赖 ACK 托管节点池。
3. **节点池能力子集**:DesiredSize 被弃用,靠 `ScalingConfig{max=min}` 表达固定数量(controller 行 304-313)→ **无弹性伸缩/autoscaler 集成**;spot、付费类型、包年包月等大量 schema 字段未暴露。
4. **网络能力窄**:只管理 VPC/VSwitch(自建或复用);无安全组规则管理、无 ELB/SLB 管理(仅回读 ID)、无 NAT/SNAT 管理(仅 bool 透传)、无路由表/对等连接/专线;Terway 需要的 podVswitchIds 是 todo(controlplane types.go:48 注释)。
5. **架构负担**:运行时依赖 terraform + alicloud provider 二进制打进镜像(Dockerfile:31-34);upjet 中间 CR 是 cluster-scoped,ownerRef 无 namespace 导致事件映射必须手写 Watch+索引器(controller 注释);10 分钟 drift 轮询。
6. **凭据模型简单**:进程级全局 AK/SK 单例(clients/aliyun-sdk.go:31),不支持多租户/多凭据;启动硬依赖一个 Secret,无 STS/RAM Role 注入。
7. **未完成点(todo 遍布)**:FailureReason/Message 规范化(todo)、NatGatewayID 恒为空(controller 行 434)、KMSEncryptedPassword 与 KeyName 冲突未解决(controller 行 301)、ImageType 更新限制、部分 webhook 校验被注释。
8. **工程化缺失**:无 GitHub Release、无 CI 真实 e2e、无 clusterctl init、stars 4、无维护承诺迹象(2025-07 后无提交)。

### 12.2 对 CCE Provider 的直接借鉴清单
1. **CRD 命名与合约(照抄思路)**:`HuaweiManagedCluster`(InfraCluster,spec.controlPlaneEndpoint 必须放 spec 供 cluster-api 读取,aliyunmanagedcluster_types.go:33-36)/ `HuaweiManagedControlPlane`(ControlPlane,status 含 clusterId、endpoint、conditions,回写 `Spec.ControlPlaneEndpoint`)/ `HuaweiManagedMachinePool`(InfraMachinePool,status.Replicas 供 MachinePool 对齐)。API 版本直接用 v1beta2 对齐 CAPI 1.7。
2. **Reconcile 编排顺序(照抄)**:ProviderConfig/凭据 → VPC → VSwitch → 集群 → kubeconfig → 等控制面 ready 再建节点池;MachinePool 未 ready 时打 `WaitingForAliyunManagedControlPlane` 条件并静默返回(controller 行 145-164)。
3. **幂等与删除(照抄)**:以固定命名 `Get` 判存在→创建,否则只更新白名单字段;finalizer 删除顺序"集群→vswitch→vpc→自身",删除卡死用 DeleteTimeout 兜底强制清 finalizer(vpc_controller.go:46-71)——**CCE 侧建议直接复用这个"超时强制清理"设计**。
4. **kubeconfig 生成(照抄)**:用 CCE 下发的 CA/证书拼 `api.Config`,`kubeconfig.GenerateSecretWithOwner` 建 `<cluster>-kubeconfig` Secret,用户 `<cluster>-capi-admin`,即可免费获得 `clusterctl get kubeconfig`/describe 兼容(controller_kubeconfig.go:83-136)。
5. **providerID 约定(借鉴)**:`region.instance-id` 并主动查询节点列表回填 `Spec.ProviderIDList`;CCE 对应 `region.node-id` 或 CCE 节点 UUID,需查 CCE 节点列表 API(对应 ESS DescribeScalingInstances 的位置,controller 行 491-518)。
6. **Webhook 校验清单(直接移植)**:vpc/vswitch 的 id/name 互斥、四象限联合校验、vswitch 数量 1-5/1-8、不可变字段白名单、keyName/password 二选一、CIDR 校验、tag 前缀限制——这些与云厂商无关,可直接改写为 CCE 语义。
7. **状态搬运模式(借鉴)**:把云侧 conditions 映射为 CAPI conditions 并区分"失败条件"与"就绪条件"(setStatus 里 crossplane conditions 不影响 Ready 的写法,controller 行 354-402)。
8. **网络双模式(借鉴)**:VPC/VSwitch 结构用 `ID`(已有)与 `Name+CIDR+Zone`(自建)表达两种模式,`GetIDList` 在未就绪时返回错误触发重试(common/vswitch.go:65-83)。
9. **镜像瘦身(反面教训)**:不要内嵌 terraform;直接用华为云 CCE Go SDK 实现集群/节点池/网络 API,可完全省掉 upjet 中间层与 cluster-scoped CR 的映射复杂度(§3.5 的手写 Watch 全部消失)。
10. **feature gate 结构(借鉴)**:按 crossplane 模式把能力开关集中管理,但建议增加 CAPI 风格 gate(如 MachinePool/autoscaler/upgrade)。

### 12.3 能力差距 / 我们可以做得更好的点
1. **升级编排**:补齐集群/节点池版本升级的 reconcile 流程与 conditions(upgrade 中/阻塞原因),ACK 项目仅透传 Version。
2. **弹性伸缩**:支持 desired≠min/max 的区间语义并对接 cluster-autoscaler/CCE autoscaler,而不是固定 min=max。
3. **完整网络生命周期**:VPC/子网/安全组及规则/ELB/NAT/路由表统一管理,支持多 AZ 与已有资源复用、删除时按依赖逆序。
4. **多凭据/多租户**:把凭据从进程级全局单例改为 per-Cluster/per-ProviderConfig 注入,支持 STS/委托/动态凭证刷新。
5. **clusterctl 一等公民**:提供 `metadata.yaml` + `components.yaml`、正式 release 流程(GitHub Actions 自动打 tag 出 components)、`clusterctl init --infrastructure huaweicloud`。
6. **真实 e2e**:用 mock/沙箱 CCE API 做"创建→就绪→扩容→缩容→删除"全链路测试,并做 CAPI conformance;ACK 项目 e2e 仅冒烟。
7. **状态机规范**:统一 FailureReason/Message、补齐事件(Event)与 metrics,避免 todo 字段(如 NatGatewayID 恒空)。
8. **双模式支持**:除托管节点池外,考虑 CCE 自管节点池/裸金属节点(对应 CAPI Machine 模式)以覆盖更广场景,这是 ACK 项目完全没有的。
9. **可观测性**:暴露云 API 调用延迟/错误指标与漂移检测;ACK 项目靠 10 分钟轮询,可改为事件驱动+按需查询。

---

## 附:关键文件索引(相对 /tmp/capal)
- 类型:`api/infrastructure/v1beta2/aliyunmanagedcluster_types.go`、`api/controlplane/v1beta2/aliyunmanagedcontrolplane_types.go`、`api/infrastructure/v1beta2/aliyunmanagedmachinepool_types.go`、`api/common/vpc.go`、`api/common/vswitch.go`、`api/cs/v1alpha1/zz_managedkubernetes_types.go`、`api/cs/v1alpha1/zz_kubernetesnodepool_types.go`、`api/alibabacloud/v1beta1/types.go`
- 控制器:`internal/controller/controlplane/aliyunmanagedcontrolplane_controller.go`(+_kubeconfig.go、_providerconfig.go、_indexer.go)、`internal/controller/infrastructure/aliyunmanagedcluster_controller.go`、`internal/controller/infrastructure/aliyunmanagedmachinepool_controller.go`(+_utils.go、_indexer.go)、`internal/controller/cs/vpc/vpc_controller.go`、`internal/controller/cs/vswitch/vswitch_controller.go`、`internal/controller/cs/*/zz_controller.go`、`internal/controller/providerconfig/config.go`
- 客户端/配置:`internal/clients/alibabacloud.go`、`internal/clients/aliyun-sdk.go`、`internal/config/provider.go`、`internal/config/external_name.go`、`internal/config/cs/config.go`、`internal/features/features.go`
- 入口/清单:`cmd/main.go`、`config/samples/ack-test.yaml`、`config/manager/provider_config.yaml`、`config/webhook/manifests.yaml`、`Makefile`、`Dockerfile`、`test/e2e/e2e_test.go`
