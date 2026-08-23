# cce-provider-for-cluster-api 架构设计文档

- 版本:v0.2(设计+PoC 验证版)
- 状态:设计定稿(实现已按本设计落地,并经真实 CCE 冒烟与 clusterctl 部署验证)
- 配套文档:[调研依据与事实清单](research-sources.md)、[需求设计文档](requirements-design.md)、[华为云 CCE 对齐问卷](archive/cce-verification-questionnaire.md)、[验证结论记录](cce-verification-findings.md)、[clusterctl 部署演练记录](clusterctl-deployment-validation.md)、[CAPA 架构分析报告](archive/CAPA架构分析报告.md)、[CAPHW 架构分析报告](archive/CAPHW架构分析报告.md)、[ACKProvider 架构分析报告](archive/ACKProvider架构分析报告.md)

> **事实基准声明**:本文所有设计结论均基于《调研依据与事实清单》中列出的真实来源(华为云官方 Go SDK 模型、华为云官方 API/用户指南文档、CAPA / alibabacloud-provider-for-Cluster-API / cluster-api-provider-huawei 源码)。
> 凡是标注 **[需验证]** 的条目,均为无法从现有公开资料完全确认、**需要对接真实华为云 CCE 实测或咨询华为云确认**后才能在实现中定稿的点,清单见 [附录 A](#附录-a需对接华为云-cce-验证的事项清单)。
>
> **验证状态(2026-08-19)**:附录 A 的 14 项中,Q1/Q2/Q3/Q5/Q7/Q8/Q13/Q14 已由真实 CCE 冒烟实测确认(含 Q14 Retry-After 抓包),Q4/Q6/Q9/Q10/Q12 已由官方文档确认,Q11 已实测(v1.33→v1.34 目标开放并跑通升级工作流;空目标=按版本线动态开放,controller 按正常状态处理);**剩余仅完整成功升级耗时待预检查通过后实测**。逐项状态见 [验证结论记录](cce-verification-findings.md) 汇总表。

---

## 1. 概述

### 1.1 背景与目标

华为云官方现有的 Cluster API Provider(CAPHW,`cluster-api-provider-huawei`)定位是"在华为云 ECS 上自建 Kubernetes 集群"(调用 ECS/VPC 等 IaaS API + kubeadm 引导),**不管理 CCE 托管集群**(用户已确认;代码事实见 research-sources.md §3.4)。

本项目 `cce-provider-for-cluster-api` 的目标是:**开发一个管理华为云 CCE 托管集群的 Cluster API 基础设施 Provider**,对标 `CAPI + AWS EKS 托管模式`(CAPA 的 EKS 模式),同时参考阿里云 ACK Provider(`alibabacloud-provider-for-Cluster-API`,阿里云官方实现"创建/删除 ACK 托管集群",README 明示遵循 CAPI 规范、兼容 clusterctl)。

### 1.2 定位与边界

| 维度 | 本项目 |
|---|---|
| Provider 类型 | Infrastructure Provider(CAPI Provider Contract) |
| 托管对象 | 华为云 CCE 托管集群(控制面由华为云托管) |
| 集群类型 | **CCE Standard(默认推荐 Turbo)** 与 **CCE Turbo** 双支持,首版默认 Turbo(对标 EKS 定位,理由见 §6.1);CCE Autopilot 列为远期(§13.3) |
| 节点管理 | 主路径:**CCE 节点池(NodePool)↔ CAPI MachinePool**;辅助路径:单节点(CCE 节点 API ↔ CAPI Machine),首版以节点池为主 |
| 与 CAPHW 的关系 | 不基于 CAPHW 改造;复用其"华为云侧适配模式"(SDK client 构建、凭证、错误分类、scope 分层),集群/节点/网络语义全部按 CCE 托管模式重写 |
| 发布方式 | 不合并 kubernetes-sigs 上游;仓库名 `cloudnative-cluster-api-provider-cce`(华为云解决方案开发者套件命名规范,已确认),发布到华为云官方组织与开发者社区,兼容 `clusterctl`;许可证 **MIT-0**(治理规范默认,已确认) |

### 1.3 核心设计原则

1. **严格遵循 CAPI Provider Contract**(版本标签、namespace-scoped、status/conditions、finalizer、clusterctl 合约),来源:用户聊天中已确认的社区对齐结论 + 官方 contract 要求。
2. **"声明式调谐 + 幂等"**:任何一步失败都可安全重试;以 CCE 资源 ID 回写 + Tag 双机制识别云资源,不依赖命名。
3. **面向托管服务的薄适配**:控制面/节点生命周期交给 CCE,Provider 只做"CRD ↔ CCE API"的翻译、编排与状态同步,不自建 VPC/子网(只引用/校验),不生成 kubeadm userdata(CCE 托管 kubelet)。
4. **分层可测**:Controller → Scope → Service(接口)三层,Service 层全部可 mock 单测;Controller 层用 envtest。
5. **全链路可观测**:conditions 状态机、Kubernetes 事件、结构化日志、指标(failed reconcile 计数等)。

---

## 2. 总体架构

### 2.1 部署拓扑

```
┌────────────────────────── 管理集群 (Management Cluster) ──────────────────────────┐
│                                                                                   │
│  ┌─ cluster-api (core) ─┐   ┌─ kubeadm bootstrap (可选,仅单节点路径) ─┐           │
│  │ Cluster/Machine/      │   │ KubeadmConfig                          │           │
│  │ MachinePool 控制器     │   └────────────────────────────────────────┘           │
│  └──────────┬────────────┘                                                         │
│             │ ownerRef / infrastructureRef / controlPlaneRef                        │
│  ┌──────────▼───────────────────────────────────────────────────────────┐          │
│  │              cce-provider-for-cluster-api (本 Provider)              │          │
│  │  ┌──────────────────────────────────────────────────────────────┐    │          │
│  │  │ CCECluster 控制器   CCEManagedControlPlane 控制器             │    │          │
│  │  │ CCEManagedMachinePool 控制器  (CCEMachine 控制器,单节点)      │    │          │
│  │  ├──────────────────────────────────────────────────────────────┤    │          │
│  │  │ scope 层(ClusterScope / ControlPlaneScope / MachinePoolScope)│    │          │
│  │  ├──────────────────────────────────────────────────────────────┤    │          │
│  │  │ services 层: cce(集群/节点池/节点/kubeconfig)、network(只读   │    │          │
│  │  │ 校验)、tags、errors(错误分类+限流退避)、identity(凭证)        │    │          │
│  │  └──────────────────────────────────────────────────────────────┘    │          │
│  └──────────┬───────────────────────────────────────────────────────────┘          │
└─────────────┼─────────────────────────────────────────────────────────────────────┘
              │ 华为云 CCE/ECS/VPC Go SDK (huaweicloud-sdk-go-v3)
              │ AK/SK(建议每集群 Secret) / IAM 委托
┌─────────────▼─────────────────────────────────────────────────────────────────────┐
│                              华为云 (目标账号 Project)                              │
│   VPC/子网(用户已有或预置) → CCE 托管集群(控制面) → 节点池(NodePool) → ECS 节点       │
│   kubeconfig(CCE 签发) → 返回管理集群 Secret → CAPI 消费                            │
└───────────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 组件职责划分

| 组件 | 职责 | 参考实现 |
|---|---|---|
| cluster-api core | 编排 Cluster/Machine/MachinePool 生命周期、集群状态汇总 | sigs.k8s.io/cluster-api |
| 本 Provider 控制器 | 翻译 CRD → CCE API;回写 status/conditions;finalizer 管理 | CAPA `controllers/` + `exp/controllers/` |
| scope 层 | 封装 CR 参数、patch helper、SDK client 注入点(依赖注入) | CAPA `pkg/cloud/scope/`;CAPHW `pkg/scope/` |
| services 层 | 每个云能力一个接口 + 实现(可 mock) | CAPA `pkg/cloud/services/{eks,ec2,network,...}`;CAPHW `pkg/services/{network,ecs}` |
| bootstrap provider | 仅单节点路径需要;节点池路径**不需要**(CCE 托管引导) | CAPA 的 CABPE / kubeadm bootstrap |

### 2.3 Provider Contract 遵循要点(对照官方要求)

1. CRD 均为 **namespace-scoped**;包含标准 TypeMeta/ObjectMeta。
2. 所有 CRD 打 **`cluster.x-k8s.io/v1beta1`(及 v1beta2)版本标签**(config/crd/kustomization.yaml `commonLabels`)。
3. API Group 采用标准 `infrastructure.cluster.x-k8s.io`(集群/机器)与 `controlplane.cluster.x-k8s.io`(托管控制面),与 CAPI 核心/ACK Provider 一致(代码事实:`/tmp/capal/api/{controlplane,infrastructure}/v1beta2/groupversion_info.go`)。
4. `InfraCluster` 通过 `status.ready` + `status.controlPlaneEndpoint`(API Server 地址)向 CAPI 报告就绪。
5. `ControlPlane` 报告 `status.initialized` / `status.ready`,并**负责 kubeconfig 的获取与 Secret 管理**(CCE 侧为 `CreateKubernetesClusterCert`,见 §8)。
6. 所有 CR 使用 finalizer 实现优雅删除(创建即加,云资源清完才移除)。
7. status.conditions 使用标准 `metav1.Conditions`/`clusterv1.Conditions` 报告进度与错误(如 `CCEClusterReadyCondition` 等,见 §4.3)。
8. 支持 `cluster.x-k8s.io/paused` 注解跳过调谐(CAPA/CAPHW 均有此模式,CAPHW 代码事实:cluster 控制器 IsPaused 检查)。
9. 发布物:`metadata.yaml` + `infrastructure-components.yaml` + `cluster-template.yaml`,支持 `clusterctl init --infrastructure cce`(对标 ACK Provider 的 README 承诺与 CAPA 的 clusterctl-settings.json 模式)。

> 注:CAPI v1.11 起引入 v1beta2 API 合约(用户聊天中已提及),实现时按目标 CAPI 版本选择合约标签;具体版本对齐矩阵见需求文档 §6.2。

---

## 3. CRD 设计

### 3.1 类型总览

| CRD | CAPI 角色 | 对应 CCE 资源 | 说明 |
|---|---|---|---|
| `CCECluster` | InfrastructureCluster | CCE 集群的"外壳" | 承载 region、VPC/子网引用、集群参数、最终 endpoint |
| `CCEManagedControlPlane` | ControlPlane | CCE 托管集群(控制面) | 创建/更新/删除 CCE 集群;管理 kubeconfig |
| `CCEManagedMachinePool` | InfrastructureMachinePool | CCE 节点池(NodePool) | 节点池生命周期 + 扩缩容 |
| `CCEMachine`(可选,二期) | InfrastructureMachine | CCE 单节点/已有 ECS 节点 | 单节点路径(依赖 CCE AddNode 语义,见 §7.4 与待验证 9) |
| `CCEMachineTemplate`(可选,二期) | InfrastructureMachineTemplate | — | 单节点模板 |

设计说明:采用"CCECluster(薄外壳)+ CCEManagedControlPlane(厚实现)"分离,与 CAPA 的 `AWSCluster`/`AWSManagedControlPlane`、ACK Provider 的 `AliyunManagedCluster`/`AliyunManagedControlPlane` 双对象模式一致(代码事实:/tmp/capal api 结构)。理由:
- CAPI 核心要求 `Cluster.spec.infrastructureRef`(→CCECluster)与 `Cluster.spec.controlPlaneRef`(→CCEManagedControlPlane)分离;
- 网络等集群级参数集中在外壳,控制面参数集中在内核,职责清晰。

### 3.2 `CCECluster`(InfraCluster)

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: CCECluster
metadata:
  name: my-cluster
  namespace: default
  labels:
    cluster.x-k8s.io/cluster-name: my-cluster   # CAPI 关联标签
spec:
  region: cn-north-4                       # 必填;SDK region 常量解析 endpoint
  projectID: ""                            # 可选;缺省用凭证所属 project
  network:                                 # CCE 不创建网络,只"引用 + 校验"
    vpc:                                   # 必填(CCE 创建前置条件:必须先有 VPC)
      id: vpc-xxxx
      cidr: 10.0.0.0/16                   # [需验证] 创建集群前校验项
    subnets:                               # 节点子网(至少 1 个)
      - id: subnet-xxxx
        az: cn-north-4a
  additionalTags: {}                        # 打到 CCE 集群的标签(tag)
status:
  ready: false
  initialization:                           # CAPI InfrastructureCluster 契约字段(实测确认)
    provisioned: false                      # status.initialization.provisioned:
                                            #   CAPI Cluster 控制器据此置 InfrastructureProvisioned
  clusterID: ""                             # CCE 集群 UUID(回写)
  controlPlaneEndpoint:                     # API Server 地址(由内核回填,见 §4.2)
    host: 10.0.0.10
    port: 5443
  conditions: []
```

字段依据:
- `region`/VPC 前置条件:官方文档 cce_02_0236("创建集群之前,您必须先确保已存在虚拟私有云")。
- "引用已有 VPC/子网而非自建":设计决策(区别于 CAPHW 的自建网络),依据:CCE 本身只"消费"网络;ACK Provider 由 VPC/VSwitch MR 自动创建网络,我们首版选择"引用+校验",自动创建作为可选增强(§13.2)。

### 3.3 `CCEManagedControlPlane`(ControlPlane)

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: CCEManagedControlPlane
metadata:
  name: my-cluster-control-plane
  namespace: default
spec:
  clusterName: my-cluster
  version: v1.30.0            # Kubernetes 版本;空则 CCE 最新(官方 SDK 注释:默认创建最新版本)
  category: Turbo             # CCE | Turbo(API 字段 category,SDK:ClusterSpecCategory CCE/TURBO)
  flavor: cce.s2.medium       # 集群规格(三控制节点,最大 200 节点;见官方文档/SDK 注释)
  containerNetwork:
    mode: eni                 # overlay_l2 | vpc-router | eni(仅 Turbo)
    cidr: 10.244.0.0/16      # 容器网段(隧道模式创建后不可改,vpc-router 可增不可改)
    eniSubnets: []            # eni 模式需要(ENI 子网)
  serviceNetwork:
    cidr: 10.247.0.0/16       # 服务网段;默认 10.247.0.0/16(官方文档)
  authentication:             # [需验证] 认证方式(认证模式/证书轮换)
    mode: rbac
  customSan: []               # API Server 证书 SAN(官方字段)
  endpointAccess:             # [需验证]
    public: true              # 是否开通公网访问(对标 ACK Provider EndpointAccess.Public)
  agencyName: cce_cluster_agency   # 委托(1.27+ 支持,官方 SDK 字段;缺省用系统委托)
  billing:
    mode: 0                   # 0=按需 1=包周期(官方 billingMode)
  addons: []                  # CCE 插件(Addon),对标 CAPA EKS Addons(见 §7.3)
  logging: {}                 # 控制面日志(对标 CAPA 的 logging)
status:
  ready: false
  initialized: false
  clusterID: ""
  controlPlaneEndpoint:
    host: 10.0.0.10
    port: 5443
  kubeconfigSecretName: ""    # 生成的 kubeconfig Secret(§8)
  version: v1.30.0
  conditions: []
```

字段依据均为官方 SDK 模型与 API 文档(见 research-sources.md §3.1):
- `category` CCE/TURBO、`flavor` cce.s1.*/cce.s2.*、`version`、`customSan`、`hostNetwork`、`containerNetwork`(mode 枚举)、`serviceNetwork`、`authentication`、`publicAccess`、`agencyName`、`billingMode`。
- 结构上对齐 ACK Provider 的 `AliyunManagedControlPlaneSpec`(Region/Version/ClusterSpec/Network/Addons/CNI/KubeProxy/EndpointAccess,代码事实:/tmp/capal/api/controlplane/v1beta2/aliyunmanagedcontrolplane_types.go)。

### 3.4 `CCEManagedMachinePool`(InfraMachinePool)

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: CCEManagedMachinePool
metadata:
  name: my-cluster-pool-0
  namespace: default
spec:
  clusterName: my-cluster
  nodePoolName: pool-0
  flavor: c7.large.2          # ECS 规格(nodeTemplate.flavor,官方节点池参数)
  os: HuaweiCloudEulerOS2.0   # 节点 OS(官方:安全等保加固必须 HCE 2.0)
  rootVolume:
    size: 40
    type: GPSSD
  dataVolumes: []             # 数据盘
  sshKey: my-keypair          # sshKey
  az: cn-north-4a             # 可用区(节点池单 AZ 或多个?)
  replicas: 3                 # 期望节点数(映射 initialNodeCount)
  autoscaling: {}             # 节点池自动扩缩容(enable/min/max/priority;与 CAPI 扩缩容协调见 §7.2/[需验证] 3)
  billingMode: 0              # 0=按需 1=包周期
  taints: []                  # 节点污点(≤20 条,官方约束)
  labels: {}                  # 节点标签
  securityGroups: []          # Turbo ≥1.21 可绑定安全组,每池最多 5 个(官方)
  additionalTags: {}
status:
  ready: false
  replicas: 3
  availableReplicas: 3
  nodePoolID: ""
  failureReason: ""
  conditions: []
```

字段依据:官方节点池文档 cce_02_0354(nodeTemplate.flavor/rootVolume/dataVolumes/os/sshKey/taints≤20、initialNodeCount、autoscaling、billingMode、nodeManagement、安全组≤5 个),结构对齐 ACK Provider `AliyunManagedMachinePoolSpec`(ScalingGroup/KubernetesConfig/AdditionalTags)。

### 3.5 版本与转换

- 首版单版本 `v1beta1`(对齐 CAPI contract 标签);预留 `v1beta2` 升级路径(conversion webhook 按需)。
- CAPI 版本对齐策略见需求文档 §6.2(依赖 `sigs.k8s.io/cluster-api` 版本由"发布系列 ↔ contract 版本"矩阵决定,对标 CAPA metadata.yaml 模式)。

---

## 4. 控制器设计

### 4.1 控制器清单与 watch 拓扑

| 控制器 | Watch | 说明 |
|---|---|---|
| `CCECluster` | CCECluster;OwnerRef → Cluster | 薄外壳:校验网络、记录 region/tags;转发控制面创建给内核 |
| `CCEManagedControlPlane` | CCEManagedControlPlane;OwnerRef → Cluster | 核心:CCE 集群全生命周期 + kubeconfig |
| `CCEManagedMachinePool` | CCEManagedMachinePool;OwnerRef → MachinePool;Watch CCE 集群状态 | 节点池全生命周期 + 扩缩容 + 状态同步 |
| `CCEMachine`(二期) | CCEMachine;OwnerRef → Machine | 单节点路径 |

### 4.2 核心调谐流程

**CCEManagedControlPlane Reconcile(创建/更新)**(参照 CAPA eks.Service.ReconcileControlPlane 的分步 condition 模式 + ACK Provider 的编排顺序,均已验证代码事实):

```
1. Get CR;Get owner Cluster;检查 paused 注解 → 跳过
2. 无 DeletionTimestamp:
   a. 等 Cluster.Status.InfrastructureReady(= CCECluster.status.ready)(ACK Provider 模式,controlplane_controller.go:131-134)
   b. AddFinalizer
   c. reconcileCredentials:解析凭证(Secret/env,§5)→ condition CredentialsReady
   d. reconcileNetwork:校验(或按需创建)VPC/子网、ENI 子网是否满足 CCE 要求(§6)→ condition NetworkReady
   e. reconcileCluster:CreateCluster(幂等:先 ShowCluster 按 ID/tag 查,不存在才创建)→ 轮询 ShowCluster 至 phase=Available
      → 回写 status.clusterID / controlPlaneEndpoint(ShowClusterEndpoints)→ condition CCEClusterReady
   f. reconcileKubeconfig:CreateKubernetesClusterCert → 写 Secret(§8)→ condition KubeconfigReady
   g. reconcileAddons(§7.3)
   h. status.initialized=true;status.ready=true
3. 有 DeletionTimestamp:reconcileDelete
   a. 先删依赖:节点池(或等待 MachinePool 控制器删完)→ 插件 → 集群(DeleteCluster)
   b. 轮询 ShowCluster 直至不存在(容忍 404)
   c. 删除 kubeconfig Secret;RemoveFinalizer
4. 全程 defer 统一 Status().Update;错误按分类(§5.3)决定 requeue 策略(限流→指数退避;可恢复→RequeueAfter 30s;永久错误→MarkFalse(Failure)+停止重试)
```

**CCEManagedMachinePool Reconcile**(参照 CAPA NodegroupService.ReconcilePool 模式):

```
1. 等控制面 ready(controlPlaneRef 就绪)
2. AddFinalizer
3. reconcileNodePool:按 nodePoolID/标签查 ListNodePools;不存在 → CreateNodePool(initialNodeCount=replicas)
   存在 → UpdateNodePool 对齐(flavor/磁盘/taints/labels/initialNodeCount 变化)→ condition NodePoolReady
4. 状态同步:ListNodePools/ShowNodePool + ListNodes → status.replicas/availableReplicas(节点 `Active` 数;CCE 节点状态枚举:Build/Installing/Upgrading/Active/Abnormal/Deleting/Error,SDK model_node_status.go)
5. 删除:DeleteNodePool → 轮询至不存在 → RemoveFinalizer
```

### 4.3 Conditions 状态机(命名与语义)

对照 CAPA(如 `EKSControlPlaneReadyCondition`)、ACK Provider(如 `ManagedKubernetesReconcileReadyCondition`)的命名模式,定义:

- 集群级:`CCEClusterReadyCondition`、`NetworkReadyCondition`、`CredentialsReadyCondition`、`KubeconfigReadyCondition`、`AddonsReadyCondition`
- 节点池级:`NodePoolReadyCondition`、`NodePoolScalingCondition`
- 每个 condition 带 reason(如 `CCEClusterReconciliationFailedReason`)+ severity(Error/Warning/Info)+ message,与 CAPI `conditions.MarkTrue/MarkFalse` 对齐。

### 4.4 幂等与并发控制

- 所有"创建前必查"(`ShowCluster`/`ListNodePools`),创建用幂等键(固定命名 + Tag),防止重复 reconcile 创建双资源(参照 ACK Provider 固定名 Get 判存在模式,controlplane_controller.go:255-339)。
- **创建失败幂等接管(实测确认)**:`CreateCluster` 冲突(已存在 `CCE.01409001` / 容器网段冲突 `CCE_CM.0410`)时,按名称查回已有集群 ID 接管——覆盖"创建成功但响应丢失"(限流边界,实测 `APIGW.0308` 写操作 10 次/分钟)场景,避免永久失败。见 `internal/services/cce/cce.go`。
- 控制面创建/删除期间用 condition 状态机互斥;`MachinePool` 扩缩容与 CCE autoscaling 并存时的协调已实测确认(autoscaling 配置与手动 `ScaleNodePool` 互不覆盖,见 Q3/B3)。
- finalizer 防误删;删除期间 owner Cluster 的删除顺序依赖 CAPI 核心按依赖图执行(MachinePool 先于 Cluster)。

### 4.5 错误处理与重试

- 错误分类(service 层统一错误处理,官方错误码全集已落地 `internal/services/errors/errors.go`,见 Q14):
  - NotFound(404/`CCE.01404001`)→ 视为幂等分支
  - Conflict/AlreadyExists(`CCE.01409001`、`CCE_CM.0410`)→ 转查询/接管分支
  - 限流/Throttle(`APIGW.0308`、`CCE.01429002/003`)→ **延迟 requeue(1 分钟)+ nil error**(`resultAfterError`),避免 controller-runtime 默认退避覆盖延迟;实测写操作限流 10 次/分钟、读操作 ~70 req/s 触发(见 Q14)
  - 配额超限(`CCE.01400007...`)→ 5 分钟 requeue
  - 权限不足(401/403)→ 30 分钟 requeue + MarkFalse(Failure)记录事件
- CCE/ECS/VPC API 的限流阈值、错误码全集 **[需验证] 14** → **已实测关闭**:错误码表已按官方 ErrorCode.html 落地;限流阈值实测见 [验证结论记录](cce-verification-findings.md) Q14。

---

## 5. 凭证与安全

### 5.1 凭证模型(设计决策)

- **推荐:每集群一个 Secret**(namespace 内,`<cluster>-credentials`),控制器按 OwnerRef 定位;对标 ACK Provider 的 ProviderConfig 思想但简化(ACK Provider 用全局 ProviderConfig + region 自动生成,代码事实:providerconfig controller)。
- 兜底:全局环境变量/默认 Secret(对标 CAPHW 的 `CLOUD_SDK_AK/SK` 模式,但**修正其两个反例**:① 不打印明文 AK;② 不把 AK/SK 注入节点 userdata —— CAPHW 的 cloudconfig.go 把明文 AK/SK 写进云配置,CCE 托管场景完全不需要)。
- Secret 内容:`accessKey` / `secretKey`(首版);二期支持 IAM 委托/临时凭证(§13.2)。
- 校验:启动时与每次调谐前校验必填;缺失 → CR 打 `CredentialsReady=False` 且 event 提示(ACK Provider 的做法:凭证缺失程序拒绝启动,README 明示)。

### 5.2 安全要点

- RBAC:控制器最小权限(仅本组 CRUD + Secret get);不授予 cluster-admin。
- Secret 不写入日志;结构化日志脱敏。
- webhook 校验:凭证引用、region 合法性、CIDR 合法性(创建后不可变的字段在 webhook 层拦截,见 §9)。
- CCE 侧委托:CCE 集群创建时使用 `agencyName`(cce_cluster_agency)限定 CCE 组件权限,避免 cce_admin_trust 全量授权(官方委托说明:cce_admin_trust 具有除 IAM 外的全部云服务管理员权限;cce_cluster_agency 仅含 CCE 组件依赖权限,1.21+ 支持)。

### 5.3 网络安全

- CCE API Server 访问:管理集群 → CCE endpoint 的网络路径(公网 endpoint / VPC 对等 / 云专线),依赖 [需验证] 13;首版支持公网 endpoint(简单、通用),私网路径二期。
- 节点池安全组:Turbo ≥1.21 支持每池 ≤5 个安全组(官方文档),用于限制节点入站(如 10250、SSH)。

---

## 6. 网络设计

### 6.1 集群类型与网络模型选择

依据官方"集群类型对比"(cce_10_0342)与"网络模型"(drawer-cce)文档:

| 维度 | CCE Standard | CCE Turbo | 设计影响 |
|---|---|---|---|
| 网络模型 | 容器隧道(overlay_l2)/ VPC(vpc-router) | 云原生网络 2.0(eni) | Turbo 与 VPC 一层,零损耗;Pod 可绑安全组 |
| 规模 | 按 flavor 上限 | 默认 2000 节点,最大 20000 | 大规模场景选 Turbo |
| Pod EIP/固定 IP | 不支持 | 支持 | Turbo 特性(远期映射) |
| 安全容器(Kata) | 不支持 | 支持(物理机) | Turbo 特性(远期) |

**结论**:默认 `category: Turbo` + `containerNetwork.mode: eni`;同时支持 `CCE` + `overlay_l2`/`vpc-router` 以满足存量场景。实现层面仅影响参数组装(同一 API),首版都支持、默认值不同。

### 6.2 VPC/子网策略

- **引用模式(首版)**:用户/平台预置 VPC 与子网,`CCECluster.spec.network` 引用;控制器只做**校验与状态记录**:
  - VPC 存在、CIDR 与容器网段不冲突(eni 模式要求 VPC 网段与 Pod 网段不重叠,**[需验证] 4**);
  - 子网数量与可用区(至少覆盖节点池 AZ,eni 模式对子网有 ENI 支持要求);
  - 服务网段默认 `10.247.0.0/16`,不与 VPC/容器网段冲突(官方文档:默认值)。
- **自动创建(可选增强,二期)**:参照 ACK Provider 的 VPC/VSwitch 自动管理,由 Provider 创建 VPC/子网(可能复用华为云 Terraform provider 思路或直调 VPC SDK);首版不做,降低复杂度。

### 6.3 网段不可变性(重要注意事项)

官方文档 cce_02_0236 明确:
- 容器隧道模式:**创建后无法修改网段**;
- vpc-router/eni 模式:创建后**可新增**网段/子网,**不可修改已有**;调整需重建集群。
→ webhook 必须对已创建集群的网段变更做校验拦截(见 §9),并在文档中提示用户"网段即集群身份的一部分"。

### 6.4 安全组与公网访问

- 控制面访问:CCE 创建集群时指定公网访问(publicAccess)/内网;`ShowClusterEndpoints` 返回 url+type(public/private),回写 `status.controlPlaneEndpoint`。
- 节点:Turbo 节点池绑安全组(≤5 个);SSH 密钥(sshKey)管理节点登录。
- 出网:节点出网依赖 VPC 的 NAT/弹性 IP,由用户在 VPC 侧配置(Provider 不创建,区别于 CAPHW 的 NAT 自动创建)。

---

## 7. 节点管理

### 7.1 主路径:节点池(MachinePool)

- `MachinePool` → `infrastructureRef` → `CCEManagedMachinePool` → CCE `NodePool`。
- **无需 bootstrap provider**:CCE 托管节点的 kubelet/引导由 CCE 完成(对标 AWSManagedMachinePool 无 bootstrap ref 模式)。
- 扩缩容:改 `MachinePool.spec.replicas` → 控制器调用节点池伸缩 API(`ScaleNodePool`,SDK 事实;IAM 授权项 `cce:nodepool:scale`)或 `UpdateNodePool` 的 `initialNodeCount`;状态回写 availableReplicas(节点 `Active` 数)。
- CCE 节点池 autoscaling(enable/min/max)与 CAPI 扩缩容并存时的语义协调 **[需验证] 3**(建议首版:节点池 autoscaling 关闭,完全由 CAPI 驱动;autoscaling 映射二期)。

### 7.2 单节点路径(二期,可选)

- `Machine` → `CCEMachine` → CCE `CreateNode`/`AddNode`。
- **待验证语义**:CCE 的 `AddNode`(把已有 ECS 加入集群)与 `CreateNode`(按模板创建节点)的引导语义(是否要求 ECS 预装 agent、是否支持 CAPI 式"免 SSH 引导")——决定该路径可行性,见 [需验证] 9。
- 若不可行或语义过重,首版**只支持节点池路径**,单节点路径从 roadmap 移除。

### 7.3 插件(Addon)管理

- CCE API 提供 `CreateAddonInstance/UpdateAddonInstance/DeleteAddonInstance`(SDK 事实),对标 CAPA 的 EKS Addons 与 CCE 控制台"插件中心"(coredns、vpc-cni、autoscaler 等)。
- 设计:`CCEManagedControlPlane.spec.addons` 声明式管理;首版支持"创建集群时附带插件"与"独立 reconcile 插件实例",失败不阻塞集群就绪(与 CAPA EKS Addons 的 Warning 级 condition 对齐:reconcileAddons 失败 MarkFalse 但可继续)。

### 7.4 节点身份与 ProviderID

- CCE 节点对应 ECS 实例 ID;ProviderID 格式 `huaweicloud://<instanceID>`(沿用 CAPHW `pkg/scope/provider.go` 的格式约定)。
- 节点状态机:CCE 节点 phase(Build/Installing/Upgrading/Active/Abnormal/Deleting/Error,SDK model_node_status.go)映射到 Machine/MachinePool 就绪语义(Active→Ready,Abnormal/Error→失败条件)。

---

## 8. kubeconfig 管理

- 来源:CCE 官方 API `CreateKubernetesClusterCert`(入参 cluster_id + 证书有效期;响应为完整 kubeconfig,含 current-context external/internal);IAM 侧仅需 `cce:cluster:get`(官方权限表)。
- 流程:控制面 Ready 且 endpoint 有效后调用(对齐 ACK Provider:endpoint 有效才 reconcileKubeconfig,代码事实 controlplane_controller.go:230-235);
  生成 Secret(`<cluster>-kubeconfig`,key `value`),供 `clusterctl get kubeconfig` 与 workload 集群访问。
- 证书轮换:有效期(ClusterCertDuration)过期前自动刷新(reconcile 检查 Secret 有效期),**[需验证] 2** 确认有效期最大值与刷新语义。
- 私网集群:current-context=internal 时 server 地址为内网 VIP,管理集群需可达(§5.3 网络路径,[需验证] 13)。

---

## 9. Webhook 设计

- **Defaulting**:category 默认 Turbo、flavor 默认 cce.s1.small(官方默认)、serviceNetwork 默认 10.247.0.0/16、billingMode 默认 0(按需)、replicas 默认 1、endpointAccess.public 默认 true(首版)。
- **Validating(创建/更新)**:
  - `category` ∈ {CCE, Turbo};`containerNetwork.mode` 与 category 一致性(eni→Turbo;SDK 文档:容器网络参数设置为 eni 模式时默认为 Turbo);
  - `version` 格式(官方约束:vX.Y[.Z[-rN]]);
  - 网段合法性 + **已创建集群的网段变更拦截**(§6.3 不可变性);
  - flavor 白名单(官方规格枚举,按 region 可能有差异 **[需验证]**);
  - taints ≤ 20 条(官方约束)、securityGroups ≤ 5 个(Turbo ≥1.21);
  - 删除保护:DeletionProtection 语义(对标 ACK Provider Spec.DeletionProtection)。
- 对齐:ACK Provider 有 AliyunManagedControlPlane/AliyunManagedMachinePool 的 webhook 实现可参照;CAPHW 的 webhook 为 scaffold 未注册(反例,需真正实现)。

---

## 10. 可观测性

- **conditions**:§4.3 状态机,聚合到 `Cluster` 的 Ready/ControlPlaneReady/WorkersReady(由 CAPI 核心聚合)。
- **事件(Events)**:创建/删除/扩缩容/凭证失败等关键动作 emit event(记录器模式参照 CAPA `pkg/record`)。
- **指标**:controller-runtime 自带 reconcile 指标 + 自定义(API 调用次数/错误率/限流次数),Prometheus 暴露(config/prometheus 对齐 CAPHW)。
- **日志**:结构化 logr(controller-runtime),含 cluster/machine 关联字段;凭证脱敏。
- **追踪/审计**:二期(可选 OpenTelemetry)。

---

## 11. clusterctl 集成与发布

- 发布物(仓库 release assets,对标 CAPA/ACK 模式):
  - `metadata.yaml`:contract 版本 + release 系列 ↔ CAPI 版本矩阵;
  - `infrastructure-components.yaml`:CRD + Controller + RBAC + Webhook(同一 namespace,make generate 产物);
  - `cluster-template.yaml`:快速起集群模板(含 Secret 占位)。
- `clusterctl init --infrastructure cce` 支持:发布到 GitHub Release 即构成 Provider 仓库(官方 clusterctl 合约);如需 `clusterctl` 预置列表需向上游提 PR(本项目不做,对标阿里云模式)。
- 镜像:发布到华为云 SWR / ghcr,镜像名 `<org>/cce-provider-controller:vX.Y.Z`(对标 CAPHW 的 ghcr 发布模式)。
- 文档:docs/book(对标 CAPHW:user-guide/dev-guide/reference)+ 华为云开发者社区博客 + 云商店开源镜像上架(用户已确认的发布策略)。

---

## 12. 代码结构与测试策略

### 12.1 仓库布局(对照 CAPA/ACK Provider/CAPHW)

```
cce-provider-for-cluster-api/
├── api/
│   ├── infrastructure/v1beta2/     # CCECluster, CCEMachine(二期), CCEMachineTemplate(二期)
│   ├── controlplane/v1beta2/       # CCEManagedControlPlane
│   └── common/                     # 共享类型(VPC/Subnet/Tags 等)
├── controllers/                    # CCECluster / CCEManagedControlPlane / CCEManagedMachinePool
├── internal/
│   ├── scope/                      # ClusterScope / ControlPlaneScope / MachinePoolScope
│   ├── services/
│   │   ├── cce/                    # 集群/节点池/节点/kubeconfig/插件
│   │   ├── network/                # VPC/子网只读校验(二期:创建)
│   │   ├── identity/               # 凭证解析
│   │   └── errors/                 # 错误分类 + 限流退避
│   └── features/                   # feature gates
├── webhooks/                       # defaulting/validating
├── config/                         # crd/rbac/manager/webhook(default kustomize)
├── docs/book/                      # 用户/开发/参考文档
├── test/e2e/                       # CAPI e2e framework
├── metadata.yaml
└── Makefile (make generate/manifests/docker-build/test/e2e)
```

### 12.2 测试策略

| 层 | 手段 | 覆盖 |
|---|---|---|
| Service 层 | 单元测试,SDK 接口 mock(参照 CAPA interfaces.go 注入模式 + CAPHW 的 errors 分类测试) | 创建/查询/删除幂等、错误分类、状态映射 |
| Controller 层 | envtest(CRD + fake client) | 调谐顺序、conditions、finalizer、删除逆序 |
| API 层 | webhook 单测 | 校验/默认值 |
| e2e | CAPI 官方 e2e framework(`sigs.k8s.io/cluster-api/test`) + 真实华为云账号 | 创建→就绪→扩缩容→删除;升级(二期) |
| 冒烟 | 脚本化冒烟(对标 CAPHW e2e 但必须真实触达 CCE) | 每发布前 |

> 重要:ACK Provider 与 CAPHW 的 e2e 目前都是"不触达真实云"的脚手架(代码事实),本项目应把"真实 CCE 冒烟"作为发布门槛。

---

## 13. 路线图与扩展点

### 13.1 阶段划分

| 阶段 | 内容 | 依据/依赖 |
|---|---|---|
| P0(首版) | CRD + 控制面创建/删除 + kubeconfig + 节点池创建/扩缩容/删除 + clusterctl 集成 + e2e 冒烟 | 本设计 §3-§12 |
| P1 | 升级编排(CreateUpgradeWorkFlow)、插件管理、自动建 VPC/子网、私网 endpoint、Autopilot 评估 | [需验证] 11 等 |
| P2 | 单节点路径(CCEMachine)、节点池 autoscaling 映射、OIDC/委托增强、多云模板 | [需验证] 9 |

### 13.2 扩展点(设计上预留)

- 凭证:Secret → IAM 委托/临时凭证(接口隔离,`identity.Service` 可替换)。
- 网络:引用 → 自动创建(service 接口不变,新增实现)。
- 集群类型:Standard/Turbo/Autopilot 由 `category` 参数化(API 已支持,见 SDK 事实)。
- 版本合约:v1beta1 → v1beta2 转换。

### 13.3 不做(首版明确排除)

- CCE Autopilot(Serverless,无节点概念,对标 Fargate Profile,语义不同,单独评估);
- 多云多 provider 编排;
- 集群休眠/唤醒调度(AwakeCluster API 存在,但属于运维策略,roadmap 候选)。

---

## 14. 关键设计风险与缓解

| 风险 | 说明 | 缓解 |
|---|---|---|
| CCE API 行为与文档假设不符 | 空集群创建、kubeconfig 可达性、扩缩容语义 | [附录 A] 全部实测;P0 前完成 API 冒烟验证 |
| 网段不可变 → 误配置成本高 | 创建后不可改,需重建 | webhook 强校验 + 文档强调 + 默认值审慎 |
| CCE 限流导致调谐抖动 | API 频率限制 | 限流错误分类 + 指数退避;批量查询合并 |
| 凭证泄露 | AK/SK 进入日志/镜像/userdata | §5.2 安全要点;e2e 禁止真实凭证入库 |
| 与 CAPI 核心版本漂移 | contract 版本演进 | metadata.yaml 矩阵 + 依赖升级 CI |
| 节点池与 CAPI 扩缩容语义冲突 | autoscaling 双驱动 | 首版关闭节点池 autoscaling,由 CAPI 单一驱动 |

---

## 附录 A:需对接华为云 CCE 验证的事项清单

> 以下 14 项无法从公开资料完全确认,必须**对接真实华为云 CCE 实测或咨询华为云**后才能定稿实现细节(完整描述见 [research-sources.md §4](research-sources.md))。
>
> **逐项当前状态(2026-08-19)**:见 [需求设计文档附录 A](requirements-design.md#附录-a需对接华为云-cce-验证事项索引) 状态表与 [验证结论记录](cce-verification-findings.md)——8 项实测确认、5 项文档确认、Q11 定论;仅 Q11 升级耗时与 Q14 Retry-After 待华为云/工单。

1. 空集群(0 节点)创建可行性、计费与配额影响。
2. `CreateKubernetesClusterCert` 证书有效期上限、external/internal kubeconfig 切换与可达性。
3. `UpdateNodePool.initialNodeCount` 触发扩缩容的语义;与节点池 autoscaling 并存时的协调。
4. Turbo(eni)对 VPC/子网的硬性要求(ENI 子网、网段不重叠规则)。
5. 安全组创建/关联行为(master/node/eni;节点池 ≤5 个的边界)。
6. 管理 CCE 所需最小 IAM 权限与 AK/SK 账号约束(项目/委托)。
7. 默认配额(集群数、节点池数、节点数、VPC/子网/ENI)与超配错误码。
8. DeleteCluster 删除时长/依赖(节点池先行?ELB/EVS/EIP 残留;Unavailable 状态可删性)。
9. `AddNode`/`AddNodesToNodePool` 对已有 ECS 的引导要求(决定单节点路径可行性)。
10. Autopilot 在 CAPI 模型中的表达(远期)。
11. `CreateUpgradeWorkFlow` 参数(目标版本、跳过策略、升级时长与状态)。
12. 计费模式细节(按需/包周期、空集群成本、休眠唤醒 AwakeCluster)。
13. 管理集群访问 CCE API Server 的网络路径(公网/对等/专线)。
14. CCE/ECS/VPC API 限流阈值与错误码全集(决定退避策略参数)。
