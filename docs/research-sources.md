# capi-cce — 调研依据与事实清单(带来源)

> 本文档是架构设计文档与需求设计文档的"事实底座"。所有设计结论都必须能回溯到本文档列出的真实来源;
> 凡是无法从真实来源确认、需要对接华为云 CCE 实测/咨询的条目,一律标注 **[需验证]**。

---

## 0. 背景与目标(来自用户既有调研)

- 用户目标:开发 `capi-cce` —— 一个管理**华为云 CCE 托管集群**的 Cluster API 基础设施 Provider,
  对标 `CAPI + AWS EKS 托管模式`(CAPA 的 EKS 模式)。
- 已知事实(用户在聊天中确认):华为云官方现有 `cluster-api-provider-huawei`(CAPHW)
  只支持在 ECS 上**自建** Kubernetes 集群,不管理 CCE 托管集群 —— 这正是本项目要填补的空缺。
- 发布策略(用户确认 + 华为云治理规范):**不合并到 kubernetes-sigs 上游**;仓库命名遵循华为云解决方案开发者套件规范(已确认:**`cloudnative-cluster-api-provider-cce`**,技术领域:云原生),许可证默认 **MIT-0**(已确认;Apache 2.0 为例外需申请);按标准套件结构(README 17 节/LICENSE/CONTRIBUTING/DCO/CODE_OF_CONDUCT/deploy/scripts/.github+CI 四件套)搭建,参考阿里云 `alibabacloud-provider-for-Cluster-API` 的发布模式。
- 集群类型取向(用户聊天中的结论):对标 EKS 托管模式,优先 **CCE Turbo**(云原生网络 2.0),
  同时兼容 CCE Standard(本项目设计为两者都支持,默认 Turbo)。

参考聊天记录(DeepSeek 分享链接,共 42 条消息):
https://chat.deepseek.com/share/nndkkwyu1gg8seypbj (内容:从 Terraform 入门 → CAPI 概念 → CAPI+EKS 托管模式调研 → CAPI+CCE 对标方案 → 开发专门 CCE Provider 的路径与注意事项清单)

---

## 1. 代码来源(本机已克隆,可直接复查)

| 仓库 | 路径 | 用途 |
|---|---|---|
| kubernetes-sigs/cluster-api-provider-aws (CAPA) | /tmp/capa | CAPI 基础设施 Provider 的行业标杆(自建 + EKS 托管双模式);分析基线 release 2.x,**依赖 sigs.k8s.io/cluster-api v1.13.4 / controller-runtime v0.24.1**,v1beta2 为 storage hub、v1beta1 为 spoke |
| AliyunContainerService/alibabacloud-provider-for-Cluster-API | /tmp/capal | 国内云厂商托管 K8s 服务的 CAPI Provider 直接范本(对接 ACK);**依赖 cluster-api v1.7.1**、crossplane/upjet v1.1.0 + Terraform 运行时 |
| huaweicloud-samples/cluster-api-provider-huawei (CAPHW) | /tmp/caphw | 华为云侧 IaaS 适配参考(SDK/凭证/网络 services),但只做自建集群;**依赖 cluster-api v1.9.3** |
| huaweicloud/huaweicloud-sdk-go-v3(稀疏克隆 services/cce) | /tmp/sdkgo | 华为云官方 Go SDK 的 CCE v3/v5 API 模型(权威 API 事实来源) |

> 版本对齐含义:三个参考仓库的 CAPI 依赖版本从 v1.7.1 到 v1.13.4 不等;本项目目标 CAPI 版本需按"发布系列 ↔ contract 版本"矩阵在 metadata.yaml 声明(详见架构文档 §3.5 与需求文档 §6.2),并随 CAPI v1beta2 合约演进选择基线(建议以 CAPA 当前线为参照,具体以立项时 CAPI 最新稳定版为准)。

## 2. 华为云 CCE 官方文档来源(已抓取,存 /tmp/*.md)

