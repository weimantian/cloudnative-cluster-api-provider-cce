# my-cluster.yaml — full parameter sample (English comments)

> Multi-document YAML in the same shape as `my-cluster.yaml`: every spec field of each kind, one comment per field. Fields present in the template show real values; optional fields are listed as comments (safe to omit).

### 1. Cluster — CAPI orchestration

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: my-cce-cluster
spec:
  # infrastructureRef/controlPlaneRef point at the kinds below (CAPI orchestration; normally unchanged)
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
    kind: CCECluster
    name: my-cce-cluster
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1beta2
    kind: CCEManagedControlPlane
    name: my-cce-cluster-control-plane
```

### 2. CCECluster — network shell

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta2
kind: CCECluster
metadata:
  name: <name>
  namespace: default
spec:
  # Region is the Huawei Cloud region (e.g. cn-north-4). Required.
  region: cn-north-4
  # Network references the VPC/subnets the CCE cluster consumes. CCE requires a VPC to exist before cluster creation.
  network:
    vpc:
      id: <VPC-ID>          # 引用已建 VPC；空则填 name/cidr 由 provider 创建
    subnets:
      - id: <节点子网-ID>
      - id: <ENI子网-ID>
```

### 3. CCEManagedControlPlane — control plane (cluster parameters)

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1beta2
kind: CCEManagedControlPlane
metadata:
  name: <name>
  namespace: default
spec:
  # ClusterName of the owning CCE cluster.
  clusterName: my-cce-cluster
  # Version of Kubernetes, e.g. "v1.30.0". Empty means CCE latest.
  version: v1.35.0
  # Category of the CCE cluster: CCE (Standard) or Turbo.
  category: Turbo
  # Flavor of the cluster (official enum e.g. cce.s1.small ... cce.s2.xlarge).
  flavor: cce.s1.small
  # ContainerNetwork of the cluster.
  containerNetwork:
    mode: eni             # overlay_l2 | vpc-router | eni（eni=Turbo）
    eniSubnets:
      - <ENI子网 neutron id>
  # ServiceNetwork of the cluster.
  serviceNetwork:
    cidr: 10.248.0.0/16
  # EndpointAccess controls public API server access.
  endpointAccess:
    public: false         # true=开公网 endpoint
    private: true          # CCE 恒有内网 endpoint
  # Billing controls billing mode: 0=on-demand, 1=subscription.
  billing:
    mode: 0               # 0=按需 1=包周期
  # AdditionalTags is an optional set of tags to add to the CCE cluster (maps to CCE clusterTags / ResourceTag), in addition to the provider owned tag cluster-api-provider-cce.cluster.<clusterName>=owned that is always added. The owned tag wins on key collision. Mirrors CAPA's spec.additionalTags. Tag updates on an already-created cluster are reconciled via the CCE BatchCreateClusterTags API (see requirements FR-1.9).
  additionalTags:         # 写入 CCE 集群 clusterTags（owned/role 保留 key 自动且优先）
    env: prod
    cost-center: cc-42
  # ---- all optional fields below (omitted by default; uncomment as needed) ----
  # Ipv6Enable enables IPv6 dual-stack for the cluster (maps to CCE spec.ipv6enable). Requires a VPC with IPv6 enabled; the service network IPv6CIDR must be set as well.
  # ipv6enable: <value>
  # EnableAutopilot creates an Autopilot (Serverless) cluster instead of a standard CCE/Turbo cluster (maps to CCE spec.enableAutopilot). Autopilot requires category Turbo and a compatible flavor (cce.autopilot.cluster).
  # enableAutopilot: <value>
  # CustomSan entries for the API server certificate.
  # customSan: <value>
  # AgencyName used by the cluster (1.27+; empty uses the system agency).
  # agencyName: <value>
  # AgencyTrustPolicy is the IAM v5 trust-policy JSON document used to auto-create the trust agency (信任委托) referenced by the role identity (identityRef -> CCEClusterRoleIdentity.spec.agencyName) when that agency does not already exist. Mirrors CAPA auto-creating the cluster IAM role: a non-empty policy + a role identity triggers EnsureAgency (List -> Create when absent); an existing agency is adopted (never overwritten). When the identity has no agency (controller/static identity), creation is skipped. The document must declare "Version": "5.0".
  # agencyTrustPolicy: <value>
  # IdentityRef references a CCECluster*Identity (Controller/Static/Role). Empty means the controller default identity (CLOUD_SDK_AK/SK env).
  # identityRef: <value>
  # Addons are the CCE addon instances to manage (declarative set; the controller installs missing ones, upgrades version drift, and removes those no longer listed — mirrors CAPA EKS addons).
  # addons: <value>
  # PodIdentityAssociations bind Kubernetes ServiceAccounts to Huawei Cloud agencies (the CCE equivalent of EKS Pod Identity). Declarative set: create missing, delete removed.
  # podIdentityAssociations: <value>
  # Logging configures control-plane log collection (mirrors CAPA EKS Logging). Maps to CCE UpdateClusterLogConfig / ShowClusterConfig.
  # logging: <value>
  # AccessPolicies declare CCE access policies (the CCE equivalent of EKS access entries). Declarative set: create missing, update drift, remove those no longer listed.
  # accessPolicies: <value>
  # EncryptionConfig controls etcd secret encryption (mirrors CAPA EKS EncryptionConfig). Mode Default leaves etcd unencrypted; KMS enables envelope encryption with a KMS key configured at the account level. Immutable after creation.
  # encryptionConfig: <value>
  # Authentication controls the API server authentication mode (mirrors CAPA EKS AccessConfig.AuthenticationMode). Default rbac; authenticating_proxy delegates auth to an external proxy (requires a CA + client cert + key). Immutable after creation.
  # authentication: <value>
  # ControlPlaneEndpoint is the API server endpoint (host:port) of the managed control plane. It is backfilled by the controller once the CCE cluster is available and is read by CAPI to populate Cluster.spec.controlPlaneEndpoint — the CAPI control-plane contract reads spec.controlPlaneEndpoint, not status.
  # controlPlaneEndpoint: <value>
