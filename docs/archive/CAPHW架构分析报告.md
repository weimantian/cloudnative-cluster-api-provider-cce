# CAPHW(cluster-api-provider-huawei)源码分析报告 —— 面向 CCE 托管集群 Provider 开发

> 本文由子代理基于 `/tmp/caphw` 浅克隆(HEAD=aa283f1,merge PR #88,module `github.com/HuaweiCloudDeveloper/cluster-api-provider-huawei`)逐文件实际阅读产出。
> 入库时修正一处:原报告"CCE 节点状态(Installed/ScalingUp/Deleting/Abnormal…)"按官方 SDK `model_node_status.go` 实际枚举更正为 **Build/Installing/Upgrading/Active/Abnormal/Deleting/Error**。

## 0. 一句话结论

该仓库是一个**早期、半成品**的 CAPI 基础设施 provider:CRD 只有 3 个类型、控制器 2 个、services 层 5 个包(VPC/子网/安全组/NAT/公网IP/ECS/ELB 全部由 provider 直管)、硬编码网段、**没有任何资源 tagging**、**没有 webhook/features/MachinePool/镜像查找**,单元测试与 e2e 都是脚手架且不触碰真实华为云。对 CCE provider 的价值在于:**华为云 SDK 客户端构建/凭证/错误分类/Scope 模式/控制器骨架**可直接复用,**所有 IaaS 直管逻辑与 kubeadm 引导逻辑**必须重写为 CCE 托管 API。

## 1. 仓库总体情况

- **README.md(仅 21 行)**:定位为 "Kubernetes-native declarative infrastructure for Huawei Cloud",在 CAPI 之上提供可选增强;K8s 版本支持见 HMIs 页。无任何功能/状态细节。
- **go.mod**:`go 1.22.0`;直接依赖:`huaweicloud-sdk-go-v3 v0.1.130`(第7行)、`sigs.k8s.io/cluster-api v1.9.3`(第17行)、`controller-runtime v0.19.3`(第18行)、`k8s.io/{api,apimachinery,client-go} v0.31.3`(第12-14行)、`github.com/pkg/errors v0.9.1`(第10行)、ginkgo v2.22.0/gomega v1.36.0。**无 feature gates 依赖**。
- **metadata.yaml**:contract `v1beta1`,release series 0.0/0.1。
- **RELEASES.md**:v0.0.1/v0.1.0 支持 K8s v1.32.0+;发布物为 `infrastructure-components.yaml`/`cluster-template.yaml`/`metadata.yaml`,镜像 `ghcr.io/huaweiclouddeveloper/cluster-api-huawei-controller:$VERSION`。
- **docs/book**:user-guide(getting-started/installation)、dev-guide(tilt/setup/testing/contributing)、architecture(空)、images/hmis.md、roadmap。**hmis.md 是关键**:发布 "Ubuntu-22.04 + Kubernetes v1.32.0" 的**预置 HMI 镜像**及各 region 的 image ID 清单(如 cn-north-4=6b201604-...),即节点引导靠"预置镜像"而非 provider 生成脚本。
- **docs/roadmap.md(中文)**:已完成 VPC/Subnet、SG、ELB、NAT、ECS+EIP、kubeadm CAPI 打通、MachineDeployment 工作节点、控制面/工作节点扩缩容、负载均衡、cloud-provider-huaweicloud 自动部署;**未完成** CNI 模板自动化部署。

## 2. CRD 类型体系(api/v1alpha1/,共 3 个类型 + 1 个模板)

1. **HuaweiCloudCluster**(`huaweicloudcluster_types.go`):
   - Spec:`NetworkSpec`(network,omitempty,第37行)、`Region`(第40行)、`ControlPlaneEndpoint clusterv1.APIEndpoint`(第44行,由 ELB 公网 IP 回填)。
   - Status:`Ready bool`(默认 false)、`Network NetworkStatus`、`Conditions clusterv1.Conditions`。
   - Finalizer:`huaweicloudcluster.infrastructure.cluster.x-k8s.io`(第25行);实现 `GetConditions/SetConditions`(第83-90行)满足 CAPI conditions 合约。
2. **HuaweiCloudMachine**(`huaweicloudmachine_types.go`):
   - Spec:`ProviderID`(第40行)、`InstanceID`(第43行)、`ImageRef *string`(第47行,镜像 ID)、`FlavorRef`(必填,MinLength 2,第52-53行)、`SSHKeyName`(第57行)、`RootVolume *Volume`(第61行)、`PublicIP *bool`(第74行)、`ElasticIPPool`(第79行)、`Subnet *HuaweiCloudResourceReference`(第89行)。
   - Status:`Ready`、`Addresses []clusterv1.MachineAddress`、`InstanceState`、`FailureMessage/FailureReason(capierrors.MachineStatusError)`、`Conditions`。
   - Finalizer:`huaweicloudmachine.infrastructure.cluster.x-k8s.io`(第28行)。
3. **HuaweiCloudMachineTemplate**(`huaweicloudmachinetemplate_types.go`):标准模板,`Template.ObjectMeta`(clusterv1.ObjectMeta)+ `Template.Spec`(HuaweiCloudMachineSpec),Status 为空。无独立控制器,由 CAPI MachineSet/MachineDeployment 克隆生成 Machine。
4. **网络类型**(`network_types.go`):`NetworkSpec{VPC,Subnets}`;`VPCSpec{Id,Name,Cidr}`;`SubnetSpec{Id,ResourceID,Cidr,GatewayIp,VpcId,NeutronNetworkId,NeutronSubnetId,IPv6CidrBlock,AvailabilityZone,IsPublic,IsIPv6}`(第47-91行,`GetResourceID/GetNeutronSubnetID` 第95-105行);`Subnets.FindByID/FilterPrivate`(第115-133行);`IngressRule/IngressRules`(协议枚举 -1/4/tcp/udp/icmp/58/50,`Equals/Difference` 第199-278行);`SecurityGroupRole`(node/controlplane/apiserver-lb/lb 等,第295-309行);`SecurityGroup{SecurityGroupRules}`(第372-383行);`LoadBalancer{Id,Name,Pools,Listeners}`(第358-370行);`NetworkStatus{SecurityGroups map[role]SG, ELB, NatGatewaysIPs}`(第391-399行)。
5. **通用类型**(`types.go`):`InstanceState` 枚举与状态集合 `InstanceRunningStates/InstanceOperationalStates/InstanceKnownStates`(第24-68行,语义沿用 AWS provider);`Volume{Size,Type(VolumeTypeGPSSD),IOPS,Throughput}`(第78-104行);`Instance`(SDK 结果的 provider 侧表示,第107-155行)。
6. **conditions 常量**(`conditions_consts.go`):`InstanceReadyCondition`、`VpcReadyCondition`、`SubnetsReadyCondition`、`ClusterSecurityGroupsReadyCondition`、`LoadBalancerReadyCondition`、`NatGatewaysReadyCondition` 及各 reason。
7. 版本:`infrastructure.cluster.x-k8s.io/v1alpha1`(`groupversion_info.go`),无 conversion webhook、无其它版本。

## 3. 控制器实现(internal/controller/)

两个控制器,均为标准 CAPI 骨架(fetch → owner → pause → scope → delete/normal 分流 → defer Close)。

**HuaweiCloudClusterReconciler**(`huaweicloudcluster_controller.go`):
- Reconcile(第78-133行):Get HCCluster → `util.GetOwnerCluster` → `capiannotations.IsPaused` 跳过 → `scope.NewClusterScope` → `defer clusterScope.Close()` → 有 DeletionTimestamp 走 `reconcileDelete`,否则 `reconcileNormal`。
- reconcileNormal(第143-183行):`controllerutil.AddFinalizer` + PatchObject → `network.NewService().ReconcileNetwork()` → `securitygroup.NewService(roles).ReconcileSecurityGroups()` → `elb.NewService().ReconcileLoadbalancers()`;任一失败 `return reconcile.Result{RequeueAfter: 30*time.Second}` + `errors.Wrap`。全部成功后 `Status.Ready = true`。
- reconcileDelete(第185-222行):删除顺序 **ELB → 安全组 → 网络(NAT→子网→VPC)**;`RemoveFinalizer` 结尾。每个 service 的删除内部逐资源处理 404。
- 角色集合:`defaultHCSecurityGroupRoles = [apiserver-lb, lb, controlplane, node]`(第42-47行)。
- SetupWithManager:`For(HuaweiCloudCluster).Named("huaweicloudcluster")`,无 Owns/watch 子资源。
- 错误处理:统一 `github.com/pkg/errors`(Wrap/Errorf)+ controller-runtime 的 `apierrors.IsNotFound`;requeue 用 `RequeueAfter` 而非 `Requeue: true`。

**HuaweiCloudMachineReconciler**(`huaweicloudmachine_controller.go`):
- Reconcile(第81-152行):Get HCMachine → `util.GetOwnerMachine` → `util.GetClusterFromMetadata` → `getInfraCluster`(按 `cluster.Spec.InfrastructureRef.Name` 取 HCCluster 并建 ClusterScope,第154-182行)→ `scope.NewMachineScope` → 分流。
- reconcileNormal(第294-410行):
  1. `ecs.NewService`;`findInstance`(第187-215行):ProviderID 非空则 `InstanceIfExists(pid)`(按 ID ShowServer);ProviderID 为空时按 tag 查询的代码被**注释掉**(第197-202行),返回 nil。
  2. AddFinalizer;instance==nil → 标记 `InstanceProvisionStarted` → `resolveUserData`(读 bootstrap Secret,第440-447行)→ `CreateInstance`;失败标记 `InstanceProvisionFailed`。
  3. `SetProviderID/SetInstanceID`(第348-349行,`huaweicloud://<instanceID>` 格式)。
  4. **实例状态机**(第360-381行):Pending→notReady+requeue 30s;Stopping/Stopped→notReady+`InstanceStopped`;Running→Ready;ShuttingDown/Terminated→notReady+`InstanceTerminated`(+FailureReason=UpdateMachineError);未知状态→FailureReason/Message。Terminated 额外置 failure(第383-386行)。
  5. `InstanceIsInKnownState && IsControlPlane` → `AttachInstanceToElb`(第389-394行);`InstanceIsOperational` → `reconcileOperationalState`(第412-425行)= SetAddresses + ensureSecurityGroups(`GetCoreSecurityGroups` 校验 node/controlplane SG 存在)。
- reconcileDelete(第225-292行):findInstance;nil→直接 RemoveFinalizer;控制面先 `DetachInstanceFromElb`;状态机:ShuttingDown→requeue 1min;Terminated→RemoveFinalizer;其它→`MarkFalse(InstanceReady, Deleting)` + `TerminateInstance`。
- 常量 `DefaultReconcilerRequeue = 30s`(第52行)。

## 4. Scope 模式(pkg/scope/)

- **ClusterScope**(`cluster.go`):字段 `client/patchHelper(patch.NewHelper)/Logger/Cluster/HCCluster/Credentials(*basic.Credentials)`;`NewClusterScope` 参数校验 + 建 patch helper(第55-82行);`Close()=PatchObject()`(第84-87行);访问器 `VPC()/Subnets()/SetSubnets()/Region()/SecurityGroups()/SetSecurityGroups()/ELB()/SetELB()/SetNatGatewaysIPs()/Network()`;`PatchObject`(第150-174行)用 `conditions.SetSummary` + `patch.WithOwnedConditions`(VpcReady/SubnetsReady/ClusterSecurityGroupsReady/NatGatewaysReady/Ready)。**有 4 个 panic stub**:`SSHKeyName/ImageLookupFormat/ImageLookupOrg/ImageLookupBaseOS`(第180-194行,镜像查找未实现)。
- **MachineScope**(`machine.go`):`GetInstanceID`(解析 ProviderID)、`SetProviderID`(GenerateProviderID)、`SetReady/SetNotReady/SetFailureReason/SetFailureMessage/SetAddresses/SetInstanceState`;`GetRawBootstrapDataWithFormat`(第191-208行)读 `Machine.Spec.Bootstrap.DataSecretName` 指向的 Secret,取 `data["value"]` 与 `data["format"]`;**这是 userdata 的唯一来源**;`InstanceIsRunning/Operational/InKnownState`(第256-271行);`IsControlPlane/Role`。
- **ECSScope 接口**(`ecs.go`):`basic.ClusterScoper + VPC/Subnets/Network/SecurityGroups/SSHKeyName/ImageLookup*`,供 ECS service 依赖注入。
- **`pkg/basic/interfaces.go`**:不是 SDK 封装!只有 `ClusterScoper` 接口(第10-40行),定义 services 层对 scope 的消费面。
- **client 构建**(`clients.go`):`NewECSClient`(第25-41行)= `ecsRegion.SafeValueOf(scope.Region())` + `EcsClientBuilder().WithRegion(region).WithCredential(scope.Credential()).SafeBuild()`。**region→endpoint 由 SDK region 包解析,无自定义 endpoint 支持**。
- **ProviderID**(`provider.go`):从 CAPI 旧版 noderefutil 拷贝;`ProviderIDPrefix = "huaweicloud://"`(第93行),`GenerateProviderID`(第98-100行)。

## 5. services 层(pkg/services/)

统一模式:每个包一个 `Service` 结构(持有 scope + 各 SDK client + errHandler),`NewService(scope)` 内构建 client,业务逻辑在独立文件。

**network**(`pkg/services/network/`):
- `service.go`:Service 持有 `vpcClient(vpc/v2)/eipClient(eip/v2)/natClient(nat/v2)` + `VPCErrorHandler`;三个 client 均 `WithRegion(SafeValueOf)+WithCredential+WithHttpConfig(config.DefaultHttpConfig())` 构建(第24-75行)。
- `network.go`:`ReconcileNetwork` 顺序 VPC→Subnets→(TODO 路由表)→NAT,每步失败 `conditions.MarkFalse` 对应 condition;`DeleteNetwork` 逆序 NAT→子网→VPC,全程 Deleting/Deleted 条件。
- `vpc.go`:`reconcileVPC`(第12-55行)——**若 `scope.VPC().Id` 非空直接跳过**;否则 `CreateVpc`,**CIDR 硬编码 `192.168.0.0/16`、name `vpc-caphw`**(第20-21行),回写 spec;`deleteVPC` 404 容忍。
- `subnet.go`:`reconcileSubnets`(第10-67行)——ListSubnets(按 VPC);无则创建,**硬编码 `192.168.1.0/24`、gateway `192.168.1.1`、name `subnet-caphw`**(第28-31行);`SetSubnets` 写回含 NeutronNetworkId/NeutronSubnetId;`FindSubnet` 用 ShowSubnet。
- `eip.go`:EIP 类型 `5_bgp`、带宽 traffic/PRE/100M、name `eip-<rand4>`(第13-33行);`releasePublicIp`。
- `natgateways.go`:每个子网一个 NAT 网关(spec=1,name `nat-<rand4>`)+ allocatePublicIp + SNAT 规则(第102-153行);`SetNatGatewaysIPs` 记录 SNAT 浮动 IP;删除时先删 SNAT/DNAT 规则并释放 EIP 再删 NAT(第155-204行)。

**securitygroup**(`pkg/services/securitygroup/`):
- `securitygroups.go`:`ReconcileSecurityGroups`(第13-120行)——**单一 SG `sg-caphw`**,存在性按 name 匹配;新建后删除默认不安全规则(ingress 0.0.0.0/0,第122-138行);只创建一条规则 **TCP 6443 from 0.0.0.0/0**(第59-67行);把**同一个 SG 映射到所有角色**(第105-111行,无 per-role 规则)。`DeleteSecurityGroups` 全量删规则再删 SG。

**ecs**(`pkg/services/ecs/`):
- `service.go`:Service 持有 `ECSClient(ecs/v2)` + `netService` + `elbService` + `ECSErrorHandler`;`NewService` 同时构建 ECS/network/elb 三个服务(第41-66行)。
- `instance.go`:
  - `CreateInstance`(第136-206行):组装 `infrav1.Instance`;RootVolume 缺省 **15 GiB**;`findSubnet`(第39-94行):machine 指定 subnet ID 则 ShowSubnet(校验 failureDomain AZ、PublicIP 子网归属),否则取 `Subnets().FilterPrivate()[0]`;**控制面机器会 `appendCloudConfig` 追加 cloud-provider 配置**(第172-184行);userdata **base64 编码**后传入;`GetCoreSecurityGroups`(第96-117行):node SG + (control-plane 追加 controlplane SG)。
  - `runInstance`(第208-273行):`CreateServers`,`XClientToken=uuid`;`PrePaidServer{Name: generateInstanceName("caphw-ecs")(8 随机字符,第119-134行), ImageRef, FlavorRef, Vpcid, UserData, Nics[0].SubnetId}`;`PublicIPOnLaunch` 时挂 EIP(5_bgp、5M、PER、DeleteOnTermination,第227-239行);RootVolume GPSSD;创建后 **`CheckJob` 轮询 ShowJob**(2 分钟超时、1s 间隔,第372-402行),再 ShowServer。
  - `SDKToInstance`(第294-364行):**ECS status→InstanceState 映射**(ACTIVE→running、BUILD/REBUILD→pending、SHUTOFF→stopped、REBOOT/HARD_REBOOT→running、DELETED/SOFT_DELETED→terminated、默认小写原样);从 Addresses 提取 FIXED(内网)/FLOATING(公网)IP 生成 `MachineAddress`。
  - `TerminateInstance`(第275-292行):`DeleteServers{DeletePublicip:true, DeleteVolume:true}`。
  - `InstanceIfExists`(第404-421行):ShowServer,`errHandler.IsNotFound`→`ErrInstanceNotFoundByID`;其它错误→`ErrShowInstance`(`errors.go`)。
- `cloudconfig.go`(第10-92行):`CloudConfig{Region,AccessKey,SecretKey,VPCID,NeutronSubnetID}`;`appendCloudConfig` 解析现有 cloud-init YAML(`write_files`+`runcmd`),追加写入 `/etc/kubernetes/cloud-config`(内容为 `[Global] region/access-key/secret-key` + `[Vpc] id/subnet-id`,第34行)与 runcmd(sleep 10;export KUBECONFIG;`kubectl -n kube-system create secret generic cloud-config`;rm,第52-58行),文件头 `## template: jinja\n#cloud-config\n`(第65行)。

**elb**(`pkg/services/elb/`):
- `loadbalancer.go`:`ReconcileLoadbalancers`(第140-201行):LB 名 `<cluster>-elb`,存在性按 name 查询;创建时带公网 IP(5_bgp/traffic/100M)、`VipSubnetCidrId=subnets[0].NeutronSubnetId`、AZ 列表;创建 **TCP listener 端口 6443**(第47-69行)→ **pool**(ROUND_ROBIN、TCP、type=instance,第80-101行);`SetELB` 并 **回填 `Spec.ControlPlaneEndpoint = LB 公网 IP:6443`**(第190-193行)——这是自建集群 API Server 的访问入口。
- `CreateMember/DeleteMember`(第398-475行):按 pool→listener 遍历,对 MachineInternalIP 批量加入 member(memberExists 去重);删除按 `InstanceId` 匹配。控制面机器由 machine 控制器在创建/删除时调用。
- `DeleteLoadbalancers`(第204-278行):pool→listener→LB→公网 IP 释放。

## 6. 华为云 SDK 用法

- **SDK**:`github.com/huaweicloud/huaweicloud-sdk-go-v3 v0.1.130`。涉及服务:`ecs/v2`、`vpc/v2`、`eip/v2`、`nat/v2`、`elb/v3`(注意 ELB 用 v3,其余 v2)。
- **client 初始化**:全部走 builder 模式——`XxxClientBuilder().WithRegion(region).WithCredential(cred).SafeBuild()`;region 一律 `xxxReg.SafeValueOf(scope.Region())` 由 SDK region 常量解析 endpoint,`scope.Region()` 来自 `HCCluster.Spec.Region`。**不支持自定义 endpoint/私有云**。
- **credentials**:`core/auth/basic` 包的 `basic.NewCredentialsBuilder().WithAk(ak).WithSk(sk).SafeBuild()`(main.go 第159-162行),进程级单一 `*basic.Credentials`,注入两个 reconciler(第168/176行),经 scope 传给所有 client(`WithCredential(scope.Credentials)`);scope 的 `Credential()` 返回 `auth.ICredential`(cluster.go 第196-198行)。
- **`pkg/basic` 澄清**:是 provider 本地包,只含 `ClusterScoper` 接口(见第4节),**不是** SDK client 封装;真正的 SDK 凭证类型在同名 `core/auth/basic` 下,命名易混淆。
- **pkg/logger**(`logger.go`):logr 的薄封装(Info/Debug/Warn/Trace/Error、WithValues/WithName、callStackHelper),定义完整但**控制器并未使用**(控制器直接 controller-runtime logr),属于未接入的工具。
- **pkg/errors**(`errors.go` + vpcerrs/ecserrs/elberrs.go):`BaseErrorHandler` 从 `*sdkerr.ServiceResponseError` 提取 `StatusCode/ErrorCode`(errors.go 第17-28行);各服务错误码常量:**VPC.0202**(VPC 不存在,vpcerrs.go 第8行)、**Ecs.0114**(ECS 不存在,ecserrs.go 第8行)、**APIGW.0101**(ELB 不存在)/**ELB.8907**(已存在冲突,elberrs.go 第8-9行);`IsNotFound/IsExists` 组合 statusCode+errorCode 判定。

## 7. 凭证处理

- **启动强制检查环境变量** `CLOUD_SDK_AK/CLOUD_SDK_SK`,缺失即退出(main.go 第147-158行,并打印明文 AK 到日志——安全反例)。
- 部署侧:`config/manager/credentials.yaml` 定义 Secret `bootstrap-credentials`(accesskey/secretkey,kustomize `${CLOUD_SDK_AK}` 替换);`config/default/manager_credentials_patch.yaml` 通过 `secretKeyRef` 注入容器 env `CLOUD_SDK_AK/CLOUD_SDK_SK`(第12-20行)。
- **全局单一凭证**:所有集群共用同一对 AK/SK,无 per-cluster Secret、无 IAM/STS/委托。
- 控制面机器的 userdata 里**内嵌明文 AK/SK**(cloudconfig.go 第34行),再通过 k8s Secret 注入 CCM——CCE 场景应避免。

## 8. 节点引导(kubeadm + 预置镜像)

- **userdata 完全由 CAPI 生态提供**:KubeadmControlPlane/KubeadmConfig 生成 cloud-init(init/join),存 Secret(`data["value"]`+`data["format"]`),CAPHW 只负责读取并透传(scope/machine.go 第191-208行;controller 第440-447行)。
- **控制面额外注入** cloud-provider-huaweicloud 配置(cloudconfig.go),使 CCM 能拿到 region/AK/SK/VPC/subnet。
- **预置镜像(HMI)**:docs/book/src/images/hmis.md 列出各 region 的 "Ubuntu-22.04 + K8s v1.32.0" 镜像 ID;machine 的 `ImageRef` 直接引用;`ImageLookup*` 全部 panic(未实现镜像查找)。
- 仓库内**没有独立 cloud-init 脚本文件**,只有 cloudconfig.go 的注入逻辑;镜像内已预装 kubeadm 所需组件。

## 9. 网络规划细节

- **网段**:VPC `192.168.0.0/16`、子网 `192.168.1.0/24`(网关 .1),**全部硬编码**,无视 spec 传入的 CIDR;单子网模型。`SubnetSpec` 里虽有 IPv6/AZ/IsPublic 字段但 reconcile 不用。
- **安全组**:单 SG `sg-caphw`,仅放行 **TCP 6443/0.0.0.0/0**(apiserver 入口),删除华为云默认的 0.0.0.0/0 全放行规则;所有角色共用同一 SG——**没有** kubelet 10250、etcd、集群内互访等细分规则(粗粒度)。
- **公网访问**:三条路径——① 节点出网走 **NAT 网关(每子网一个)+ SNAT + 独立 EIP**;② 机器 `publicIP: true` 时创建时挂 EIP(5_bgp/5M,随实例删除);③ ELB 自带公网 IP(100M)。
- **控制平面负载均衡**:华为云 **ELB v3**,TCP:6443 listener + ROUND_ROBIN pool(type=instance),后端为控制面机器内网 IP;`ControlPlaneEndpoint` 指向 ELB 公网 IP(见第5节 elb)。
- **资源关联方式**:**无 tagging**——VPC/子网/SG 靠 name(`vpc-caphw`/`subnet-caphw`/`sg-caphw`)或 VPC 内列表匹配,ELB 靠 `<cluster>-elb` 名称,ECS 靠 ProviderID。这意味着"一 region 一集群"假设,且 `findInstance` 的 tag 查找被注释掉(instance.go 未启用、controller 第197-202行)。
- 路由表 reconcile 是 TODO(network.go 第35行)。

## 10. 特性与能力边界

- **无 features 包/feature gates**;无 MachinePool 类型(仅支持 MachineSet/MachineDeployment 驱动的 Machine)。
- **Machine 操作**:创建(create+CheckJob 轮询)、删除(terminate+公网IP/磁盘随删)、查询/状态同步;无 start/stop/reboot 方法;**无 provider 侧"替换"逻辑**(滚动替换交给 KCP/MachineDeployment 建新 Machine)。
- **集群升级**:provider 无升级逻辑,依赖 KCP 滚动(改 `version`/`imageRef` → 新 Machine)。
- **webhook**:main.go 里 scaffold 了 webhook server(第97-99行)但**注册了 0 个 webhook**;无 ValidateCreate/Update、无 Default();config/webhook、certmanager 在 kustomization 里全部注释;CRD 无 conversion(单版本 v1alpha1)。
- **其它未实现**:SSHKeyName(panic)、数据盘(注释)、镜像查找(panic)、IPv6(字段存在未用)、路由表(TODO)、CNI 模板(roadmap 未勾选)。
- **已支持**:paused annotation 跳过(cluster 控制器第105-108行)、conditions 合约、finalizer 清理、leader election(LeaderElectionID `3b0bc392.cluster.x-k8s.io`,main.go 第129行)。

## 11. 测试(test/)

- **单元测试**(`internal/controller/*_test.go`,各 84/87 行):envtest(CRD from config/crd/bases,K8s 1.31.0 资产)建 CR + 调一次 Reconcile,**无任何真实断言**(reconcile 因无 owner 提前返回 nil,测试恒过;FlavorRef 用 "todo")。services/scope 层**零单测**。
- **e2e**(`test/e2e/`):kubebuilder 脚手架——kind 集群、make generate/manifests/docker-build、部署 manager,检查 pod Running;metrics 用例被 Skip;AK/SK 用假值 `ak_test/sk_test`(e2e_test.go 第63-64行)。**完全不触达华为云,不创建工作负载集群**。
- CI(`.github/workflows/`):test.yml(make test)、test-e2e.yml(kind + make test-e2e)、lint.yml、release.yaml(goreleaser)。

## 12. 可复用资产清单(华为云 API 适配角度)

**可直接复用/借鉴(CCE provider 照搬模式):**
1. **SDK client 构建模式**:`pkg/scope/clients.go` 的 `SafeValueOf(region)+Builder().WithRegion().WithCredential().SafeBuild()` 范式,替换为 `services/cce/v3`(及 vpc/ecs 查询用 client)。
2. **凭证读取与注入**:main.go 第147-166行 env→`basic.NewCredentialsBuilder`;config/manager/credentials.yaml + manager_credentials_patch.yaml 的 Secret→env 注入链路(CCE 建议升级为 per-cluster Secret,但骨架照搬)。
3. **错误分类**:`pkg/errors` 的 `BaseErrorHandler`(sdkerr.ServiceResponseError)+ 各服务错误码常量 + `IsNotFound/IsExists` 模式——新增 CCE 错误码(如 CCE.01410001 集群不存在等)即可复用。
4. **Scope 模式**:`pkg/scope/cluster.go+machine.go` 的 patch helper、`Close()=PatchObject()`、`conditions.SetSummary + WithOwnedConditions`、ProviderID 解析(`pkg/scope/provider.go`,CCE 节点同样需要 `huaweicloud://<instanceID>` 格式)。
5. **控制器骨架**:finalizer 管理、`RequeueAfter` 而非 `Requeue`、reconcileNormal/reconcileDelete 分流、删除逆序、paused 检查、machine 状态机(把 ECS status 映射换成 CCE 节点/集群状态映射)。
6. **conditions 常量体系**:`conditions_consts.go` 的分组命名模式。
7. **services 分层**:每服务 `Service{scope, client, errHandler}` + `NewService(scope)` + 独立业务文件,利于替换实现。
8. **userdata 透传**:`GetRawBootstrapDataWithFormat`(Secret dataSecretName)模式,若 CCE 节点仍需自定义脚本可用。
9. **HMIs/预置镜像思路**:CCE 节点池不适用,但"按 region 维护镜像清单"的文档化方式可参考。
10. **e2e/test utils**:kind 部署链路可参考;但必须新增真实 CCE 冒烟测试。

**必须重写(自建 vs 托管差异):**
1. **CRD 全量重设计**:`HuaweiCloudCluster`→`CceCluster`(spec 应为:region、**VPC/子网引用**(CCE 不创建网络,只引用已存在 VPC/subnet)、CCE 集群参数:flavor/kubernetesVersion/containerNetwork/serviceNetwork/authentication(或引用已有 kubeconfig Secret)/计费);`HuaweiCloudMachine`→`CceMachine`(节点池/节点:flavor、磁盘、节点数、sshKey、az);模板类型同步。
2. **network 服务删除**:VPC/子网/SG/NAT/EIP 的创建删除逻辑(vpc.go/subnet.go/eip.go/natgateways.go/securitygroups.go)**整包不适用**——CCE 只消费网络,不管理网络;最多保留"查询/校验 VPC 子网存在性与 CIDR"的只读逻辑。
3. **ECS 实例管理重写**:`ecs/instance.go` 的 CreateServers/DeleteServers/CheckJob/状态映射 → CCE `CreateCluster/DeleteCluster/ListClusters/ShowCluster` + 节点池 `CreateNodePool/DeleteNodePool/ListNodes`;节点状态机用 CCE 状态(Build/Installing/Upgrading/Active/Abnormal/Deleting/Error,官方 SDK model_node_status.go)。
4. **节点引导**:kubeadm userdata + cloud-config 注入(cloudconfig.go)**不再需要**——CCE 托管 kubelet/控制面;CCE 集群 kubeconfig 由 CCE 签发,provider 应将其写入 Secret 供 CAPI/KCP 消费。
5. **控制平面与 ELB**:自建的控制面 ELB 创建/回填 ControlPlaneEndpoint 逻辑重写——CCE 集群自带 API Server(创建集群时指定 EIP/ELB 或内网),provider 从 `ShowCluster` 响应取 endpoint。
6. **凭证模型**:进程级单一 AK/SK → 建议 per-cluster Secret(或至少保留 env 兜底);避免把 AK/SK 写进 userdata(CCE 不需要 CCM 明文注入,若仍需 CCM 用 IRSA/委托方式)。
7. **能力边界重构**:无 MachinePool → CCE 节点池天然对应 MachinePool(需实现 MachinePool 合约或复用节点池式 Machine);升级:CCE 集群可原地升级 Kubernetes 版本,provider 应实现 cluster 升级协调而非依赖 KCP 重建。
8. **测试**:现有单测/e2e 无价值,需重写为 CCE mock(或 httptest 代理 SDK) + 真实 CCE 冒烟。

---

(以上报告约 500 行中文,含全部关键文件路径与行号;仓库根 `/tmp/caphw`。)