| 主题 | URL | 本地文件 |
|---|---|---|
| 创建集群 API (CreateCluster) | https://support.huaweicloud.com/api-cce/cce_02_0236.html | /tmp/doc_create_cluster.md |
| 创建节点池 API (CreateNodePool) | https://support.huaweicloud.com/api-cce/cce_02_0354.html | /tmp/doc_nodepool.md |
| 集群类型对比 (Standard/Turbo/Autopilot) | https://support.huaweicloud.com/intl/en-us/usermanual-cce/cce_10_0342.html | /tmp/doc_cluster_types.md |
| 网络模型对比 (隧道/VPC/云原生2.0) | https://support.huaweicloud.com/drawer-cce/Cluster_ContainerNetwork_mode.html | /tmp/doc_network_model.md |
| CCE 权限概述(IAM action 表) | https://support.huaweicloud.com/usermanual-cce/cce_10_0187.html | /tmp/doc_perms2.md |
| CCE 系统委托说明 | https://support.huaweicloud.com/usermanual-cce/cce_10_0556.html | /tmp/doc_agency.md |
| 设置资源配额及限制(命名空间级) | https://support.huaweicloud.com/usermanual-cce/cce_10_0287.html | /tmp/doc_quota2.md |
| 平台级配额(集群/节点等)**[需验证] 7** | 未找到公开权威页面(productdesc-cce 约束与限制页 404),需控制台/工单确认 | — |

---

## 3. 已从真实来源验证的事实(设计依据)

### 3.1 华为云 CCE API 事实(来源:官方 Go SDK 模型 + 官方 API 文档)

**集群类型(官方 SDK `ClusterSpecCategory` 枚举,文件 `services/cce/v3/model/model_cluster_spec.go`):**
- `CCE`(标准版)与 `TURBO`(Turbo 版)是 API 层面的 `category` 字段取值;`containerNetwork.mode=eni` 时默认为 Turbo。
- Autopilot 有独立 API 族(同一 SDK 内 `CreateAutopilotCluster` 等,`AutopilotClusterSpec.EnableAutopilot`),对标 AWS Fargate(Serverless)。

**集群规格 Flavor(官方文档 cce_02_0236 + SDK 注释):**
- `cce.s1.small/medium/large`(单控制节点,最大 50/200/1000 节点)、`cce.s2.small/medium/large/xlarge`(三控制节点,最大 50/200/1000/2000 节点)。

**容器网络模型(官方文档 + SDK `ContainerNetworkMode` 枚举 `model_container_network.go`):**
- `overlay_l2` 容器隧道网络:OVS overlay,与 VPC 叠加,有性能损耗;创建后网段不可修改。
- `vpc-router` VPC 网络:ipvlan + 自定义 VPC 路由的 underlay L2;创建后可新增网段、不可修改已有网段。
- `eni` 云原生网络 2.0:深度整合 VPC ENI,仅 Turbo 支持,ELB 可直通容器,性能无损;Turbo 默认 2000 节点、最大 20000 节点。

**创建集群前置条件(官方文档 cce_02_0236):**
- 必须先有 VPC;容器网段与服务网段需提前规划(隧道模式创建后不可改)。
- 服务网段默认 `10.247.0.0/16`;可配 `customSan`(API Server 证书 SAN)、`hostNetwork`、`publicAccess`(公网访问)、`authentication`(认证方式,含认证模式/证书)。
- **创建集群 API 的请求体中不包含节点** —— CCE 集群(控制面)与节点/节点池是分离的资源,节点通过 `CreateNodePool` / `CreateNode` / `AddNode` 等接口管理。→ 与 CAPI 的 Cluster(控制面)与 MachinePool/Machine(工作节点)分离模型天然对应。
- 集群状态 `phase`(官方文档):`Available`(可用)/ `Unavailable`(异常,需手动删除)/ `ScalingUp` / `ScalingDown` / `Creating` 等。

**节点池 API(官方文档 cce_02_0354):**
- `CreateNodePool`:仅在集群处于可用/扩容/缩容状态时可调用;关键参数:`nodeTemplate`(flavor 规格、rootVolume、dataVolumes、os、sshKey、runtime、taints≤20 条、labels)、`initialNodeCount`、`autoscaling`(enable/minNodeCount/maxNodeCount/scaleDownCooldownTime/priority)、`billingMode`(0=按需/1=包周期)、`nodeManagement`(云服务器组)。
- Turbo(≥1.21)节点池支持绑定安全组,每池最多 5 个;节点池支持 `alpha.cce/NodeImageID` 私有镜像。
- 节点池扩缩容 = `UpdateNodePool`(改 initialNodeCount/autoscaling,SDK `model_update_node_pool_request.go`)或依赖集群自动扩缩容(autoscaling)。
- 单节点管理:`CreateNode` / `DeleteNode` / `UpdateNode` / `AddNode` / `AddNodesToNodePool` / `ListNodes` / `BatchSyncNodes`。