```

### 4. MachinePool + CCEManagedMachinePool — node pool (one per pool; pool-0 shown)

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: MachinePool
metadata:
  name: my-cce-cluster-pool-0
spec:
  clusterName: my-cce-cluster
  replicas: 1        # node count; kubectl scale resizes
  template:
    spec:
      clusterName: my-cce-cluster
      version: v1.35.0
      bootstrap:
        dataSecretName: my-cce-cluster-bootstrap  # managed pool: empty Secret
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
  # ClusterName of the owning CCE cluster (matches Cluster name).
  clusterName: my-cce-cluster
  # NodePoolName is the CCE node pool name.
  nodePoolName: pool-0
  # Flavor is the ECS instance flavor of the nodes (nodeTemplate.flavor).
  flavor: c7.large.2
  # OS image family of the nodes (nodeTemplate.os), e.g. "Huawei Cloud EulerOS 2.0". Required by the webhook (official CreateNodePool requires os unless a private image is used).
  os: Huawei Cloud EulerOS 2.0
  # RootVolume of the nodes (nodeTemplate.rootVolume). Pointer so an empty value is omitted; required by the webhook (size 40-1024 GiB).
  rootVolume:
    size: 40              # GiB（40-1024）
    type: GPSSD
  # DataVolumes of the nodes (nodeTemplate.dataVolumes).
  dataVolumes:
    - size: 100
      type: GPSSD
  # SSHKey used to access the nodes (nodeTemplate.sshKey).
  sshKey: capi-bastion-key
  # AvailabilityZone of the nodes. Required by the webhook (CCE does not support random AZ via API).
  availabilityZone: cn-north-4a
  # Replicas is the desired node count (maps to the node pool expected count). It is normally driven by the owning MachinePool.spec.replicas.
  replicas: 1
  # AdditionalTags is an optional set of tags to add to the CCE node pool (maps to CCE userTags / UserTag), in addition to the provider owned tag cluster-api-provider-cce.cluster.<clusterName>=owned that is always added. The owned tag wins on key collision. Mirrors CAPA's spec.additionalTags.
  additionalTags:         # 写入 CCE 节点池 userTags
    team: platform
  # ---- all optional fields below (omitted by default; uncomment as needed) ----
  # ProviderIDList is the list of provider IDs of the nodes in the pool, populated by the controller so Cluster API can fill MachinePool.status.nodeRefs (and the deprecated readyReplicas). Each entry has the form huaweicloud:///<serverId>, matching the spec.providerID of the corresponding workload node. The controller owns this field.
  # providerIDList: <value>
  # BillingMode: 0=on-demand, 1=subscription.
  # billingMode: <value>
  # Spot requests spot (竞价) instances for the node pool. Only effective when billingMode=0 (on-demand); maps to nodeTemplate.extendParam. marketType=spot.
  # spot: <value>
  # SpotPrice is the maximum hourly price the user is willing to pay for spot instances. Empty = the on-demand price is used as the spot price. Only effective when spot is set and billingMode=0.
  # spotPrice: <value>
  # ExtensionScaleGroups extends the node pool into additional availability zones (CCE 扩展伸缩组). Each entry carries its own flavor and AZ; the base nodeTemplate.az remains the primary AZ.
  # extensionScaleGroups: <value>
  # Taints applied to the nodes (max 20 per official constraint).
  # taints: <value>
  # Labels applied to the nodes.
  # labels: <value>
  # SecurityGroups to bind to the node pool (Turbo >= 1.21, max 5 per official constraint).
  # securityGroups: <value>
  # Autoscaling maps to the CCE node pool autoscaling (NodePoolNodeAutoscaling). Only honored when the NodePoolAutoscaling feature gate is enabled (Alpha, off by default); otherwise scaling is driven solely by CAPI MachinePool replicas (questionnaire Q3, FR-2.6).
  # autoscaling: <value>
  # UpdateConfig controls how spec changes are rolled onto existing nodes (CCE 同步节点池 UpgradeNodePool, the analogue of CAPA's UpdateConfig / rolling update). Node attributes such as securityGroups, taints, labels and OS only apply to newly created nodes, so the controller calls UpgradeNodePool to synchronise them onto running nodes.
  # updateConfig: <value>
  # NodeRepair enables node auto-repair (mirrors CAPA NodeRepairConfig. Enabled). CCE has no EKS-style auto-repair switch, so the provider detects Abnormal/Error nodes and resets them via CCE ResetNode.
  # nodeRepair: <value>
  # EcsGroupId is the ECS server group ID (云服务器组) for the nodes (nodeTemplate.ecsGroupId). Used to place nodes according to the group's affinity/anti-affinity policy.
  # ecsGroupId: <value>
  # FaultDomain is the fault domain (故障域) for the nodes (nodeTemplate.faultDomain). Enables single-AZ multi-fault-domain placement.
  # faultDomain: <value>
  # DedicatedHostId is the dedicated host (专属主机) ID for the nodes (nodeTemplate.dedicatedHostId). Only effective for dedicated-host flavors.
  # dedicatedHostId: <value>
  # PreInstall is the base64-encoded script run before node installation (nodeTemplate.extendParam["alpha.cce/preInstall"]). Mirrors the CCE node lifecycle preInstall hook; the value must already be base64-encoded.
  # preInstall: <value>
  # PostInstall is the base64-encoded script run after node installation (nodeTemplate.extendParam["alpha.cce/postInstall"]). Mirrors the CCE node lifecycle postInstall hook; the value must already be base64- encoded.
  # postInstall: <value>
  # WaitPostInstallFinish blocks pod scheduling until the post-install script finishes (nodeTemplate.waitPostInstallFinish). Mirrors CCE's NodeLifecycleConfig waitPostInstallFinish.
  # waitPostInstallFinish: <value>
```