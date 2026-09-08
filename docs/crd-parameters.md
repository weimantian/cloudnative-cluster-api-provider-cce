# my-cluster.yaml 完整参数示例（中文注释）

> 与 `my-cluster.yaml` 同构的多文档 YAML：列出每个 kind 的**全部 spec 字段**，每个字段带中文注释。模板出现的字段给了真实示例值；其余可选字段以注释列出（默认省略即可）。

### 1. Cluster — CAPI orchestration（编排外壳）

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: my-cce-cluster
spec:
  # infrastructureRef/controlPlaneRef 指向下面类型（CAPI 编排，通常不改）
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: CCECluster
    name: my-cce-cluster
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1beta2
    kind: CCEManagedControlPlane
    name: my-cce-cluster-control-plane
```

### 2. CCECluster — 网络外壳

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: CCECluster
metadata:
  name: <name>
  namespace: default
spec:
  # 华为云区域（必填）
  region: cn-north-4
  # 网络外壳：引用/创建 VPC 与子网（CCE 要求先有 VPC）
  network:
    vpc:
      id: <VPC-ID>          # 引用已建 VPC；空则填 name/cidr 由 provider 创建
    subnets:
      - id: <节点子网-ID>
      - id: <ENI子网-ID>
```

### 3. CCEManagedControlPlane — 托管控制面（集群参数）

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: CCEManagedControlPlane
metadata:
  name: <name>
  namespace: default
spec:
  # 所属 CCE 集群名（必填）
  clusterName: my-cce-cluster
  # Kubernetes 版本；空=CCE 最新
  version: v1.35.0
  # 集群类型 CCE(Standard)/Turbo（默认 Turbo）
  category: Turbo
  # 集群规格（官方枚举）
  flavor: cce.s1.small
  # 容器网络（eni=Turbo）
  containerNetwork:
    mode: eni             # overlay_l2 | vpc-router | eni（eni=Turbo）
    eniSubnets:
      - <ENI子网 neutron id>
  # 服务(ClusterIP)网络
  serviceNetwork:
    cidr: 10.248.0.0/16
  # API 访问（公网/私网/白名单）
  endpointAccess:
    public: false         # true=开公网 endpoint
    private: true          # CCE 恒有内网 endpoint
  # 计费 0=按需/1=包周期
  billing:
    mode: 0               # 0=按需 1=包周期
  # 用户附加标签（写 CCE clusterTags/userTags；保留 key 优先）
  additionalTags:         # 写入 CCE 集群 clusterTags（owned/role 保留 key 自动且优先）
    env: prod
    cost-center: cc-42
  # ---- 以下为全部可选字段（默认省略；按需取消注释）----
  # IPv6 双栈（需 VPC 开 IPv6+ipv6CIDR）
  # ipv6enable: <value>
  # Autopilot(Serverless) 集群
  # enableAutopilot: <value>
  # API Server 证书附加 SAN
  # customSan: <value>
  # 集群委托名（1.27+；空=系统委托）
  # agencyName: <value>
  # IAM v5 信任策略（自动建信任委托）
  # agencyTrustPolicy: <value>
  # 凭证引用 CCECluster*Identity；空=默认(AK/SK)
  # identityRef: <value>
  # CCE 插件实例（声明式）
  # addons: <value>
  # SA 绑定委托（对标 Pod Identity）
  # podIdentityAssociations: <value>
  # 控制面日志（ttl/logs）
  # logging: <value>
  # CCE 访问策略（对标 access entries）
  # accessPolicies: <value>
  # etcd 加密（不可变）
  # encryptionConfig: <value>
  # API 认证 rbac/authenticating_proxy（不可变）
  # authentication: <value>
  # API endpoint（控制器回填）
  # controlPlaneEndpoint: <value>
```

### 4. MachinePool + CCEManagedMachinePool — 节点池（每池一份，pool-0 示例）

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: MachinePool
metadata:
  name: my-cce-cluster-pool-0
spec:
  clusterName: my-cce-cluster
  replicas: 1        # 节点数；kubectl scale 驱动扩缩容
  template:
    spec:
      clusterName: my-cce-cluster
      version: v1.35.0
      bootstrap:
        dataSecretName: my-cce-cluster-bootstrap  # 托管节点池：空 Secret
      infrastructureRef:
        apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
        kind: CCEManagedMachinePool
        name: my-cce-cluster-pool-0
```
```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: CCEManagedMachinePool
metadata:
  name: <name>
  namespace: default
spec:
  # 所属 CCE 集群名（必填）
  clusterName: my-cce-cluster
  # CCE 节点池名（必填，不可变）
  nodePoolName: pool-0
  # 集群规格（官方枚举）
  flavor: c7.large.2
  # 节点 OS 镜像（如 Huawei Cloud EulerOS 2.0）
  os: Huawei Cloud EulerOS 2.0
  # 系统盘
  rootVolume:
    size: 40              # GiB（40-1024）
    type: GPSSD
  # 数据盘
  dataVolumes:
    - size: 100
      type: GPSSD
  # 节点 SSH 密钥对
  sshKey: capi-bastion-key
  # 节点可用区（webhook 必填；CCE API 不支持随机 AZ）
  availabilityZone: cn-north-4a
  # 期望节点数（MachinePool.replicas 驱动）
  replicas: 1
  # 用户附加标签（写 CCE clusterTags/userTags；保留 key 优先）
  additionalTags:         # 写入 CCE 节点池 userTags
    team: platform
  # ---- 以下为全部可选字段（默认省略；按需取消注释）----
  # 节点 providerID（控制器回填）
  # providerIDList: <value>
  # 0=按需/1=包周期
  # billingMode: <value>
  # 竞价实例（仅按需）
  # spot: <value>
  # 竞价出价上限
  # spotPrice: <value>
  # 扩展伸缩组（每额外 AZ 一个：flavor+AZ）
  # extensionScaleGroups: <value>
  # 节点污点（≤20）
  # taints: <value>
  # 节点 k8s 标签
  # labels: <value>
  # 安全组（Turbo≥1.21，≤5）
  # securityGroups: <value>
  # CCE 节点池自动伸缩（feature gate 默认关）
  # autoscaling: <value>
  # 滚动更新同步（maxUnavailable 1-20）
  # updateConfig: <value>
  # 节点自愈（Reset 异常节点）
  # nodeRepair: <value>
  # ECS 云服务器组
  # ecsGroupId: <value>
  # 故障域
  # faultDomain: <value>
  # 专属主机
  # dedicatedHostId: <value>
  # 预安装脚本(base64)
  # preInstall: <value>
  # 后安装脚本(base64)
  # postInstall: <value>
  # 等待 postInstall 完成再调度
  # waitPostInstallFinish: <value>
```