**kubeconfig(官方 SDK `CreateKubernetesClusterCert`):**
- `POST` 下载集群证书接口,入参 `cluster_id` + `ClusterCertDuration`(证书有效期);响应为完整 kubeconfig(`kind: Config`,clusters/users/contexts/current-context,current-context 取值 `external` 公网 / `internal` 私网)。
- API Server 地址:集群状态/`ShowClusterEndpoints` 返回 `url` + `type`(public/private)。

**其他生命周期能力(SDK 客户端方法清单 `services/cce/v3/cce_client.go`):**
- 集群:CreateCluster / ShowCluster / ListClusters / UpdateCluster / DeleteCluster / AwakeCluster(唤醒)/ HibernateCluster(休眠)/ ResizeCluster(变更规格)/ UpdateClusterEip / ShowClusterEndpoints。
- 节点池伸缩:**`ScaleNodePool`(节点池伸缩,专门 API)**,另有 LockNodepoolNodeScaleDown / UnlockNodepoolNodeScaleDown(锁缩容)。
- 升级:CreateUpgradeWorkFlow / CreatePreCheck / CreatePostCheck / ShowClusterUpgradeInfo / ContinueUpgradeClusterTask(→ CCE 原生支持集群升级编排)。
- 插件(Addon):CreateAddonInstance / UpdateAddonInstance / DeleteAddonInstance / ListAddonInstances(→ 对标 CAPA 的 EKS Addons / CCE 的"插件中心")。
- 标签:BatchCreateClusterTags / BatchDeleteClusterTags(→ CAPI 对象关联识别机制)。

**IAM 权限模型(官方文档 usermanual-cce/cce_10_0187.html"CCE权限概述",已抓取 /tmp/doc_perms2.md,原文权限表):**
- 集群:`cce:cluster:list/get/create/update/delete/upgrade/start/stop/resize`(对应 POST /clusters、/operation/upgrade、/operation/awake、/operation/hibernate、/operation/resize 等)。
- **获取集群证书(下载 kubeconfig):POST /clusters/{id}/clustercert,授权项 `cce:cluster:generateClientCredential`(别名/兼容旧名 `cce:cluster:get`,官方 IAMActions 表;依赖授权项为 `-`)**,吊销证书 `cce:cluster:revokeClientCredential`。
- **IAM action 新旧命名(IAMActions 新版 = 旧版别名)**:createCluster(create)/getCluster(get)/deleteCluster(delete)/updateCluster(update)/upgrade/start/stop/resize/list、createNodePool(create)/deleteNodePool(delete)/updateNodepool(update)/getNodepool(get)/scale、createNode(create)/getNode(get)/delete/update/remove/migrate、showQuotas→`cce:quota:get`;旧文档(cce_10_0187)的 `cce:cluster:get` 等写法兼容于新版别名。
- **GetClusterQuota(`GET /cce/v1/projects/{project_id}/quota`)授权项官方表未收录,标注 [推断] `cce:quota:get`**(ShowQuotas 的授权项)。
- 节点:`cce:node:list/get/create/update/delete/remove/migrate`。
- 节点池:`cce:nodepool:list/get/create/update/delete/scale`(**伸缩节点池 POST /nodepools/{id}/operation/scale**)。
- Job:`cce:job:get/list/delete`。
- 全局权限说明:企业项目授权下创建节点需额外 `evs:quotas:get`、`evs:types:get` 全局权限(官方文档注)。
- 系统委托(官方文档 usermanual-cce/cce_10_0556.html,已抓取 /tmp/doc_agency.md):`cce_admin_trust`(除 IAM 外全部云服务管理员权限,用于 CCE 调用依赖服务)与 `cce_cluster_agency`(仅 CCE 组件依赖的云服务资源操作权限,用于生成集群组件临时凭证,1.21+ 支持)。

### 3.2 CAPA(kubernetes-sigs/cluster-api-provider-aws)事实(代码来源:/tmp/capa)

- 双模式:自建(EC2+kubeadm,`controllers/` 下 AWSCluster/AWSMachine)与 EKS 托管(`exp/` 下 AWSManagedControlPlane/AWSManagedMachinePool)。
- 架构分层:
  - `controllers/`(top-level reconciler,处理 ownerRef/finalizer/条件编排)→ `pkg/cloud/scope/`(ClusterScope/MachineScope/ManagedControlPlaneScope 封装云客户端+参数)→ `pkg/cloud/services/`(ec2/eks/network/elb/securitygroup/iam 等,每个 service 一个接口 + 实现)。
  - `pkg/cloud/interfaces.go` 定义 service 接口,便于单测注入 mock(已亲自验证 `pkg/cloud/services/eks/eks.go`:`Service.ReconcileControlPlane()` 按条件分步:IAM 角色→EKS 集群→Addons→IdentityProvider→PodIdentity,每步成功打 `conditions.MarkTrue`,失败 `MarkFalse` 并返回)。
- 条件驱动状态机:自定义 condition 常量(如 `EKSControlPlaneReadyCondition`)挂在 CR status.conditions 上,CAPI 核心据此判断就绪。
- 网络自举:VPC→子网→安全组→IGW/NAT→ELB 的创建顺序与 Tagging 约定(`sigs.k8s.io/cluster-api-provider-aws/` 前缀 tag),命名+Tag 双重识别。
- 凭证:static AK/SK、SSM、IRSA(WebIdentity)多来源;ControllerIdentity(集群级默认身份)。
- 实验特性在 `exp/`:EKS 托管模式、MachinePool 属于实验性 API,feature gate 控制。
- clusterctl 合约:`metadata.yaml`、`infrastructure-components.yaml`(make generate 产物)、`tilt-provider.json`。
- 测试:`test/e2e` 使用 `sigs.k8s.io/cluster-api/test/framework` + clusterctl 快速起集群。

### 3.3 阿里云 ACK Provider(alibabacloud-provider-for-Cluster-API)事实(代码来源:/tmp/capal)

- 定位:阿里云官方实现"创建和删除 ACK 托管集群",遵循 CAPI 规范,兼容 clusterctl(README 明确:clusterctl describe cluster 输出一致)。
- **关键架构事实**:构建在 **Crossplane/Upjet** 之上 —— `api/cs/v1alpha1/` 下的 `zz_*_terraformed.go`(ManagedKubernetes/KubernetesNodePool/VPC/VSwitch)是用 Upjet 从 Terraform aliyun provider 生成的 CRD 类型;`api/alibabacloud/v1beta1/ProviderConfig` + `internal/controller/providerconfig` 管理云凭证(access_key/secret_key,由 `config/manager/provider_config.yaml` 注入,缺失则程序拒绝启动)。
- CAPI 侧类型(api/infrastructure/v1beta2、api/controlplane/v1beta2):
  - `AliyunManagedCluster`(InfraCluster)
  - `AliyunManagedControlPlane`(ControlPlane;Spec 含 Region/Version/ClusterSpec/Network(Vpc/VSwitches/PodCIDR/ServiceCIDR/NatGateway/SecurityGroup)/Addons/CNI/KubeProxy/EndpointAccess;Status 含 Ready/Initialized/ClusterID/NetworkStatus(ApiServerSlbID/NatGatewayID/SecurityGroup))
  - `AliyunManagedMachinePool`(InfraMachinePool;Spec 含 ScalingGroup/Version/KubernetesConfig/AdditionalTags)
- 控制面 reconcile 顺序(已亲自验证 `/tmp/capal/internal/controller/controlplane/aliyunmanagedcontrolplane_controller.go` Reconcile):
  1. 等 Cluster.Status.InfrastructureReady
  2. 加 finalizer
  3. reconcileProviderConfig(凭证)
  4. ReconcileVPC → ReconcileVSwitch(Crossplane MR)
  5. reconcileManagedKubernetes(创建 ACK 托管集群 MR,等待就绪)
  6. setStatus + ControlPlaneEndpoint 有效后 reconcileKubeconfig
  - 全程以 condition 常量(MarkFalse/MarkTrue)报告进度;使用 defer 统一 Status().Update + Spec 回滚模式。
- 局限(代码事实):README 明确仅"创建和删除"ACK 托管集群;代码中大量 `todo:` 注释(删除流程、region 来源、terway/flannel CNI 细节、endpoint public 等);升级/更新能力未实现或未验证。

### 3.4 CAPHW(cluster-api-provider-huawei)事实(代码来源:/tmp/caphw)

- 定位:华为云官方(社区维护)的 CAPI 基础设施 provider,仅自建集群(ECS + kubeadm),**不支持 CCE**(用户已确认)。
- 结构:`api/v1alpha1/`(HuaweiCloudCluster/HuaweiCloudMachine/HuaweiCloudMachineTemplate)、`internal/controller/`、`pkg/scope/`(ClusterScope/MachineScope)、`pkg/services/network/`(VPC/子网/安全组/EIP)、`pkg/services/ecs/`(ECS 生命周期)、`pkg/basic/`(SDK client 封装)、`pkg/errors`、`pkg/logger`。
- 可复用资产(华为云侧):SDK client 初始化(region/credentials)、AK/SK 凭证读取模式、错误分类、network/ecs services 分层、docs/book 文档结构。
- 必须重写部分:集群级逻辑从"kubeadm 自建"换成"CCE API 托管";节点逻辑从"ECS+userdata 引导"换成"CCE 节点池/节点 API";网络从"为自建集群建 VPC/安全组"改为"为 CCE 集群准备 VPC/子网(ENI)等"。

---

## 4. 需要对接华为云 CCE 验证的事项清单(全部标注在正式文档中)

> **可直接发送的正式问卷(中英双语,含填写说明与汇总表):[华为云 CCE 对齐问卷](archive/cce-verification-questionnaire.md) / [English](archive/cce-verification-questionnaire.en.md)。** 以下 14 项与该问卷一一对应,在全部确认/实测前不进入 PoC 编码。

1. **[需验证]** CCE 创建集群 API 是否允许"0 节点空集群"(文档显示创建集群请求体不含节点,但需实测确认空集群可创建、可后续单独建节点池;以及空集群是否计费/是否受配额限制)。
2. **[需验证]** 下载证书接口 `CreateKubernetesClusterCert` 返回的 kubeconfig 中,`current-context` 为 external/internal 的切换逻辑、证书有效期(ClusterCertDuration)最大值;私网集群(无公网 IP)时 kubeconfig 的 server 地址是否可达管理集群。
3. **[需验证]** 节点池扩缩容:`UpdateNodePool` 修改 `initialNodeCount` 是否即触发扩缩容;autoscaling 开关与 CAPI 扩缩容(MachinePool replicas)同时存在时的语义冲突与协调方式。
4. **[需验证]** Turbo ENI 模式下创建集群前对 VPC/子网的要求(子网必须支持 ENI、VPC 网段规划、每个可用区至少一个子网?);Pod 网段(eniNetwork)与 VPC 网段重叠规则。
5. **[需验证]** 安全组:CCE 集群的 master/node/eni 安全组创建/关联方式;节点池绑定安全组(最多 5 个)的 API 行为;是否允许自定义安全组规则用于控制面访问。
6. **[需验证]** IAM 权限:管理 CCE 需要的最小权限策略(CCE FullAccess?VPC/ECS/ELB 相关权限?);AK/SK 所用账号是否必须是项目(project)主账号或具有委托(cce_admin_trust / cce_cluster_agency)授权。
7. **[需验证]** 配额:默认每项目 CCE 集群数、节点池数、节点数、VPC/子网/ENI 配额上限,以及超配时的 API 错误码与重试策略。
8. **[需验证]** 删除语义:DeleteCluster 的删除时长/依赖(节点池是否必须先行删除、ELB/EVS/EIP 残留);`Unavailable` 状态集群是否可删。
9. **[需验证]** 节点加入方式:CCE 节点池的节点由 CCE 自动引导加入;但 `AddNode`/`AddNodesToNodePool`(把已有 ECS 加入集群)是否要求 ECS 预装 agent/脚本 —— 决定我们是否/如何支持 CAPI 的 Machine(非 MachinePool)路径。
10. **[需验证]** Autopilot(Serverless)在 CAPI 模型中的表达(无节点概念,对标 Fargate Profile,是否首版不做)。
11. **[需验证]** 集群升级:`CreateUpgradeWorkFlow` 的编排参数(目标版本、是否支持跳过版本、升级时长)、升级期间集群状态。
12. **[需验证]** 计费:按需 vs 包周期(billingMode)、集群本身是否计费、空集群最小成本;休眠/唤醒(AwakeCluster)机制。
13. **[已确认]** 终端节点/公私网:CCE API Server 私网访问 = 平台托管 VPC 内网端点(`ShowClusterEndpoints.privateEndpoint`,HA 返回 VIP,`https://<VPC内网IP>:5443`,仅同 VPC 可达);跨 VPC 官方方案 = VPC 对等连接 / 企业路由器(ER)/ 云专线 / VPN(非 VPCEP);跨 Region 官方无专门推荐 → 需咨询/实测。**VPCEP(VPC Endpoint)面向 OBS/DNS 等云服务或用户私有服务,CCE API Server 非其支持类别**——gap-analysis §4.3 的「VPC Endpoint 打通 CCE 私网 API Server」为误解,定性为「云能力差异(平台托管),provider 无需实现」。
14. **[需验证]** API 限流:CCE/ECS/VPC 各服务的 API 调用频率限制(每分钟调用次数),决定控制器 requeue/退避策略参数。
